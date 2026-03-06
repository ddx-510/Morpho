// Package agent implements the morphogenetic simulation: autonomous
// agents that sense chemical gradients, differentiate via lateral
// inhibition, migrate via chemotaxis, work, divide, and die.
// Also includes the gradient field, chemical system, tissue detection,
// and the engine (tick clock + physics).
package agent

import (
	"fmt"
	"math"
	"strings"

	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/tool"
)

// Role is the specialized function an agent differentiates into.
type Role = string

// Undifferentiated is the initial state before differentiation.
const Undifferentiated Role = "undifferentiated"

// Phase tracks the agent lifecycle.
type Phase int

const (
	Nascent   Phase = iota // just born, sensing environment
	Seeking                // differentiated, looking for work or migrating
	Working                // actively analyzing (LLM call)
	Resting                // post-work cooldown, deciding next move
	Apoptotic              // dying
)

func (p Phase) String() string {
	switch p {
	case Nascent:
		return "nascent"
	case Seeking:
		return "seeking"
	case Working:
		return "working"
	case Resting:
		return "resting"
	case Apoptotic:
		return "apoptotic"
	default:
		return "unknown"
	}
}

// RoleMapping defines how signals map to roles and their prompts.
type RoleMapping struct {
	SignalToRole map[Signal]string
	RoleToSignal map[string]Signal
	RolePrompts  map[string]string
}

func NewRoleMapping() *RoleMapping {
	return &RoleMapping{
		SignalToRole: make(map[Signal]string),
		RoleToSignal: make(map[string]Signal),
		RolePrompts:  make(map[string]string),
	}
}

// ── Tick result types ───────────────────────────────────────────────

// ChemEmission is a chemical an agent wants to secrete into the field.
type ChemEmission struct {
	PointID  string
	Chemical string
	Amount   float64
}

// TickEvent describes what happened during an agent's tick.
type TickEvent struct {
	Kind   string // "differentiate", "move", "work", "divide", "die", "idle"
	Detail string
}

// TickResult is what Agent.Tick() returns to the engine.
type TickResult struct {
	Events    []TickEvent
	Emissions []ChemEmission
	Offspring *Agent // non-nil if agent divided (mitosis)
	Work      string // finding text if agent worked this tick
	Tokens    int    // tokens consumed this tick
}

// ── Agent ───────────────────────────────────────────────────────────

// Agent is an autonomous morphogenetic cell that reads local gradients,
// secretes chemicals, and self-organizes with neighbors.
//
// Memory model: agents have NO personal memory. All knowledge flows through
// the field (Point.Content for pre-loaded data, Point.Findings for stigmergic
// sharing). Cross-session learning lives in the FactStore at the organism level.
type Agent struct {
	ID        string
	Role      Role
	Phase     Phase
	PointID   string
	Energy    float64
	IdleTicks int
	WorkLog   []string

	provider    llm.Provider
	tools       *tool.Registry
	roles       *RoleMapping
	tick        int
	ContextHint string // organism-level facts injected here

	// ToolHook is called whenever the agent invokes a tool during work.
	ToolHook   func(agentID, toolName, args, result string)
	lastTokens int // tokens from most recent LLM call
}

// New creates an undifferentiated agent at the given point.
func New(id, pointID string, provider llm.Provider, tools *tool.Registry, roles *RoleMapping) *Agent {
	return &Agent{
		ID:       id,
		Role:     Undifferentiated,
		Phase:    Nascent,
		PointID:  pointID,
		Energy:   1.0,
		provider: provider,
		tools:    tools,
		roles:    roles,
	}
}

// SetTick updates the agent's current tick.
func (a *Agent) SetTick(tick int) { a.tick = tick }

// ── Autonomous tick ─────────────────────────────────────────────────

// Tick is the agent's autonomous behavior loop. Called once per engine tick.
func (a *Agent) Tick(f *GradientField) TickResult {
	if a.Phase == Apoptotic {
		return TickResult{}
	}

	pt, ok := f.FieldPoint(a.PointID)
	if !ok {
		a.Phase = Apoptotic
		return TickResult{Events: []TickEvent{{Kind: "die", Detail: "point disappeared"}}}
	}

	var result TickResult

	switch a.Phase {
	case Nascent:
		a.tickNascent(f, pt, &result)
	case Seeking:
		a.tickSeeking(f, pt, &result)
	case Working:
		a.tickWorking(f, pt, &result)
	case Resting:
		a.tickResting(f, pt, &result)
	}

	a.checkDeath(pt, &result)
	a.Energy -= 0.03
	return result
}

