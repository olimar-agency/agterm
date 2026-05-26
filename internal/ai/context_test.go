package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/imattos78/agterm/internal/block"
)

func makeStore(cmds []struct {
	cmd  string
	exit int
	out  string
}) *block.Store {
	store := block.NewStore(100)
	for _, c := range cmds {
		store.Add(&block.Block{
			Command:   c.cmd,
			ExitCode:  c.exit,
			Output:    c.out,
			Duration:  500 * time.Millisecond,
			StartedAt: time.Now(),
		})
	}
	return store
}

func TestBuildContext_Empty(t *testing.T) {
	store := block.NewStore(10)
	if got := BuildContext(store, 5); got != "" {
		t.Errorf("expected empty string for empty store, got %q", got)
	}
}

func TestBuildContext_IncludesCommandAndExit(t *testing.T) {
	store := makeStore([]struct {
		cmd  string
		exit int
		out  string
	}{{"ls", 0, "file1\nfile2\n"}})

	ctx := BuildContext(store, 10)
	if !strings.Contains(ctx, "$ ls") {
		t.Error("context missing command")
	}
	if !strings.Contains(ctx, "exit 0") {
		t.Error("context missing exit code")
	}
	if !strings.Contains(ctx, "file1") {
		t.Error("context missing output")
	}
}

func TestBuildContext_TruncatesLongOutput(t *testing.T) {
	longOut := strings.Repeat("x", maxOutputChars+100)
	store := makeStore([]struct {
		cmd  string
		exit int
		out  string
	}{{"cmd", 0, longOut}})

	ctx := BuildContext(store, 10)
	if !strings.Contains(ctx, "[output truncated]") {
		t.Error("expected truncation marker")
	}
}

func TestBuildContext_RespectsN(t *testing.T) {
	store := makeStore([]struct {
		cmd  string
		exit int
		out  string
	}{
		{"cmd1", 0, ""},
		{"cmd2", 0, ""},
		{"cmd3", 0, ""},
	})

	ctx := BuildContext(store, 2)
	if strings.Contains(ctx, "cmd1") {
		t.Error("expected oldest block excluded when n=2")
	}
	if !strings.Contains(ctx, "cmd3") {
		t.Error("expected newest block included")
	}
}

func TestBuildQuestion_NoContext(t *testing.T) {
	got := BuildQuestion("", "why?")
	if got != "why?" {
		t.Errorf("expected bare question, got %q", got)
	}
}

func TestBuildQuestion_WithContext(t *testing.T) {
	got := BuildQuestion("ctx", "why?")
	if !strings.Contains(got, "ctx") || !strings.Contains(got, "why?") {
		t.Errorf("expected context+question, got %q", got)
	}
}
