package agent

import (
	"context"
	"os"
	"strings"

	"github.com/cloudwego/eino/adk"

<<<<<<< HEAD
	"aiagent/internal/config"
	"aiagent/internal/tool"
=======
	"aiagent/internal/components/tool"
	"aiagent/internal/config"
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
)

// 环境变量 AGENT_MODE（不区分大小写）：
//   - 空或未设置：supervisor（主管 + 多技能子 Agent，默认）
//   - faq_only：单 ChatModelAgent（知识库检索 + 计算器），无 transfer_to_agent 分流，延迟与调用链更短。
const (
	EnvAgentMode   = "AGENT_MODE"
	ModeFAQOnly    = "faq_only"
	ModeSupervisor = "supervisor"
)

// AgentMode 返回规范化模式；空值视为 supervisor。
func AgentMode() string {
	s := strings.ToLower(strings.TrimSpace(os.Getenv(EnvAgentMode)))
	if s == "" {
		return ModeSupervisor
	}
	return s
}

// NewCustomerAgent 按 [AgentMode] 装配根 Agent。
func NewCustomerAgent(ctx context.Context, c *config.Config, kb tool.KnowledgeRetriever) (adk.Agent, error) {
	if AgentMode() == ModeFAQOnly {
		return NewFAQOnlyAgent(ctx, c, kb)
	}
	return NewSupervisorCustomerAgent(ctx, c, kb)
}
