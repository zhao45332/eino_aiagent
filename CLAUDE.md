# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

- Run the interactive agent: `go run ./cmd/aiagent`
- Resume an existing JSONL session: `go run ./cmd/aiagent -session <session-id>`
- Build the binary: `go build ./cmd/aiagent`
- Compile all packages: `go test ./...`
- Run one package's tests: `go test ./internal/<package>`
- Run one Go test when tests exist: `go test ./internal/<package> -run TestName`
- Format Go code: `gofmt -w <files>`
- Start the local Milvus dependency for RAG: `docker compose -f milvus-standalone-docker-compose.yml up -d`

There is no project-specific lint command or test suite currently checked in beyond standard Go tooling.

## Runtime configuration

- Copy `config/.env.example` to `config/.env` for local secrets and model settings; `internal/config.LoadConfig` loads both `config/.env` and root `.env` from the working directory or executable directory.
- Default model path is OpenAI-compatible (`MODEL_TYPE=openai`) using `OPENAI_API_KEY` or `DASHSCOPE_API_KEY`; `MODEL_TYPE=ark` uses `ARK_API_KEY` plus `ARK_MODEL`/`CHAT_MODEL`.
- `AGENT_MODE` controls root agent wiring: empty/`supervisor` uses the supervisor plus skill agents; `faq_only` uses one FAQ agent with fewer tools.
- RAG is enabled only when `CS_RAG=1` or `KNOWLEDGE_BASE_ENABLED=1`; otherwise the app still runs but FAQ agents do not get `search_knowledge`.
- Sessions are stored as JSONL under `data/sessions` by default; override with `AIAGENT_SESSION_DIR`.

## Architecture

This is a Go CLI example built on CloudWeGo Eino ADK for an interactive customer-service agent.

- `cmd/aiagent/main.go` is the process entry point. It loads configuration, optionally initializes RAG, constructs the root agent, opens or creates a persisted session, then delegates stdin/stdout interaction to `internal/interactive`.
- `internal/agent` owns ADK agent composition. `NewCustomerAgent` chooses between supervisor mode and `faq_only`. Supervisor mode creates one supervisor ChatModelAgent and skill sub-agents for FAQ, weather, geolocation, and math; the supervisor routes via `transfer_to_agent`. `faq_only` creates a single FAQ ChatModelAgent with knowledge search when available plus calculator.
- `internal/bootstrap` and `internal/model` isolate model construction. Chat models support OpenAI-compatible and Ark providers; embeddings use the OpenAI-compatible embedding component and must match the Milvus collection dimension.
- `internal/tool` exposes Eino invokable tools. `search_knowledge` depends on the small `KnowledgeRetriever` interface so tools do not import the RAG service directly; weather, geolocation, and calculator are separate tools mounted only on relevant agents.
- `internal/rag`, `internal/retriever`, and `internal/vectorstore` form the RAG stack. `rag.Service` builds the embedder and Milvus-backed retriever, ensures the collection schema, syncs `data/corpus/faq.md` into Milvus when the corpus hash changes, and returns filtered context to `search_knowledge`.
- `internal/interactive` creates the ADK Runner with streaming enabled, trims persisted history to the last 24 messages before each run, suppresses tool/transfer noise from output, and appends successful user/assistant turns to the session.
- `internal/mem` persists sessions as one JSON object per line: a session header followed by Eino schema messages. Failed turns are rolled back by truncating the last appended user message.
- `internal/prompt` contains the system instructions used by the supervisor and skill agents.

## Data and local services

- `data/corpus/faq.md` is the FAQ corpus used for RAG ingestion when present.
- `data/corpus/.kb_faq_state.json` records the last ingested FAQ hash and count.
- Milvus defaults to `localhost:19530` with collection `cs_faq`; set `MILVUS_ADDR`, `MILVUS_USER`, `MILVUS_PASSWORD`, and `MILVUS_KB_COLLECTION` to override.
- If the embedding dimension changes for an existing collection, the app reports a dimension mismatch; use a new collection name or recreate the Milvus collection.
