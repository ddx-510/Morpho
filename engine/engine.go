package engine

import (
	"fmt"
	"strings"
	"sync"
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

// ProgressEvent is emitted during the simulation for live UI updates.
type ProgressEvent struct {
	Kind    string // "tick", "spawn", "differentiate", "work_start", "work_done", "apoptosis", "tissue", "complete"
	Tick    int
	Agent   string
	Role    string
	Point   string
	Detail  string
	Alive   int
	Total   int
	Finding int // finding count so far
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

	tick       int
	agentID    int
	llmCalls   int
	log        func(string)
	progress   func(ProgressEvent)
	result     RunResult
	coveredBy  map[string]map[string]bool // pointID -> set of roles already covering it
}

// New creates a new engine.
func New(f *field.GradientField, cfg Config, tools *tool.Registry, longMem *memory.LongTerm) *Engine {
	return &Engine{
		Field:     f,
		Bus:       morphogen.NewBus(),
		Detector:  tissue.NewDetector(),
		Config:    cfg,
		Tools:     tools,
		LongMem:   longMem,
		log:       func(s string) { fmt.Println(s) },
		progress:  func(ProgressEvent) {},
		coveredBy: make(map[string]map[string]bool),
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

// SetProgress sets a callback for live progress events.
func (e *Engine) SetProgress(fn func(ProgressEvent)) {
	e.progress = fn
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
		e.progress(ProgressEvent{Kind: "tick", Tick: e.tick, Total: e.Config.MaxTicks})
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

	e.progress(ProgressEvent{
		Kind:    "complete",
		Finding: len(e.result.Findings),
		Total:   e.result.AgentsTotal,
	})
	e.log("\n=== Complete ===")
	return e.result
}

func (e *Engine) stepSpawn() {
	points := e.Field.Points()
	if len(points) == 0 {
		return
	}

	// Find points that still need coverage — prefer regions with high signal
	// and no existing alive specialist for that signal's role.
	type candidate struct {
		pointID string
		signal  float64
	}
	var candidates []candidate
	for _, pid := range points {
		pt, ok := e.Field.Point(pid)
		if !ok {
			continue
		}
		// Check if there's a high signal without an active agent covering it.
		for sig, val := range pt.Signals {
			if val < 0.1 {
				continue
			}
			role := agent.Undifferentiated
			for s, r := range map[field.Signal]agent.Role{
				field.BugDensity:   agent.BugHunter,
				field.TestCoverage: agent.TestWriter,
				field.Security:     agent.SecurityAuditor,
				field.Complexity:   agent.Refactorer,
				field.DocDebt:      agent.Documenter,
				field.Performance:  agent.Optimizer,
			} {
				if s == sig {
					role = r
					break
				}
			}
			if role == agent.Undifferentiated {
				continue
			}
			// Skip if this role is already active at this point.
			if e.coveredBy[pid] != nil && e.coveredBy[pid][string(role)] {
				continue
			}
			candidates = append(candidates, candidate{pointID: pid, signal: val})
			break // one candidate per point
		}
	}

	// If no uncovered candidates, fall back to round-robin.
	spawned := 0
	for _, c := range candidates {
		if spawned >= e.Config.SpawnPerTick {
			break
		}
		e.agentID++
		id := fmt.Sprintf("a%d", e.agentID)
		a := agent.New(id, c.pointID, e.Config.Provider, e.Tools, e.LongMem, e.Config.ShortTermCapacity)
		a.SetTick(e.tick)
		e.Agents = append(e.Agents, a)
		e.log(fmt.Sprintf("  spawn %s at %s (signal=%.2f)", id, c.pointID, c.signal))
		e.progress(ProgressEvent{Kind: "spawn", Tick: e.tick, Agent: id, Point: c.pointID})
		spawned++
	}

	// Fill remaining slots with round-robin if needed.
	for spawned < e.Config.SpawnPerTick {
		pointID := points[e.agentID%len(points)]
		e.agentID++
		id := fmt.Sprintf("a%d", e.agentID)
		a := agent.New(id, pointID, e.Config.Provider, e.Tools, e.LongMem, e.Config.ShortTermCapacity)
		a.SetTick(e.tick)
		e.Agents = append(e.Agents, a)
		e.log(fmt.Sprintf("  spawn %s at %s", id, pointID))
		e.progress(ProgressEvent{Kind: "spawn", Tick: e.tick, Agent: id, Point: pointID})
		spawned++
	}
}

func (e *Engine) stepDifferentiate() {
	for _, a := range e.Agents {
		a.SetTick(e.tick)
		prevRole := a.Role
		a.Differentiate(e.Field)
		if a.Role != prevRole {
			e.log(fmt.Sprintf("  %s -> %s", a.ID, a.Role))
			e.progress(ProgressEvent{Kind: "differentiate", Tick: e.tick, Agent: a.ID, Role: string(a.Role), Point: a.PointID})
			// Track coverage.
			if e.coveredBy[a.PointID] == nil {
				e.coveredBy[a.PointID] = make(map[string]bool)
			}
			e.coveredBy[a.PointID][string(a.Role)] = true
		}
	}
}

type workResult struct {
	agent *agent.Agent
	work  string
}

func (e *Engine) stepWork() {
	// Collect agents that will work this tick.
	// Skip agents that would duplicate work already done by same role at same point.
	seen := make(map[string]bool) // "point:role" -> already queued this tick
	var working []*agent.Agent
	for _, a := range e.Agents {
		if a.State != agent.Alive || a.Role == agent.Undifferentiated {
			continue
		}
		key := a.PointID + ":" + string(a.Role)
		if seen[key] {
			a.IdleTicks++ // skip redundant work
			e.log(fmt.Sprintf("  skip %s (duplicate %s@%s)", a.ID, a.Role, a.PointID))
			continue
		}
		seen[key] = true
		working = append(working, a)
	}
	if len(working) == 0 {
		return
	}

	e.progress(ProgressEvent{Kind: "work_start", Tick: e.tick, Total: len(working)})

	// Run all agent work in parallel — each agent reads its own region
	// and makes one LLM call, so they are independent.
	results := make([]workResult, len(working))
	var wg sync.WaitGroup
	var doneCount int64
	var mu sync.Mutex
	for i, a := range working {
		wg.Add(1)
		go func(idx int, ag *agent.Agent) {
			defer wg.Done()
			work := ag.Work(e.Field, e.Bus)
			results[idx] = workResult{agent: ag, work: work}
			mu.Lock()
			doneCount++
			done := int(doneCount)
			mu.Unlock()
			e.progress(ProgressEvent{
				Kind:  "work_done",
				Tick:  e.tick,
				Agent: ag.ID,
				Role:  string(ag.Role),
				Point: ag.PointID,
				Alive: done,
				Total: len(working),
			})
		}(i, a)
	}
	wg.Wait()

	// Collect results sequentially to avoid races on engine state.
	for _, r := range results {
		if r.work != "" {
			e.llmCalls++
			e.result.Findings = append(e.result.Findings, r.work)
			e.result.ByRole[string(r.agent.Role)]++
			e.result.ByPoint[r.agent.PointID]++
			e.log(fmt.Sprintf("  %s", r.work))
		}
	}
}

func (e *Engine) stepApoptosis() {
	for _, a := range e.Agents {
		prev := a.State
		a.CheckApoptosis(e.Field)
		if a.State == agent.Apoptotic && prev != agent.Apoptotic {
			e.log(fmt.Sprintf("  apoptosis: %s (%s)", a.ID, a.Role))
			e.progress(ProgressEvent{Kind: "apoptosis", Tick: e.tick, Agent: a.ID, Role: string(a.Role), Point: a.PointID})
			// Remove from coverage so the role can be re-filled.
			if e.coveredBy[a.PointID] != nil {
				delete(e.coveredBy[a.PointID], string(a.Role))
			}
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
