// Package retriever 实现 eino 的 [github.com/cloudwego/eino/components/retriever.Retriever]，用于在 Graph/编排中接知识库（本包为示例 RAG）。
package retriever

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/embedding"
	eiretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"

	"aiagent/internal/vectorstore"
)

// MilvusRetriever 将 Milvus 向量检索与 Embedder 组合为 eino Retriever（默认取 TopK=5，可由 [eiretriever.WithTopK] 覆盖）。
type MilvusRetriever struct {
	KB     *vectorstore.KB
	Embed  embedding.Embedder
	TopK   int
	Prefix string
}

// NewMilvusRetriever topK 传 0 则默认 5。
func NewMilvusRetriever(kb *vectorstore.KB, emb embedding.Embedder, topK int) *MilvusRetriever {
	if topK <= 0 {
		topK = 5
	}
	return &MilvusRetriever{KB: kb, Embed: emb, TopK: topK, Prefix: "kb"}
}

// Retrieve 实现 eino Retriever：query 为自然语言，返回命中的 Document（Content 为片段文本，MetaData 含距离等）。
func (c *MilvusRetriever) Retrieve(ctx context.Context, query string, opts ...eiretriever.Option) ([]*schema.Document, error) {
	co := eiretriever.GetCommonOptions(&eiretriever.Options{TopK: &c.TopK, Embedding: c.Embed}, opts...)
	emb := co.Embedding
	if emb == nil {
		emb = c.Embed
	}
	if emb == nil {
		return nil, fmt.Errorf("MilvusRetriever: 无 Embedder")
	}
	tk := c.TopK
	if co.TopK != nil {
		tk = *co.TopK
	}
	if tk <= 0 {
		tk = 5
	}
	qv, err := vectorstore.EmbedQuery(ctx, emb, query)
	if err != nil {
		return nil, err
	}
	hits, err := c.KB.Search(ctx, qv, tk)
	if err != nil {
		return nil, err
	}
	docs := make([]*schema.Document, 0, len(hits))
	for i, h := range hits {
		id := c.Prefix + "-" + h.PK
		if h.PK == "" {
			id = fmt.Sprintf("%s-%d", c.Prefix, i)
		}
		docs = append(docs, &schema.Document{
			ID:      id,
			Content: h.Text,
			MetaData: map[string]any{
				"distance": h.Score,
				"pk":       h.PK,
			},
		})
	}
	return docs, nil
}
