// 入口：交互式客服。默认 supervisor（主管 + 技能子 Agent）；AGENT_MODE=faq_only 时为单 Agent FAQ+RAG。
package main

import (
<<<<<<< HEAD
	"context"
	"flag"
=======
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
	"log"
	"os"
	"strings"

<<<<<<< HEAD
	"aiagent/internal/agent"
	"aiagent/internal/config"
	"aiagent/internal/interactive"
	"aiagent/internal/mem"
	"aiagent/internal/rag"
)

=======
	einoadk "github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"aiagent/internal/agent"
	"aiagent/internal/config"
	"aiagent/internal/csvc"
	"aiagent/internal/mem"
)

// 与模型多轮能力匹配：过长则丢弃最早轮次，避免超上下文。
const maxToolAgentHistoryMessages = 24

// knowledgeBaseBackendEnabled 为 true 时尝试连接 Milvus；成功则 skill-faq 挂载 search_knowledge，失败则该技能无检索工具。
func knowledgeBaseBackendEnabled() bool {
	return os.Getenv("CS_RAG") == "1" || strings.TrimSpace(os.Getenv("KNOWLEDGE_BASE_ENABLED")) == "1"
}

>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
// 默认会话文件目录，可通过环境变量 AIAGENT_SESSION_DIR 覆盖。
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

<<<<<<< HEAD
	if os.Getenv("CS_SEED") == "1" && !rag.Enabled() {
		log.Print("警告: 已设置 CS_SEED=1 但未设置 CS_RAG=1 或 KNOWLEDGE_BASE_ENABLED=1，不会连接 Milvus，灌库与 search_knowledge 均不会生效。")
	}

	// 等价于旧的 knowledgeBaseBackendEnabled()==true：连接 Milvus 并在 skill-faq 挂载 search_knowledge（细节见 internal/rag/env.go）。
	var kb *rag.Service
	if rag.Enabled() {
		var errKB error
		kb, errKB = rag.New(ctx, cfg)
=======
	if os.Getenv("CS_SEED") == "1" && !knowledgeBaseBackendEnabled() {
		log.Print("警告: 已设置 CS_SEED=1 但未设置 CS_RAG=1 或 KNOWLEDGE_BASE_ENABLED=1，不会连接 Milvus，灌库与 search_knowledge 均不会生效。")
	}

	var kb *csvc.RAGService
	if knowledgeBaseBackendEnabled() {
		var errKB error
		kb, errKB = csvc.NewRAGService(ctx, cfg)
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
		if errKB != nil {
			log.Printf("知识库后端未就绪，skill-faq 将不含 search_knowledge: %v", errKB)
			kb = nil
		} else {
			defer kb.Close() //nolint:errcheck
		}
	}

<<<<<<< HEAD
	// 模型 / ChatModelAgent / 工具与主管装配（原 main 里「Agent 创建」整段逻辑），见 internal/agent。
=======
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
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

<<<<<<< HEAD
	// 原 main 中的 runInteractive：stdin 读入、Runner 事件流、流式打印与回合结束写 Session，见 internal/interactive。
	if err := interactive.Run(ctx, ag, kb != nil, sess); err != nil {
		log.Fatalf("对话: %v", err)
	}
}
=======
	if err := runInteractive(ctx, ag, kb != nil, sess); err != nil {
		log.Fatalf("对话: %v", err)
	}
}

