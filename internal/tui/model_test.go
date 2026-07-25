package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/imattos78/agterm/internal/block"
	"github.com/imattos78/agterm/internal/config"
)

func TestKeyBytes(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"runes", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ls")}, "ls"},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, " "},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "\x1b[B"},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "\x1b[C"},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{"ctrl-a", tea.KeyMsg{Type: tea.KeyCtrlA}, "\x01"},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{"unmapped key returns nil", tea.KeyMsg{Type: tea.KeyF1}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keyBytes(tt.msg)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("keyBytes(%v) = %q, want nil", tt.msg, got)
				}
				return
			}
			if string(got) != tt.want {
				t.Fatalf("keyBytes(%v) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestBlockLines_HeaderOnly(t *testing.T) {
	b := &block.Block{Command: "ls -la", ExitCode: 0, Duration: 250 * time.Millisecond}
	lines := blockLines(b, 80)
	if len(lines) != 1 {
		t.Fatalf("expected 1 header line for empty output, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "ls -la") {
		t.Errorf("header line %q missing command", lines[0])
	}
}

func TestBlockLines_WithOutput(t *testing.T) {
	b := &block.Block{Command: "echo hi", ExitCode: 1, Duration: time.Second, Output: "hi\nbye\n"}
	lines := blockLines(b, 80)
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 output lines, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "echo hi") {
		t.Errorf("header line %q missing command", lines[0])
	}
	if !strings.Contains(lines[1], "hi") || !strings.Contains(lines[2], "bye") {
		t.Errorf("output lines missing content: %v", lines[1:])
	}
}

func TestBlockLines_NarrowWidthStillProducesHeader(t *testing.T) {
	b := &block.Block{Command: strings.Repeat("x", 200), ExitCode: 0}
	lines := blockLines(b, 10)
	if len(lines) != 1 {
		t.Fatalf("expected 1 header line, got %d", len(lines))
	}
}

func TestActiveLines(t *testing.T) {
	b := &block.Block{Command: "sleep 5", Output: "still running\n"}
	lines := activeLines(b, 80)
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 output line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "sleep 5") {
		t.Errorf("header line %q missing command", lines[0])
	}
	if !strings.Contains(lines[1], "still running") {
		t.Errorf("output line %q missing content", lines[1])
	}
}

func TestActiveLines_NoOutputYet(t *testing.T) {
	b := &block.Block{Command: "sleep 5"}
	lines := activeLines(b, 80)
	if len(lines) != 1 {
		t.Fatalf("expected only the header line, got %d: %v", len(lines), lines)
	}
}

func TestBuildProvider_LocalOnlyDisablesProvider(t *testing.T) {
	cfg := config.Config{LocalOnly: true, Provider: "ollama", Providers: map[string]config.ProviderConfig{
		"ollama": {BaseURL: "http://localhost:11434", Model: "llama3.2"},
	}}
	p, sendContext := buildProvider(cfg)
	if p != nil {
		t.Errorf("expected nil provider when LocalOnly is set, got %v", p)
	}
	if sendContext {
		t.Errorf("expected sendContext false when LocalOnly is set")
	}
}

func TestBuildProvider_NoActiveProviderConfigured(t *testing.T) {
	cfg := config.Config{Provider: "anthropic", Providers: map[string]config.ProviderConfig{}}
	p, sendContext := buildProvider(cfg)
	if p != nil {
		t.Errorf("expected nil provider when active provider is not configured, got %v", p)
	}
	if sendContext {
		t.Errorf("expected sendContext false when active provider is not configured")
	}
}

func TestBuildProvider_UnknownProviderName(t *testing.T) {
	cfg := config.Config{Provider: "not-a-real-provider", Providers: map[string]config.ProviderConfig{
		"not-a-real-provider": {BaseURL: "http://example.com", Model: "x"},
	}}
	p, sendContext := buildProvider(cfg)
	if p != nil {
		t.Errorf("expected nil provider for an unregistered provider name, got %v", p)
	}
	if sendContext {
		t.Errorf("expected sendContext false when ai.Build fails")
	}
}

func TestBuildProvider_ValidOllamaProvider(t *testing.T) {
	// ollama does not require an API key, so this should succeed even with
	// no credentials configured — it is registered via model.go's blank
	// import of internal/ai/ollama.
	cfg := config.Config{Provider: "ollama", Providers: map[string]config.ProviderConfig{
		"ollama": {BaseURL: "http://localhost:11434", Model: "llama3.2", SendContext: true},
	}}
	p, sendContext := buildProvider(cfg)
	if p == nil {
		t.Fatal("expected a non-nil provider for a valid ollama config")
	}
	if p.Name() != "ollama" {
		t.Errorf("expected provider name ollama, got %q", p.Name())
	}
	if !sendContext {
		t.Errorf("expected sendContext true, matching the provider config")
	}
}

func TestView_ReturnsErrorMessageWhenSet(t *testing.T) {
	m := Model{err: errBoom{}}
	view := m.View()
	if !strings.Contains(view, "agterm:") || !strings.Contains(view, "boom") {
		t.Errorf("expected error view to mention the error, got %q", view)
	}
}

func TestView_EmptyBeforeFirstWindowSize(t *testing.T) {
	m := Model{}
	if got := m.View(); got != "" {
		t.Errorf("expected empty view before a WindowSizeMsg, got %q", got)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
