package agent

import (
	"fmt"
	"math"
	"time"

	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/memory"
	"github.com/ddx-510/Morpho/morphogen"
	"github.com/ddx-510/Morpho/tool"
)

// Role is the specialized function an agent differentiates into.
type Role string

const (
	Undifferentiated Role = "undifferentiated"
	BugHunter        Role = "bug_hunter"
	TestWriter       Role = "test_writer"
	SecurityAuditor  Role = "security_auditor"
	Refactorer       Role = "refactorer"
	Documenter       Role = "documenter"
	Optimizer        Role = "optimizer"
)

// State tracks the agent lifecycle.
type State int

const (
	Alive State = iota
	Apoptotic
)

// roleToolMap maps roles to the tool names they prefer.
var roleToolMap = map[Role][]string{
	BugHunter:       {"grep", "read_file", "shell"},
	TestWriter:      {"read_file", "shell", "patch_file"},
	SecurityAuditor: {"grep", "read_file", "list_files"},
	Refactorer:      {"read_file", "patch_file", "grep"},
	Documenter:      {"read_file", "list_files", "grep"},
	Optimizer:       {"read_file", "grep", "shell"},
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

	provider  llm.Provider
	tools     *tool.Registry
	ShortMem  *memory.ShortTerm
	longMem   *memory.LongTerm
	tick      int
}

// New creates an undifferentiated agent at the given point.
func New(id string, pointID string, provider llm.Provider, tools *tool.Registry, longMem *memory.LongTerm, stmCapacity int) *Agent {
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
	}
}

// SetTick updates the agent's current tick (set by engine each step).
func (a *Agent) SetTick(tick int) {
	a.tick = tick
}

// roleSignalMap maps each signal to the role that responds to it.
var roleSignalMap = map[field.Signal]Role{
	field.BugDensity:   BugHunter,
	field.TestCoverage: TestWriter,
	field.Security:     SecurityAuditor,
	field.Complexity:   Refactorer,
	field.DocDebt:      Documenter,
	field.Performance:  Optimizer,
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

	if role, ok := roleSignalMap[bestSig]; ok {
		a.Role = role
		a.remember("observation", fmt.Sprintf("differentiated into %s based on %s=%.2f at %s", role, bestSig, bestVal, a.PointID))
	}
}

// roleTools returns the LLM tool specs this agent's role can use.
func (a *Agent) roleTools() []llm.ToolSpec {
	if a.tools == nil {
		return nil
	}
	preferred, ok := roleToolMap[a.Role]
	if !ok {
		return a.tools.ToLLMSpecs()
	}
	var specs []llm.ToolSpec
	for _, name := range preferred {
		if t, ok := a.tools.Get(name); ok {
			specs = append(specs, llm.ToolSpec{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			})
		}
	}
	return specs
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

	var targetSig field.Signal
	for sig, role := range roleSignalMap {
		if role == a.Role {
			targetSig = sig
			break
		}
	}

	localVal := pt.Signals[targetSig]
	if localVal < 0.05 {
		a.IdleTicks++
		return ""
	}

	a.IdleTicks = 0

	// Build prompt with memory context.
	memCtx := a.ShortMem.Summary()

	systemPrompt := fmt.Sprintf(`You are a %s specialist analyzing the "%s" region of a codebase.
Signal %s = %.2f (0=fine, 1=critical).

INSTRUCTIONS:
1. Use the tools to read actual source files in the "%s/" directory.
2. Find SPECIFIC issues — cite file names, function names, and quote problematic code.
3. Do NOT narrate what you plan to do. Just do it and report findings.
4. Output a numbered list of concrete findings. Each must reference a real file.`, a.Role, a.PointID, targetSig, localVal, a.PointID)

	prompt := fmt.Sprintf("Previous findings:\n%s\nFind issues now. Use tools, then report.", memCtx)

	// Round 1: call LLM with tools.
	resp := a.provider.Generate(llm.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   prompt,
		Tools:        a.roleTools(),
	})

	if resp.Err != nil {
		a.remember("error", resp.Err.Error())
		a.IdleTicks++
		return ""
	}

	// Execute tool calls and feed results back for a second round.
	var toolOutput string
	if a.tools != nil && len(resp.ToolCalls) > 0 {
		toolOutput = a.tools.ExecuteCalls(resp.ToolCalls)
		a.remember("tool_result", toolOutput)

		// Round 2: feed tool results back to get actual analysis.
		if toolOutput != "" {
			followUp := a.provider.Generate(llm.Request{
				SystemPrompt: systemPrompt,
				UserPrompt: fmt.Sprintf("Tool results:\n%s\n\nBased on these results, list the specific issues you found. Each finding must cite a file and describe the actual problem.", toolOutput),
			})
			if followUp.Err == nil && followUp.Content != "" {
				resp.Content = followUp.Content
			}
		}
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
