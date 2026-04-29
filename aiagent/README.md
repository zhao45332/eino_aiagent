# aiagent

基于 [CloudWeGo Eino ADK](https://github.com/cloudwego/eino) 的智能客服示例。默认 **Supervisor（主管代理）** + 多个 **技能子 ChatModelAgent**，由主管通过 `transfer_to_agent` 分流；各技能仅挂载部分工具（`search_knowledge` / `get_weather` / `get_geolocation` / `calculator`）。设置 **`AGENT_MODE=faq_only`** 时可改为**单个** FAQ Agent（仅 `search_knowledge` + `calculator`，无主管与天气/定位）。向量检索装配见 `internal/rag`，交互式 Runner 见 `internal/interactive`。

## 目录说明（分层）

| 路径 | 职责 |
|------|------|
| `cmd/aiagent/` | 进程入口：配置、可选向量库、`interactive` |
| `internal/agent/` | 装配 Supervisor / `faq_only` 根 Agent |
| `internal/bootstrap/` | ChatModel、Embedding（对齐 Eino quickstart） |
| `internal/config/` | 环境与模型加载 |
| `internal/interactive/` | stdin 交互 + ADK Runner、流式输出拼装 |
| `internal/mem/` | 会话 JSONL 持久化 |
| `internal/model/` | OpenAI 兼容 ChatModel 所用 HTTP Client 等 |
| `internal/prompt/` | 系统提示词常量 |
| `internal/rag/` | Milvus、灌库、`RetrieveContext`（供工具实现接口） |
| `internal/retriever/` | 实现 Eino `retriever.Retriever`（Milvus + Embed） |
| `internal/tool/` | InvokableTool：`search_knowledge`、天气、定位、`calculator` 等 |
| `internal/vectorstore/` | Milvus 集合 schema、写入与向量检索 |

原先中间的 `internal/components/` 已去掉：**prompt / tool / model / retriever** 一律提升到 `internal/` 一级，路径即语义，避免「什么都往里扔的 components」。

## 运行

```bash
go run ./cmd/aiagent
```

启动后进入交互，输入 `exit` / `quit` 退出。

- **持久化多轮会话**：对话写入 `data/sessions/<uuid>.jsonl`（JSONL）。恢复：`go run ./cmd/aiagent -session <id>`。目录可用 **`AIAGENT_SESSION_DIR`** 覆盖默认 `data/sessions`。
- **装配模式**：`AGENT_MODE` 省略或为 `supervisor`（默认）；`faq_only` = 单 Agent FAQ+RAG（见 `internal/agent/mode.go`）。
- **可选向量知识库**：`CS_RAG=1` 或 `KNOWLEDGE_BASE_ENABLED=1`，且 Milvus、Embedding 可用时注册 `search_knowledge`。
- **语料更新**：默认 **启动时** 比较 `data/corpus/faq.md` 内容哈希，有变化则自动清空 `faq-*` 主键并重灌（见 `data/corpus/.kb_faq_state.json`），一般**不必**再手动开关 `CS_SEED`。关闭自动同步：`CS_KB_AUTO_SYNC=0`；强制全量重灌：`CS_SEED=1`。
- **长跑进程**：修改 `faq.md` 后需**重启本进程**才会触发同步；线上多采用独立索引服务或 Admin API 热更新，不必整站重启。

可拷贝 `config/.env.example` 为 `config/.env` 填密钥。环境变量说明见 `internal/config/config.go`。

## 参考

- [Eino Supervisor Agent](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_implementation/supervisor/)
- [Eino 文档](https://www.cloudwego.io/docs/eino/)、[eino-examples](https://github.com/cloudwego/eino-examples)
