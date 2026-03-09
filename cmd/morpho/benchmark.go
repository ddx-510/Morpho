package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/domain"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/tool"
)

// runBenchmark compares morphogenetic swarm vs naive-parallel analysis.
func runBenchmark(provider llm.Provider, cfg *config.Config, targetDir, task string) {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("  BENCHMARK: Morpho Swarm vs Naive Parallel")
	fmt.Printf("  Target: %s\n", targetDir)
	fmt.Printf("  Task: %s\n", task)
	fmt.Println("═══════════════════════════════════════════════════════════")

	// Generate domain once (shared between both approaches).
	dom, err := domain.Auto(provider, task, targetDir)
	if err != nil {
		fmt.Printf("domain error: %v\n", err)
		return
	}
	fmt.Printf("\nDomain: %s — %s\n", dom.Name, dom.Description)

	f, err := dom.Seeder(targetDir)
	if err != nil {
		fmt.Printf("seed error: %v\n", err)
		return
	}
	points := f.Points()
	if len(points) == 0 {
		fmt.Println("No content to analyze.")
		return
	}
	// Cap regions for benchmark to keep runtime manageable.
	const benchMaxRegions = 8
	if len(points) > benchMaxRegions {
		fmt.Printf("Regions: %d (capped to %d for benchmark)\n\n", len(points), benchMaxRegions)
		points = points[:benchMaxRegions]
	} else {
		fmt.Printf("Regions: %d\n\n", len(points))
	}

	// ── Run 1: Naive Parallel ───────────────────────────────────
	fmt.Println("─── Naive Parallel ────────────────────────────────────────")
	naiveResult := runNaiveParallel(provider, dom, targetDir, task, points, f)

	// Re-seed for morpho run (field state was consumed).
	f2, _ := dom.Seeder(targetDir)

	// ── Run 2: Morpho Swarm ─────────────────────────────────────
	fmt.Println("\n─── Morpho Swarm ──────────────────────────────────────────")
	morphoResult := runMorphoSwarm(provider, cfg, dom, targetDir, task, f2)

	// ── Comparison ──────────────────────────────────────────────
	fmt.Println("\n═══════════════════════════════════════════════════════════")
	fmt.Println("  COMPARISON")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("  %-25s  %10s  %10s\n", "Metric", "Naive", "Morpho")
	fmt.Printf("  %-25s  %10s  %10s\n", "─────────────────────────", "──────────", "──────────")
	fmt.Printf("  %-25s  %10d  %10d\n", "Findings", naiveResult.findings, morphoResult.findings)
	fmt.Printf("  %-25s  %10d  %10d\n", "LLM Calls", naiveResult.llmCalls, morphoResult.llmCalls)
	fmt.Printf("  %-25s  %10d  %10d\n", "Total Tokens", naiveResult.tokens, morphoResult.tokens)
	fmt.Printf("  %-25s  %10s  %10s\n", "Duration", naiveResult.duration.Round(time.Millisecond), morphoResult.duration.Round(time.Millisecond))
	fmt.Printf("  %-25s  %10d  %10d\n", "Regions Covered", naiveResult.regionsCovered, morphoResult.regionsCovered)

	if naiveResult.findings > 0 {
		fmt.Printf("  %-25s  %10s  %10s\n", "Findings/LLM Call",
			fmt.Sprintf("%.2f", float64(naiveResult.findings)/float64(max(1, naiveResult.llmCalls))),
			fmt.Sprintf("%.2f", float64(morphoResult.findings)/float64(max(1, morphoResult.llmCalls))))
	}
	if naiveResult.tokens > 0 {
		fmt.Printf("  %-25s  %10s  %10s\n", "Tokens/Finding",
			fmt.Sprintf("%d", naiveResult.tokens/max(1, naiveResult.findings)),
			fmt.Sprintf("%d", morphoResult.tokens/max(1, morphoResult.findings)))
	}

	// Unique findings comparison.
	naiveSet := map[string]bool{}
	for _, f := range naiveResult.findingTexts {
		naiveSet[f] = true
	}
	morphoSet := map[string]bool{}
	for _, f := range morphoResult.findingTexts {
		morphoSet[f] = true
	}
	morphoOnly := 0
	for f := range morphoSet {
		if !naiveSet[f] {
			morphoOnly++
		}
	}
	naiveOnly := 0
	for f := range naiveSet {
		if !morphoSet[f] {
			naiveOnly++
		}
	}
	fmt.Printf("  %-25s  %10d  %10d\n", "Unique Findings", naiveOnly, morphoOnly)
	fmt.Println("═══════════════════════════════════════════════════════════")
}

type benchResult struct {
	findings       int
	findingTexts   []string
	llmCalls       int
	tokens         int
	duration       time.Duration
	regionsCovered int
}

