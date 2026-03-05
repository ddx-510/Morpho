package engine

import (
	"fmt"

	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/morphogen"
	"github.com/ddx-510/Morpho/tissue"
	"github.com/ddx-510/Morpho/tool"
)

// Config holds engine parameters.
type Config struct {
	MaxTicks          int
	DecayRate         float64
	DiffusionRate     float64
	SpawnPerTick      int
	ShortTermCapacity int
	Provider          llm.Provider
}

// DefaultConfig returns sensible defaults.
func DefaultConfig(provider llm.Provider) Config {
	return Config{
		MaxTicks:          10,
		DecayRate:         0.05,
		DiffusionRate:     0.3,
		SpawnPerTick:      2,
		ShortTermCapacity: 20,
		Provider:          provider,
	}
}

// Engine runs the tick-based morphogenetic simulation.
type Engine struct {
	Field    *field.GradientField
	Bus      *morphogen.Bus
	Agents   []*agent.Agent
	Detector *tissue.Detector
	Config   Config
	Tools    *tool.Registry
	LongMem  *memory.LongTerm

	tick    int
	agentID int
	log     func(string)
}

// New creates a new engine.
func New(f *field.GradientField, cfg Config, tools *tool.Registry, longMem *memory.LongTerm) *Engine {
	return &Engine{
		Field:    f,
		Bus:      morphogen.NewBus(),
		Detector: tissue.NewDetector(),
		Config:   cfg,
		Tools:    tools,
		LongMem:  longMem,
		log:      func(s string) { fmt.Println(s) },
	}
}

// SetLogger replaces the default logger.
func (e *Engine) SetLogger(fn func(string)) {
	e.log = fn
}

// Run executes the full simulation loop.
func (e *Engine) Run() {
	e.log(fmt.Sprintf("=== Morphogenetic Engine Start (provider: %s, tools: %d, memories: %d) ===",
		e.Config.Provider.Name(), len(e.Tools.All()), e.LongMem.Count()))

	for e.tick = 1; e.tick <= e.Config.MaxTicks; e.tick++ {
		e.log(fmt.Sprintf("\n--- Tick %d ---", e.tick))
		e.stepSpawn()
		e.stepDifferentiate()
		e.stepWork()
		e.stepApoptosis()
		e.stepTissue()
		e.stepMorphogens()
		e.stepDecay()
		e.logStatus()
	}

	e.log("\n=== Simulation Complete ===")
	e.logSummary()
	e.logMemorySummary()
}

func (e *Engine) stepSpawn() {
	points := e.Field.Points()
	if len(points) == 0 {
		return
	}
	for i := 0; i < e.Config.SpawnPerTick; i++ {
		pointID := points[e.agentID%len(points)]
		e.agentID++
		id := fmt.Sprintf("a%d", e.agentID)
		a := agent.New(id, pointID, e.Config.Provider, e.Tools, e.LongMem, e.Config.ShortTermCapacity)
		a.SetTick(e.tick)
		e.Agents = append(e.Agents, a)
		e.log(fmt.Sprintf("  spawn %s at %s", id, pointID))
	}
}

func (e *Engine) stepDifferentiate() {
	for _, a := range e.Agents {
		a.SetTick(e.tick)
		prevRole := a.Role
		a.Differentiate(e.Field)
		if a.Role != prevRole {
			e.log(fmt.Sprintf("  %s differentiated -> %s", a.ID, a.Role))
		}
	}
}

func (e *Engine) stepWork() {
	for _, a := range e.Agents {
		if work := a.Work(e.Field, e.Bus); work != "" {
			e.log(fmt.Sprintf("  work: %s", work))
		}
	}
}

func (e *Engine) stepApoptosis() {
	for _, a := range e.Agents {
		prev := a.State
		a.CheckApoptosis(e.Field)
		if a.State == agent.Apoptotic && prev != agent.Apoptotic {
			e.log(fmt.Sprintf("  apoptosis: %s (%s) [%d memories]", a.ID, a.Role, len(a.ShortMem.All())))
		}
	}
}

func (e *Engine) stepTissue() {
	clusters := e.Detector.Detect(e.Agents)
	for _, c := range clusters {
		e.log(fmt.Sprintf("  tissue: %s", c))
	}
}

func (e *Engine) stepMorphogens() {
	applied := e.Bus.Flush(e.Field)
	if len(applied) > 0 {
		e.log(fmt.Sprintf("  morphogens flushed: %d signals", len(applied)))
	}
}

func (e *Engine) stepDecay() {
	e.Field.Decay(e.Config.DecayRate)
	e.Field.Diffuse(e.Config.DiffusionRate)
}

func (e *Engine) logStatus() {
	alive := 0
	for _, a := range e.Agents {
		if a.State == agent.Alive {
			alive++
		}
	}
	e.log(fmt.Sprintf("  status: %d alive, %d total", alive, len(e.Agents)))
	e.log("  field:\n" + e.Field.Snapshot())
}

func (e *Engine) logSummary() {
	e.log("\n--- Agent Summary ---")
	for _, a := range e.Agents {
		e.log(fmt.Sprintf("  %s", a))
		for _, w := range a.WorkLog {
			e.log(fmt.Sprintf("    %s", w))
		}
	}
}

func (e *Engine) logMemorySummary() {
	e.log(fmt.Sprintf("\n--- Long-Term Memory (%d entries) ---", e.LongMem.Count()))
	for _, entry := range e.LongMem.All() {
		e.log(fmt.Sprintf("  %s", entry))
	}
}
