// Engine provides the tick clock and physics for the morphogenetic
// simulation. It does NOT orchestrate agents — it runs the clock,
// applies physics (decay, diffusion), and observes what autonomous
// agents do. All behavioral decisions live in the Agent.
package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/tool"
)

// EngineConfig holds engine parameters.
type EngineConfig struct {
	MaxTicks          int
	DecayRate         float64 // base signal decay
	DiffusionRate     float64 // base signal diffusion
	ChemDecayRate     float64 // chemical decay (0 = use DecayRate)
	ChemDiffusionRate float64 // chemical diffusion (0 = use DiffusionRate)
	InitialAgents int // stem cells to seed (0 = auto)
	Provider      llm.Provider
}

// DefaultEngineConfig returns sensible defaults.
func DefaultEngineConfig(provider llm.Provider) EngineConfig {
	return EngineConfig{
		MaxTicks:          15,
		DecayRate:         0.05,
		DiffusionRate:     0.3,
		ChemDecayRate:     0.25,
		ChemDiffusionRate: 0.2,
		InitialAgents: 0,
		Provider:      provider,
	}
}

// ProgressEvent is emitted during the simulation for live UI updates.
type ProgressEvent struct {
	Kind    string // "tick", "spawn", "differentiate", "move", "work_start", "work_done", "tool_use", "tool_result", "divide", "apoptosis", "tissue", "field_state", "complete"
	Tick    int
	Agent   string
	Role    string
	Point   string
	Detail  string
	Alive   int
	Total   int
	Finding int
	Tokens  int          // total tokens used (populated for "complete")
	Regions []RegionInfo // populated for "field_state" events
}

// RegionInfo captures the state of a gradient field point for visualization.
type RegionInfo struct {
	ID        string             `json:"id"`
	Signals   map[string]float64 `json:"signals"`
	Chemicals map[string]float64 `json:"chemicals"`
	Agents    []AgentInfo        `json:"agents"`
}

// AgentInfo captures minimal agent state for visualization.
type AgentInfo struct {
	ID     string  `json:"id"`
	Role   string  `json:"role"`
	Phase  string  `json:"phase"`
	Energy float64 `json:"energy"`
}

// RunResult captures what the engine produced.
type RunResult struct {
	Findings    []string
	ByRole      map[string]int
	ByPoint     map[string]int
	Tissues     []string
	AgentsTotal int
	AgentsDied  int
	LLMCalls    int
	TotalTokens int
	Duration    time.Duration
}

// Engine runs the morphogenetic simulation.
// It is a clock + physics layer. Agents are autonomous.
type Engine struct {
	Field    *GradientField
	Agents   []*Agent
	Detector *TissueDetector
	Config   EngineConfig
	Tools  *tool.Registry
	Roles  *RoleMapping
	Skills *tool.SkillLibrary // markdown skills, nil-safe

	tick        int
	agentID     int
	log         func(string)
	progress    func(ProgressEvent)
	result      RunResult
	contextHint string
}

// NewEngine creates a new engine. Roles must be provided — they are
// always LLM-generated via domain.Auto(), never hardcoded.
func NewEngine(f *GradientField, cfg EngineConfig, tools *tool.Registry, roles *RoleMapping) *Engine {
	if cfg.MaxTicks < 8 {
		cfg.MaxTicks = 8
	}
	if cfg.ChemDecayRate == 0 {
		cfg.ChemDecayRate = cfg.DecayRate * 3
	}
	if cfg.ChemDiffusionRate == 0 {
		cfg.ChemDiffusionRate = cfg.DiffusionRate * 0.7
	}
	if roles == nil {
		roles = NewRoleMapping()
	}
	return &Engine{
		Field:    f,
		Detector: NewTissueDetector(),
		Config:   cfg,
		Tools: tools,
		Roles: roles,
		log:      func(s string) { fmt.Println(s) },
		progress: func(ProgressEvent) {},
		result: RunResult{
			ByRole:  make(map[string]int),
			ByPoint: make(map[string]int),
		},
	}
}

// SetContextHint injects additional context into agent prompts.
func (e *Engine) SetContextHint(hint string) { e.contextHint = hint }

// SetLogger replaces the default logger.
func (e *Engine) SetLogger(fn func(string)) { e.log = fn }

// SetProgress sets a callback for live progress events.
func (e *Engine) SetProgress(fn func(ProgressEvent)) { e.progress = fn }

// Quiet disables logging.
func (e *Engine) Quiet() { e.log = func(string) {} }

// ── Run ─────────────────────────────────────────────────────────────

