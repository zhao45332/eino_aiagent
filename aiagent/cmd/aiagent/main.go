// 入口：交互式客服。默认 supervisor（主管 + 技能子 Agent）；AGENT_MODE=faq_only 时为单 Agent FAQ+RAG。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"aiagent/internal/agent"
	"aiagent/internal/config"
	"aiagent/internal/interactive"
	"aiagent/internal/mem"
	"aiagent/internal/rag"
)

// 默认会话文件目录，可通过环境变量 AIAGENT_SESSION_DIR 覆盖。。
const defaultSessionDir = "data/sessions"

var sessionFlag = flag.String("session", "", "恢复已有会话 ID（data/sessions/<id>.jsonl）；留空则新建")

func main() {
	flag.Parse()

	ctx := context.Background()
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置: %v", err)
	}
	log.Printf("MODEL_TYPE=%s 使用模型: %s AGENT_MODE=%s\n", cfg.ModelType, cfg.Model, agent.AgentMode())

	if os.Getenv("CS_SEED") == "1" && !rag.Enabled() {
		log.Print("警告: 已设置 CS_SEED=1 但未设置 CS_RAG=1 或 KNOWLEDGE_BASE_ENABLED=1，不会连接 Milvus，灌库与 search_knowledge 均不会生效。")
	}

	// 等价于旧的 knowledgeBaseBackendEnabled()==true：连接 Milvus 并在 skill-faq 挂载 search_knowledge（细节见 internal/rag/env.go）。
	var kb *rag.Service
	if rag.Enabled() {
		var errKB error
		kb, errKB = rag.New(ctx, cfg)
		if errKB != nil {
			log.Printf("知识库后端未就绪，skill-faq 将不含 search_knowledge: %v", errKB)
			kb = nil
		} else {
			defer kb.Close() //nolint:errcheck
		}
	}

	// 模型 / ChatModelAgent / 工具与主管装配（原 main 里「Agent 创建」整段逻辑），见 internal/agent。
	ag, err := agent.NewCustomerAgent(ctx, cfg, kb)
	if err != nil {
		log.Fatalf("Agent: %v", err)
	}

	sessionDir := strings.TrimSpace(os.Getenv("AIAGENT_SESSION_DIR"))
	if sessionDir == "" {
		sessionDir = defaultSessionDir
	}
	store, err := mem.NewStore(sessionDir) // 持久化存储
	if err != nil {
		log.Fatalf("会话存储: %v", err)
	}
	sess, _, err := store.GetOrCreate(strings.TrimSpace(*sessionFlag)) // 获取或创建 Session
	if err != nil {
		log.Fatalf("会话: %v", err)
	}

	// 原 main 中的 runInteractive：stdin 读入、Runner 事件流、流式打印与回合结束写 Session，见 internal/interactive。
	if err := interactive.Run(ctx, ag, kb != nil, sess); err != nil {
		log.Fatalf("对话: %v", err)
	}
}
