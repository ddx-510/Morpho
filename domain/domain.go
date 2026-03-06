package domain

import (
	"github.com/ddx-510/Morpho/agent"
	"github.com/ddx-510/Morpho/tool"
)

// Domain defines a morphogenetic task domain — the signals, roles,
// prompts, and seeding logic that shape how agents differentiate and work.
type Domain struct {
	Name        string
	Description string
	Signals     []SignalDef
	Roles       []RoleDef
	Seeder      func(input string) (*agent.GradientField, error)
	ToolBuilder func(input string) *tool.Registry
}

// SignalDef defines a signal dimension in the gradient field.
type SignalDef struct {
	Name        agent.Signal
	Description string
}

// RoleDef defines a specialist role agents can differentiate into.
type RoleDef struct {
	Name        string
	Signal      agent.Signal
	Description string
	Prompt      string // template — use {{.Region}}, {{.Signal}}, {{.Value}}, {{.Code}}
}
