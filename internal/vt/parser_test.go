package vt

import "testing"

func runesOf(cells []Cell) string {
	rs := make([]rune, len(cells))
	for i, c := range cells {
		rs[i] = c.Rune
	}
	return string(rs)
}

func TestSGRForegroundBasic(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[31mA\x1b[0m"))
	if len(cells) != 1 || cells[0].Rune != 'A' {
		t.Fatalf("expected single cell 'A', got %+v", cells)
	}
	fg, ok := cells[0].Style.Fg.(ColorIndexed)
	if !ok || fg.Index != 1 {
		t.Fatalf("expected ColorIndexed{1} fg, got %+v", cells[0].Style.Fg)
	}
}

func TestSGRBackgroundBasic(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[42mA"))
	bg, ok := cells[0].Style.Bg.(ColorIndexed)
	if !ok || bg.Index != 2 {
		t.Fatalf("expected ColorIndexed{2} bg, got %+v", cells[0].Style.Bg)
	}
}

func TestSGRBoldAndReset(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[1mX\x1b[22mY"))
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(cells))
	}
	if cells[0].Style.Attributes&AttrBold == 0 {
		t.Error("expected first cell bold")
	}
	if cells[1].Style.Attributes&AttrBold != 0 {
		t.Error("expected second cell not bold")
	}
}

func TestSGRCompoundParams(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[1;31;42mZ"))
	c := cells[0]
	if c.Style.Attributes&AttrBold == 0 {
		t.Error("expected bold")
	}
	if fg, ok := c.Style.Fg.(ColorIndexed); !ok || fg.Index != 1 {
		t.Errorf("expected fg ColorIndexed{1}, got %+v", c.Style.Fg)
	}
	if bg, ok := c.Style.Bg.(ColorIndexed); !ok || bg.Index != 2 {
		t.Errorf("expected bg ColorIndexed{2}, got %+v", c.Style.Bg)
	}
}

func TestSGREmptyParamIsReset(t *testing.T) {
	p := NewParser()
	p.Feed([]byte("\x1b[31m"))
	cells := p.Feed([]byte("\x1b[mA"))
	if _, ok := cells[0].Style.Fg.(ColorDefault); !ok {
		t.Fatalf("expected default fg after bare reset, got %+v", cells[0].Style.Fg)
	}
}

func TestSGR256ColorFallback(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[38;5;123mA"))
	if len(cells) != 1 || cells[0].Rune != 'A' {
		t.Fatalf("expected single clean cell 'A', got %+v", cells)
	}
	if _, ok := cells[0].Style.Fg.(ColorDefault); !ok {
		t.Fatalf("expected ColorDefault fallback, got %+v", cells[0].Style.Fg)
	}
}

func TestSGRTruecolorFallback(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[38;2;10;20;30mA"))
	if len(cells) != 1 || cells[0].Rune != 'A' {
		t.Fatalf("expected single clean cell 'A', got %+v", cells)
	}
	if _, ok := cells[0].Style.Fg.(ColorDefault); !ok {
		t.Fatalf("expected ColorDefault fallback, got %+v", cells[0].Style.Fg)
	}
}

func TestSGRTruecolorFallbackThenTrailingParamsApply(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[38;5;123;1;31mA"))
	c := cells[0]
	if _, ok := c.Style.Fg.(ColorIndexed); !ok {
		t.Fatalf("expected trailing '1;31' to apply after truecolor fallback skip, got fg=%+v", c.Style.Fg)
	}
	fg := c.Style.Fg.(ColorIndexed)
	if fg.Index != 1 {
		t.Errorf("expected fg index 1 (red), got %d", fg.Index)
	}
	if c.Style.Attributes&AttrBold == 0 {
		t.Error("expected bold to apply after skipping the 256-color subparams")
	}
}

func TestFragmentedSequenceCrossChunk(t *testing.T) {
	p := NewParser()
	p.Feed([]byte("\x1b["))
	cells := p.Feed([]byte("31mA"))
	if len(cells) != 1 || cells[0].Rune != 'A' {
		t.Fatalf("expected single cell 'A', got %+v", cells)
	}
	fg, ok := cells[0].Style.Fg.(ColorIndexed)
	if !ok || fg.Index != 1 {
		t.Fatalf("fragmented CSI did not apply correctly: %+v", cells[0].Style.Fg)
	}
}

func TestInvalidSequenceIgnoredSilently(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[99zABC"))
	if runesOf(cells) != "ABC" {
		t.Fatalf("expected clean 'ABC', got %q", runesOf(cells))
	}
}

func TestUnsupportedFinalByteIgnored(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[10AZ"))
	if runesOf(cells) != "Z" {
		t.Fatalf("expected clean 'Z' with no extra cells, got %q", runesOf(cells))
	}
}

func TestDECSETIgnoredSilently(t *testing.T) {
	p := NewParser()
	cells := p.Feed([]byte("\x1b[?25hZ"))
	if runesOf(cells) != "Z" {
		t.Fatalf("expected clean 'Z', got %q", runesOf(cells))
	}
}

func TestParserStateSurvivesAcrossFeeds(t *testing.T) {
	p := NewParser()
	p.Feed([]byte("\x1b[1m"))
	cells := p.Feed([]byte("plain"))
	for _, c := range cells {
		if c.Style.Attributes&AttrBold == 0 {
			t.Fatalf("expected bold style to survive across Feed calls, cell %+v", c)
		}
	}
}

func TestFragmentedUTF8CrossChunk(t *testing.T) {
	p := NewParser()
	full := []byte("ñ")
	if len(full) != 2 {
		t.Fatalf("test assumption broken: 'ñ' should be 2 bytes, got %d", len(full))
	}
	p.Feed(full[:1])
	cells := p.Feed(full[1:])
	if len(cells) != 1 || cells[0].Rune != 'ñ' {
		t.Fatalf("expected single rune 'ñ', got %+v", cells)
	}
}

func TestFragmentedEmojiCrossChunk(t *testing.T) {
	p := NewParser()
	full := []byte("🚀")
	var cells []Cell
	for i := 0; i < len(full); i++ {
		cells = append(cells, p.Feed(full[i:i+1])...)
	}
	if len(cells) != 1 || cells[0].Rune != '🚀' {
		t.Fatalf("expected single rune emoji, got %+v", cells)
	}
}
