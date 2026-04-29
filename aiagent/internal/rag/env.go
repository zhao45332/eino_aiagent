package rag

import (
	"os"
	"strings"
)

// Enabled 为 true 时主程序会尝试连接 Milvus；成功则 skill-faq 挂载 search_knowledge，失败则该技能无检索工具。
func Enabled() bool {
	return os.Getenv("CS_RAG") == "1" || strings.TrimSpace(os.Getenv("KNOWLEDGE_BASE_ENABLED")) == "1"
}