// Run executes the morphogenetic simulation.
func (e *Engine) Run() RunResult {
	start := time.Now()
	toolCount := 0
	if e.Tools != nil {
		toolCount = len(e.Tools.All())
	}
	e.log(fmt.Sprintf("=== Morphogenetic Engine (provider: %s, tools: %d) ===", e.Config.Provider.Name(), toolCount))

	e.seedInitial()

	idleCycles := 0

	for e.tick = 1; e.tick <= e.Config.MaxTicks; e.tick++ {
		e.log(fmt.Sprintf("\n--- Cycle %d ---", e.tick))

		alive := e.aliveCount()
		findingsBefore := len(e.result.Findings)

		e.progress(ProgressEvent{
			Kind:  "tick",
			Tick:  e.tick,
			Total: e.Config.MaxTicks,
			Alive: alive,
		})

		e.stepAgents()
		e.stepRecruitment()

		e.stepPropagate() // cross-region knowledge propagation

		e.Field.Decay(e.Config.DecayRate)
		e.Field.Diffuse(e.Config.DiffusionRate)
		e.Field.DecayChemicals(e.Config.ChemDecayRate)
		e.Field.DiffuseChemicals(e.Config.ChemDiffusionRate)

		if e.tick > 2 {
			e.stepTissue()
		}
		e.emitFieldState()
		e.logStatus()

		aliveNow := e.aliveCount()
		newFindings := len(e.result.Findings) - findingsBefore

		if aliveNow == 0 {
			e.log("  convergence: all agents completed or died")
			break
		}
		// Don't count early ticks or ticks with working agents as idle.
		hasWorking := false
		for _, a := range e.Agents {
			if a.Phase == Working {
				hasWorking = true
				break
			}
		}
		if e.tick > 2 && newFindings == 0 && aliveNow <= alive && !hasWorking {
			idleCycles++
		} else if newFindings > 0 {
			idleCycles = 0
		}
		if idleCycles >= 3 {
			e.log("  convergence: stable state reached (no new findings)")
			break
		}
	}

	e.result.Duration = time.Since(start)
	e.result.AgentsTotal = len(e.Agents)
	for _, a := range e.Agents {
		if a.Phase == Apoptotic {
			e.result.AgentsDied++
		}
	}
	e.progress(ProgressEvent{
		Kind:    "complete",
		Finding: len(e.result.Findings),
		Total:   e.result.AgentsTotal,
		Tokens:  e.result.TotalTokens,
	})
	e.log("\n=== Complete ===")
	return e.result
}

func (e *Engine) aliveCount() int {
	n := 0
	for _, a := range e.Agents {
		if a.Phase != Apoptotic {
			n++
		}
	}
	return n
}

// ── Seeding ─────────────────────────────────────────────────────────

