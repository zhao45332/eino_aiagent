package tool

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	tutils "github.com/cloudwego/eino/components/tool/utils"
)

// KnowledgeRetriever 由知识库服务（如 rag.Service）实现，供工具注册，避免 tool 包直接依赖业务包形成循环引用。
type KnowledgeRetriever interface {
	RetrieveContext(ctx context.Context, query string) (string, error)
}

type knowledgeSearchInput struct {
	Query string `json:"query" jsonschema:"description=用于检索的自然语言问题或关键词，与当前用户意图一致，用中文"`
}

// NewKnowledgeSearchTool 将向量检索暴露为 search_knowledge，由模型在 ReAct 中按需调用。
func NewKnowledgeSearchTool(svc KnowledgeRetriever) (tool.InvokableTool, error) {
	if svc == nil {
		return nil, fmt.Errorf("知识库服务为空")
	}
	desc := `从已向量化导入的企业语料中按 query 检索相关片段（内容以当前灌库语料为准）。在 skill-faq 中须先调用本工具；query 应紧贴用户问题用语，必要时可加同义改写以便命中，多轮对话须带上文已出现实体。答复仅基于返回片段，不得编造；两数相加请用 calculator，气温与降雨请用 get_weather；勿用本工具做算术或查天气。`
	return tutils.InferTool("search_knowledge", desc, func(ctx context.Context, in knowledgeSearchInput) (string, error) {
		return svc.RetrieveContext(ctx, in.Query)
	})
}
