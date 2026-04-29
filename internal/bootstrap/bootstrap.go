// Package bootstrap 创建 BaseChatModel（Eino quickstart「创建 ChatModel」一步）。
// 参考： https://www.cloudwego.io/zh/docs/eino/quick_start/chapter_01_chatmodel_and_message/
package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	emodel "github.com/cloudwego/eino/components/model"

	"aiagent/internal/config"
	"aiagent/internal/model"
)

// NewChatModel 根据 [config.Config.ModelType] 创建 ChatModel：openai（含智谱等 OpenAI 兼容网关）或 ark。
func NewChatModel(ctx context.Context, c *config.Config) (emodel.BaseChatModel, error) {
	switch strings.ToLower(strings.TrimSpace(c.ModelType)) {
	case "ark":
		return newArk(ctx, c)
	case "openai":
		return model.NewOpenAI(ctx, c)
	default:
		return nil, fmt.Errorf("bootstrap: 未知 ModelType %q", c.ModelType)
	}
}

func newArk(ctx context.Context, c *config.Config) (emodel.BaseChatModel, error) {
	cfg := &ark.ChatModelConfig{
		APIKey: c.ArkAPIKey,
		Model:  c.Model,
	}
	if c.ArkBaseURL != "" {
		cfg.BaseURL = c.ArkBaseURL
	}
	m, err := ark.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ark chat model: %w", err)
	}
	return m, nil
}
