package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProviderConfig struct {
	APIKey      string `json:"api_key"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	SendContext bool   `json:"send_context"` // default false — opt-in per provider
}

type Config struct {
	Version   int                       `json:"version"`
	Provider  string                    `json:"provider"`
	LocalOnly bool                      `json:"local_only"` // disables all remote providers
	Providers map[string]ProviderConfig `json:"providers"`
}

func Default() Config {
	return Config{
		Version:  1,
		Provider: "ollama",
		Providers: map[string]ProviderConfig{
			"ollama": {
				BaseURL: "http://localhost:11434",
				Model:   "llama3.2",
			},
		},
	}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agterm", "config.json")
}

// Load reads config from DefaultPath, falls back to defaults if the file
// doesn't exist. $VAR references in api_key fields are expanded at load time.
// Migration detection runs on every load; the file is never rewritten here.
func Load() (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(DefaultPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}

	// version=0 assumed when field absent; no migrations yet in v1
	if cfg.Version == 0 {
		cfg.Version = 1
	}

	// expand $VAR in api_key fields
	expanded := make(map[string]ProviderConfig, len(cfg.Providers))
	for name, p := range cfg.Providers {
		if strings.HasPrefix(p.APIKey, "$") {
			p.APIKey = os.Getenv(strings.TrimPrefix(p.APIKey, "$"))
		}
		expanded[name] = p
	}
	cfg.Providers = expanded

	return cfg, nil
}

// ActiveProvider returns the ProviderConfig for the active provider and
// whether it was found.
func (c Config) ActiveProvider() (string, ProviderConfig, bool) {
	p, ok := c.Providers[c.Provider]
	return c.Provider, p, ok
}
