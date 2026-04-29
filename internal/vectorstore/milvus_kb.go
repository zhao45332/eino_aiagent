// Package vectorstore 封装 Milvus 集合的创建、写入与向量检索（Float + COSINE），供智能客服 RAG 使用。
package vectorstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	fieldPK      = "pk"
	fieldContent = "content"
	fieldVector  = "embedding"
)

// KB 绑定一个 Milvus 集合（Schema: pk VarChar, content VarChar, embedding FloatVector）。
type KB struct {
	cli    client.Client
	coll   string
	dim    int
	metric entity.MetricType
}

// NewKB 连接 Milvus；集合可不存在，由 [EnsureSchema] 创建。
func NewKB(ctx context.Context, addr, user, pass, collection string, dim int) (*KB, error) {
	cfg := client.Config{Address: addr}
	if user != "" || pass != "" {
		cfg.Username = user
		cfg.Password = pass
	}
	cli, err := client.NewClient(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("milvus client: %w", err)
	}
	return &KB{cli: cli, coll: collection, dim: dim, metric: entity.COSINE}, nil
}

// Close 释放连接。
func (k *KB) Close() error { return k.cli.Close() }

// EnsureSchema 若集合不存在则创建、建索引并 Load；已存在则跳过（请保证维度与嵌入模型一致）。
func (k *KB) EnsureSchema(ctx context.Context) error {
	ok, err := k.cli.HasCollection(ctx, k.coll)
	if err != nil {
		return fmt.Errorf("has collection: %w", err)
	}
	if ok {
		return k.ensureLoaded(ctx)
	}
	sch := entity.NewSchema().
		WithName(k.coll).
		WithDescription("aiagent customer service kb").
		WithField(entity.NewField().WithName(fieldPK).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(fieldContent).WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535)).
		WithField(entity.NewField().WithName(fieldVector).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(k.dim)))
	if err := k.cli.CreateCollection(ctx, sch, 1, client.WithConsistencyLevel(entity.ClBounded)); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	return k.ensureIndexAndLoad(ctx)
}

func (k *KB) ensureLoaded(ctx context.Context) error {
	coll, err := k.cli.DescribeCollection(ctx, k.coll)
	if err != nil {
		return err
	}
	if err := k.assertVectorDimMatch(coll); err != nil {
		return err
	}
	if !coll.Loaded {
		if err := k.ensureIndexAndLoad(ctx); err != nil {
			return err
		}
	}
	return nil
}

// assertVectorDimMatch 已存在集合时检查 embedding 字段维度与当前 k.dim（EMBEDDING_DIM）一致。
func (k *KB) assertVectorDimMatch(coll *entity.Collection) error {
	if coll == nil || coll.Schema == nil {
		return nil
	}
	for _, f := range coll.Schema.Fields {
		if f.Name != fieldVector {
			continue
		}
		ds, ok := f.TypeParams[entity.TypeParamDim]
		if !ok || ds == "" {
			return nil
		}
		got, err := strconv.Atoi(ds)
		if err != nil {
			return nil
		}
		if got != k.dim {
			return fmt.Errorf(
				"集合 %q 中向量字段 %q 维度为 %d，与当前配置 EMBEDDING_DIM=%d 不一致（曾用旧维度建表？）。"+
					"请任选其一：在 Milvus 中删除该集合并重新启动程序以自动建表；或设置新的 MILVUS_KB_COLLECTION 名称后重试",
				k.coll, fieldVector, got, k.dim,
			)
		}
		return nil
	}
	return nil
}

