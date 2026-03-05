package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/domain"
	"github.com/ddx-510/Morpho/engine"
	"github.com/ddx-510/Morpho/memory"
)

// ANSI colors
const (
	reset   = "\033[0m"
	dim     = "\033[2m"
	bold    = "\033[1m"
	cyan    = "\033[36m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	red     = "\033[31m"
	magenta = "\033[35m"
	blue    = "\033[34m"
)

var roleColor = map[string]string{
	"bug_hunter":       red,
	"test_writer":      green,
	"security_auditor": yellow,
	"refactorer":       cyan,
	"documenter":       blue,
	"optimizer":        magenta,
}

func main() {
	configPath := flag.String("config", "morpho.json", "Path to config file")
	verbose := flag.Bool("v", false, "Verbose mode (show all engine logs)")
	domainName := flag.String("domain", "code_review", "Domain: code_review, research, writing_review, data_analysis, or free-text task description")
	flag.Parse()

	target := "."
	if flag.NArg() > 0 {
		target = flag.Arg(0)
	}
	if target == "." {
		target, _ = os.Getwd()
	}

	info, err := os.Stat(target)
	if err != nil {
		log.Fatalf("cannot access %s: %v", target, err)
	}
	if !info.IsDir() {
		log.Fatalf("%s is not a directory", target)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	provider, err := cfg.BuildProvider()
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	// Resolve domain — built-in name or auto-generate from task description.
	var dom *domain.Domain
	if d, ok := domain.Get(*domainName); ok {
		dom = d
	} else {
		// Treat the domain flag as a free-text task description.
		fmt.Printf("%s%s MORPHO %s Generating domain from task...%s\n", bold, cyan, dim, reset)
		fmt.Printf("%s task: %s%s\n", dim, *domainName, reset)
		d, err := domain.Auto(provider, *domainName, target)
		if err != nil {
			log.Fatalf("auto domain: %v", err)
		}
		dom = d
		fmt.Printf("%s generated domain: %s%s%s — %s\n", dim, bold, dom.Name, reset, dom.Description)
		fmt.Printf("%s signals: ", dim)
		for i, s := range dom.Signals {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(string(s.Name))
		}
		fmt.Printf("%s\n", reset)
		fmt.Printf("%s roles: ", dim)
		for i, r := range dom.Roles {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(r.Name)
		}
		fmt.Printf("%s\n\n", reset)
	}

	// Header
	fmt.Printf("\n%s%s MORPHO %s %s%s\n", bold, cyan, dim, dom.Description, reset)
	fmt.Printf("%s target: %s%s\n", dim, target, reset)
	fmt.Printf("%s provider: %s%s\n", dim, provider.Name(), reset)
	fmt.Printf("%s domain: %s%s\n\n", dim, dom.Name, reset)

	// Seed the gradient field using the domain's seeder.
	f, err := dom.Seeder(target)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	points := f.Points()
	if len(points) == 0 {
		fmt.Println("No content found to analyze.")
		return
	}

	fmt.Printf("%s scanning...%s found %d regions: %s\n\n", dim, reset, len(points), strings.Join(points, ", "))

	// Show gradient field
	fmt.Printf("%s%s GRADIENT FIELD %s\n", bold, yellow, reset)
	snapshot := f.Snapshot()
	for _, line := range strings.Split(strings.TrimSpace(snapshot), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()

	// Build role mapping from domain.
	roles := agent.NewRoleMapping()
	for _, r := range dom.Roles {
		roles.SignalToRole[r.Signal] = r.Name
		roles.RoleToSignal[r.Name] = r.Signal
		roles.RolePrompts[r.Name] = r.Prompt
	}

	// Set up tools and memory.
	var tools = dom.ToolBuilder(target)
	longMem := memory.NewLongTerm(cfg.Memory.LongTermPath)

	engCfg := engine.Config{
		MaxTicks:          cfg.Engine.MaxTicks,
		DecayRate:         cfg.Engine.DecayRate,
		DiffusionRate:     cfg.Engine.DiffusionRate,
		SpawnPerTick:      cfg.Engine.SpawnPerTick,
		ShortTermCapacity: cfg.Memory.ShortTermCapacity,
		Provider:          provider,
	}

	eng := engine.New(f, engCfg, tools, longMem)
	eng.SetRoles(roles)
	if !*verbose {
		eng.Quiet()
	}

	start := time.Now()

	// Progress handler — live output like OpenClaw/Nanobot
	eng.SetProgress(func(ev engine.ProgressEvent) {
		switch ev.Kind {
		case "tick":
			bar := progressBar(ev.Tick, ev.Total, 30)
			fmt.Printf("\n%s%s TICK %d/%d %s %s\n", bold, magenta, ev.Tick, ev.Total, bar, reset)

		case "spawn":
			fmt.Printf("  %s+%s %s%s%s spawned at %s%s%s\n",
				green, reset, bold, ev.Agent, reset, cyan, ev.Point, reset)

		case "differentiate":
			color := roleColor[ev.Role]
			if color == "" {
				color = dim
			}
			fmt.Printf("  %s~%s %s %s-> %s%s%s%s at %s\n",
				yellow, reset, ev.Agent, dim, reset, color, ev.Role, reset, ev.Point)

		case "work_start":
			fmt.Printf("  %s...%s %d agents analyzing %s(parallel)%s\n",
				dim, reset, ev.Total, dim, reset)

		case "work_done":
			color := roleColor[ev.Role]
			if color == "" {
				color = dim
			}
			elapsed := time.Since(start).Round(time.Millisecond)
			fmt.Printf("  %s✓%s %s%s%s %s%s%s@%s %s[%d/%d %s]%s\n",
				green, reset, color, ev.Role, reset, bold, ev.Agent, reset, ev.Point,
				dim, ev.Alive, ev.Total, elapsed, reset)

		case "apoptosis":
			fmt.Printf("  %s✗%s %s %s(%s)%s died at %s\n",
				red, reset, ev.Agent, dim, ev.Role, reset, ev.Point)

		case "complete":
			elapsed := time.Since(start).Round(time.Millisecond)
			fmt.Printf("\n%s%s COMPLETE %s %d findings in %s%s\n",
				bold, green, reset, ev.Finding, elapsed, reset)
		}
	})

	result := eng.Run()

	// Final report
	fmt.Printf("\n%s%s REPORT %s\n", bold, cyan, reset)
	fmt.Println(engine.PrintReport(result))
}

func progressBar(current, total, width int) string {
	if total == 0 {
		return ""
	}
	filled := (current * width) / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
