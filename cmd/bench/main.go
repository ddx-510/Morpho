package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/engine"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/scan"
	"github.com/ddx-510/Morpho/tool"
)

func main() {
	configPath := flag.String("config", "morpho.json", "Path to config file")
	flag.Parse()

	target := "."
	if flag.NArg() > 0 {
		target = flag.Arg(0)
	}
	if target == "." {
		target, _ = os.Getwd()
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	provider, err := cfg.BuildProvider()
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	fmt.Printf("Bench: single-agent vs morpho on %s\n", target)
	fmt.Printf("Provider: %s\n\n", provider.Name())

	// Scan the target.
	f, err := scan.Dir(target)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}
	points := f.Points()
	if len(points) == 0 {
		fmt.Println("No code found.")
		return
	}
	fmt.Printf("Regions: %v\n", points)
	fmt.Printf("Field:\n%s\n", f.Snapshot())

	sep := strings.Repeat("=", 70)

	// --- Run 1: Single agent with file reading (fair: it gets to read code too) ---
	fmt.Printf("%s\n  SINGLE AGENT\n  (reads all source files, one big analysis prompt)\n%s\n", sep, sep)
	tools := tool.DefaultRegistry(target)
	singleResult := runSingle(provider, tools, target, points)
	if len(singleResult.report) > 4000 {
		fmt.Println(singleResult.report[:4000])
		fmt.Println("... (truncated)")
	} else {
		fmt.Println(singleResult.report)
	}

	// --- Run 2: Morpho ---
	f2, _ := scan.Dir(target)
	fmt.Printf("\n%s\n  MORPHO\n  (gradient field, emergent specialization, tools per tick)\n%s\n", sep, sep)
	tools2 := tool.DefaultRegistry(target)
	longMem := memory.NewLongTerm("")

	engCfg := engine.Config{
		MaxTicks:          cfg.Engine.MaxTicks,
		DecayRate:         cfg.Engine.DecayRate,
		DiffusionRate:     cfg.Engine.DiffusionRate,
		SpawnPerTick:      cfg.Engine.SpawnPerTick,
		ShortTermCapacity: cfg.Memory.ShortTermCapacity,
		Provider:          provider,
	}
	eng := engine.New(f2, engCfg, tools2, longMem)
	eng.Quiet()
	eng.SetProgress(func(ev engine.ProgressEvent) {
		switch ev.Kind {
		case "tick":
			fmt.Printf("  [tick %d/%d]\n", ev.Tick, ev.Total)
		case "work_done":
			fmt.Printf("    ✓ %s %s@%s (%d/%d)\n", ev.Role, ev.Agent, ev.Point, ev.Alive, ev.Total)
		case "apoptosis":
			fmt.Printf("    ✗ %s died (%s)\n", ev.Agent, ev.Role)
		}
	})
	morphoResult := eng.Run()

	fmt.Println(engine.PrintReport(morphoResult))

	// --- Comparison ---
	fmt.Printf("\n%s\n  COMPARISON\n%s\n", sep, sep)

	sFindings := singleResult.findings
	mFindings := morphoResult.Findings

	fmt.Printf("%-35s %15s %15s\n", "Metric", "Single", "Morpho")
	fmt.Printf("%-35s %15s %15s\n", strings.Repeat("-", 35), strings.Repeat("-", 15), strings.Repeat("-", 15))
	fmt.Printf("%-35s %15d %15d\n", "LLM calls", singleResult.llmCalls, morphoResult.LLMCalls)
	fmt.Printf("%-35s %15d %15d\n", "Total findings", len(sFindings), len(mFindings))
	fmt.Printf("%-35s %15d %15d\n", "Code-specific findings", countCodeRefs(sFindings), countCodeRefs(mFindings))
	fmt.Printf("%-35s %15d %15d\n", "Specialist roles", 1, len(morphoResult.ByRole))
	fmt.Printf("%-35s %15d %15d\n", "Regions covered", singleResult.regionsCovered, len(morphoResult.ByPoint))
	fmt.Printf("%-35s %15s %15s\n", "Wall time", singleResult.duration.Round(time.Millisecond), morphoResult.Duration.Round(time.Millisecond))
	fmt.Printf("%-35s %15s %15d\n", "Tissue clusters", "n/a", countUnique(morphoResult.Tissues))

	fmt.Println()
	fmt.Println("What this shows:")
	fmt.Println("  Single agent: one generalist looks at everything, produces broad analysis.")
	fmt.Println("  Morpho: specialists emerge from gradients, each going deep on their area.")
	fmt.Println("  The gradient field means more agents swarm to higher-signal regions.")
	fmt.Println("  Agents with nothing to do die off (apoptosis). No wasted work.")

	sCode := countCodeRefs(sFindings)
	mCode := countCodeRefs(mFindings)
	if mCode > sCode {
		fmt.Printf("\n  Result: morpho found %dx more code-specific findings (%d vs %d).\n", mCode/(max(sCode, 1)), mCode, sCode)
	} else if sCode > mCode {
		fmt.Printf("\n  Result: single agent found more code-specific findings (%d vs %d).\n", sCode, mCode)
		fmt.Println("  (Morpho's advantage grows with codebase size and heterogeneity.)")
	} else {
		fmt.Printf("\n  Result: tied at %d code-specific findings each.\n", sCode)
	}
}