// runNaiveParallel: one LLM call per region, all in parallel, no communication.
func runNaiveParallel(provider llm.Provider, dom *domain.Domain, targetDir, task string, points []string, f *agent.GradientField) benchResult {
	start := time.Now()
	tools := dom.ToolBuilder(targetDir)
	toolSpecs := tools.ToLLMSpecs()

	type regionResult struct {
		findings []string
		tokens   int
		calls    int
	}

	results := make([]regionResult, len(points))
	var wg sync.WaitGroup

	for i, pid := range points {
		wg.Add(1)
		go func(idx int, pointID string) {
			defer wg.Done()
			pt, ok := f.FieldPoint(pointID)
			if !ok {
				return
			}

			content := pt.Content
			if len(content) > 12000 {
				content = content[:12000]
			}

			// Single multi-turn tool loop per region (like a basic agent).
			messages := []llm.ChatMessage{
				{Role: "system", Content: fmt.Sprintf(`Analyze the code region "%s" for the following task.
Working directory: %s

SOURCE CODE (already loaded — analyze directly):
%s

Analyze the code above directly. Only use tools for targeted follow-up (grep for patterns, read files not shown).
Report all findings as separate bullet points.
Each finding should include: what you found, where (file:line), and severity (critical/high/medium/low).`, pointID, targetDir, content)},
				{Role: "user", Content: task},
			}

			rr := regionResult{}
			for turn := 0; turn < 4; turn++ {
				resp := provider.Generate(llm.Request{
					Messages: messages,
					Tools:    toolSpecs,
				})
				rr.tokens += resp.Tokens.Total
				rr.calls++
				if resp.Err != nil || len(resp.ToolCalls) == 0 {
					if resp.Content != "" {
						rr.findings = extractFindings(resp.Content)
					}
					break
				}
				messages = append(messages, llm.ChatMessage{
					Role:      "assistant",
					Content:   resp.Content,
					ToolCalls: resp.ToolCalls,
				})
				for _, call := range resp.ToolCalls {
					result := tools.ExecuteCall(call)
					out := result.Output
					if result.Err != nil {
						out = fmt.Sprintf("error: %v", result.Err)
					}
					if len(out) > 4000 {
						out = out[:4000]
					}
					messages = append(messages, llm.ChatMessage{
						Role:       "tool",
						Content:    out,
						ToolCallID: call.ID,
					})
				}
			}
			results[idx] = rr
		}(i, pid)
	}
	wg.Wait()

	br := benchResult{duration: time.Since(start)}
	coveredRegions := map[string]bool{}
	for i, rr := range results {
		br.llmCalls += rr.calls
		br.tokens += rr.tokens
		br.findings += len(rr.findings)
		br.findingTexts = append(br.findingTexts, rr.findings...)
		if len(rr.findings) > 0 {
			coveredRegions[points[i]] = true
		}
	}
	br.regionsCovered = len(coveredRegions)

	fmt.Printf("  Findings: %d | LLM Calls: %d | Tokens: %d | Time: %s\n",
		br.findings, br.llmCalls, br.tokens, br.duration.Round(time.Millisecond))
	return br
}

// runMorphoSwarm: full morphogenetic engine.
func runMorphoSwarm(provider llm.Provider, cfg *config.Config, dom *domain.Domain, targetDir, task string, f *agent.GradientField) benchResult {
	start := time.Now()

	roles := agent.NewRoleMapping()
	for _, r := range dom.Roles {
		roles.SignalToRole[r.Signal] = r.Name
		roles.RoleToSignal[r.Name] = r.Signal
		roles.RolePrompts[r.Name] = r.Prompt
	}

	allTools := dom.ToolBuilder(targetDir)
	swarmTools := tool.ReadOnlyRegistry(allTools)

	// Use reduced ticks for benchmark (faster comparison).
	benchTicks := cfg.Engine.MaxTicks
	if benchTicks > 5 {
		benchTicks = 5
	}
	if benchTicks < 3 {
		benchTicks = 3
	}
	engCfg := agent.EngineConfig{
		MaxTicks:      benchTicks,
		DecayRate:     cfg.Engine.DecayRate,
		DiffusionRate: cfg.Engine.DiffusionRate,
		Provider:      provider,
	}

	eng := agent.NewEngine(f, engCfg, swarmTools, roles)
	eng.SetLogger(func(s string) {
		fmt.Printf("  %s\n", s)
	})
	result := eng.Run()

	br := benchResult{
		findings:       len(result.Findings),
		findingTexts:   result.Findings,
		llmCalls:       result.LLMCalls,
		tokens:         result.TotalTokens,
		duration:       time.Since(start),
		regionsCovered: len(result.ByPoint),
	}

	fmt.Printf("  Findings: %d | LLM Calls: %d | Tokens: %d | Time: %s\n",
		br.findings, br.llmCalls, br.tokens, br.duration.Round(time.Millisecond))
	fmt.Printf("  Agents: %d spawned, %d died | Tissues: %d\n",
		result.AgentsTotal, result.AgentsDied, len(result.Tissues))
	return br
}

// extractFindings splits an LLM response into individual findings.
func extractFindings(content string) []string {
	var findings []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines starting with - or * or numbered are findings.
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") ||
			(len(line) > 2 && line[0] >= '1' && line[0] <= '9' && line[1] == '.') {
			findings = append(findings, line)
		}
	}
	if len(findings) == 0 && len(content) > 20 {
		// No bullet points — treat the whole thing as one finding.
		findings = append(findings, content)
	}
	return findings
}

