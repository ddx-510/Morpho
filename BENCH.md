# Morpho Benchmark Results

## 2026-03-05 — Single Agent vs Morpho (Self-Analysis)

**Target:** Morpho codebase itself (13 regions, ~2k LOC Go)
**Provider:** claude-sonnet-4-20250514 via local proxy (OpenAI-compatible)
**Config:** max_ticks=5, decay=0.05, diffusion=0.3, spawn_per_tick=2

### Results

| Metric                  |  Single |   Morpho |
|-------------------------|--------:|---------:|
| LLM calls               |       1 |       28 |
| Total findings           |      78 |       28 |
| Code-specific findings   |      27 |       28 |
| Specialist roles         |       1 |        4 |
| Regions covered          |       9 |        5 |
| Wall time                | 43.38s  | 7m27.68s |
| Tissue clusters          |     n/a |        6 |

### Morpho Role Breakdown

| Role              | Findings |
|-------------------|----------|
| refactorer        |       22 |
| test_writer       |        4 |
| security_auditor  |        1 |
| documenter        |        1 |

### Morpho Region Breakdown

| Region  | Findings |
|---------|----------|
| memory  |       14 |
| engine  |        7 |
| cmd     |        5 |
| scan    |        1 |
| .claude |        1 |

### Tissue Clusters Formed

- `Tissue[memory]` — up to 4 refactorer agents swarming high-complexity region
- `Tissue[engine]` — mixed cluster: test_writer + refactorer + security_auditor
- Agents naturally concentrated on highest-signal regions (memory, engine)

### Analysis

**Single agent strengths:**
- Fast (one LLM call, 43s wall time)
- Broad coverage (touched 9/13 regions)
- High total finding count (78 lines parsed as findings)

**Morpho strengths:**
- Higher code-specificity rate (28/28 = 100% vs 27/78 = 35%)
- Specialist depth — security_auditor found issues generalist missed
- Emergent resource allocation — more agents where signals are strongest
- Tissue formation shows collaborative clustering

**Current weaknesses (to improve):**
- Wall time ~10x slower (sequential agent execution)
- Redundant findings across agents in same region (no deduplication)
- Only 5/13 regions covered (agents died in low-signal areas before reaching others)

---

## 2026-03-05 — After Parallel Execution

**Same target, config, and provider as above.**
**Change:** agents now run concurrently within each tick via goroutines.

### Results

| Metric                  |  Single |   Morpho |
|-------------------------|--------:|---------:|
| LLM calls               |       1 |       24 |
| Total findings           |      84 |       24 |
| Code-specific findings   |      27 |       22 |
| Specialist roles         |       1 |        4 |
| Regions covered          |      10 |        8 |
| Wall time                | 41.05s  |  1m40.5s |
| Tissue clusters          |     n/a |        2 |

### Timing Impact

| Version    | Morpho Wall Time | Speedup |
|------------|-----------------|---------|
| Sequential | 7m27.68s        | —       |
| Parallel   | 1m40.5s         | 4.5x   |

### Morpho Role Breakdown

| Role              | Findings |
|-------------------|----------|
| security_auditor  |       12 |
| refactorer        |        5 |
| test_writer       |        4 |
| documenter        |        3 |

### Morpho Region Breakdown

| Region | Findings |
|--------|----------|
| llm    |        5 |
| agent  |        5 |
| cmd    |        3 |
| root   |        3 |
| field  |        3 |
| config |        2 |
| tool   |        2 |
| scan   |        1 |

### Analysis

- **4.5x speedup** from parallel agent execution — agents at different points make independent LLM calls concurrently
- **Better region coverage** (8/13 vs 5/13 previously) — faster ticks allow agents to reach more regions
- **More balanced role distribution** — security_auditor emerged as dominant role (12 findings) due to high security signals in config/scan/root
- Morpho wall time now only ~2.5x single agent (vs ~10x before)
- Remaining gap is inherent: morpho makes 24 LLM calls vs 1, but each is scoped and specialized

---

## Improvement Roadmap

### Timing
1. ~~**Parallel agent execution**~~ — DONE (4.5x speedup)
2. **Reduce redundant ticks** — agents re-analyzing same files each tick waste LLM calls; cache file reads across ticks
3. **Smarter spawning** — don't spawn agents in regions that already have active specialists

### Quality
1. **Finding deduplication** — hash or embed findings to suppress near-duplicates
2. **Cross-agent memory** — let agents in same tissue share short-term memory
3. **Adaptive spawn** — don't spawn new agents in regions already saturated with specialists

### Coverage
1. **Migration** — agents in low-signal regions should migrate to high-signal neighbors before dying
2. **Lower apoptosis threshold** — keep agents alive longer in early ticks to cover more ground