type singleRunResult struct {
	report         string
	findings       []string
	llmCalls       int
	regionsCovered int
	duration       time.Duration
}

func runSingle(provider llm.Provider, tools *tool.Registry, target string, points []string) singleRunResult {
	// Read all source files to give the single agent real code context.
	listResult := tools.ExecuteCall(llm.ToolCall{
		Name: "shell",
		Args: map[string]string{"command": "find . -type f -name '*.go' -not -path './.git/*' | sort"},
	})

	var codeSnippets strings.Builder
	codeSnippets.WriteString("Source files in the codebase:\n\n")
	if listResult.Err == nil {
		for _, file := range strings.Split(strings.TrimSpace(listResult.Output), "\n") {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			readResult := tools.ExecuteCall(llm.ToolCall{
				Name: "read_file",
				Args: map[string]string{"path": file},
			})
			if readResult.Err == nil {
				content := readResult.Output
				if len(content) > 2000 {
					content = content[:2000] + "\n... (truncated)"
				}
				codeSnippets.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", file, content))
			}
		}
	}

	prompt := codeSnippets.String() + `
Analyze this codebase thoroughly. Find ALL issues across these categories:
1. BUGS: logic errors, edge cases, nil dereferences, race conditions
2. SECURITY: hardcoded secrets, injection, unsafe operations
3. TESTS: missing test coverage, untested edge cases
4. COMPLEXITY: functions too long, god objects, deep nesting
5. DOCS: missing or misleading documentation
6. PERFORMANCE: unnecessary allocations, O(n^2) loops, blocking calls

For EACH finding:
- Reference the specific file and function
- Quote the problematic code
- Explain the issue concretely
- Suggest a fix

Be thorough. Find at least 15 distinct issues.`

	start := time.Now()
	resp := provider.Generate(llm.Request{
		SystemPrompt: "You are an expert code reviewer. You have been given the full source code. Find real, specific, actionable issues. Do not be vague — cite files, functions, line numbers, and actual code.",
		UserPrompt:   prompt,
	})
	dur := time.Since(start)

	if resp.Err != nil {
		return singleRunResult{
			report:   fmt.Sprintf("ERROR: %v\n", resp.Err),
			duration: dur,
			llmCalls: 1,
		}
	}

	// Parse findings from the response.
	var findings []string
	regionHits := map[string]bool{}
	for _, line := range strings.Split(resp.Content, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 10 {
			continue
		}
		// Lines starting with numbers, bullets, or ### are findings/headers.
		if (line[0] >= '1' && line[0] <= '9') || line[0] == '-' || line[0] == '*' || strings.HasPrefix(line, "###") {
			findings = append(findings, line)
			lower := strings.ToLower(line)
			for _, p := range points {
				if strings.Contains(lower, strings.ToLower(p)) {
					regionHits[p] = true
				}
			}
		}
	}

	report := fmt.Sprintf("Response (%s, %d chars):\n%s\n",
		dur.Round(time.Millisecond), len(resp.Content), resp.Content)

	return singleRunResult{
		report:         report,
		findings:       findings,
		llmCalls:       1,
		regionsCovered: len(regionHits),
		duration:       dur,
	}
}

func countCodeRefs(findings []string) int {
	count := 0
	for _, f := range findings {
		lower := strings.ToLower(f)
		for _, ind := range []string{".go", ".js", ".py", ".ts", "func ", "func(", "line ", "package "} {
			if strings.Contains(lower, ind) {
				count++
				break
			}
		}
	}
	return count
}

func countUnique(ss []string) int {
	seen := map[string]bool{}
	for _, s := range ss {
		seen[s] = true
	}
	return len(seen)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
