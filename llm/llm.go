package llm

import "fmt"

// Request is a prompt sent to an LLM provider.
type Request struct {
	SystemPrompt string
	UserPrompt   string
	Tools        []ToolSpec // available tools for function calling
}

// ToolSpec describes a tool the LLM can call.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]string // param name -> description
}

// ToolCall is a function call the LLM wants to make.
type ToolCall struct {
	Name   string
	Args   map[string]string
}

// Response is what the LLM returns.
type Response struct {
	Content   string
	ToolCalls []ToolCall
	Err       error
}

// Provider is a pluggable LLM interface.
type Provider interface {
	Generate(req Request) Response
	Name() string
}

// DemoProvider is a deterministic provider for testing and demos.
type DemoProvider struct{}

func (d *DemoProvider) Name() string { return "DemoProvider" }

func (d *DemoProvider) Generate(req Request) Response {
	// If tools are available, simulate a tool call for the first tool.
	if len(req.Tools) > 0 {
		args := make(map[string]string)
		for param := range req.Tools[0].Parameters {
			args[param] = fmt.Sprintf("demo_%s_value", param)
		}
		return Response{
			Content: fmt.Sprintf("[demo] Analyzed: %s", truncate(req.UserPrompt, 80)),
			ToolCalls: []ToolCall{{
				Name: req.Tools[0].Name,
				Args: args,
			}},
		}
	}
	return Response{
		Content: fmt.Sprintf("[demo] Analyzed: %s", truncate(req.UserPrompt, 80)),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