func (k *KB) ensureIndexAndLoad(ctx context.Context) error {
	indexes, err := k.cli.DescribeIndex(ctx, k.coll, fieldVector)
	if err != nil {
		// 可能尚未建索引
		indexes = nil
	}
	if len(indexes) == 0 {
		idx, err := entity.NewIndexAUTOINDEX(k.metric)
		if err != nil {
			return err
		}
		if err := k.cli.CreateIndex(ctx, k.coll, fieldVector, idx, false); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	if err := k.cli.LoadCollection(ctx, k.coll, true); err != nil {
		return fmt.Errorf("load collection: %w", err)
	}
	return nil
}

// IndexDocuments 对文本批量向量化后写入；pk 与 texts 一一对应。
func (k *KB) IndexDocuments(ctx context.Context, emb embedding.Embedder, pk []string, texts []string) error {
	if len(pk) != len(texts) || len(pk) == 0 {
		return fmt.Errorf("pk 与 texts 条数需一致且非空")
	}
	vecd, err := emb.EmbedStrings(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	rows := make([][]float32, len(vecd))
	for i, v := range vecd {
		if len(v) != k.dim {
			return fmt.Errorf("嵌入维度为 %d，与配置的 EMBEDDING_DIM=%d 不一致", len(v), k.dim)
		}
		rows[i] = floats64to32(v)
	}
	colPK := entity.NewColumnVarChar(fieldPK, pk)
	colCT := entity.NewColumnVarChar(fieldContent, texts)
	colV := entity.NewColumnFloatVector(fieldVector, k.dim, rows)
	if _, err := k.cli.Insert(ctx, k.coll, "", colPK, colCT, colV); err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	if err := k.cli.Flush(ctx, k.coll, false); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return k.ensureIndexAndLoad(ctx)
}

// DeleteFAQPKsUpTo 删除主键 faq-1 … faq-maxN（灌库前用于清空旧向量，避免条数变化后残留）。
func (k *KB) DeleteFAQPKsUpTo(ctx context.Context, maxN int) error {
	if maxN <= 0 {
		return nil
	}
	if maxN > 4096 {
		maxN = 4096
	}
	quoted := make([]string, 0, maxN)
	for i := 1; i <= maxN; i++ {
		quoted = append(quoted, fmt.Sprintf(`"faq-%d"`, i))
	}
	expr := "pk in [" + strings.Join(quoted, ",") + "]"
	if err := k.cli.Delete(ctx, k.coll, "", expr); err != nil {
		return fmt.Errorf("milvus delete: %w", err)
	}
	if err := k.cli.Flush(ctx, k.coll, false); err != nil {
		return fmt.Errorf("milvus flush after delete: %w", err)
	}
	return nil
}

// SearchResult 单条命中。
type SearchResult struct {
	PK    string
	Text  string
	Score float32
}

// Search 用查询向量做 ANN 检索。
func (k *KB) Search(ctx context.Context, query []float32, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	if len(query) != k.dim {
		return nil, fmt.Errorf("查询向量维度 %d 与库维度 %d 不一致", len(query), k.dim)
	}
	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}
	vectors := []entity.Vector{entity.FloatVector(query)}
	res, err := k.cli.Search(ctx, k.coll, nil, "", []string{fieldPK, fieldContent}, vectors, fieldVector, k.metric, topK, sp)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	sr := res[0]
	// 空集合或无相似命中时 ResultCount 为 0，Milvus 可能不填充 Fields，禁止继续 GetColumn
	if sr.ResultCount == 0 {
		return nil, nil
	}
	if sr.Fields == nil {
		return nil, fmt.Errorf("milvus 检索结果无 Fields")
	}
	out := make([]SearchResult, 0, sr.ResultCount)
	pkCol := sr.Fields.GetColumn(fieldPK)
	txtCol := sr.Fields.GetColumn(fieldContent)
	if pkCol == nil || txtCol == nil {
		return nil, fmt.Errorf("milvus 未返回 %s / %s 列", fieldPK, fieldContent)
	}
	for i := 0; i < sr.ResultCount; i++ {
		pkv, _ := pkCol.Get(i)
		txtv, _ := txtCol.Get(i)
		pks, _ := pkv.(string)
		txs, _ := txtv.(string)
		var sc float32
		if i < len(sr.Scores) {
			sc = sr.Scores[i]
		}
		out = append(out, SearchResult{PK: pks, Text: txs, Score: sc})
	}
	return out, nil
}

func floats64to32(v []float64) []float32 {
	o := make([]float32, len(v))
	for i, x := range v {
		o[i] = float32(x)
	}
	return o
}

// EmbedQuery 将问句向量化成 float32 切片供 [Search] 使用。
func EmbedQuery(ctx context.Context, emb embedding.Embedder, q string) ([]float32, error) {
	v, err := emb.EmbedStrings(ctx, []string{strings.TrimSpace(q)})
	if err != nil {
		return nil, err
	}
	if len(v) == 0 || len(v[0]) == 0 {
		return nil, fmt.Errorf("空向量")
	}
	return floats64to32(v[0]), nil
}
