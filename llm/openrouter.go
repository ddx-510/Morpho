package llm

import "fmt"

// OpenRouterProvider routes through OpenRouter's OpenAI-compatible API.
// Supports any model available on OpenRouter (Claude, GPT, Llama, Mistral, etc).
type OpenRouterProvider struct {
	APIKey string
	Model  string // e.g. anthropic/claude-sonnet-4-20250514, openai/gpt-4o, etc.
	inner  *OpenAIProvider
}

func (p *OpenRouterProvider) Name() string {
	return fmt.Sprintf("OpenRouter(%s)", p.Model)
}

func (p *OpenRouterProvider) ensure() {
	if p.inner == nil {
		p.inner = &OpenAIProvider{
			APIKey:  p.APIKey,
			Model:   p.Model,
			BaseURL: "https://openrouter.ai/api/v1",
		}
	}
}

func (p *OpenRouterProvider) Generate(req Request) Response {
	p.ensure()
	return p.inner.Generate(req)
}
