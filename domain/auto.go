package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ddx-510/Morpho/field"
	"github.com/ddx-510/Morpho/llm"
	"github.com/ddx-510/Morpho/scan"
	"github.com/ddx-510/Morpho/tool"
)

// autoDomainSchema is what the LLM generates to define a domain.
type autoDomainSchema struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Signals     []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"signals"`
	Roles []struct {
		Name        string `json:"name"`
		Signal      string `json:"signal"`
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	} `json:"roles"`
}

// Auto uses an LLM to generate a domain definition from a free-text task description.
// The user says "analyze my marketing copy" or "review this architecture" and the LLM
// generates the signals, roles, and prompts automatically.
func Auto(provider llm.Provider, task string, inputPath string) (*Domain, error) {
	systemPrompt := `You are a domain architect for Morpho, a morphogenetic multi-agent system.
Given a task description, you design the analysis domain — the signal dimensions that agents
will sense in the gradient field, and the specialist roles they can differentiate into.

Rules:
- Create 4-6 signals (dimensions of analysis). Each is a measurable property (0=fine, 1=critical).
- Create 4-6 roles. Each role is triggered by one signal and has a specific analysis focus.
- Each role needs a prompt template that tells the agent what to look for.
- Use {{.Region}} for the region name, {{.Value}} for signal strength, {{.Code}} for the content.
- Prompts must instruct agents to output numbered findings, cite specific content, and NOT narrate.

Respond with ONLY a JSON object (no markdown, no explanation):
{
  "name": "domain_name",
  "description": "What this domain analyzes",
  "signals": [
    {"name": "signal_name", "description": "What this signal measures"}
  ],
  "roles": [
    {
      "name": "role_name",
      "signal": "signal_name",
      "description": "What this role does",
      "prompt": "You are a ... specialist analyzing \"{{.Region}}\".\nSignal signal_name = {{.Value}}.\n\nCONTENT:\n{{.Code}}\n\nINSTRUCTIONS:\n- Find specific issues...\n- Output numbered findings with severity."
    }
  ]
}`

	userPrompt := fmt.Sprintf("Design an analysis domain for this task:\n\n%s", task)

	resp := provider.Generate(llm.Request{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
	if resp.Err != nil {
		return nil, fmt.Errorf("LLM domain generation failed: %w", resp.Err)
	}

	// Parse JSON from response (strip markdown fences if present).
	content := strings.TrimSpace(resp.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var schema autoDomainSchema
	if err := json.Unmarshal([]byte(content), &schema); err != nil {
		return nil, fmt.Errorf("failed to parse LLM domain response: %w\nResponse was: %s", err, content[:min(len(content), 500)])
	}

	if len(schema.Signals) == 0 || len(schema.Roles) == 0 {
		return nil, fmt.Errorf("LLM generated empty domain (no signals or roles)")
	}

	// Build the domain.
	d := &Domain{
		Name:        schema.Name,
		Description: schema.Description,
	}

	for _, s := range schema.Signals {
		d.Signals = append(d.Signals, SignalDef{
			Name:        field.Signal(s.Name),
			Description: s.Description,
		})
	}

	for _, r := range schema.Roles {
		d.Roles = append(d.Roles, RoleDef{
			Name:        r.Name,
			Signal:      field.Signal(r.Signal),
			Description: r.Description,
			Prompt:      r.Prompt,
		})
	}

	// Use code scanner seeder by default — works for any file-based input.
	// The scan package handles code, docs, and any text files.
	d.Seeder = func(input string) (*field.GradientField, error) {
		return autoSeed(input, d)
	}
	d.ToolBuilder = func(input string) *tool.Registry {
		return tool.DefaultRegistry(input)
	}

	return d, nil
}

// autoSeed creates a gradient field from the input directory,
// distributing domain-specific signals based on content heuristics.
func autoSeed(input string, d *Domain) (*field.GradientField, error) {
	// First try the standard code scanner for structure.
	f, err := scan.Dir(input)
	if err != nil {
		return nil, err
	}

	// If the code scanner found points, augment them with the domain's signals.
	points := f.Points()
	if len(points) == 0 {
		// No structure found — create a single point.
		sigs := make(map[field.Signal]float64)
		for _, s := range d.Signals {
			sigs[s.Name] = 0.5 // moderate initial signal
		}
		f.AddPoint(&field.Point{ID: "root", Signals: sigs})
		return f, nil
	}

	// For each existing point, spread the domain signals evenly.
	// The agents will refine these through their work and morphogen signals.
	for _, pid := range points {
		pt, ok := f.Point(pid)
		if !ok {
			continue
		}
		// Keep existing signals and add domain signals.
		for _, s := range d.Signals {
			if _, exists := pt.Signals[s.Name]; !exists {
				pt.Signals[s.Name] = 0.3 // moderate initial signal
			}
		}
		f.AddPoint(&field.Point{ID: pid, Signals: pt.Signals, Links: pt.Links})
	}

	return f, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
