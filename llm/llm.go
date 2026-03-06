package llm

import "encoding/json"

// ChatMessage is a single message in a multi-turn conversation.
type ChatMessage struct {
	Role       string     // "system", "user", "assistant", "tool"
	Content    string
	ToolCalls  []ToolCall // for assistant messages with tool calls
	ToolCallID string     // for tool result messages
}

// Request is a prompt sent to an LLM provider.
type Request struct {
	SystemPrompt string
	UserPrompt   string
	Messages     []ChatMessage // multi-turn conversation (if set, overrides SystemPrompt/UserPrompt)
	Tools        []ToolSpec    // available tools for function calling
}

// ToolSpec describes a tool the LLM can call.
// Parameters is a raw JSON Schema object ({"type":"object","properties":{...}}).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema
}

// ToolCall is a function call the LLM wants to make.
type ToolCall struct {
	ID   string // provider-assigned ID for multi-turn correlation
	Name string
	Args map[string]any
}

// Response is what the LLM returns.
type Response struct {
	Content   string
	ToolCalls []ToolCall
	Err       error
	Tokens    TokenUsage
}

// TokenUsage tracks token consumption for a single LLM call.
type TokenUsage struct {
	Input  int
	Output int
	Total  int
}

// Provider is a pluggable LLM interface.
type Provider interface {
	Generate(req Request) Response
	Name() string
}

// ParamSchema builds a JSON Schema from a simple param name -> description map.
// All params are typed as "string" unless overridden via opts.
func ParamSchema(params map[string]ParamDef) json.RawMessage {
	props := make(map[string]any)
	var required []string
	for name, def := range params {
		prop := map[string]any{
			"type":        def.Type,
			"description": def.Description,
		}
		if def.Type == "" {
			prop["type"] = "string"
		}
		props[name] = prop
		if def.Required {
			required = append(required, name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	data, _ := json.Marshal(schema)
	return data
}

// ParamDef defines a single parameter.
type ParamDef struct {
	Type        string // "string", "integer", "boolean"; defaults to "string"
	Description string
	Required    bool
}
