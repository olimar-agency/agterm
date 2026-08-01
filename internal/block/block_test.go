package block

import (
	"testing"

	"github.com/imattos78/agterm/internal/vt"
)

// TestPlainText_LegacyStripsPrivateModeSequences guards the legacy fallback
// path (Cells == nil, e.g. a block loaded from history.jsonl) against CSI
// private-mode sequences — such as "\x1b[?25h" (cursor show/hide) — leaking
// through unstripped. The VT parser already drops these for freshly parsed
// blocks; the regex fallback must match that behavior.
func TestPlainText_LegacyStripsPrivateModeSequences(t *testing.T) {
	b := &Block{Output: "\x1b[?25hHello\x1b[?25l world\n"}
	got := b.PlainText()
	want := "Hello world\n"
	if got != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
}

// TestPlainText_LegacyStripsOSCSequences guards the legacy fallback against
// OSC/DCS payloads (e.g. an xterm title-set "\x1b]0;title\x07") leaking
// through unstripped — vt.Parser already drops these for freshly-parsed
// blocks; the regex fallback must match that behavior.
func TestPlainText_LegacyStripsOSCSequences(t *testing.T) {
	b := &Block{Output: "\x1b]0;my title\x07Hello world\n"}
	got := b.PlainText()
	want := "Hello world\n"
	if got != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
}

// TestPlainText_LegacyStripsBareControlChars guards the legacy fallback
// against standalone C0 control bytes (BEL, backspace, ...) that some
// program wrote to stdout directly, outside of any escape sequence — these
// aren't renderable text and shouldn't reach the AI provider layer. '\n'
// must survive since it's the line separator, not junk.
func TestPlainText_LegacyStripsBareControlChars(t *testing.T) {
	b := &Block{Output: "AB\x07\x08CD\nEF\n"}
	got := b.PlainText()
	want := "ABCD\nEF\n"
	if got != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
}

// TestSemanticText_DelegatesToVTAnnotate confirms Block.SemanticText wraps
// vt.Annotate's own «error: ...» marker around cells styled as error,
// without duplicating vt's classification logic here (agterm#16). Full
// classification-rule coverage lives in internal/vt/inject_test.go.
func TestSemanticText_DelegatesToVTAnnotate(t *testing.T) {
	b := &Block{Cells: [][]vt.Cell{
		{
			{Rune: 'o', Style: vt.DefaultStyle(), Width: 1},
			{Rune: 'k', Style: vt.DefaultStyle(), Width: 1},
			{Rune: ' ', Style: vt.DefaultStyle(), Width: 1},
			{Rune: 'x', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 1}}, Width: 1},
		},
	}}
	got := b.SemanticText()
	want := "ok " + vt.ErrorMarkerOpen + "x" + vt.ErrorMarkerClose
	if got != want {
		t.Errorf("SemanticText() = %q, want %q", got, want)
	}
}

// TestSemanticText_FallsBackToPlainTextWhenCellsIsNil covers a block loaded
// from history.jsonl (Output only, no Cells persisted — agterm#6): there is
// no color information left to classify, so SemanticText must degrade to
// PlainText rather than panic or silently drop the block's text.
func TestSemanticText_FallsBackToPlainTextWhenCellsIsNil(t *testing.T) {
	b := &Block{Output: "plain history text\n"}
	got := b.SemanticText()
	want := b.PlainText()
	if got != want {
		t.Errorf("SemanticText() = %q, want it to fall back to PlainText() = %q", got, want)
	}
	if got != "plain history text\n" {
		t.Errorf("SemanticText() = %q, want %q", got, "plain history text\n")
	}
}

// TestErrorLineCount_DelegatesToVT confirms the wiring to
// vt.CountErrorLines; the counting rule itself is covered in
// internal/vt/inject_test.go.
func TestErrorLineCount_DelegatesToVT(t *testing.T) {
	b := &Block{Cells: [][]vt.Cell{
		{{Rune: 'x', Style: vt.CellStyle{Fg: vt.ColorIndexed{Index: 1}}, Width: 1}},
		{{Rune: 'o', Style: vt.DefaultStyle(), Width: 1}},
	}}
	if got := b.ErrorLineCount(); got != 1 {
		t.Errorf("ErrorLineCount() = %d, want 1", got)
	}
}

// TestErrorLineCount_ZeroWhenCellsIsNil mirrors the SemanticText fallback:
// a history-loaded block has no color information to classify from.
func TestErrorLineCount_ZeroWhenCellsIsNil(t *testing.T) {
	b := &Block{Output: "no cells here\n"}
	if got := b.ErrorLineCount(); got != 0 {
		t.Errorf("ErrorLineCount() = %d, want 0", got)
	}
}
