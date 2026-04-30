// NewFAQOnlyAgent：单 Agent 模式，适合「只要企业 FAQ + RAG」、不需要天气/定位/多路分流的部署。

package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	etool "github.com/cloudwego/eino/components/tool"
	ecompose "github.com/cloudwego/eino/compose"

	"aiagent/internal/bootstrap"
<<<<<<< HEAD
	"aiagent/internal/config"
	"aiagent/internal/prompt"
	"aiagent/internal/tool"
=======
	"aiagent/internal/components/prompt"
	"aiagent/internal/components/tool"
	"aiagent/internal/config"
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
)

// NewFAQOnlyAgent 定义agent
func NewFAQOnlyAgent(ctx context.Context, c *config.Config, kb tool.KnowledgeRetriever) (adk.Agent, error) {
	cm, err := bootstrap.NewChatModel(ctx, c) // ChatModel负责与大语言模型通信的组件，屏蔽不同模型提供商的差异。类比理解： ChatModel 就像"数据库驱动"：负责与数据库通信，屏蔽 MySQL/PostgreSQL 的差异
	if err != nil {
		return nil, err
	}
<<<<<<< HEAD

	tools := make([]etool.BaseTool, 0, 4)
=======
	tools := make([]etool.BaseTool, 0, 2)
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
	if kb != nil {
		kbT, errKB := tool.NewKnowledgeSearchTool(kb)
		if errKB != nil {
			return nil, errKB
		}
		tools = append(tools, kbT)
	}
	calcT, err := tool.NewCalculatorTool()
	if err != nil {
		return nil, err
	}
	tools = append(tools, calcT)

	instruction := prompt.SkillFAQInstructionFAQOnlyNoKB
	if kb != nil {
		instruction = prompt.SkillFAQInstructionFAQOnly
	}

	// Agent 基于模型构建的智能体，可以调用模型，但还能做更多事。类比理解：ChatModelAgent 就像"业务逻辑层"：基于数据库驱动构建，但还包含业务规则、事务管理等
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{ // 基于ChatModel创建，所以只具备了对话能力，需要添加工具来实现更高级的功能
		Name:        SkillFAQAgentName,
		Description: "企业 FAQ：知识库检索与简单计算（AGENT_MODE=faq_only）。",
		Instruction: instruction,                                                              // 作为系统级提示的命令
		Model:       cm,                                                                       // 配置调用三方ai模型
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: tools}}, // 工具集合
	})
	if err != nil {
		return nil, fmt.Errorf("faq_only ChatModelAgent: %w", err)
	}
	return agent, nil
}
