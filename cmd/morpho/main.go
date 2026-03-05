package main

import (
	"fmt"

	"github.com/ddx-510/Morpho/engine"
	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/llm"
)

func main() {
	fmt.Println("Morphogenetic Agent Architecture — Demo")
	fmt.Println("Simulating analysis of a web app with known issues...\n")

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

	// Configure and run the engine with the demo provider.
	provider := &llm.DemoProvider{}
	cfg := engine.DefaultConfig(provider)
	cfg.MaxTicks = 8
	cfg.SpawnPerTick = 3

	eng := engine.New(f, cfg)
	eng.Run()
}
