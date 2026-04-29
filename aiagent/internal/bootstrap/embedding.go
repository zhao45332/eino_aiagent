package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/embedding"

	embedopenai "github.com/cloudwego/eino-ext/components/embedding/openai"

	"aiagent/internal/config"
	"aiagent/internal/model"
)

// NewOpenAIEmbedder 使用与对话模型相同来源的 OpenAI 兼容 Embedding API（需与 [config.Config.EmbeddingDim] 及 Milvus 集合维度一致）。
func NewOpenAIEmbedder(ctx context.Context, c *config.Config) (embedding.Embedder, error) {
	key := c.EmbeddingKey()
	if key == "" {
		return nil, fmt.Errorf("向量化需要 EMBEDDING_API_KEY，或在 MODEL_TYPE=openai 时配置 OPENAI_API_KEY")
	}
	encoding := embedopenai.EmbeddingEncodingFormatFloat
	dim := c.EmbeddingDim
	dimPtr := &dim
	cfg := &embedopenai.EmbeddingConfig{
		APIKey:         key,
		BaseURL:        c.EmbeddingBase(),
		Model:          c.EmbeddingModel,
		HTTPClient:     model.NewOpenAICompatHTTPClient(c.EmbeddingBase(), 60*time.Second),
		Dimensions:     dimPtr,
		EncodingFormat: &encoding,
	}
	return embedopenai.NewEmbedder(ctx, cfg)
}
