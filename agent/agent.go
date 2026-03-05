package agent

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/morphogen"
	"github.com/ddx-510/Morpho/tool"
)

// Role is the specialized function an agent differentiates into.
// In a domain-agnostic system, roles are strings defined by the domain.
type Role = string

// Undifferentiated is the initial state before differentiation.
const Undifferentiated Role = "undifferentiated"

// State tracks the agent lifecycle.
type State int

const (
	Alive State = iota
	Apoptotic
)

// RoleMapping defines how signals map to roles and their prompts.
// This replaces the old hardcoded roleSignalMap.
type RoleMapping struct {
	SignalToRole map[field.Signal]string // signal -> role name
	RoleToSignal map[string]field.Signal // role name -> signal
	RolePrompts  map[string]string       // role name -> prompt template
}

// NewRoleMapping creates an empty role mapping.
func NewRoleMapping() *RoleMapping {
	return &RoleMapping{
		SignalToRole: make(map[field.Signal]string),
		RoleToSignal: make(map[string]field.Signal),
		RolePrompts:  make(map[string]string),
	}
}

// Agent is a morphogenetic agent that reads gradients and specializes.
type Agent struct {
	ID        string
	Role      Role
	State     State
	PointID   string  // current location in the field
	Energy    float64 // depletes over time; triggers apoptosis at 0
	IdleTicks int
	WorkLog   []string

	provider llm.Provider
	tools    *tool.Registry
	ShortMem *memory.ShortTerm
	longMem  *memory.LongTerm
	roles    *RoleMapping
	tick     int
}

// New creates an undifferentiated agent at the given point.
func New(id string, pointID string, provider llm.Provider, tools *tool.Registry, longMem *memory.LongTerm, stmCapacity int, roles *RoleMapping) *Agent {
	return &Agent{
		ID:       id,
		Role:     Undifferentiated,
		State:    Alive,
		PointID:  pointID,
		Energy:   1.0,
		provider: provider,
		tools:    tools,
		ShortMem: memory.NewShortTerm(stmCapacity),
		longMem:  longMem,
		roles:    roles,
	}
}

// SetTick updates the agent's current tick (set by engine each step).
func (a *Agent) SetTick(tick int) {
	a.tick = tick
}

// Differentiate reads the local gradient and specializes into the role
// matching the strongest signal. Only works if currently undifferentiated.
func (a *Agent) Differentiate(f *field.GradientField) {
	if a.Role != Undifferentiated || a.State != Alive {
		return
	}

	pt, ok := f.Point(a.PointID)
	if !ok {
		return
	}

	var bestSig field.Signal
	var bestVal float64
	for sig, val := range pt.Signals {
		if val > bestVal {
			bestVal = val
			bestSig = sig
		}
	}

	if bestVal < 0.1 {
		return
	}

	if role, ok := a.roles.SignalToRole[bestSig]; ok {
		a.Role = role
		a.remember("observation", fmt.Sprintf("differentiated into %s based on %s=%.2f at %s", role, bestSig, bestVal, a.PointID))
	}
}

// readRegionFiles reads all source files in this agent's region directly,
// returning their contents as a formatted string for the LLM prompt.
func (a *Agent) readRegionFiles() string {
	if a.tools == nil {
		return ""
	}
	listTool, ok := a.tools.Get("list_files")
	if !ok {
		return ""
	}
	listResult := listTool.Execute(map[string]string{"path": a.PointID})
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
		result := readTool.Execute(map[string]string{"path": file})
		if result.Err == nil && result.Output != "" {
			content := result.Output
			if len(content) > 3000 {
				content = content[:3000] + "\n... (truncated)"
			}
			fmt.Fprintf(&buf, "=== %s ===\n%s\n\n", file, content)
			if buf.Len() > maxTotal {
				buf.WriteString("... (remaining files omitted — context limit)\n")
				break
			}
		}
	}
	return buf.String()
}

