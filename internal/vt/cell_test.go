package vt

import "testing"

func TestCellRoundtripASCII(t *testing.T) {
	c := Cell{Rune: 'a', Style: DefaultStyle(), Width: 1}
	if c.Rune != 'a' || c.Width != 1 {
		t.Fatalf("unexpected cell: %+v", c)
	}
}

func TestCellRoundtripUTF8Multibyte(t *testing.T) {
	for _, r := range []rune{'ñ', '日', '🚀'} {
		c := Cell{Rune: r, Style: DefaultStyle(), Width: 1}
		if c.Rune != r {
			t.Errorf("rune roundtrip failed for %q", r)
		}
	}
}

// TestColorRGBAssignable exercises the forward-compatibility requirement from
// the contract in #6: Phase 8 must be able to introduce ColorRGB without any
// change to Cell or CellStyle.
func TestColorRGBAssignable(t *testing.T) {
	var c Color = ColorRGB{R: 255, G: 0, B: 0}
	style := CellStyle{Fg: c, Bg: ColorDefault{}}
	cell := Cell{Rune: 'x', Style: style, Width: 1}

	rgb, ok := cell.Style.Fg.(ColorRGB)
	if !ok || rgb.R != 255 {
		t.Fatalf("ColorRGB did not round-trip through CellStyle.Fg: %+v", cell.Style.Fg)
	}
}
