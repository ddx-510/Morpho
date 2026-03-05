package engine

import (
	"fmt"
	"strings"
	"time"

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

// RunResult captures what the engine produced for comparison.
type RunResult struct {
	Findings     []string          // all work log entries
	ByRole       map[string]int    // count of findings per role
	ByPoint      map[string]int    // count of findings per point
	Tissues      []string          // tissue clusters detected
	AgentsTotal  int
	AgentsDied   int
	LLMCalls     int
	Duration     time.Duration
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

	tick     int
	agentID  int
	llmCalls int
	log      func(string)
	result   RunResult
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
		result: RunResult{
			ByRole:  make(map[string]int),
			ByPoint: make(map[string]int),
		},
	}
}

// SetLogger replaces the default logger.
func (e *Engine) SetLogger(fn func(string)) {
	e.log = fn
}

// Quiet disables logging.
func (e *Engine) Quiet() {
	e.log = func(string) {}
}

// Run executes the full simulation loop and returns results.
func (e *Engine) Run() RunResult {
	start := time.Now()
	toolCount := 0
	if e.Tools != nil {
		toolCount = len(e.Tools.All())
	}
	memCount := 0
	if e.LongMem != nil {
		memCount = e.LongMem.Count()
	}
	e.log(fmt.Sprintf("=== Morphogenetic Engine (provider: %s, tools: %d, memories: %d) ===",
		e.Config.Provider.Name(), toolCount, memCount))

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

	e.result.Duration = time.Since(start)
	e.result.AgentsTotal = len(e.Agents)
	for _, a := range e.Agents {
		if a.State == agent.Apoptotic {
			e.result.AgentsDied++
		}
	}
	e.result.LLMCalls = e.llmCalls

	e.log("\n=== Complete ===")
	return e.result
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
			e.log(fmt.Sprintf("  %s -> %s", a.ID, a.Role))
		}
	}
}

func (e *Engine) stepWork() {
	for _, a := range e.Agents {
		if work := a.Work(e.Field, e.Bus); work != "" {
			e.llmCalls++
			e.result.Findings = append(e.result.Findings, work)
			e.result.ByRole[string(a.Role)]++
			e.result.ByPoint[a.PointID]++
			e.log(fmt.Sprintf("  %s", work))
		}
	}
}

func (e *Engine) stepApoptosis() {
	for _, a := range e.Agents {
		prev := a.State
		a.CheckApoptosis(e.Field)
		if a.State == agent.Apoptotic && prev != agent.Apoptotic {
			e.log(fmt.Sprintf("  apoptosis: %s (%s)", a.ID, a.Role))
		}
	}
}

func (e *Engine) stepTissue() {
	clusters := e.Detector.Detect(e.Agents)
	for _, c := range clusters {
		desc := c.String()
		e.result.Tissues = append(e.result.Tissues, desc)
		e.log(fmt.Sprintf("  tissue: %s", desc))
	}
}

func (e *Engine) stepMorphogens() {
	applied := e.Bus.Flush(e.Field)
	if len(applied) > 0 {
		e.log(fmt.Sprintf("  morphogens: %d", len(applied)))
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
	e.log(fmt.Sprintf("  alive: %d/%d", alive, len(e.Agents)))
}

// PrintReport outputs a structured summary of findings.
func PrintReport(r RunResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Findings: %d | LLM calls: %d | Agents: %d spawned, %d died | Time: %s\n",
		len(r.Findings), r.LLMCalls, r.AgentsTotal, r.AgentsDied, r.Duration.Round(time.Millisecond)))

	sb.WriteString("\nBy role:\n")
	for role, count := range r.ByRole {
		sb.WriteString(fmt.Sprintf("  %-20s %d\n", role, count))
	}
	sb.WriteString("\nBy region:\n")
	for point, count := range r.ByPoint {
		sb.WriteString(fmt.Sprintf("  %-20s %d\n", point, count))
	}

	if len(r.Tissues) > 0 {
		sb.WriteString("\nTissue clusters formed:\n")
		seen := map[string]bool{}
		for _, t := range r.Tissues {
			if !seen[t] {
				sb.WriteString(fmt.Sprintf("  %s\n", t))
				seen[t] = true
			}
		}
	}

	sb.WriteString("\nFindings:\n")
	for i, f := range r.Findings {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, truncate(f, 150)))
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
