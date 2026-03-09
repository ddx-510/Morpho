# Morpho

**A morphogenetic multi-agent framework where AI agents self-organize like biological cells.**

No orchestrator. No predefined workflows. Agents spawn, differentiate, communicate through chemical signals, migrate toward problems, divide when overwhelmed, and die when done — all from local rules, just like embryonic development.

## Why Morphogenesis?

Every multi-agent AI framework today uses the same pattern: a central orchestrator assigns tasks to worker agents. This is a factory floor, not intelligence.

Biological systems solve complex problems differently. During embryonic development, cells don't receive instructions from a manager — they:

- **Sense local chemical gradients** and specialize based on what's needed nearby
- **Communicate indirectly** through the environment (stigmergy), not direct messaging
- **Self-organize into tissues** — functional clusters emerge from local interactions
- **Adapt dynamically** — if one area needs more attention, cells divide and migrate there

Morpho applies this to AI agents. The result: emergent coordination that scales naturally, adapts to any domain, and finds cross-cutting patterns that linear analysis misses.

## How It Works

```
                    ┌─────────────────────────────────────────┐
                    │           Gradient Field                │
                    │                                         │
                    │   ┌─────┐  signals  ┌─────┐            │
                    │   │  A  │ ────────> │  B  │            │
                    │   │ src │ <──────── │ api │            │
                    │   └──┬──┘ chemicals └──┬──┘            │
                    │      │                 │               │
                    │      │    ┌─────┐      │               │
                    │      └──> │  C  │ <────┘               │
                    │           │ db  │                       │
                    │           └─────┘                       │
                    │                                         │
                    │   Agents: ○ ○ ● ◐ ○ ● ○               │
                    │   ○ stem  ● specialized  ◐ migrating   │
                    └─────────────────────────────────────────┘
```

### The Agent Lifecycle

1. **Seeding** — The codebase is scanned into regions. Import analysis builds a semantic topology (which regions depend on which). Signals are set based on complexity and the task.

2. **Spawning** — Undifferentiated stem cell agents appear at high-signal regions.

3. **Differentiation** — Each agent reads local chemical gradients and self-specializes into the role the environment needs most. Lateral inhibition (PRESENCE chemicals) prevents redundant specialization — just like Notch-Delta signaling in real cells.

4. **Work** — Regions are pre-loaded with source code (~30K chars). Agents analyze directly in a single LLM call — no wasted tool calls on navigation. Tools are reserved for exploring beyond pre-loaded content.

5. **Stigmergy** — Findings are deposited into the field. Other agents see them. Discovery chemicals attract more investigators. Knowledge propagates across linked regions through the field topology.

6. **Tissue Formation** — Agents that cluster with diverse roles form functional tissues, gaining energy bonuses. This rewards collaboration over redundancy.

7. **Adaptation** — Agents migrate via chemotaxis (toward distress, away from saturation). They divide when overwhelmed. They die when their region is thoroughly covered.

8. **Convergence** — The system naturally stops when no new findings emerge across consecutive cycles. No fixed iteration count needed.

### Chemical Signaling

| Chemical | Purpose | Biological Analog |
|----------|---------|-------------------|
| `presence` | Role-keyed — prevents duplicate specialists nearby | Notch-Delta signaling |
| `finding` | Attracts complementary roles to form tissues | Morphogen gradients |
| `saturation` | Discourages redundant work in covered areas | Contact inhibition |
| `distress` | Spreads fast, recruits help, triggers division | Inflammatory cytokines |
| `nutrient` | Released on death, triggers stem cell recruitment | Apoptotic signals |
| `discovery` | Emitted on significant findings, attracts investigators | Chemokine signaling |

### Cross-Region Knowledge Propagation

When an agent at region `src/` finds a security issue, that finding doesn't stay local. The engine propagates a digest to all linked regions (determined by import analysis). An agent at `api/` — which imports from `src/` — will see the finding and can investigate whether the vulnerability is exposed through the API layer.

This is how Morpho finds **cross-cutting issues** that single-region analysis misses.

## What Makes This Different

| | Traditional Multi-Agent | Morpho |
|---|---|---|
| **Coordination** | Central orchestrator | Emergent from local chemical rules |
| **Communication** | Direct agent-to-agent messages | Stigmergic (through the environment) |
| **Task assignment** | Top-down delegation | Self-specialization via gradients |
| **Scaling** | Manual agent count configuration | Agents divide/die dynamically |
| **Cross-cutting insights** | Requires explicit wiring | Knowledge propagates through field topology |
| **Domain adaptation** | Code new agent types | LLM generates everything at runtime |
| **Memory** | None or manual state | Tissue memory persists across sessions |
| **Resource management** | Timeout or manual kill | Apoptosis — agents self-terminate |

## Architecture

```
cmd/morpho/       Entry point + benchmark mode
chat/              3-tier auto-routing: chat → assist → swarm
agent/             Agents, gradient field, chemicals, tissues, engine
domain/            LLM-generated signals, roles, prompts, code scanner
llm/               Provider abstraction (OpenAI, Claude, Gemini, etc.)
tool/              Tool registry, built-in tools, MCP client, skills
memory/            FactStore, SessionStore, TissueMemory
event/             Pub-sub bus bridging engine to UIs
ui/                Terminal (ANSI) + Web UI (React + SSE)
skills/            Markdown methodology files injected by role
```

