package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddx-510/Morpho/llm"
)

// Config is the top-level configuration loaded from morpho.json.
type Config struct {
	Provider  ProviderConfig `json:"provider"`
	Engine    EngineConfig   `json:"engine"`
	Memory    MemoryConfig   `json:"memory"`
}

// ProviderConfig selects and configures the LLM provider.
type ProviderConfig struct {
	Type    string `json:"type"`    // "openai", "claude", "gemini", "openrouter", "demo"
	APIKey  string `json:"api_key"` // can also use env var references like "$OPENAI_API_KEY"
	Model   string `json:"model"`
	BaseURL string `json:"base_url,omitempty"`
}

// EngineConfig holds simulation parameters.
type EngineConfig struct {
	MaxTicks      int     `json:"max_ticks"`
	DecayRate     float64 `json:"decay_rate"`
	DiffusionRate float64 `json:"diffusion_rate"`
	SpawnPerTick  int     `json:"spawn_per_tick"`
}

// MemoryConfig controls agent memory behavior.
type MemoryConfig struct {
	ShortTermCapacity int    `json:"short_term_capacity"`
	LongTermPath      string `json:"long_term_path"`
}

// Load reads a config file from disk. Falls back to defaults if the file doesn't exist.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Provider: ProviderConfig{Type: "demo"},
		Engine: EngineConfig{
			MaxTicks:      10,
			DecayRate:     0.05,
			DiffusionRate: 0.3,
			SpawnPerTick:  2,
		},
		Memory: MemoryConfig{
			ShortTermCapacity: 20,
			LongTermPath:      ".morpho_memory.json",
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

	// Resolve env var references in API key.
	cfg.Provider.APIKey = resolveEnv(cfg.Provider.APIKey)

	return cfg, nil
}

// resolveEnv replaces "$VAR_NAME" with the environment variable value.
func resolveEnv(s string) string {
	if len(s) > 0 && s[0] == '$' {
		if val := os.Getenv(s[1:]); val != "" {
			return val
		}
	}
	return s
}

// BuildProvider creates an LLM provider from the config.
func (c *Config) BuildProvider() (llm.Provider, error) {
	switch c.Provider.Type {
	case "demo", "":
		return &llm.DemoProvider{}, nil
	case "openai":
		if c.Provider.APIKey == "" {
			return nil, fmt.Errorf("openai provider requires api_key (or set $OPENAI_API_KEY)")
		}
		model := c.Provider.Model
		if model == "" {
			model = "gpt-4o"
		}
		return &llm.OpenAIProvider{
			APIKey:  c.Provider.APIKey,
			Model:   model,
			BaseURL: c.Provider.BaseURL,
		}, nil
	case "claude":
		if c.Provider.APIKey == "" {
			return nil, fmt.Errorf("claude provider requires api_key (or set $ANTHROPIC_API_KEY)")
		}
		model := c.Provider.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		return &llm.ClaudeProvider{
			APIKey:  c.Provider.APIKey,
			Model:   model,
			BaseURL: c.Provider.BaseURL,
		}, nil
	case "gemini":
		if c.Provider.APIKey == "" {
			return nil, fmt.Errorf("gemini provider requires api_key (or set $GEMINI_API_KEY)")
		}
		model := c.Provider.Model
		if model == "" {
			model = "gemini-2.0-flash"
		}
		return &llm.GeminiProvider{
			APIKey: c.Provider.APIKey,
			Model:  model,
		}, nil
	case "openrouter":
		if c.Provider.APIKey == "" {
			return nil, fmt.Errorf("openrouter provider requires api_key (or set $OPENROUTER_API_KEY)")
		}
		model := c.Provider.Model
		if model == "" {
			model = "anthropic/claude-sonnet-4-20250514"
		}
		return &llm.OpenRouterProvider{
			APIKey: c.Provider.APIKey,
			Model:  model,
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider type: %q", c.Provider.Type)
	}
}
