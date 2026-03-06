package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddx-510/Morpho/llm"
)

// Config is the top-level configuration loaded from morpho.json.
type Config struct {
	Provider   ProviderConfig    `json:"provider"`
	Engine     EngineConfig      `json:"engine"`
	Memory     MemoryConfig      `json:"memory"`
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`
}

// MCPServerConfig describes an MCP server to connect to for additional tools.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// ProviderConfig selects and configures the LLM provider.
type ProviderConfig struct {
	Type    string `json:"type"`              // "openai", "claude", "gemini", "openrouter", "demo"
	APIKey  string `json:"api_key"`           // can use env var references like "$OPENAI_API_KEY"
	Model   string `json:"model"`
	BaseURL string `json:"base_url,omitempty"` // override default endpoint
}

// EngineConfig holds simulation parameters.
type EngineConfig struct {
	MaxTicks      int     `json:"max_ticks"`
	DecayRate     float64 `json:"decay_rate"`
	DiffusionRate float64 `json:"diffusion_rate"`
	SpawnPerTick  int     `json:"spawn_per_tick"`
}

// MemoryConfig controls memory behavior.
type MemoryConfig struct {
	FactsPath  string `json:"facts_path"`
	SessionDir string `json:"session_dir"`
}

// Load reads a config file from disk. Falls back to defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Provider: ProviderConfig{},
		Engine: EngineConfig{
			MaxTicks:      10,
			DecayRate:     0.05,
			DiffusionRate: 0.3,
			SpawnPerTick:  2,
		},
		Memory: MemoryConfig{
			FactsPath:  ".morpho/facts.json",
			SessionDir: ".morpho/sessions",
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.Provider.APIKey = resolveEnv(cfg.Provider.APIKey)
	return cfg, nil
}

func resolveEnv(s string) string {
	if len(s) > 0 && s[0] == '$' {
		if val := os.Getenv(s[1:]); val != "" {
			return val
		}
	}
	return s
}

// providerPreset holds the defaults for a known provider type.
type providerPreset struct {
	Label   string
	Format  llm.APIFormat
	BaseURL string
	Model   string
}

var presets = map[string]providerPreset{
	"openai":     {Label: "OpenAI", Format: llm.FormatOpenAI, BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
	"claude":     {Label: "Claude", Format: llm.FormatAnthropic, BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-20250514"},
	"gemini":     {Label: "Gemini", Format: llm.FormatGemini, BaseURL: "https://generativelanguage.googleapis.com", Model: "gemini-2.0-flash"},
	"openrouter": {Label: "OpenRouter", Format: llm.FormatOpenAI, BaseURL: "https://openrouter.ai/api/v1", Model: "anthropic/claude-sonnet-4-20250514"},
	"groq":       {Label: "Groq", Format: llm.FormatOpenAI, BaseURL: "https://api.groq.com/openai/v1", Model: "llama-3.3-70b-versatile"},
	"together":   {Label: "Together", Format: llm.FormatOpenAI, BaseURL: "https://api.together.xyz/v1", Model: "meta-llama/Llama-3.3-70B-Instruct-Turbo"},
	"deepseek":   {Label: "DeepSeek", Format: llm.FormatOpenAI, BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
	"ollama":     {Label: "Ollama", Format: llm.FormatOpenAI, BaseURL: "http://localhost:11434/v1", Model: "llama3.2"},
}

// BuildProvider creates an LLM provider from the config.
func (c *Config) BuildProvider() (llm.Provider, error) {
	if c.Provider.Type == "" {
		return nil, fmt.Errorf("provider type is required in config (openai, claude, gemini, openrouter, groq, together, deepseek, ollama)")
	}

	preset, known := presets[c.Provider.Type]
	if !known {
		// Unknown type: assume OpenAI-compatible with base_url required.
		if c.Provider.BaseURL == "" {
			return nil, fmt.Errorf("unknown provider %q: set base_url for custom OpenAI-compatible endpoints", c.Provider.Type)
		}
		preset = providerPreset{
			Label:   c.Provider.Type,
			Format:  llm.FormatOpenAI,
			BaseURL: c.Provider.BaseURL,
			Model:   "default",
		}
	}

	if c.Provider.APIKey == "" && c.Provider.Type != "ollama" {
		return nil, fmt.Errorf("%s provider requires api_key", preset.Label)
	}

	model := c.Provider.Model
	if model == "" {
		model = preset.Model
	}
	baseURL := c.Provider.BaseURL
	if baseURL == "" {
		baseURL = preset.BaseURL
	}

	return &llm.HTTPProvider{
		Label:   preset.Label,
		APIKey:  c.Provider.APIKey,
		Model:   model,
		BaseURL: baseURL,
		Format:  preset.Format,
	}, nil
}