func runInteractive(ctx context.Context, ag einoadk.Agent, hasKB bool, sess *mem.Session) error {
	r := einoadk.NewRunner(ctx, einoadk.RunnerConfig{Agent: ag, EnableStreaming: true})
	switch agent.AgentMode() {
	case agent.ModeFAQOnly:
		if hasKB {
			fmt.Fprintln(os.Stdout, "企业 FAQ（单 Agent：知识库检索 + 计算器）。输入 exit 或 quit 退出。")
		} else {
			fmt.Fprintln(os.Stdout, "企业 FAQ（单 Agent；未启用向量库则无 search_knowledge）。KNOWLEDGE_BASE_ENABLED=1 或 CS_RAG=1 + Milvus。输入 exit 或 quit 退出。")
		}
	default:
		if hasKB {
			fmt.Fprintln(os.Stdout, "智能客服（主管 + 技能：知识库/天气/定位/计算）。输入 exit 或 quit 退出。")
		} else {
			fmt.Fprintln(os.Stdout, "智能客服（主管 + 技能；未启用向量库则无 search_knowledge）。KNOWLEDGE_BASE_ENABLED=1 或 CS_RAG=1 + Milvus；定位需 AMAP_KEY。输入 exit 或 quit 退出。")
		}
	}
	err := readPrintLoop(func(line string) error {
		if err := sess.Append(schema.UserMessage(line)); err != nil { // 追加用户消息
			return err
		}
		history := sess.GetMessages()
		history = trimMessageHistory(history, maxToolAgentHistoryMessages)
		iter := r.Run(ctx, history)
		assistant, err := drainAgentIterator(iter) // 消费事件流：模型流式 chunk 用 fmt.Print 边收边输出，结束后拼成完整文本供持久化
		if err != nil {
			if rerr := sess.TruncateLast(1); rerr != nil {
				log.Printf("回滚用户消息失败: %v", rerr)
			}
			return err
		}
		assistant = strings.TrimSpace(assistant)
		if assistant != "" {
			if err := sess.Append(schema.AssistantMessage(assistant, nil)); err != nil {
				return err
			}
		} else {
			if err := sess.TruncateLast(1); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func readPrintLoop(handle func(string) error) error {
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stdout, "你: ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if isExitLine(line) {
			fmt.Fprintln(os.Stdout, "再见。")
			return nil
		}
		if err := handle(line); err != nil {
			log.Printf("处理失败: %v", err)
		}
		fmt.Fprintln(os.Stdout)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return nil
}

func isExitLine(s string) bool {
	switch strings.ToLower(s) {
	case "exit", "quit", "q", "bye", "退出":
		return true
	default:
		return false
	}
}

func trimMessageHistory(h []einoadk.Message, max int) []einoadk.Message {
	if max <= 0 || len(h) <= max {
		return h
	}
	return h[len(h)-max:]
}

func drainAgentIterator(iter *einoadk.AsyncIterator[*einoadk.AgentEvent]) (assistantJoined string, _ error) {
	if iter == nil {
		return "", nil
	}
	var supParts []string
	var lastFAQ string
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			return pickAssistantOut(lastFAQ, supParts), ev.Err
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		if mv.Role == schema.Tool {
			continue
		}
		var text string
		var err error
		if mv.IsStreaming && mv.MessageStream != nil {
			text, err = streamAssistantContentToStdout(mv.MessageStream)
		} else {
			var m *schema.Message
			m, err = mv.GetMessage()
			if err != nil {
				return pickAssistantOut(lastFAQ, supParts), err
			}
			if m == nil || strings.TrimSpace(m.Content) == "" {
				continue
			}
			if m.Role == schema.Tool {
				continue
			}
			if isTransferToolNoise(m.Content) {
				continue
			}
			fmt.Print(m.Content)
			text = strings.TrimSpace(m.Content)
		}
		if err != nil {
			return pickAssistantOut(lastFAQ, supParts), err
		}
		if text == "" {
			continue
		}
		if isTransferToolNoise(text) {
			continue
		}
		switch ev.AgentName {
		case agent.SkillFAQAgentName:
			lastFAQ = text
		case agent.SupervisorAgentName:
			supParts = append(supParts, text)
		default:
			supParts = append(supParts, text)
		}
	}
	return pickAssistantOut(lastFAQ, supParts), nil
}

// streamAssistantContentToStdout 边 Recv 边打印 chunk.Content（增量片段），关闭流后用 ConcatMessages 得到与 ADK 一致的合并正文。
func streamAssistantContentToStdout(stream einoadk.MessageStream) (fullText string, err error) {
	defer stream.Close()
	var chunks []*schema.Message
	for {
		chunk, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			return "", recvErr
		}
		if chunk == nil {
			continue
		}
		if chunk.Role == schema.Tool {
			continue
		}
		chunks = append(chunks, chunk)
		if chunk.Content != "" {
			fmt.Print(chunk.Content)
		}
	}
	if len(chunks) == 0 {
		return "", nil
	}
	merged, err := schema.ConcatMessages(chunks)
	if err != nil {
		return "", err
	}
	if merged == nil {
		return "", nil
	}
	return strings.TrimSpace(merged.Content), nil
}

// pickAssistantOut：主管模式下若 skill-faq 有正文，以其为准展示（避免主管用百科覆盖检索结论）；否则对主管段落做折叠。
func pickAssistantOut(lastFAQ string, supParts []string) string {
	if strings.TrimSpace(lastFAQ) != "" {
		return strings.TrimSpace(lastFAQ)
	}
	return collapseAssistantParts(supParts)
}

func isTransferToolNoise(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	return strings.Contains(s, "successfully transferred to agent") ||
		strings.Contains(s, "成功移交任务至 agent")
}

// collapseAssistantParts：单 Agent ReAct 一轮里可能多次产出带 Content 的助手消息（例如误把工具原文写出后再给最终答复）。
func collapseAssistantParts(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return parts[len(parts)-1]
	}
}
>>>>>>> 76ebdb424921ecf0d37df06b0dc69867613fb3fc
