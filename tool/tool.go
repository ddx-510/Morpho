package tool

import (
	"fmt"
	"strings"

	"github.com/ddx-510/Morpho/llm"
)

// Tool is a capability that agents can invoke during work.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]string // param name -> description
	Execute(args map[string]string) Result
}

// Result is what a tool returns after execution.
type Result struct {
	Output string
	Err    error
}

// Registry holds available tools, organized by role suitability.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns all registered tools.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ToLLMSpecs converts tools to LLM tool specs for function calling.
func (r *Registry) ToLLMSpecs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return specs
}

// ExecuteCall runs a tool call and returns the result.
func (r *Registry) ExecuteCall(call llm.ToolCall) Result {
	t, ok := r.tools[call.Name]
	if !ok {
		return Result{Err: fmt.Errorf("unknown tool: %s", call.Name)}
	}
	return t.Execute(call.Args)
}

// ExecuteCalls runs multiple tool calls and returns a summary.
func (r *Registry) ExecuteCalls(calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var parts []string
	for _, call := range calls {
		result := r.ExecuteCall(call)
		if result.Err != nil {
			parts = append(parts, fmt.Sprintf("[%s] error: %v", call.Name, result.Err))
		} else {
			parts = append(parts, fmt.Sprintf("[%s] %s", call.Name, truncate(result.Output, 200)))
		}
	}
	return strings.Join(parts, "; ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
