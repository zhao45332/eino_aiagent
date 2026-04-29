// Package config 从环境变量加载（支持方式 A：OpenAI 兼容 / 方式 B：Ark）；LoadConfig 内会 LoadDotEnv。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 环境变量与模型配置。
type Config struct {
	ModelType  string
	APIKey     string // openai
	BaseURL    string
	ArkAPIKey  string // ark
	ArkBaseURL string
	Model      string

	// 向量知识库 + RAG（与 Milvus 搭配；向量化需 OpenAI 兼容的 Embedding 接口）
	MilvusAddr       string
	MilvusUser       string
	MilvusPassword   string
	KBCollection     string
	EmbeddingAPIKey  string
	EmbeddingBaseURL string
	EmbeddingModel   string
	EmbeddingDim     int
}

// EmbeddingKey 向量化 API 密钥：优先 EMBEDDING_API_KEY，否则在 openai 对话路径下回退为 OPENAI_API_KEY。
func (c *Config) EmbeddingKey() string {
	if strings.TrimSpace(c.EmbeddingAPIKey) != "" {
		return c.EmbeddingAPIKey
	}
	if c.ModelType == "openai" {
		return c.APIKey
	}
	return ""
}

// EmbeddingBase 向量化 BaseURL，优先 EMBEDDING_BASEURL，openai 路径下可回退 OPENAI_BASE_URL。
func (c *Config) EmbeddingBase() string {
	if strings.TrimSpace(c.EmbeddingBaseURL) != "" {
		return c.EmbeddingBaseURL
	}
	if c.ModelType == "openai" {
		return c.BaseURL
	}
	return ""
}

const bigModelCompatibleBase = "https://open.bigmodel.cn/api/paas/v4/"

// LoadConfig 读取环境变量并尝试加载 config/.env、.env。密钥勿入库。
func LoadConfig() (*Config, error) {
	_ = LoadDotEnv()
	mt := strings.ToLower(strings.TrimSpace(os.Getenv("MODEL_TYPE")))
	if mt == "ark" {
		return loadArk()
	}
	return loadOpenAICompat(mt)
}

func loadArk() (*Config, error) {
	key := strings.TrimSpace(os.Getenv("ARK_API_KEY"))
	if key == "" {
		return nil, fmt.Errorf("MODEL_TYPE=ark 时请设置 ARK_API_KEY")
	}
	model := strings.TrimSpace(os.Getenv("ARK_MODEL"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("CHAT_MODEL"))
	}
	if model == "" {
		return nil, fmt.Errorf("请设置 ARK_MODEL 或 CHAT_MODEL")
	}
	return &Config{
		ModelType:        "ark",
		ArkAPIKey:        key,
		ArkBaseURL:       strings.TrimSpace(os.Getenv("ARK_BASE_URL")),
		Model:            model,
		MilvusAddr:       milvusAddr(),
		MilvusUser:       strings.TrimSpace(os.Getenv("MILVUS_USER")),
		MilvusPassword:   strings.TrimSpace(os.Getenv("MILVUS_PASSWORD")),
		KBCollection:     kbCollection(),
		EmbeddingAPIKey:  strings.TrimSpace(os.Getenv("EMBEDDING_API_KEY")),
		EmbeddingBaseURL: strings.TrimSpace(os.Getenv("EMBEDDING_BASEURL")),
		EmbeddingModel:   embeddingModel(),
		EmbeddingDim:     embeddingDim(1536),
	}, nil
}

func loadOpenAICompat(normalizedType string) (*Config, error) {
	if normalizedType == "" {
		normalizedType = "openai"
	}
	if normalizedType != "openai" {
		return nil, fmt.Errorf("不支持的 MODEL_TYPE=%q，请使用 openai 或 ark", os.Getenv("MODEL_TYPE"))
	}

	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	}
	if key == "" {
		return nil, fmt.Errorf("请设置 OPENAI_API_KEY 或 DASHSCOPE_API_KEY（OpenAI 兼容路径）")
	}

	base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	// 仅填了 DASHSCOPE_API_KEY 时保留原默认大模型端点；智谱/其它兼容请自行设 OPENAI_BASE_URL
	if base == "" && strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")) != "" {
		base = bigModelCompatibleBase
	}

	model := firstNonEmpty(os.Getenv("CHAT_MODEL"), os.Getenv("OPENAI_MODEL"))
	if model == "" {
		if base != "" && strings.Contains(base, "dashscope") {
			model = "qwen-turbo"
		} else {
			model = "gpt-4o-mini"
		}
	}

	// 向量化：未设置 EMBEDDING_MODEL 时，智谱用 embedding-2（1024 维），其它 OpenAI 兼容默认 text-embedding-3-small（1536 维）
	defEmbModel, defEmbDim := "text-embedding-3-small", 1536
	if strings.Contains(strings.ToLower(base), "bigmodel.cn") {
		defEmbModel, defEmbDim = "embedding-2", 1024
	}
	embModel := strings.TrimSpace(os.Getenv("EMBEDDING_MODEL"))
	if embModel == "" {
		embModel = defEmbModel
	}

	return &Config{
		ModelType:        normalizedType,
		APIKey:           key,
		BaseURL:          base,
		Model:            model,
		MilvusAddr:       milvusAddr(),
		MilvusUser:       strings.TrimSpace(os.Getenv("MILVUS_USER")),
		MilvusPassword:   strings.TrimSpace(os.Getenv("MILVUS_PASSWORD")),
		KBCollection:     kbCollection(),
		EmbeddingAPIKey:  strings.TrimSpace(os.Getenv("EMBEDDING_API_KEY")),
		EmbeddingBaseURL: strings.TrimSpace(os.Getenv("EMBEDDING_BASEURL")),
		EmbeddingModel:   embModel,
		EmbeddingDim:     embeddingDim(defEmbDim),
	}, nil
}

func milvusAddr() string {
	if s := strings.TrimSpace(os.Getenv("MILVUS_ADDR")); s != "" {
		return s
	}
	return "localhost:19530"
}

func kbCollection() string {
	if s := strings.TrimSpace(os.Getenv("MILVUS_KB_COLLECTION")); s != "" {
		return s
	}
	return "cs_faq"
}

func embeddingModel() string {
	if s := strings.TrimSpace(os.Getenv("EMBEDDING_MODEL")); s != "" {
		return s
	}
	return "text-embedding-3-small"
}

func embeddingDim(def int) int {
	s := strings.TrimSpace(os.Getenv("EMBEDDING_DIM"))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func firstNonEmpty(s ...string) string {
	for _, t := range s {
		if v := strings.TrimSpace(t); v != "" {
			return v
		}
	}
	return ""
}