// ── Phase behaviors ─────────────────────────────────────────────────

func (a *Agent) tickNascent(f *GradientField, pt Point, result *TickResult) {
	presenceByRole := make(map[string]float64)
	for key, conc := range pt.Chemicals {
		if strings.HasPrefix(key, Presence+":") {
			role := strings.TrimPrefix(key, Presence+":")
			presenceByRole[role] += conc
		}
	}

	var bestRole string
	var bestScore float64
	for sig, val := range pt.Signals {
		role, ok := a.roles.SignalToRole[sig]
		if !ok || val < 0.05 {
			continue
		}
		presence := presenceByRole[role]
		score := val * math.Max(0, 1.0-presence*2)
		if score > bestScore {
			bestScore = score
			bestRole = role
		}
	}

	if bestRole == "" || bestScore < 0.02 {
		a.Phase = Seeking
		a.IdleTicks++
		return
	}

	a.Role = bestRole
	// differentiated — no memory call needed, field stigmergy handles knowledge

	result.Emissions = append(result.Emissions, ChemEmission{
		PointID:  a.PointID,
		Chemical: Keyed(Presence, a.Role),
		Amount:   0.3,
	})
	result.Events = append(result.Events, TickEvent{
		Kind:   "differentiate",
		Detail: bestRole,
	})

	// Fast-track: if signal is strong enough, go straight to Working (skip Seeking tick).
	// This reduces warm-up from 3 ticks to 2.
	targetSig, ok := a.roles.RoleToSignal[a.Role]
	if ok {
		localVal := pt.Signals[targetSig]
		saturation := pt.Chemicals[Keyed(Saturation, a.Role)]
		effective := localVal - saturation*0.5
		if effective > 0.1 {
			a.Phase = Working
			a.IdleTicks = 0
			result.Emissions = append(result.Emissions, ChemEmission{
				PointID:  a.PointID,
				Chemical: Keyed(Presence, a.Role),
				Amount:   0.2,
			})
			return
		}
	}
	a.Phase = Seeking
}

func (a *Agent) tickSeeking(f *GradientField, pt Point, result *TickResult) {
	if a.Role == Undifferentiated {
		a.Phase = Nascent
		return
	}

	targetSig, ok := a.roles.RoleToSignal[a.Role]
	if !ok {
		a.IdleTicks++
		return
	}

	localVal := pt.Signals[targetSig]
	saturation := pt.Chemicals[Keyed(Saturation, a.Role)]
	effective := localVal - saturation*0.5

	if effective > 0.1 {
		a.Phase = Working
		a.IdleTicks = 0
		result.Emissions = append(result.Emissions, ChemEmission{
			PointID:  a.PointID,
			Chemical: Keyed(Presence, a.Role),
			Amount:   0.2,
		})
		return
	}

	newPoint := a.chemotaxis(f, pt, targetSig)
	if newPoint != a.PointID {
		old := a.PointID
		a.PointID = newPoint
		a.IdleTicks = 0
		result.Events = append(result.Events, TickEvent{
			Kind:   "move",
			Detail: fmt.Sprintf("%s -> %s (chemotaxis toward %s)", old, newPoint, targetSig),
		})
		return
	}

	a.IdleTicks++
}

func (a *Agent) chemotaxis(f *GradientField, pt Point, targetSig Signal) string {
	satKey := Keyed(Saturation, a.Role)
	presKey := Keyed(Presence, a.Role)

	bestPoint := a.PointID
	bestVal := pt.Signals[targetSig] - pt.Chemicals[satKey]*0.5

	for _, linkID := range pt.Links {
		linked, ok := f.FieldPoint(linkID)
		if !ok {
			continue
		}
		val := linked.Signals[targetSig]
		val -= linked.Chemicals[satKey] * 0.5
		val += linked.Chemicals[Distress] * 0.3
		val -= linked.Chemicals[presKey] * 0.3
		val += linked.Chemicals[Nutrient] * 0.2

		if val > bestVal {
			bestVal = val
			bestPoint = linkID
		}
	}
	return bestPoint
}

