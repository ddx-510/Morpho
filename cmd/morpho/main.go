package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/engine"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/scan"
	"github.com/ddx-510/Morpho/tool"
)

func main() {
	configPath := flag.String("config", "morpho.json", "Path to config file")
	quiet := flag.Bool("q", false, "Quiet mode (only show final report)")
	flag.Parse()

	target := "."
	if flag.NArg() > 0 {
		target = flag.Arg(0)
	}

	// Resolve target to absolute path.
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

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	provider, err := cfg.BuildProvider()
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	fmt.Printf("Morpho — analyzing %s\n", target)
	fmt.Printf("Provider: %s\n", provider.Name())

	// Scan the directory to seed the gradient field.
	f, err := scan.Dir(target)
	if err != nil {
		log.Fatalf("scan: %v", err)
	}

	points := f.Points()
	if len(points) == 0 {
		fmt.Println("No code found to analyze.")
		return
	}
	fmt.Printf("Regions: %v\n", points)
	fmt.Printf("Initial field:\n%s\n", f.Snapshot())

	// Set up tools and memory.
	tools := tool.DefaultRegistry(target)
	longMem := memory.NewLongTerm(cfg.Memory.LongTermPath)

	// Build engine.
	engCfg := engine.Config{
		MaxTicks:          cfg.Engine.MaxTicks,
		DecayRate:         cfg.Engine.DecayRate,
		DiffusionRate:     cfg.Engine.DiffusionRate,
		SpawnPerTick:      cfg.Engine.SpawnPerTick,
		ShortTermCapacity: cfg.Memory.ShortTermCapacity,
		Provider:          provider,
	}

	eng := engine.New(f, engCfg, tools, longMem)
	if *quiet {
		eng.Quiet()
	}

	result := eng.Run()

	fmt.Println("\n" + engine.PrintReport(result))
}