### Three Execution Tiers

Every message is auto-classified by an LLM router:

| Tier | Cost | When | How |
|------|------|------|-----|
| **Chat** | 1 LLM call | Simple questions, greetings | Direct response |
| **Assist** | 2-30 LLM calls | Read a file, search code, fix a bug | Single agent + tools |
| **Swarm** | 10-100+ LLM calls | Security audit, full codebase review | Morphogenetic multi-agent |

### Domain Agnostic

Nothing is hardcoded. When a swarm launches, an LLM generates:
- **Signal dimensions** — what to look for (e.g., security_risk, complexity, error_handling)
- **Roles** — what specialists to create (e.g., security_auditor, performance_analyst)
- **Prompts** — how each role should behave
- **Tool configurations** — what tools agents can use

Morpho adapts to any task — security audit, performance review, architecture analysis, documentation gaps — without code changes.

### Cross-Session Tissue Memory

Morpho remembers across sessions. After each swarm run:
- Signal strengths and findings are persisted per region
- On the next run, residual signals influence agent placement (time-decayed, ~7 day half-life)
- Prior findings are injected so agents don't repeat work
- The system learns which regions need more attention over time

## Quick Start

```bash
# Build
go build -o morpho cmd/morpho/main.go

# Configure
cat > morpho.json << 'EOF'
{
  "providers": [{
    "name": "default",
    "type": "openai",
    "api_key": "$OPENAI_API_KEY",
    "model": "gpt-4o"
  }]
}
EOF

# Terminal mode
./morpho
./morpho -dir /path/to/project

# Web UI (serves on :8390)
./morpho -web

# Benchmark: morpho vs naive parallel
./morpho -bench "find security vulnerabilities" -dir /path/to/repo
```

### Supported Providers

| Type | Default Model |
|------|--------------|
| `openai` | gpt-4o |
| `claude` | claude-sonnet-4-20250514 |
| `gemini` | gemini-2.0-flash |
| `openrouter` | anthropic/claude-sonnet-4-20250514 |
| `groq` | llama-3.3-70b-versatile |
| `together` | meta-llama/Llama-3.3-70B |
| `deepseek` | deepseek-chat |
| `ollama` | llama3 |

Any provider with a `base_url` is treated as OpenAI-compatible. Use `$ENV_VAR` syntax in `api_key`.

## Benchmarks

Morpho includes a built-in benchmark comparing the morphogenetic swarm against naive parallel analysis (one independent agent per region, no communication). Tested against real open-source projects:

```bash
./morpho -bench "find security vulnerabilities" -dir ./target-repo
```

### Results: Security Vulnerability Analysis

| Project | | Findings | Regions | Tokens | Time | Tokens/Finding |
|---------|---|---------|---------|--------|------|---------------|
| **[Gogs](https://github.com/gogs/gogs)** | Naive | 37 | 4 | 87K | 52s | 2,358 |
| *(Go, 85K LOC)* | **Morpho** | **188 (5.1x)** | **17** | 921K | 4m42s | 4,896 |
| **[Juice Shop](https://github.com/juice-shop/juice-shop)** | Naive | 80 | 6 | 81K | 38s | 1,008 |
| *(JS, 48K LOC)* | **Morpho** | **276 (3.5x)** | **19** | 1.1M | 4m46s | 4,075 |
| **[Saleor](https://github.com/saleor/saleor)** | Naive | 46 | 4 | 127K | 21s | 2,766 |
| *(Python, 210K LOC)* | **Morpho** | **331 (7.2x)** | **21** | 1.5M | 3m18s | 4,458 |

**Average: 5.3x more findings, 4.3x more region coverage.** Morpho uses ~2x more tokens per finding but finds 3.5-7.2x more issues — a strong ROI for thoroughness over speed.

The naive baseline gives each region its own agent with the same pre-loaded code — it's already a strong approach. Morpho's advantage comes from:
- **Adaptive coverage** — agents migrate to under-explored regions via chemotaxis
- **Cross-region propagation** — findings at `src/` propagate to linked `api/` regions through field topology
- **Emergent specialization** — lateral inhibition ensures role diversity within each region
- **Dynamic resource allocation** — agents divide in critical areas, die in saturated ones

## Extending Morpho

### Markdown Skills

Drop `.md` files in a `skills/` directory to inject domain expertise into agent prompts:

```markdown
---
name: security_audit
description: Security vulnerability detection
roles: [security, vulnerability, audit]
---

## Security Audit Checklist

### Injection
- SQL injection: string concatenation in database queries
- Command injection: user input passed to exec/system calls
- Path traversal: user-controlled file paths without sanitization

### Authentication
- Hardcoded credentials: grep for "password", "secret", "api_key"
- Missing auth checks on API endpoints
```

Skills are matched to agent roles via the `roles` field and automatically injected during differentiation.

### MCP Tool Servers

Extend agent capabilities with external tools via the Model Context Protocol:

```json
{
  "mcp_servers": [{
    "name": "my-tools",
    "command": "npx",
    "args": ["-y", "@my/mcp-server"]
  }]
}
```

## Tech Stack

- **Go 1.22** — zero external dependencies
- **React** — web UI with real-time SSE streaming
- **Any LLM** — provider-agnostic via adapter pattern

## License

MIT

---

*Morpho: because the best multi-agent systems don't need a manager.*
