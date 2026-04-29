package csvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"

	"aiagent/internal/bootstrap"
	"aiagent/internal/components/retriever"
	"aiagent/internal/config"
	"aiagent/internal/vectorstore"
)

// RAGService 复用 Milvus 与向量化检索；最终回复由主 Agent 与 search_knowledge 工具链生成。
type RAGService struct {
	kb  *vectorstore.KB
	rag *retriever.CSRAG
}

const (
	faqCorpusPath  = "data/corpus/faq.md"
	kbFaqStatePath = "data/corpus/.kb_faq_state.json"
)

// kbFaqState 记录已写入 Milvus 的 faq.md 内容哈希与条数，用于启动时判断是否需要重灌。
type kbFaqState struct {
	SHA256 string `json:"sha256"`
	N      int    `json:"n"`
}

// NewRAGService 使用向量数据库
func NewRAGService(ctx context.Context, cfg *config.Config) (*RAGService, error) {
	emb, err := bootstrap.NewOpenAIEmbedder(ctx, cfg)
	if err != nil {
		return nil, err
	}
	kb, err := vectorstore.NewKB(ctx, cfg.MilvusAddr, cfg.MilvusUser, cfg.MilvusPassword, cfg.KBCollection, cfg.EmbeddingDim)
	if err != nil {
		return nil, err
	}
	if err := kb.EnsureSchema(ctx); err != nil {
		_ = kb.Close() //nolint:errcheck
		return nil, err
	}
	// 同步最新语料库到向量数据库
	if err := syncFaqCorpusToMilvus(ctx, kb, emb); err != nil {
		_ = kb.Close() //nolint:errcheck
		return nil, err
	}
	// 多取几条再在 RetrieveContext 里按分数落差过滤，减少「弱相关 FAQ」混进工具返回。
	rag := retriever.NewCSRAG(kb, emb, 8)
	return &RAGService{kb: kb, rag: rag}, nil
}

func syncFaqCorpusToMilvus(ctx context.Context, kb *vectorstore.KB, emb embedding.Embedder) error {
	force := os.Getenv("CS_SEED") == "1" // 检查是否需要同步最新语料库
	auto := strings.TrimSpace(os.Getenv("CS_KB_AUTO_SYNC")) != "0"
	if !force && !auto {
		return nil
	}
	raw, errFile := os.ReadFile(faqCorpusPath)
	if errFile != nil {
		if !force {
			return nil
		}
		lines := loadFaqCorpus()
		if len(lines) == 0 {
			return nil
		}
		pk := make([]string, len(lines))
		for i := range lines {
			pk[i] = fmt.Sprintf("faq-%d", i+1)
		}
		maxDel := len(lines) + 64
		if err := kb.DeleteFAQPKsUpTo(ctx, maxDel); err != nil {
			return fmt.Errorf("清理旧 faq 向量: %w", err)
		}
		if err := kb.IndexDocuments(ctx, emb, pk, lines); err != nil {
			return fmt.Errorf("写入知识库(CS_SEED): %w", err)
		}
		log.Printf("知识库: 已按 CS_SEED=1 灌入 %d 条（未使用 %s 时走默认语料）", len(lines), faqCorpusPath)
		return nil
	}

	sum := sha256.Sum256(raw)
	h := hex.EncodeToString(sum[:])
	st, _ := readKbFaqState()
	if !force && st.SHA256 == h {
		return nil
	}

	lines := parseFaqMarkdown(raw)
	if len(lines) == 0 {
		return nil
	}
	maxDel := st.N
	if len(lines) > maxDel {
		maxDel = len(lines)
	}
	maxDel += 32
	if err := kb.DeleteFAQPKsUpTo(ctx, maxDel); err != nil {
		return fmt.Errorf("清理旧 faq 向量: %w", err)
	}
	pk := make([]string, len(lines))
	for i := range lines {
		pk[i] = fmt.Sprintf("faq-%d", i+1)
	}
	if err := kb.IndexDocuments(ctx, emb, pk, lines); err != nil {
		return fmt.Errorf("写入知识库: %w", err)
	}
	if err := writeKbFaqState(kbFaqState{SHA256: h, N: len(lines)}); err != nil {
		return fmt.Errorf("写入同步状态: %w", err)
	}
	reason := "语料已变更"
	if force {
		reason = "CS_SEED=1 强制"
	}
	log.Printf("知识库: %s，已重新向量化写入 Milvus（%d 条）", reason, len(lines))
	return nil
}

