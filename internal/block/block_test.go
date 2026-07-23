package block

import "testing"

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
