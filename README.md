# Morpho

A bio-inspired multi-agent code analysis system built in Go. Agents differentiate into specialist roles based on gradient signals detected in source code, mimicking morphogenetic processes in biology.

## How It Works

```
Source Code → Scan → Gradient Field → Spawn Agents → Differentiate → Analyze → Report
```

1. **Scan** — walks a codebase directory, groups files by package/folder, and seeds a gradient field with heuristic signals (complexity, bug density, test coverage, security, performance, doc debt)
2. **Gradient Field** — a multi-dimensional signal space where each region has signal strengths that decay and diffuse over time
3. **Agents** — spawn as undifferentiated cells, read local gradients, and specialize into roles:
   - `bug_hunter` — logic errors, edge cases, nil dereferences
   - `test_writer` — missing test coverage
   - `security_auditor` — injection, hardcoded secrets, unsafe operations
   - `refactorer` — complexity, duplication, god objects
   - `documenter` — missing or misleading docs
   - `optimizer` — performance issues, unnecessary allocations
4. **Morphogen Bus** — stigmergic signaling between agents (PRESENCE, NEED, SATURATION, ALARM) that modulates the field
5. **Tissue Formation** — co-located agents cluster into tissues for collaborative analysis
6. **Apoptosis** — agents with no signal or depleted energy die off, preventing wasted work

## Architecture

```
morpho/
├── field/       # Gradient field with signal decay + diffusion
├── morphogen/   # Stigmergic signal bus
├── agent/       # Agent lifecycle: differentiation, work, apoptosis
├── tissue/      # Cluster detection for co-located agents
├── engine/      # Tick-based simulation loop
├── llm/         # Multi-provider LLM interface (OpenAI, Claude, Gemini, etc.)
├── tool/        # Built-in tools: read_file, grep, patch_file, shell, list_files
├── memory/      # Short-term (per-agent) + long-term (shared) memory
├── scan/        # Directory scanner that seeds gradient signals
├── config/      # JSON config with provider presets
├── cmd/
│   ├── morpho/  # Interactive CLI entry point
│   └── bench/   # Benchmark: single-agent vs morpho comparison
```

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
  },
  "memory": {
    "short_term_capacity": 20,
    "long_term_path": ".morpho_memory.json"
  }
}
EOF

# Analyze a codebase
go run cmd/morpho/main.go /path/to/project

# Run benchmark comparison
go run cmd/bench/main.go /path/to/project
```

## Supported Providers

Configure via `morpho.json` — set `type` to any of:

| Type         | Default Model                    | Base URL                                        |
|--------------|----------------------------------|-------------------------------------------------|
| `openai`     | gpt-4o                          | https://api.openai.com/v1                       |
| `claude`     | claude-sonnet-4-20250514        | https://api.anthropic.com                       |
| `gemini`     | gemini-2.0-flash                | https://generativelanguage.googleapis.com        |
| `openrouter` | anthropic/claude-sonnet-4-20250514 | https://openrouter.ai/api/v1                 |
| `groq`       | llama-3.3-70b-versatile         | https://api.groq.com/openai/v1                  |
| `together`   | meta-llama/Llama-3.3-70B...     | https://api.together.xyz/v1                     |
| `deepseek`   | deepseek-chat                   | https://api.deepseek.com/v1                     |
| `ollama`     | llama3                          | http://localhost:11434/v1                        |

Any unknown type with a `base_url` is treated as OpenAI-compatible.

Use `$ENV_VAR` syntax in `api_key` to reference environment variables.

## Benchmark Results

See [BENCH.md](BENCH.md) for detailed comparison data.

**TL;DR** — On a ~2k LOC Go codebase, morpho matches a single-agent's code-specific finding count (28 vs 27) while achieving 100% code-specificity rate (vs 35% for generalist). Specialist agents find issues a generalist overlooks. The gradient field naturally allocates more agents to higher-signal regions.

## Key Design Decisions

- **Pre-read, don't tool-loop** — agents read their region's source files directly and inject code into the LLM prompt, rather than asking the LLM to call tools. This eliminates the "let me read the file" narration problem.
- **One LLM call per agent per tick** — keeps the agent loop simple and predictable.
- **Signal decay + diffusion** — prevents agents from re-analyzing already-covered regions while spreading awareness of nearby issues.
- **Apoptosis over orchestration** — agents die when their local signal drops below threshold, rather than a central controller deciding who lives. This is emergent resource management.

## License

MIT