func (a *Agent) tickWorking(f *GradientField, pt Point, result *TickResult) {
	targetSig, ok := a.roles.RoleToSignal[a.Role]
	if !ok {
		a.Phase = Resting
		return
	}
	localVal := pt.Signals[targetSig]

	work := a.doWork(pt, targetSig, localVal)
	if work == "" {
		a.IdleTicks++
		a.Phase = Resting
		return
	}

	a.IdleTicks = 0
	result.Work = work
	result.Tokens = a.lastTokens
	result.Events = append(result.Events, TickEvent{Kind: "work", Detail: work})

	// Deposit finding into the field — stigmergic knowledge sharing.
	// Other agents at this point will see this in their next tick.
	f.DepositFinding(a.PointID, fmt.Sprintf("[%s/%s] %s", a.ID, a.Role, truncateStr(work, 300)))

	result.Emissions = append(result.Emissions, ChemEmission{
		PointID:  a.PointID,
		Chemical: Keyed(Saturation, a.Role),
		Amount:   0.4,
	})
	result.Emissions = append(result.Emissions, ChemEmission{
		PointID:  a.PointID,
		Chemical: Keyed(Presence, a.Role),
		Amount:   0.3,
	})
	if localVal > 0.4 {
		result.Emissions = append(result.Emissions, ChemEmission{
			PointID:  a.PointID,
			Chemical: Finding,
			Amount:   localVal * 0.3,
		})
	}
	if localVal > 0.7 {
		result.Emissions = append(result.Emissions, ChemEmission{
			PointID:  a.PointID,
			Chemical: Distress,
			Amount:   0.5,
		})
		if a.Energy > 0.4 {
			offspring := a.divide()
			result.Offspring = offspring
			result.Events = append(result.Events, TickEvent{
				Kind:   "divide",
				Detail: fmt.Sprintf("mitosis -> %s at %s", offspring.ID, a.PointID),
			})
		}
	}

	a.Energy -= 0.1
	a.Phase = Resting
}

func (a *Agent) tickResting(f *GradientField, pt Point, result *TickResult) {
	targetSig, ok := a.roles.RoleToSignal[a.Role]
	if !ok {
		a.Phase = Seeking
		return
	}

	localVal := pt.Signals[targetSig]
	saturation := pt.Chemicals[Keyed(Saturation, a.Role)]
	effective := localVal - saturation*0.5

	if effective > 0.2 && a.Energy > 0.2 {
		a.Phase = Working
		return
	}

	a.Phase = Seeking
	result.Emissions = append(result.Emissions, ChemEmission{
		PointID:  a.PointID,
		Chemical: Keyed(Presence, a.Role),
		Amount:   0.15,
	})
}

// ── Death ───────────────────────────────────────────────────────────

func (a *Agent) checkDeath(pt Point, result *TickResult) {
	if a.Phase == Apoptotic {
		return
	}

	die := false
	reason := ""

	if a.Energy <= 0 {
		die = true
		reason = "energy depleted"
	} else if a.IdleTicks >= 4 {
		die = true
		reason = fmt.Sprintf("idle %d ticks — no work reachable", a.IdleTicks)
	} else if a.Role != Undifferentiated {
		satKey := Keyed(Saturation, a.Role)
		if pt.Chemicals[satKey] > 0.8 {
			die = true
			reason = fmt.Sprintf("crowded out (saturation=%.2f)", pt.Chemicals[satKey])
		}
	}

	if die {
		a.Phase = Apoptotic
		result.Events = append(result.Events, TickEvent{Kind: "die", Detail: reason})
		result.Emissions = append(result.Emissions, ChemEmission{
			PointID:  a.PointID,
			Chemical: Nutrient,
			Amount:   0.4,
		})
	}
}

// ── Division (mitosis) ──────────────────────────────────────────────

func (a *Agent) divide() *Agent {
	childEnergy := a.Energy * 0.4
	a.Energy -= childEnergy

	return &Agent{
		ID:          a.ID + "c",
		Role:        Undifferentiated,
		Phase:       Nascent,
		PointID:     a.PointID,
		Energy:      childEnergy,
		provider:    a.provider,
		tools:       a.tools,
		roles:       a.roles,
		ContextHint: a.ContextHint,
	}
}

