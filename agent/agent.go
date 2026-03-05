package agent

import (
	"fmt"
	"math"

	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/morphogen"
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

// Agent is a morphogenetic agent that reads gradients and specializes.
type Agent struct {
	ID       string
	Role     Role
	State    State
	PointID  string  // current location in the field
	Energy   float64 // depletes over time; triggers apoptosis at 0
	IdleTicks int
	WorkLog  []string

	provider llm.Provider
}

// New creates an undifferentiated agent at the given point.
func New(id string, pointID string, provider llm.Provider) *Agent {
	return &Agent{
		ID:       id,
		Role:     Undifferentiated,
		State:    Alive,
		PointID:  pointID,
		Energy:   1.0,
		provider: provider,
	}
}

// roleSignalMap maps each signal to the role that responds to it.
var roleSignalMap = map[field.Signal]Role{
	field.BugDensity:  BugHunter,
	field.TestCoverage: TestWriter,
	field.Security:    SecurityAuditor,
	field.Complexity:  Refactorer,
	field.DocDebt:     Documenter,
	field.Performance: Optimizer,
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
		return // no strong signal
	}

	if role, ok := roleSignalMap[bestSig]; ok {
		a.Role = role
	}
}

// Work performs one tick of work based on the agent's role.
// Returns morphogen signals to emit and a work description.
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

	// Find the signal this role cares about.
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

	// Consult the LLM.
	prompt := fmt.Sprintf("Role: %s | Point: %s | Signal %s=%.2f — analyze and suggest fix.",
		a.Role, a.PointID, targetSig, localVal)
	resp := a.provider.Generate(llm.Request{
		SystemPrompt: fmt.Sprintf("You are a %s agent in a morphogenetic system.", a.Role),
		UserPrompt:   prompt,
	})

	work := fmt.Sprintf("[%s@%s] %s → %s", a.ID, a.PointID, a.Role, resp.Content)
	a.WorkLog = append(a.WorkLog, work)

	// Emit PRESENCE to suppress local signal (we're handling it).
	bus.Emit(morphogen.Signal{
		Kind:    morphogen.PRESENCE,
		Source:  a.ID,
		PointID: a.PointID,
		Channel: targetSig,
		Value:   localVal * 0.3,
	})

	// Consume energy proportional to work.
	a.Energy -= 0.1

	// If signal is very high, emit ALARM to attract more agents.
	if localVal > 0.8 {
		bus.Emit(morphogen.Signal{
			Kind:    morphogen.ALARM,
			Source:  a.ID,
			PointID: a.PointID,
			Channel: targetSig,
			Value:   localVal * 0.2,
		})
	}

	// Reduce the signal we worked on.
	f.AddSignal(a.PointID, targetSig, -localVal*0.2)

	return work
}

// CheckApoptosis determines if this agent should die.
func (a *Agent) CheckApoptosis(f *field.GradientField) {
	if a.State != Alive {
		return
	}

	// Die if energy depleted.
	if a.Energy <= 0 {
		a.State = Apoptotic
		return
	}

	// Die if idle too long.
	if a.IdleTicks >= 3 {
		a.State = Apoptotic
		return
	}

	// Die if local gradients are fully depleted.
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
	return fmt.Sprintf("Agent{%s role=%s point=%s energy=%.2f state=%s idle=%d}",
		a.ID, a.Role, a.PointID, a.Energy, state, a.IdleTicks)
}
