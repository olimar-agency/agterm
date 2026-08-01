package ai

import (
	"strings"
	"testing"
	"time"

	"github.com/imattos78/agterm/internal/block"
	"github.com/imattos78/agterm/internal/vt"
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

// ── SemanticText wiring + marker-balanced truncation (agterm#16) ────────────

func TestTruncateAnnotated_ShortStringUnchanged(t *testing.T) {
	s := "ok " + vt.ErrorMarkerOpen + "bad" + vt.ErrorMarkerClose
	if got := truncateAnnotated(s, 100); got != s {
		t.Errorf("truncateAnnotated() = %q, want unchanged %q", got, s)
	}
}

func TestTruncateAnnotated_ClosesOpenMarkerAtCutPoint(t *testing.T) {
	// Cut lands inside the marked span: "«error: this is a lo" — the open
	// marker has no matching close within the cut prefix.
	s := vt.ErrorMarkerOpen + "this is a long error message that keeps going" + vt.ErrorMarkerClose
	cutAt := len(vt.ErrorMarkerOpen) + 20 // well inside the span, before ErrorMarkerClose
	got := truncateAnnotated(s, cutAt)

	if !strings.HasSuffix(strings.TrimSuffix(got, "\n[output truncated]"), vt.ErrorMarkerClose) {
		t.Fatalf("truncateAnnotated() = %q, want the dangling marker closed before the truncation suffix", got)
	}
	if strings.Count(got, vt.ErrorMarkerOpen) != strings.Count(got, vt.ErrorMarkerClose) {
		t.Fatalf("truncateAnnotated() = %q, want balanced markers", got)
	}
}

func TestTruncateAnnotated_CutAfterMarkerAlreadyClosedDoesNotDoubleClose(t *testing.T) {
	s := vt.ErrorMarkerOpen + "bad" + vt.ErrorMarkerClose + " and then a lot more trailing text here"
	closedAt := strings.Index(s, vt.ErrorMarkerClose) + len(vt.ErrorMarkerClose)
	got := truncateAnnotated(s, closedAt)

	if strings.Count(got, vt.ErrorMarkerOpen) != strings.Count(got, vt.ErrorMarkerClose) {
		t.Fatalf("truncateAnnotated() = %q, want balanced markers (no extra close appended)", got)
	}
	if strings.Count(got, vt.ErrorMarkerClose) != 1 {
		t.Fatalf("truncateAnnotated() = %q, want exactly one close marker, not doubled", got)
	}
}

func TestBuildContext_UsesSemanticTextForCellsPopulatedBlocks(t *testing.T) {
	store := block.NewStore(10)
	store.Add(&block.Block{
		Command:  "lint",
		ExitCode: 0,
		Duration: 500 * time.Millisecond,
		Cells: [][]vt.Cell{
			{
				{Rune: 'o', Style: vt.DefaultStyle(), Width: 1},
				{Rune: 'k', Style: vt.DefaultStyle(), Width: 1},
				{Rune: ' ', Style: vt.DefaultStyle(), Width: 1},
				{Rune: 'x', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 1}}, Width: 1},
			},
		},
	})

	ctx := BuildContext(store, 10)
	if !strings.Contains(ctx, vt.ErrorMarkerOpen+"x"+vt.ErrorMarkerClose) {
		t.Errorf("expected BuildContext to include the semantic error marker, got:\n%s", ctx)
	}
}

func TestSystemPrompt_DescribesErrorMarkerFormat(t *testing.T) {
	if !strings.Contains(SystemPrompt, vt.ErrorMarkerOpen) {
		t.Errorf("expected SystemPrompt to describe the %q marker format", vt.ErrorMarkerOpen)
	}
}
