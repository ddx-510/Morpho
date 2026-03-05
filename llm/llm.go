package llm

import "fmt"

// Request is a prompt sent to an LLM provider.
type Request struct {
	SystemPrompt string
	UserPrompt   string
}

// Response is what the LLM returns.
type Response struct {
	Content string
	Err     error
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