func readKbFaqState() (kbFaqState, error) {
	var st kbFaqState
	b, err := os.ReadFile(kbFaqStatePath)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func writeKbFaqState(st kbFaqState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(kbFaqStatePath, b, 0o600)
}

// Close 释放向量库连接。
func (s *RAGService) Close() error {
	if s == nil || s.kb == nil {
		return nil
	}
	return s.kb.Close()
}

// RetrieveContext 仅做向量检索并拼接片段，供 search_knowledge 工具使用。
func (s *RAGService) RetrieveContext(ctx context.Context, userQuery string) (string, error) {
	q := strings.TrimSpace(userQuery)
	if q == "" {
		return "", fmt.Errorf("检索 query 不能为空")
	}
	docs, err := s.rag.Retrieve(ctx, q)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "（知识库中未检索到与当前问题相近的片段，可建议用户联系人工或换一种问法。）", nil
	}
	docs = filterDocsByScoreGap(docs, retrieveScoreGapFromEnv())
	if len(docs) == 0 {
		return "（知识库中未检索到与当前问题相近的片段，可建议用户联系人工或换一种问法。）", nil
	}
	var b strings.Builder
	b.WriteString("（以下片段仅供你内部参考；答复用户时只采用与当前问题直接相关的句子，勿输出本说明、勿逐条照抄无关条目。）\n")
	for _, d := range docs {
		b.WriteString(d.Content)
		b.WriteString("\n---\n")
	}
	return strings.TrimSpace(b.String()), nil
}

// defaultRetrieveScoreGap：与最优命中相比，相似度落差超过该值则丢弃（COSINE 通常越大越相似）。
const defaultRetrieveScoreGap = float32(0.12)

func retrieveScoreGapFromEnv() float32 {
	s := strings.TrimSpace(os.Getenv("CS_RAG_SCORE_GAP"))
	if s == "" {
		return defaultRetrieveScoreGap
	}
	f, err := strconv.ParseFloat(s, 32)
	if err != nil || f <= 0 {
		return defaultRetrieveScoreGap
	}
	return float32(f)
}

func docScore(d *schema.Document) (float32, bool) {
	if d == nil || d.MetaData == nil {
		return 0, false
	}
	v, ok := d.MetaData["distance"]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float32:
		return x, true
	case float64:
		return float32(x), true
	default:
		return 0, false
	}
}

// filterDocsByScoreGap 保留首条，其余仅当与首条分数足够接近时保留，避免弱相关 FAQ 挤进工具结果。
// 根据相邻两条相对大小推断「越大越相似」还是「越小越相似」（与 Milvus COSINE / L2 返回一致）。
func filterDocsByScoreGap(docs []*schema.Document, gap float32) []*schema.Document {
	if gap <= 0 || len(docs) <= 1 {
		return docs
	}
	best, ok := docScore(docs[0])
	if !ok {
		return docs
	}
	s1, ok1 := docScore(docs[1])
	higherIsBetter := !ok1 || s1 <= best
	out := []*schema.Document{docs[0]}
	for i := 1; i < len(docs); i++ {
		si, ok := docScore(docs[i])
		if !ok {
			continue
		}
		if higherIsBetter {
			if best-si <= gap {
				out = append(out, docs[i])
			}
		} else {
			if si-best <= gap {
				out = append(out, docs[i])
			}
		}
	}
	if len(out) == 0 {
		return docs[:1]
	}
	return out
}

func loadFaqCorpus() []string {
	b, err := os.ReadFile(faqCorpusPath)
	if err != nil {
		return nil
	}
	lines := parseFaqMarkdown(b)
	if len(lines) == 0 {
		return nil
	}
	return lines
}

func parseFaqMarkdown(b []byte) []string {
	var lines []string
	for _, s := range strings.Split(string(b), "\n") {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "- ")
		lines = append(lines, s)
	}
	return lines
}
