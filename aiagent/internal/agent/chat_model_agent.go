// Package agent 使用 Eino ADK 装配主管代理（Supervisor）与多个技能子 ChatModelAgent。
package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
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

// NewSupervisorCustomerAgent 创建主管 + 子技能（各绑部分工具），由主管通过 transfer_to_agent 分流。
func NewSupervisorCustomerAgent(ctx context.Context, c *config.Config, kb tool.KnowledgeRetriever) (adk.Agent, error) {
	cm, err := bootstrap.NewChatModel(ctx, c)
	if err != nil {
		return nil, err
	}
	sup, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        SupervisorAgentName,
		Description: "企业智能客服主管：按意图将任务移交给知识库、天气、定位或计算专员。",
		Instruction: prompt.SupervisorInstruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: []etool.BaseTool{}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("supervisor ChatModelAgent: %w", err)
	}

	var subs []adk.Agent

	faqTools := make([]etool.BaseTool, 0, 1)
	if kb != nil {
		kbT, errKB := tool.NewKnowledgeSearchTool(kb)
		if errKB != nil {
			return nil, errKB
		}
		faqTools = append(faqTools, kbT)
	}
	faqAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        SkillFAQAgentName,
		Description: "企业 FAQ、政策、退换货、支付物流、发票与知识库检索。",
		Instruction: prompt.SkillFAQInstruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: faqTools}},
	})
	if err != nil {
		return nil, fmt.Errorf("skill-faq: %w", err)
	}
	subs = append(subs, faqAgent)

	weatherT, err := tool.NewGetWeatherTool()
	if err != nil {
		return nil, err
	}
	weatherAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        SkillWeatherName,
		Description: "查询指定城市或地区的天气、气温、降雨等。",
		Instruction: prompt.SkillWeatherInstruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: []etool.BaseTool{weatherT}}},
	})
	if err != nil {
		return nil, fmt.Errorf("skill-weather: %w", err)
	}
	subs = append(subs, weatherAgent)

	geoT, err := tool.NewGeolocationTool()
	if err != nil {
		return nil, err
	}
	geoAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        SkillGeoName,
		Description: "高德 IP 定位、经纬度逆地理编码、我在哪等地理位置问题。",
		Instruction: prompt.SkillGeoInstruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: []etool.BaseTool{geoT}}},
	})
	if err != nil {
		return nil, fmt.Errorf("skill-geo: %w", err)
	}
	subs = append(subs, geoAgent)

	calcT, err := tool.NewCalculatorTool()
	if err != nil {
		return nil, err
	}
<<<<<<< HEAD
	mathTools := []etool.BaseTool{calcT}
	mathAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        SkillMathName,
		Description: "精确计算：唯一的流式工具 calculator（逐项累加）。",
		Instruction: prompt.SkillMathInstruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: mathTools}},
=======
	mathAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        SkillMathName,
		Description: "两个数的加法等简单计算。",
		Instruction: prompt.SkillMathInstruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: ecompose.ToolsNodeConfig{Tools: []etool.BaseTool{calcT}}},
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
	})
	if err != nil {
		return nil, fmt.Errorf("skill-math: %w", err)
	}
	subs = append(subs, mathAgent)

	root, err := supervisor.New(ctx, &supervisor.Config{
		Supervisor: sup,
		SubAgents:  subs,
	})
	if err != nil {
		return nil, fmt.Errorf("supervisor.New: %w", err)
	}
	return root, nil
}
