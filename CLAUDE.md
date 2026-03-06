# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Terminal mode (default)
go run cmd/morpho/main.go
go run cmd/morpho/main.go -dir /path/to/project

# Web UI mode (serves on :8390)
go run cmd/morpho/main.go -web
go run cmd/morpho/main.go -web -dir /path/to/project

# Build binary
go build -o morpho cmd/morpho/main.go

# Standard Go commands (no tests exist yet)
go build ./...
go vet ./...
```

Single entry point: `cmd/morpho/main.go`. Flags: `-config`, `-web`, `-port`, `-dir`.

## Architecture

Morpho is a chat-first agent with three execution tiers, auto-routed by an LLM classifier.

### Packages (10 total)

| Package | Role |
|---------|------|
| `agent/` | Core simulation: agents, gradient field, chemicals, tissue detection, engine |
| `chat/` | App struct, 3-tier routing (router + cron scheduler), session management |
| `cmd/morpho/` | Single entry point |
| `config/` | JSON config loading, provider settings |
| `domain/` | Domain struct, `Auto()` LLM generation, dir scanning/seeding |
| `event/` | Pub-sub bus bridging engine to UIs |
| `llm/` | Provider abstraction, multi-turn chat messages, tool call support |
| `memory/` | FactStore, SessionStore (JSONL with Steps) |
| `tool/` | Tool interface, Registry, built-in tools, MCP client, LLM-powered skills |
| `ui/` | Terminal ANSI output, web SSE server |

### Three Tiers (chat/)

The `chat.Router` uses one LLM call to classify each message into:
- **Chat** — direct LLM response, no tools
- **Assist** — single agent with multi-turn tool-calling loop (up to 25 turns)
- **Swarm** — full morpho multi-agent system

Classification considers conversation history. The `chat.Cron` runs scheduled recurring jobs through the same pipeline.

### Swarm Engine (agent/engine.go)

Each tick: **spawn** → **differentiate** → **work** (parallel LLM calls) → **apoptosis** → **tissue detection** → **morphogen signals** → **decay/diffuse**

### Signal → Role → Work Pipeline

1. **Domain** (`domain/`) defines signal dimensions, roles, prompt templates, seeder, and tools. All auto-generated via LLM (`domain/auto.go`).

2. **Seeder** (`domain/scan.go` for code) populates the **gradient field** (`agent/field.go`) — thread-safe map of points with signal values (0-1).

3. **Agents** (`agent/agent.go`) spawn undifferentiated, read local gradients, specialize into the role matching the strongest signal. Multi-turn tool calling with proper conversation history.

4. **Chemicals** (`agent/morphogen.go`) carry stigmergic signals (PRESENCE, FINDING, SATURATION, DISTRESS, NUTRIENT) that modulate the field.

5. **Apoptosis** — agents die when energy depletes, idle too long, or signal drops below threshold.

### Key Design Patterns

- **Multi-turn tool calling**: agents build proper `llm.ChatMessage` conversation history with structured tool calls and results.
- **Deduplication**: engine skips redundant work (`coveredBy` map + per-tick `seen` set).
- **Parallel work**: all agent LLM calls within a tick run concurrently via goroutines.
- **Domain-agnostic via `Domain` struct**: signals, roles, prompts, seeder, tool builder are all pluggable.

### Tool System

`tool/` provides `Tool` interface and `Registry`. Built-in tools: `read_file`, `grep`, `patch_file`, `shell`, `list_files`. MCP client (`tool/mcp.go`) for external tool servers. Skills (`tool/skill.go`) are LLM-powered tools (web_fetch, summarize, extract, transform, reason) in the same package.

### Event System

`event/` is a pub-sub bus bridging engine progress to UIs (terminal ANSI output, SSE/web).

## Config

`morpho.json` at project root (gitignored). Provider `type`: `openai`, `claude`, `gemini`, `openrouter`, `groq`, `together`, `deepseek`, `ollama`, or custom with `base_url`. Use `$ENV_VAR` syntax in `api_key`.

## Module

`github.com/ddx-510/Morpho` — Go 1.22, no external dependencies.
