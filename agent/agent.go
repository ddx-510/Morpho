// Package agent implements the morphogenetic simulation: autonomous
// agents that sense chemical gradients, differentiate via lateral
// inhibition, migrate via chemotaxis, work, divide, and die.
// Also includes the gradient field, chemical system, tissue detection,
// and the engine (tick clock + physics).
package agent

import (
	"encoding/json"
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
	lastTokens int    // tokens from most recent LLM call
	Emerged    bool   // true after agent has self-refined its role
	Focus      string // emergent focus area discovered during work
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
		val += linked.Chemicals[Discovery] * 0.4 // attracted to discoveries

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

	// Discovery-driven signal amplification: findings boost local signals,
	// attracting more agents to regions where important issues are found.
	severity := classifyFindingSeverity(work)
	if severity > 0.3 {
		// Amplify base signal — this region needs more attention.
		f.AddSignal(a.PointID, targetSig, severity*0.2)
		// Emit discovery chemical — attracts agents from neighboring regions.
		result.Emissions = append(result.Emissions, ChemEmission{
			PointID:  a.PointID,
			Chemical: Discovery,
			Amount:   severity * 0.4,
		})
	}

	if severity > 0.7 || localVal > 0.7 {
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

// ── Graded effort ────────────────────────────────────────────────

// effortBudget maps signal strength to max tool-call iterations.
// Low signal = quick scan, high signal = deep investigation.
func effortBudget(signalStrength float64) int {
	switch {
	case signalStrength >= 0.6:
		return 6 // deep investigation (code already pre-loaded)
	case signalStrength >= 0.3:
		return 4 // moderate
	default:
		return 2 // quick scan
	}
}

// classifyFindingSeverity estimates severity from agent output text (no LLM call).
func classifyFindingSeverity(work string) float64 {
	upper := strings.ToUpper(work)
	var sev float64
	switch {
	case strings.Contains(upper, "CRITICAL"):
		sev = 0.9
	case strings.Contains(upper, "HIGH"):
		sev = 0.7
	case strings.Contains(upper, "MEDIUM"):
		sev = 0.4
	case strings.Contains(upper, "LOW"):
		sev = 0.2
	}
	// More findings = higher severity
	count := strings.Count(work, "\n")
	countSev := math.Min(float64(count)*0.05, 0.8)
	if countSev > sev {
		sev = countSev
	}
	return sev
}

// ── Work (multi-turn tool-calling with stigmergic context) ──────

func (a *Agent) doWork(pt Point, targetSig Signal, localVal float64) string {
	budget := effortBudget(localVal)

	// Build context from existing findings (stigmergy). Cap at 10 to limit tokens.
	existingFindings := ""
	if len(pt.Findings) > 0 {
		var fb strings.Builder
		fb.WriteString("EXISTING FINDINGS (do NOT repeat):\n")
		cap := len(pt.Findings)
		start := 0
		if cap > 10 {
			start = cap - 10
			cap = 10
		}
		for _, f := range pt.Findings[start:] {
			fb.WriteString("- " + truncateStr(f, 150) + "\n")
		}
		existingFindings = fb.String()
	}

	// Build system prompt. Pass empty code to template — we add content once below.
	var systemPrompt string
	if tmpl, ok := a.roles.RolePrompts[a.Role]; ok && tmpl != "" {
		systemPrompt = expandTemplate(tmpl, a.PointID, localVal, "")
	} else {
		systemPrompt = fmt.Sprintf("You are a %s specialist analyzing the \"%s\" region. Signal %s = %.2f.",
			a.Role, a.PointID, targetSig, localVal)
	}

	// Include pre-loaded content ONCE (was duplicated via {{.Code}} + explicit append).
	if pt.Content != "" {
		systemPrompt += "\n\nSOURCE CODE:\n" + truncateStr(pt.Content, 12000)
	}

	// Emergent focus: on first work cycle, agent refines its specialization.
	emergentInstruction := ""
	if !a.Emerged {
		emergentInstruction = `
8. IMPORTANT — EMERGENT FOCUS: This is your first investigation of this region.
   As you explore, identify what specific aspect needs the most attention.
   Start your response with [FOCUS: specific_area] (e.g. [FOCUS: SQL injection in query builders]).
   This helps the swarm understand what you discovered and allocate effort.`
	} else if a.Focus != "" {
		systemPrompt += fmt.Sprintf("\n\nYour discovered focus area: %s — dig deeper into this.", a.Focus)
	}

	systemPrompt += fmt.Sprintf(`

INSTRUCTIONS:
1. Analyze the source code above directly. Output a numbered findings list.
2. Each finding: severity (Critical/High/Medium/Low), file:line, description.
3. Do NOT repeat existing findings. Do NOT refuse.%s`, emergentInstruction)

	if existingFindings != "" {
		systemPrompt += "\n\n" + existingFindings
	}
	if a.ContextHint != "" {
		systemPrompt += "\n\n" + a.ContextHint
	}

	userPrompt := fmt.Sprintf("Investigate the \"%s\" region for %s issues. Produce a numbered findings list.", a.PointID, a.Role)

	// If content is pre-loaded (>500 chars), use a single LLM call — no tool overhead.
	// Tools are only valuable when the agent needs to explore beyond what's pre-loaded.
	if len(pt.Content) > 500 {
		return a.doWorkSingleCall(systemPrompt, userPrompt)
	}

	// No pre-loaded content — use multi-turn tool loop to explore.
	if a.tools == nil || len(a.tools.All()) == 0 {
		return a.doWorkSingleCall(systemPrompt, userPrompt)
	}

	// Multi-turn tool loop — the agent explores its region with tools.
	messages := []llm.ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
	toolSpecs := a.tools.ToLLMSpecs()
	totalTokens := 0
	callCounter := 0

	for turn := 0; turn < budget; turn++ {
		resp := a.provider.Generate(llm.Request{
			Messages: messages,
			Tools:    toolSpecs,
		})
		totalTokens += resp.Tokens.Total

		if resp.Err != nil {
			break
		}
		if len(resp.ToolCalls) == 0 {
			// Final response — this is the finding.
			a.lastTokens = totalTokens
			if resp.Content == "" {
				return ""
			}
			a.extractFocus(resp.Content)
			work := fmt.Sprintf("[%s@%s] %s: %s", a.ID, a.PointID, a.Role, resp.Content)
			a.WorkLog = append(a.WorkLog, work)
			return work
		}

		// Append assistant message with tool calls.
		messages = append(messages, llm.ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call.
		for _, call := range resp.ToolCalls {
			// Handle garbled args from model.
			if parseErr, ok := call.Args["__parse_error"]; ok && parseErr == true {
				rawArgs, _ := call.Args["__raw"].(string)
				callCounter++
				callID := call.ID
				if callID == "" {
					callID = fmt.Sprintf("call_%d", callCounter)
				}
				messages = append(messages, llm.ChatMessage{
					Role:       "tool",
					Content:    fmt.Sprintf("error: invalid JSON arguments: %s — retry with valid JSON", truncateStr(rawArgs, 100)),
					ToolCallID: callID,
				})
				continue
			}

			result := a.tools.ExecuteCall(call)
			resultStr := result.Output
			if result.Err != nil {
				resultStr = "error: " + result.Err.Error()
			}
			if len(resultStr) > 2000 {
				resultStr = resultStr[:2000] + "\n...(truncated)"
			}

			// Emit tool progress via hook.
			if a.ToolHook != nil {
				argsJSON, _ := json.Marshal(call.Args)
				a.ToolHook(a.ID, call.Name, string(argsJSON), resultStr)
			}

			callCounter++
			callID := call.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%d", callCounter)
			}
			messages = append(messages, llm.ChatMessage{
				Role:       "tool",
				Content:    resultStr,
				ToolCallID: callID,
			})
		}
	}

	// Hit budget limit — ask for final findings without tools.
	messages = append(messages, llm.ChatMessage{
		Role:    "user",
		Content: "Budget reached. Summarize your findings now as a numbered list with severity levels.",
	})
	resp := a.provider.Generate(llm.Request{Messages: messages})
	totalTokens += resp.Tokens.Total
	a.lastTokens = totalTokens

	if resp.Err != nil || resp.Content == "" {
		return ""
	}
	work := fmt.Sprintf("[%s@%s] %s: %s", a.ID, a.PointID, a.Role, resp.Content)
	a.WorkLog = append(a.WorkLog, work)
	return work
}

// doWorkSingleCall is the fallback for domains without tools.
func (a *Agent) doWorkSingleCall(systemPrompt, userPrompt string) string {
	resp := a.provider.Generate(llm.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
	a.lastTokens = resp.Tokens.Total
	if resp.Err != nil || resp.Content == "" {
		return ""
	}
	a.extractFocus(resp.Content)
	work := fmt.Sprintf("[%s@%s] %s: %s", a.ID, a.PointID, a.Role, resp.Content)
	a.WorkLog = append(a.WorkLog, work)
	return work
}

// extractFocus parses [FOCUS: ...] from agent output and stores it.
func (a *Agent) extractFocus(content string) {
	if a.Emerged {
		return
	}
	a.Emerged = true
	if idx := strings.Index(content, "[FOCUS:"); idx >= 0 {
		end := strings.Index(content[idx:], "]")
		if end > 0 {
			a.Focus = strings.TrimSpace(content[idx+7 : idx+end])
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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