// ── Work (stigmergic analysis) ────────────────────────────────────
//
// Morphogenetic work: agents don't independently tool-loop.
// Instead, they read pre-loaded content from the field (stigmergy),
// see what other agents already found, do ONE focused LLM call,
// and deposit their findings back into the field for others to see.

func (a *Agent) doWork(pt Point, targetSig Signal, localVal float64) string {
	// 1. Get content from the field (pre-loaded at seeding time).
	regionContent := pt.Content
	if regionContent == "" {
		// Fallback: read files via tools if field has no content.
		regionContent = a.readRegionFiles()
	}
	if regionContent == "" {
		regionContent = "(no content available for this region)"
	}

	// 2. Build context from existing findings at this point (stigmergy).
	existingFindings := ""
	if len(pt.Findings) > 0 {
		var fb strings.Builder
		fb.WriteString("EXISTING FINDINGS AT THIS REGION (from other agents — do NOT repeat these):\n")
		for _, f := range pt.Findings {
			fb.WriteString("  - " + truncateStr(f, 200) + "\n")
		}
		existingFindings = fb.String()
	}

	// 3. Build system prompt.
	var systemPrompt string
	if tmpl, ok := a.roles.RolePrompts[a.Role]; ok && tmpl != "" {
		systemPrompt = expandTemplate(tmpl, a.PointID, localVal, regionContent)
	} else {
		systemPrompt = fmt.Sprintf(`You are a %s specialist analyzing the "%s" region.
Signal %s = %.2f (0=fine, 1=critical).

%s`, a.Role, a.PointID, targetSig, localVal, regionContent)
	}

	systemPrompt += `

WORK INSTRUCTIONS:
1. Analyze the code/content provided above for issues relevant to your role.
2. Output a numbered findings list with severity (Critical/High/Medium/Low).
3. Be specific — reference file names and line numbers where possible.
4. NEVER refuse or say the task is too large. Always produce findings.
5. Focus on NEW insights — do not repeat existing findings.`

	if existingFindings != "" {
		systemPrompt += "\n\n" + existingFindings
	}
	if a.ContextHint != "" {
		systemPrompt += "\n\n" + a.ContextHint
	}

	// 4. ONE focused LLM call — no multi-turn tool loop.
	resp := a.provider.Generate(llm.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   fmt.Sprintf("Analyze the \"%s\" region for %s issues. Produce a numbered findings list.", a.PointID, a.Role),
	})
	a.lastTokens = resp.Tokens.Total
	if resp.Err != nil {
		return ""
	}
	content := resp.Content
	if content == "" {
		return ""
	}

	work := fmt.Sprintf("[%s@%s] %s: %s", a.ID, a.PointID, a.Role, content)
	a.WorkLog = append(a.WorkLog, work)
	return work
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ── Helpers ─────────────────────────────────────────────────────────

func (a *Agent) readRegionFiles() string {
	if a.tools == nil {
		return ""
	}
	listTool, ok := a.tools.Get("list_files")
	if !ok {
		return ""
	}
	listResult := listTool.Execute(map[string]any{"path": a.PointID})
	if listResult.Err != nil || listResult.Output == "" || listResult.Output == "(empty directory)" {
		return ""
	}
	readTool, ok := a.tools.Get("read_file")
	if !ok {
		return ""
	}

	var buf strings.Builder
	const maxTotal = 24000
	for _, file := range strings.Split(listResult.Output, "\n") {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		result := readTool.Execute(map[string]any{"path": file})
		if result.Err == nil && result.Output != "" {
			content := result.Output
			if len(content) > 3000 {
				content = content[:3000] + "\n... (truncated)"
			}
			fmt.Fprintf(&buf, "=== %s ===\n%s\n\n", file, content)
			if buf.Len() > maxTotal {
				buf.WriteString("... (remaining files omitted)\n")
				break
			}
		}
	}
	return buf.String()
}

func expandTemplate(tmpl string, region string, value float64, code string) string {
	r := strings.NewReplacer(
		"{{.Region}}", region,
		"{{.Value}}", fmt.Sprintf("%.2f", value),
		"{{.Code}}", code,
	)
	return r.Replace(tmpl)
}

func (a *Agent) String() string {
	return fmt.Sprintf("Agent{%s role=%s phase=%s point=%s energy=%.2f idle=%d}",
		a.ID, a.Role, a.Phase, a.PointID, a.Energy, a.IdleTicks)
}
