package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, cfg Config) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(cfg)
	os.WriteFile(path, data, 0o644)
	// patch DefaultPath by overriding HOME
	t.Setenv("HOME", dir)
	// create the expected sub-path
	os.MkdirAll(filepath.Join(dir, ".config", "agterm"), 0o755)
	dest := filepath.Join(dir, ".config", "agterm", "config.json")
	os.WriteFile(dest, data, 0o644)
	return dest
}

func TestLoad_DefaultsWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no config file
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != "ollama" {
		t.Errorf("expected default provider ollama, got %q", cfg.Provider)
	}
}

func TestLoad_ReadsFile(t *testing.T) {
	writeConfig(t, Config{
		Version:  1,
		Provider: "anthropic",
		Providers: map[string]ProviderConfig{
			"anthropic": {APIKey: "key123", Model: "claude-test"},
		},
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("expected anthropic, got %q", cfg.Provider)
	}
}

func TestLoad_EnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_AGTERM_KEY", "expanded-secret")
	writeConfig(t, Config{
		Version:  1,
		Provider: "anthropic",
		Providers: map[string]ProviderConfig{
			"anthropic": {APIKey: "$TEST_AGTERM_KEY", Model: "claude-test"},
		},
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, p, _ := cfg.ActiveProvider()
	if p.APIKey != "expanded-secret" {
		t.Errorf("env var not expanded: got %q", p.APIKey)
	}
}

func TestLoad_VersionZeroNormalisedToOne(t *testing.T) {
	writeConfig(t, Config{Provider: "ollama"}) // Version omitted → 0

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1 after normalisation, got %d", cfg.Version)
	}
}

func TestActiveProvider_Found(t *testing.T) {
	cfg := Config{
		Provider: "anthropic",
		Providers: map[string]ProviderConfig{
			"anthropic": {APIKey: "k", Model: "m"},
		},
	}
	name, p, ok := cfg.ActiveProvider()
	if !ok || name != "anthropic" || p.APIKey != "k" {
		t.Errorf("unexpected: name=%q ok=%v key=%q", name, ok, p.APIKey)
	}
}

func TestActiveProvider_Missing(t *testing.T) {
	cfg := Config{Provider: "missing", Providers: map[string]ProviderConfig{}}
	_, _, ok := cfg.ActiveProvider()
	if ok {
		t.Error("expected ok=false for missing provider")
	}
}
