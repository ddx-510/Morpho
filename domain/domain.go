package domain

import (
	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/tool"
)

// Domain defines a morphogenetic task domain — the signals, roles,
// prompts, and seeding logic that shape how agents differentiate and work.
// The engine, gradient field, morphogen bus, and tissue detection are
// domain-agnostic; this is where you plug in your specific use case.
type Domain struct {
	Name        string
	Description string

	// Signals are the dimensions of the gradient field.
	// Each signal represents a measurable property of the input space.
	Signals []SignalDef

	// Roles are the specializations agents can differentiate into.
	// Each role is triggered by a specific signal and carries a prompt template.
	Roles []RoleDef

	// Seeder creates the initial gradient field from the input.
	// For code review, this scans a directory. For research, this could parse a topic.
	// For data analysis, this could read a dataset. Anything.
	Seeder func(input string) (*field.GradientField, error)

	// Tools returns the tool registry available to agents in this domain.
	ToolBuilder func(input string) *tool.Registry
}

// SignalDef defines a signal dimension in the gradient field.
type SignalDef struct {
	Name        field.Signal
	Description string
}

// RoleDef defines a specialist role agents can differentiate into.
type RoleDef struct {
	Name        string       // role identifier (e.g. "bug_hunter", "fact_checker")
	Signal      field.Signal // which signal triggers this role
	Emoji       string       // for display (optional)
	Description string       // what this role does
	Prompt      string       // system prompt template — use {{.Region}}, {{.Signal}}, {{.Value}}, {{.Code}}
}

// SignalNames returns all signal names defined in this domain.
func (d *Domain) SignalNames() []field.Signal {
	sigs := make([]field.Signal, len(d.Signals))
	for i, s := range d.Signals {
		sigs[i] = s.Name
	}
	return sigs
}

// RoleForSignal returns the role name triggered by a given signal.
func (d *Domain) RoleForSignal(sig field.Signal) (string, bool) {
	for _, r := range d.Roles {
		if r.Signal == sig {
			return r.Name, true
		}
	}
	return "", false
}

// SignalForRole returns the signal that triggers a given role.
func (d *Domain) SignalForRole(role string) (field.Signal, bool) {
	for _, r := range d.Roles {
		if r.Name == role {
			return r.Signal, true
		}
	}
	return "", false
}

// RolePrompt returns the prompt template for a role.
func (d *Domain) RolePrompt(role string) string {
	for _, r := range d.Roles {
		if r.Name == role {
			return r.Prompt
		}
	}
	return ""
}
