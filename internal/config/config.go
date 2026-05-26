package config

import (
	"os"
	"path/filepath"
)

type ProviderConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

type Config struct {
	Provider  string                    `json:"provider"`
	Providers map[string]ProviderConfig `json:"providers"`
}

func Default() Config {
	return Config{
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