func (e *Engine) seedInitial() {
	points := e.Field.Points()
	if len(points) == 0 {
		return
	}

	type scored struct {
		id    string
		score float64
	}
	var candidates []scored
	for _, pid := range points {
		pt, ok := e.Field.FieldPoint(pid)
		if !ok {
			continue
		}
		total := 0.0
		for _, v := range pt.Signals {
			total += v
		}
		if total > 0.05 {
			candidates = append(candidates, scored{pid, total})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	limit := e.Config.InitialAgents
	if limit == 0 {
		limit = len(candidates)
		if limit > 12 {
			limit = 12
		}
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}

	for i := 0; i < limit; i++ {
		e.spawnAt(candidates[i].id, candidates[i].score)
	}
}

func (e *Engine) spawnAt(pointID string, signal float64) *Agent {
	e.agentID++
	id := fmt.Sprintf("a%d", e.agentID)
	a := New(id, pointID, e.Config.Provider, e.Tools, e.Roles)
	a.SetTick(e.tick)
	a.ContextHint = e.contextHint
	e.Agents = append(e.Agents, a)
	e.log(fmt.Sprintf("  seed %s at %s (signal=%.2f)", id, pointID, signal))
	e.progress(ProgressEvent{Kind: "spawn", Tick: e.tick, Agent: id, Point: pointID})
	return a
}

// ── Agent tick (parallel) ───────────────────────────────────────────

type agentTickResult struct {
	agent  *Agent
	result TickResult
}

func (e *Engine) stepAgents() {
	var alive []*Agent
	for _, a := range e.Agents {
		if a.Phase != Apoptotic {
			alive = append(alive, a)
		}
	}
	if len(alive) == 0 {
		return
	}

	for _, a := range alive {
		a.SetTick(e.tick)
		ag := a
		ag.ToolHook = func(agentID, toolName, args, result string) {
			e.progress(ProgressEvent{
				Kind:   "tool_use",
				Tick:   e.tick,
				Agent:  agentID,
				Point:  ag.PointID,
				Role:   string(ag.Role),
				Detail: toolName + ": " + args,
			})
			e.progress(ProgressEvent{
				Kind:   "tool_result",
				Tick:   e.tick,
				Agent:  agentID,
				Point:  ag.PointID,
				Role:   string(ag.Role),
				Detail: toolName + ": " + engTruncate(result, 200),
			})
		}
	}

	results := make([]agentTickResult, len(alive))
	var wg sync.WaitGroup
	for i, a := range alive {
		wg.Add(1)
		go func(idx int, ag *Agent) {
			defer wg.Done()
			tr := ag.Tick(e.Field)
			results[idx] = agentTickResult{agent: ag, result: tr}
		}(i, a)
	}
	wg.Wait()

	var newAgents []*Agent
	for _, atr := range results {
		a := atr.agent
		tr := atr.result

		for _, em := range tr.Emissions {
			e.Field.Secrete(em.PointID, em.Chemical, em.Amount)
		}

		if tr.Offspring != nil {
			e.agentID++
			tr.Offspring.ID = fmt.Sprintf("a%d", e.agentID)
			tr.Offspring.SetTick(e.tick)
			newAgents = append(newAgents, tr.Offspring)
		}

		for _, ev := range tr.Events {
			switch ev.Kind {
			case "differentiate":
				// Inject markdown skills for the agent's new role.
				if e.Skills != nil {
					skills := e.Skills.ForRole(ev.Detail)
					if len(skills) > 0 {
						var sb strings.Builder
						sb.WriteString("SKILLS & METHODOLOGY:\n")
						for _, s := range skills {
							sb.WriteString("### " + s.Name + "\n" + s.Content + "\n\n")
						}
						a.ContextHint += "\n\n" + sb.String()
					}
				}
				e.log(fmt.Sprintf("  %s -> %s at %s", a.ID, ev.Detail, a.PointID))
				e.progress(ProgressEvent{Kind: "differentiate", Tick: e.tick, Agent: a.ID, Role: ev.Detail, Point: a.PointID})
			case "move":
				e.log(fmt.Sprintf("  %s moved: %s", a.ID, ev.Detail))
				e.progress(ProgressEvent{Kind: "move", Tick: e.tick, Agent: a.ID, Role: string(a.Role), Point: a.PointID, Detail: ev.Detail})
			case "work":
				e.result.LLMCalls++
				e.progress(ProgressEvent{Kind: "work_done", Tick: e.tick, Agent: a.ID, Role: string(a.Role), Point: a.PointID, Detail: ev.Detail, Tokens: tr.Tokens})
			case "divide":
				e.log(fmt.Sprintf("  %s divided: %s", a.ID, ev.Detail))
				e.progress(ProgressEvent{Kind: "spawn", Tick: e.tick, Agent: tr.Offspring.ID, Point: a.PointID, Detail: "mitosis from " + a.ID})
			case "die":
				e.log(fmt.Sprintf("  apoptosis: %s (%s) — %s", a.ID, a.Role, ev.Detail))
				e.progress(ProgressEvent{Kind: "apoptosis", Tick: e.tick, Agent: a.ID, Role: string(a.Role), Point: a.PointID, Detail: ev.Detail})
			}
		}

		if tr.Tokens > 0 {
			e.result.TotalTokens += tr.Tokens
		}

		if tr.Work != "" {
			// Split agent output into individual findings (bullet points).
			individual := splitFindings(tr.Work)
			e.result.Findings = append(e.result.Findings, individual...)
			e.result.ByRole[string(a.Role)] += len(individual)
			e.result.ByPoint[a.PointID] += len(individual)
			e.log(fmt.Sprintf("  %s (%d findings)", engTruncate(tr.Work, 100), len(individual)))
		}
	}

	e.Agents = append(e.Agents, newAgents...)
}

// ── Nutrient recruitment ────────────────────────────────────────────

func (e *Engine) stepRecruitment() {
	// Build index of points with alive agents (avoids O(P*A) scan).
	occupied := make(map[string]bool, len(e.Agents))
	for _, a := range e.Agents {
		if a.Phase != Apoptotic {
			occupied[a.PointID] = true
		}
	}

	for _, pid := range e.Field.Points() {
		nutrient := e.Field.Sense(pid, Nutrient)
		if nutrient < 0.3 {
			continue
		}
		if !occupied[pid] {
			pt, ok := e.Field.FieldPoint(pid)
			if !ok {
				continue
			}
			totalSignal := 0.0
			for _, v := range pt.Signals {
				totalSignal += v
			}
			if totalSignal > 0.05 {
				a := e.spawnAt(pid, totalSignal)
				_ = a
				e.log(fmt.Sprintf("  recruited stem cell at %s (nutrient=%.2f)", pid, nutrient))
			}
		}
	}
}

// ── Cross-region knowledge propagation ──────────────────────────────

// stepPropagate spreads finding digests to linked regions.
// This is the key morphogenetic behavior: knowledge doesn't stay local,
// it diffuses through the field topology (import-based links).
func (e *Engine) stepPropagate() {
	// Track what we've already propagated to avoid duplicates.
	type propKey struct{ finding, target string }
	propagated := map[propKey]bool{}

	for _, pid := range e.Field.Points() {
		pt, ok := e.Field.FieldPoint(pid)
		if !ok || len(pt.Findings) == 0 || len(pt.Links) == 0 {
			continue
		}
		// Only propagate findings from this tick (avoid re-propagating old ones).
		// We tag propagated findings with [from:region] so we can detect them.
		for _, finding := range pt.Findings {
			if strings.HasPrefix(finding, "[from:") || strings.HasPrefix(finding, "[prior]") {
				continue // already propagated or from prior session
			}
			// Create a short digest (first 150 chars).
			digest := finding
			if len(digest) > 150 {
				digest = digest[:150] + "..."
			}
			tagged := fmt.Sprintf("[from:%s] %s", pid, digest)
			for _, linkID := range pt.Links {
				key := propKey{finding: digest, target: linkID}
				if propagated[key] {
					continue
				}
				propagated[key] = true
				e.Field.DepositFinding(linkID, tagged)
			}
		}
	}
}

// ── Tissue detection ────────────────────────────────────────────────

func (e *Engine) stepTissue() {
	clusters := e.Detector.Detect(e.Agents)
	for _, c := range clusters {
		desc := c.String()
		e.result.Tissues = append(e.result.Tissues, desc)
		e.log(fmt.Sprintf("  tissue: %s", desc))

		diversity := float64(len(c.Roles)) / float64(len(c.Agents))
		for _, a := range c.Agents {
			a.Energy += 0.05 * diversity
			if a.Energy > 1.0 {
				a.Energy = 1.0
			}
		}

		for role, count := range c.Roles {
			if count >= 2 {
				e.Field.Secrete(c.PointID, Keyed(Saturation, string(role)), float64(count)*0.1)
			}
		}
		if len(c.Roles) >= 2 {
			e.Field.Secrete(c.PointID, Finding, diversity*0.15)
		}
	}
}

// ── Field state snapshot ─────────────────────────────────────────────

func (e *Engine) emitFieldState() {
	var regions []RegionInfo
	for _, pid := range e.Field.Points() {
		pt, ok := e.Field.FieldPoint(pid)
		if !ok {
			continue
		}
		ri := RegionInfo{
			ID:        pid,
			Signals:   make(map[string]float64),
			Chemicals: make(map[string]float64),
		}
		for sig, val := range pt.Signals {
			if val > 0.01 {
				ri.Signals[string(sig)] = val
			}
		}
		for chem, val := range pt.Chemicals {
			if val > 0.01 {
				ri.Chemicals[chem] = val
			}
		}
		for _, a := range e.Agents {
			if a.PointID == pid && a.Phase != Apoptotic {
				ri.Agents = append(ri.Agents, AgentInfo{
					ID:     a.ID,
					Role:   string(a.Role),
					Phase:  a.Phase.String(),
					Energy: a.Energy,
				})
			}
		}
		regions = append(regions, ri)
	}
	e.progress(ProgressEvent{Kind: "field_state", Tick: e.tick, Regions: regions})
}

// ── Status ──────────────────────────────────────────────────────────

func (e *Engine) logStatus() {
	alive := 0
	byPhase := make(map[string]int)
	for _, a := range e.Agents {
		if a.Phase != Apoptotic {
			alive++
		}
		byPhase[a.Phase.String()]++
	}
	e.log(fmt.Sprintf("  population: %d alive / %d total  phases: %v", alive, len(e.Agents), byPhase))
	e.log(fmt.Sprintf("  field: %s", strings.TrimSpace(e.Field.Snapshot())))
}

// ── Report ──────────────────────────────────────────────────────────

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
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, engTruncate(f, 150)))
	}
	return sb.String()
}

func engTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// splitFindings splits an agent's work output into individual findings.
// Looks for numbered items (1. 2. 3.) or bullet points (- *).
func splitFindings(work string) []string {
	var findings []string
	for _, line := range strings.Split(work, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Numbered findings: "1. ...", "2. ..."
		if len(line) > 2 && line[0] >= '1' && line[0] <= '9' && (line[1] == '.' || (line[1] >= '0' && line[1] <= '9' && len(line) > 3 && line[2] == '.')) {
			findings = append(findings, line)
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			findings = append(findings, line)
		}
	}
	if len(findings) == 0 && len(work) > 20 {
		// No structured findings — count the whole output as one.
		findings = append(findings, work)
	}
	return findings
}
