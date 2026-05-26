package ai

import (
	"context"
	"testing"
)

// fakeProvider is a no-op Provider used in registry tests.
type fakeProvider struct{ name string }

func (f *fakeProvider) Name() string                                  { return f.name }
func (f *fakeProvider) Stream(_ context.Context, _ Request) <-chan StreamResult {
	ch := make(chan StreamResult, 1)
	ch <- StreamResult{Done: true}
	close(ch)
	return ch
}

func TestRegistry_BuildKnownProvider(t *testing.T) {
	Register("_test_provider", func(apiKey, baseURL, model string) Provider {
		return &fakeProvider{name: "_test_provider"}
	})
	defer delete(registry, "_test_provider")

	p, err := Build("_test_provider", "key", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "_test_provider" {
		t.Errorf("got name %q", p.Name())
	}
}

func TestRegistry_BuildUnknownProvider(t *testing.T) {
	_, err := Build("_nonexistent_xyz", "", "", "")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestRegistry_BuildMissingAPIKey(t *testing.T) {
	Register("_test_remote", func(apiKey, baseURL, model string) Provider {
		return &fakeProvider{name: "_test_remote"}
	})
	defer delete(registry, "_test_remote")

	_, err := Build("_test_remote", "", "", "")
	if err == nil {
		t.Error("expected error when API key missing for remote provider")
	}
}

func TestRegistry_OllamaNoKeyRequired(t *testing.T) {
	Register("ollama", func(apiKey, baseURL, model string) Provider {
		return &fakeProvider{name: "ollama"}
	})

	p, err := Build("ollama", "", "", "")
	if err != nil {
		t.Fatalf("ollama should not require API key: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}
