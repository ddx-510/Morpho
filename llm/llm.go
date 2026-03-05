package llm


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

