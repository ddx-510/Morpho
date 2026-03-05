package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/engine"
	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/tool"
)

func main() {
	configPath := flag.String("config", "morpho.json", "Path to config file")
	workDir := flag.String("workdir", ".", "Workspace directory for agent tools")
	flag.Parse()

	fmt.Println("Morphogenetic Agent Architecture")
	fmt.Println("================================")

	// Load configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Build LLM provider from config.
	provider, err := cfg.BuildProvider()
	if err != nil {
		log.Fatalf("provider: %v", err)
	}
	fmt.Printf("Provider: %s\n", provider.Name())

	// Initialize tools.
	absWorkDir := *workDir
	if absWorkDir == "." {
		absWorkDir, _ = os.Getwd()
	}
	tools := tool.DefaultRegistry(absWorkDir)
	fmt.Printf("Tools: %d registered (read_file, grep, patch_file, shell, list_files)\n", len(tools.All()))

	// Initialize long-term memory.
	longMem := memory.NewLongTerm(cfg.Memory.LongTermPath)
	fmt.Printf("Memory: long-term at %s (%d prior entries)\n", cfg.Memory.LongTermPath, longMem.Count())

	// Create the gradient field representing code regions.
	f := field.New()

	// Auth module: high bug density + security concerns.
	f.AddPoint(&field.Point{
		ID:    "auth",
		Links: []string{"api", "db"},
		Signals: map[field.Signal]float64{
			field.BugDensity: 0.9,
			field.Security:   0.85,
			field.Complexity: 0.4,
		},
	})

	// API layer: high complexity, moderate doc debt.
	f.AddPoint(&field.Point{
		ID:    "api",
		Links: []string{"auth", "db"},
		Signals: map[field.Signal]float64{
			field.Complexity: 0.8,
			field.DocDebt:    0.6,
			field.BugDensity: 0.3,
		},
	})

	// Database layer: performance issues, low test coverage.
	f.AddPoint(&field.Point{
		ID:    "db",
		Links: []string{"auth", "api"},
		Signals: map[field.Signal]float64{
			field.Performance:  0.85,
			field.TestCoverage: 0.7,
			field.BugDensity:   0.2,
		},
	})

	// Frontend: doc debt and low test coverage.
	f.AddPoint(&field.Point{
		ID:    "frontend",
		Links: []string{"api"},
		Signals: map[field.Signal]float64{
			field.TestCoverage: 0.75,
			field.DocDebt:      0.5,
			field.Complexity:   0.3,
		},
	})

	// Build engine config.
	engCfg := engine.Config{
		MaxTicks:          cfg.Engine.MaxTicks,
		DecayRate:         cfg.Engine.DecayRate,
		DiffusionRate:     cfg.Engine.DiffusionRate,
		SpawnPerTick:      cfg.Engine.SpawnPerTick,
		ShortTermCapacity: cfg.Memory.ShortTermCapacity,
		Provider:          provider,
	}

	eng := engine.New(f, engCfg, tools, longMem)
	eng.Run()
}
