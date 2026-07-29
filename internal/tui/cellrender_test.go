package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/imattos78/agterm/internal/block"
	"github.com/imattos78/agterm/internal/vt"
)

func TestLipglossColor_Indexed(t *testing.T) {
	c, ok := lipglossColor(vt.ColorIndexed{Index: 2})
	if !ok {
		t.Fatalf("expected ok=true for ColorIndexed")
	}
	if c != lipgloss.Color("2") {
		t.Fatalf("expected lipgloss.Color(%q), got %v", "2", c)
	}
}

func TestLipglossColor_RGB(t *testing.T) {
	c, ok := lipglossColor(vt.ColorRGB{R: 0xFF, G: 0x80, B: 0x00})
	if !ok {
		t.Fatalf("expected ok=true for ColorRGB")
	}
	if c != lipgloss.Color("#ff8000") {
		t.Fatalf("expected #ff8000, got %v", c)
	}
}

func TestLipglossColor_DefaultIsUnset(t *testing.T) {
	if _, ok := lipglossColor(vt.ColorDefault{}); ok {
		t.Fatalf("expected ok=false for ColorDefault — it must not set Foreground/Background")
	}
}

func TestLipglossStyleFor_MapsColorsAndAttributes(t *testing.T) {
	s := vt.CellStyle{
		Fg:         vt.ColorIndexed{Index: 1},
		Bg:         vt.ColorDefault{},
		Attributes: vt.AttrBold | vt.AttrUnderline | vt.AttrReverse,
	}
	ls := lipglossStyleFor(s)

	if ls.GetForeground() != lipgloss.Color("1") {
		t.Errorf("expected foreground color 1, got %v", ls.GetForeground())
	}
	if ls.GetBackground() != (lipgloss.NoColor{}) {
		t.Errorf("expected background left unset for ColorDefault, got %v", ls.GetBackground())
	}
	if !ls.GetBold() {
		t.Errorf("expected Bold")
	}
	if !ls.GetUnderline() {
		t.Errorf("expected Underline")
	}
	if !ls.GetReverse() {
		t.Errorf("expected Reverse")
	}
	if ls.GetItalic() {
		t.Errorf("expected Italic false — not in the attribute set")
	}
}

func TestLipglossStyleFor_DefaultStyleSetsNothing(t *testing.T) {
	ls := lipglossStyleFor(vt.DefaultStyle())
	if ls.GetForeground() != (lipgloss.NoColor{}) {
		t.Errorf("expected no foreground set, got %v", ls.GetForeground())
	}
	if ls.GetBackground() != (lipgloss.NoColor{}) {
		t.Errorf("expected no background set, got %v", ls.GetBackground())
	}
	if ls.GetBold() || ls.GetItalic() || ls.GetUnderline() || ls.GetReverse() {
		t.Errorf("expected no attributes set for DefaultStyle")
	}
}

func plainCell(r rune) vt.Cell { return vt.Cell{Rune: r, Style: vt.DefaultStyle(), Width: 1} }

func TestRenderCells_PreservesRuneOrderAcrossStyleRuns(t *testing.T) {
	row := []vt.Cell{
		{Rune: 'A', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 1}}, Width: 1},
		{Rune: 'B', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 1}}, Width: 1},
		{Rune: 'C', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 2}}, Width: 1},
		{Rune: 'D', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 2}}, Width: 1},
	}
	got := renderCells(row)
	if got != "ABCD" {
		t.Fatalf("expected %q (this test env has no color profile, so styling is invisible in the string), got %q", "ABCD", got)
	}
}

func TestRenderCells_SkipsControlCharacters(t *testing.T) {
	row := []vt.Cell{
		plainCell('h'),
		{Rune: '\a', Style: vt.DefaultStyle(), Width: 0}, // BEL, not renderable
		plainCell('i'),
	}
	got := renderCells(row)
	if got != "hi" {
		t.Fatalf("expected control character to be skipped, got %q", got)
	}
}

// TestRenderCells_AppliesEachStyleAfterARunBoundary forces a real color
// profile (go test's non-tty environment otherwise strips all ANSI, see the
// other tests in this file) to catch a regression where flushing a run on a
// style change failed to also adopt the new style for the next run — every
// character after the first run boundary would silently keep rendering in
// the previous style.
func TestRenderCells_AppliesEachStyleAfterARunBoundary(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(old)

	row := []vt.Cell{
		{Rune: 'A', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 1}}, Width: 1}, // red
		{Rune: 'B', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 2}}, Width: 1}, // green
		{Rune: 'C', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 2}}, Width: 1}, // green, same run as B
	}
	got := renderCells(row)

	redA := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("A")
	greenBC := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("BC")
	want := redA + greenBC
	if got != want {
		t.Fatalf("expected each run to carry its own style:\n got  %q\n want %q", got, want)
	}
}

func TestRenderCells_EmptyRow(t *testing.T) {
	if got := renderCells(nil); got != "" {
		t.Fatalf("expected empty string for an empty row, got %q", got)
	}
}

func TestStyledOutputLines_PrefersCellsOverStaleOutput(t *testing.T) {
	b := &block.Block{
		Output: "stale plain text\n",
		Cells: [][]vt.Cell{
			{plainCell('h'), plainCell('i')},
			{plainCell('b'), plainCell('y'), plainCell('e')},
		},
	}
	lines := styledOutputLines(b)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines from Cells, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "hi") || !strings.Contains(lines[1], "bye") {
		t.Fatalf("expected lines derived from Cells, got %v", lines)
	}
	if strings.Contains(lines[0], "stale") {
		t.Fatalf("expected Cells to take priority over stale Output, got %v", lines)
	}
}

func TestStyledOutputLines_FallsBackToPlainTextWhenCellsNil(t *testing.T) {
	b := &block.Block{Output: "\x1b[31mred\x1b[0m text\n"} // Cells nil — legacy block
	lines := styledOutputLines(b)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if strings.Contains(lines[0], "\x1b") {
		t.Fatalf("expected raw ANSI escapes to be stripped via PlainText(), got %q", lines[0])
	}
	if !strings.Contains(lines[0], "red text") {
		t.Fatalf("expected stripped text content preserved, got %q", lines[0])
	}
}

func TestStyledOutputLines_EmptyBlockReturnsNil(t *testing.T) {
	b := &block.Block{}
	if lines := styledOutputLines(b); len(lines) != 0 {
		t.Fatalf("expected no lines for an empty block, got %v", lines)
	}
}

func TestBlockLines_WithCells_RendersFromCellsNotStaleOutput(t *testing.T) {
	b := &block.Block{
		Command: "ls",
		Output:  "stale\n",
		Cells:   [][]vt.Cell{{plainCell('f'), plainCell('r'), plainCell('e'), plainCell('s'), plainCell('h')}},
	}
	lines := blockLines(b, 80)
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 output line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "fresh") {
		t.Fatalf("expected output line derived from Cells, got %q", lines[1])
	}
}

func TestActiveLines_WithCells_RendersFromCells(t *testing.T) {
	b := &block.Block{
		Command: "sleep 5",
		Cells:   [][]vt.Cell{{plainCell('r'), plainCell('u'), plainCell('n'), plainCell('n'), plainCell('i'), plainCell('n'), plainCell('g')}},
	}
	lines := activeLines(b, 80)
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 output line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "running") {
		t.Fatalf("expected output line derived from Cells, got %q", lines[1])
	}
}
