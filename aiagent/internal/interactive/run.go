// Package interactive：从 cmd/aiagent/main.go 抽离的交互与会话写回（原 runInteractive、readPrintLoop、drainAgentIterator 等），使用 ADK Runner。
package interactive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	einoadk "github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"aiagent/internal/agent"
	"aiagent/internal/mem"
)

// 与模型多轮能力匹配：过长则丢弃最早轮次，避免超上下文。
const maxToolAgentHistoryMessages = 24

func envSuppressSupervisorAfterSkill() bool {
	v := strings.TrimSpace(os.Getenv("AIAGENT_SUPPRESS_SUPERVISOR_AFTER_SKILL"))
	return v != "0" && v != "false"
}

// envDebugADKEvents 为 true 时每条事件打一行 [adk-event]：用于确认当前是哪种子 Agent / 是否为工具回流。
func envDebugADKEvents() bool {
	return strings.TrimSpace(os.Getenv("AIAGENT_DEBUG_EVENTS")) == "1"
}

// Run 对应早期 main 中的 runInteractive：创建 [einoadk.Runner]、读 stdin 多轮、流式 chunk 打印到 stdout，回合结束将合并正文写入 Session。
func Run(ctx context.Context, ag einoadk.Agent, hasKB bool, sess *mem.Session) error {
	r := einoadk.NewRunner(ctx, einoadk.RunnerConfig{Agent: ag, EnableStreaming: true})
	switch agent.AgentMode() {
	case agent.ModeFAQOnly:
		if hasKB {
			fmt.Fprintln(os.Stdout, "企业 FAQ（单 Agent：知识库检索 + calculator）。输入 exit 或 quit 退出。")
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
	return readPrintLoop(func(line string) error {
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
	var lastMath string
	var lastMathStdoutPrinted string
	supPress := envSuppressSupervisorAfterSkill()
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			return pickAssistantOut(lastFAQ, lastMath, supParts), ev.Err
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		if envDebugADKEvents() {
			kind := "assistant"
			if mv.Role == schema.Tool {
				kind = "tool:" + mv.ToolName
			}
			log.Printf("[adk-event] agent=%s kind=%s streaming=%v", ev.AgentName, kind, mv.IsStreaming)
		}
		if mv.Role == schema.Tool {
			continue
		}
		isMathSkill := ev.AgentName == agent.SkillMathName
		// skill-math 已有正文时默认不打印主管复读（可用 AIAGENT_SUPPRESS_SUPERVISOR_AFTER_SKILL=0 关闭）。
		if supPress && lastMath != "" && ev.AgentName == agent.SupervisorAgentName {
			if mv.IsStreaming && mv.MessageStream != nil {
				if _, err := streamAssistantContent(mv.MessageStream, false); err != nil {
					return pickAssistantOut(lastFAQ, lastMath, supParts), err
				}
			} else {
				if _, err := mv.GetMessage(); err != nil {
					return pickAssistantOut(lastFAQ, lastMath, supParts), err
				}
			}
			continue
		}
		var text string
		var err error
		if mv.IsStreaming && mv.MessageStream != nil {
			if isMathSkill {
				text, err = streamAssistantContent(mv.MessageStream, false)
				if err == nil && text != "" {
					text = cleanAssistantStdoutText(text)
					text = dedupeAdjacentDuplicateChineseClauses(text)
					writeMathStdoutOnce(&lastMathStdoutPrinted, text)
				}
			} else {
				text, err = streamAssistantContent(mv.MessageStream, true)
				if err == nil && text != "" {
					text = cleanAssistantStdoutText(text)
				}
			}
		} else {
			var m *schema.Message
			m, err = mv.GetMessage()
			if err != nil {
				return pickAssistantOut(lastFAQ, lastMath, supParts), err
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
			raw := strings.TrimSpace(m.Content)
			if isMathSkill {
				text = cleanAssistantStdoutText(raw)
				text = dedupeAdjacentDuplicateChineseClauses(text)
				writeMathStdoutOnce(&lastMathStdoutPrinted, text)
			} else {
				display := cleanAssistantStdoutText(m.Content)
				if display != "" {
					fmt.Print(display)
				}
				text = strings.TrimSpace(display)
			}
		}
		if err != nil {
			return pickAssistantOut(lastFAQ, lastMath, supParts), err
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
		case agent.SkillMathName:
			lastMath = text
			supParts = append(supParts, text)
		case agent.SupervisorAgentName:
			supParts = append(supParts, text)
		default:
			supParts = append(supParts, text)
		}
	}
	return pickAssistantOut(lastFAQ, lastMath, supParts), nil
}

func streamAssistantContent(stream einoadk.MessageStream, printToStdout bool) (fullText string, err error) {
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
		if chunk.Content != "" && printToStdout {
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

func dedupeAdjacentDuplicateChineseClauses(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "。")
	var b strings.Builder
	var prevNorm string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n := strings.ReplaceAll(p, " ", "")
		if n == prevNorm {
			continue
		}
		prevNorm = n
		if b.Len() > 0 {
			b.WriteString("。")
		}
		b.WriteString(p)
	}
	if b.Len() == 0 {
		return s
	}
	out := b.String()
	if strings.HasSuffix(s, "。") {
		return out + "。"
	}
	return out
}

func writeMathStdoutOnce(lastPrinted *string, text string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	if *lastPrinted != "" && t == strings.TrimSpace(*lastPrinted) {
		return
	}
	fmt.Print(text)
	*lastPrinted = t
}

func cleanAssistantStdoutText(s string) string {
	var b strings.Builder
	normalized := strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		lt := strings.ToLower(t)
		switch lt {
		case agent.SkillMathName, agent.SkillFAQAgentName, agent.SkillWeatherName, agent.SkillGeoName, agent.SupervisorAgentName:
			continue
		}
		if stubJSONNoise(t) {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, "\r"))
	}
	return strings.TrimSpace(b.String())
}

func stubJSONNoise(line string) bool {
	x := strings.TrimSpace(strings.ReplaceAll(line, " ", ""))
	x = strings.ReplaceAll(x, "\t", "")
	return x == "{}" || x == "[]"
}

// pickAssistantOut：FAQ 优先；其次数学专员正文（减轻主管复述）；其余折叠主管段落。
func pickAssistantOut(lastFAQ string, lastMath string, supParts []string) string {
	if strings.TrimSpace(lastFAQ) != "" {
		return strings.TrimSpace(lastFAQ)
	}
	if strings.TrimSpace(lastMath) != "" {
		return strings.TrimSpace(lastMath)
	}
	return collapseAssistantParts(supParts)
}

func isTransferToolNoise(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	return strings.Contains(s, "successfully transferred to agent") ||
		strings.Contains(s, "成功移交任务至 agent")
}

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