// Work performs one tick of work based on the agent's role.
func (a *Agent) Work(f *field.GradientField, bus *morphogen.Bus) string {
	if a.State != Alive || a.Role == Undifferentiated {
		a.IdleTicks++
		return ""
	}

	pt, ok := f.Point(a.PointID)
	if !ok {
		a.IdleTicks++
		return ""
	}

	targetSig, ok := a.roles.RoleToSignal[a.Role]
	if !ok {
		a.IdleTicks++
		return ""
	}

	localVal := pt.Signals[targetSig]
	if localVal < 0.05 {
		a.IdleTicks++
		return ""
	}

	a.IdleTicks = 0

	// Pre-read all source files in this region.
	codeContext := a.readRegionFiles()
	if codeContext == "" {
		codeContext = "(no content found in this region)"
	}

	memCtx := a.ShortMem.Summary()

	// Build prompt from domain template or fallback.
	var systemPrompt string
	if tmpl, ok := a.roles.RolePrompts[a.Role]; ok && tmpl != "" {
		systemPrompt = expandTemplate(tmpl, a.PointID, localVal, codeContext)
		if memCtx != "" {
			systemPrompt += "\n\nPrevious context: " + memCtx
		}
	} else {
		// Generic fallback prompt.
		systemPrompt = fmt.Sprintf(`You are a %s specialist analyzing the "%s" region.
Signal %s = %.2f (0=fine, 1=critical).

CONTENT:
%s

Previous context: %s

INSTRUCTIONS:
- Analyze the content above. Find SPECIFIC issues related to your specialty.
- Output a numbered list of concrete findings with severity.
- Do NOT narrate. Analyze directly.`, a.Role, a.PointID, targetSig, localVal, codeContext, memCtx)
	}

	resp := a.provider.Generate(llm.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   fmt.Sprintf("Analyze the %s content above. List every %s issue you find.", a.PointID, a.Role),
	})

	if resp.Err != nil {
		a.remember("error", resp.Err.Error())
		a.IdleTicks++
		return ""
	}

	content := resp.Content
	if content == "" {
		content = "(no output)"
	}

	work := fmt.Sprintf("[%s@%s] %s: %s", a.ID, a.PointID, a.Role, content)
	a.WorkLog = append(a.WorkLog, work)
	a.remember("finding", content)

	// Promote significant findings to long-term memory.
	if localVal > 0.5 && a.longMem != nil {
		a.longMem.Store(memory.Entry{
			Tick:      a.tick,
			Timestamp: time.Now(),
			AgentID:   a.ID,
			Category:  "finding",
			Content:   fmt.Sprintf("[%s@%s] %s (signal=%.2f)", a.Role, a.PointID, content, localVal),
		})
	}

	// Emit PRESENCE.
	bus.Emit(morphogen.Signal{
		Kind:    morphogen.PRESENCE,
		Source:  a.ID,
		PointID: a.PointID,
		Channel: targetSig,
		Value:   localVal * 0.3,
	})

	a.Energy -= 0.1

	if localVal > 0.8 {
		bus.Emit(morphogen.Signal{
			Kind:    morphogen.ALARM,
			Source:  a.ID,
			PointID: a.PointID,
			Channel: targetSig,
			Value:   localVal * 0.2,
		})
	}

	f.AddSignal(a.PointID, targetSig, -localVal*0.2)

	return work
}

// expandTemplate replaces {{.Region}}, {{.Value}}, {{.Code}} in a prompt template.
func expandTemplate(tmpl string, region string, value float64, code string) string {
	r := strings.NewReplacer(
		"{{.Region}}", region,
		"{{.Value}}", fmt.Sprintf("%.2f", value),
		"{{.Code}}", code,
	)
	return r.Replace(tmpl)
}

// remember stores an observation in short-term memory.
func (a *Agent) remember(category, content string) {
	a.ShortMem.Add(memory.Entry{
		Tick:      a.tick,
		Timestamp: time.Now(),
		AgentID:   a.ID,
		Category:  category,
		Content:   content,
	})
}

// CheckApoptosis determines if this agent should die.
func (a *Agent) CheckApoptosis(f *field.GradientField) {
	if a.State != Alive {
		return
	}

	if a.Energy <= 0 {
		a.State = Apoptotic
		return
	}

	if a.IdleTicks >= 3 {
		a.State = Apoptotic
		return
	}

	pt, ok := f.Point(a.PointID)
	if !ok {
		a.State = Apoptotic
		return
	}

	totalSignal := 0.0
	for _, v := range pt.Signals {
		totalSignal += math.Abs(v)
	}
	if totalSignal < 0.05 {
		a.State = Apoptotic
	}
}

func (a *Agent) String() string {
	state := "alive"
	if a.State == Apoptotic {
		state = "dead"
	}
	return fmt.Sprintf("Agent{%s role=%s point=%s energy=%.2f state=%s idle=%d mem=%d}",
		a.ID, a.Role, a.PointID, a.Energy, state, a.IdleTicks, len(a.ShortMem.All()))
}
