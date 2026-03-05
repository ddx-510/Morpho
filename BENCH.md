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

## Improvement Roadmap

### Timing (Priority)
1. **Parallel agent execution** — agents at different points are independent; run them concurrently with goroutines
2. **Batch LLM calls** — group agents by region and send concurrent requests
3. **Reduce redundant ticks** — agents re-analyzing same files each tick waste LLM calls; cache file reads across ticks

### Quality
1. **Finding deduplication** — hash or embed findings to suppress near-duplicates
2. **Cross-agent memory** — let agents in same tissue share short-term memory
3. **Adaptive spawn** — don't spawn new agents in regions already saturated with specialists

### Coverage
1. **Migration** — agents in low-signal regions should migrate to high-signal neighbors before dying
2. **Lower apoptosis threshold** — keep agents alive longer in early ticks to cover more ground
