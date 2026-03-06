# Morpho

A bio-inspired multi-agent system built in Go. Agents differentiate into specialist roles based on gradient signals, mimicking morphogenesis in biology. No external dependencies.

## What It Does

Morpho is a chat-first agent that automatically scales its execution based on task complexity:

```
Simple question  →  chat   →  direct LLM response
Focused task     →  assist →  single agent with tool-calling loop
Broad analysis   →  swarm  →  multi-agent morphogenetic system
```

The **router** uses an LLM call to classify intent — not keyword matching. It reads conversation history for context, returns a structured plan, and supports escalation (assist can upgrade to swarm mid-task if the scope turns out to be larger than expected).

## Why This Architecture

**The problem with most agent systems:** they're either too simple (one LLM call, no tools) or too heavy (always spawn a full orchestration pipeline). There's no middle ground, and no way for the system itself to decide.

**Morpho's approach — three tiers with emergent routing:**

| Tier | Cost | When |
|------|------|------|
| **Chat** | 1 LLM call | Greetings, explanations, simple questions |
| **Assist** | 2-8 LLM calls | Read a file, search code, fix a specific bug |
| **Swarm** | 10-50+ LLM calls | Codebase-wide security audit, comprehensive review |

The router doesn't just save tokens — it gives you the right **interaction pattern** for each task. A simple question shouldn't spawn 20 agents. A security audit shouldn't be one LLM call.

**The swarm tier is where the biology comes in:**

1. **Gradient field** — a multi-dimensional signal space seeded from the input (code complexity, bug density, security risk, etc). Each region has signal strengths that decay and diffuse over time.

2. **Differentiation** — agents spawn undifferentiated, read local gradients, and specialize into the role matching the strongest signal. No central controller assigns roles.

3. **Morphogen bus** — stigmergic signaling (PRESENCE, NEED, SATURATION, ALARM) between agents modulates the field. If an agent finds a critical security issue, it emits ALARM, attracting more agents to that region.

4. **Apoptosis** — agents die when their local signal drops below threshold or energy depletes. This is emergent resource management — no orchestrator decides who lives.

5. **Pre-read, don't tool-loop** — agents inject source content directly into the LLM prompt instead of making the LLM call tools iteratively. One LLM call per agent per tick. This eliminates the "let me read the file" narration problem.

The result: agents naturally concentrate on high-signal regions, avoid redundant work, and self-terminate when done. The system adapts to the shape of the input.

**Domain-agnostic** — the engine, field, and morphogen bus know nothing about code review. The `domain/` package defines signals, roles, prompts, and seeders. Built-in domains: `code_review`, `research`, `writing_review`, `data_analysis`. Or pass any free-text task description and Morpho auto-generates a domain via LLM.

## Quick Start

```bash
# Configure your LLM provider
cat > morpho.json << 'EOF'
{
  "provider": {
    "type": "openai",
    "api_key": "$OPENAI_API_KEY",
    "model": "gpt-4o"
  },
  "engine": {
    "max_ticks": 5,
    "decay_rate": 0.05,
    "diffusion_rate": 0.3,
    "spawn_per_tick": 2
  }
}
EOF

# Terminal mode (default)
go run cmd/morpho/main.go

# Web UI mode
go run cmd/morpho/main.go -web

# Point at a specific directory
go run cmd/morpho/main.go -dir /path/to/project
```

### Terminal UI

```
  morpho
  ──────────────────────────────────────────────────
  provider  OpenAI(gpt-4o)
  dir       /path/to/project
  routing   chat │ assist │ swarm (auto)
  ──────────────────────────────────────────────────

  ❯ hi
  [chat] conversational greeting
  │ Hello! How can I help?

  ❯ what does the Run method in engine.go do?
  [assist] focused question about specific function
  ▸ read_file  {path: engine/engine.go}
  │ The Run method executes the tick-based simulation loop...

  ❯ review this codebase for security issues
  [swarm] broad multi-file security analysis
  ━━ tick 1/5
   + a1 at agent/
   ~ a1 → security_auditor
   ✓ security_auditor a1@agent/
  ...
```

### Scheduled Tasks

```
  ❯ /cron add 1h review this project for new bugs
  scheduled [job1] every 1h0m0s: review this project for new bugs

  ❯ /cron list
  [job1] every 1h0m0s (active): review this project for new bugs

  ❯ /cron pause job1
  ❯ /cron rm job1
```

## Supported Providers

Configure `type` in `morpho.json`:

| Type | Default Model | Base URL |
|------|--------------|----------|
| `openai` | gpt-4o | https://api.openai.com/v1 |
| `claude` | claude-sonnet-4-20250514 | https://api.anthropic.com |
| `gemini` | gemini-2.0-flash | https://generativelanguage.googleapis.com |
| `openrouter` | anthropic/claude-sonnet-4-20250514 | https://openrouter.ai/api/v1 |
| `groq` | llama-3.3-70b-versatile | https://api.groq.com/openai/v1 |
| `together` | meta-llama/Llama-3.3-70B... | https://api.together.xyz/v1 |
| `deepseek` | deepseek-chat | https://api.deepseek.com/v1 |
| `ollama` | llama3 | http://localhost:11434/v1 |

Any unknown type with a `base_url` is treated as OpenAI-compatible. Use `$ENV_VAR` syntax in `api_key`.

## License

MIT
