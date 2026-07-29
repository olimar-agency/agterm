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

func TestCellStyleEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b CellStyle
		want bool
	}{
		{"both DefaultStyle", DefaultStyle(), DefaultStyle(), true},
		{
			"same ColorIndexed fg", CellStyle{Fg: ColorIndexed{Index: 2}}, CellStyle{Fg: ColorIndexed{Index: 2}}, true,
		},
		{
			"different ColorIndexed fg", CellStyle{Fg: ColorIndexed{Index: 1}}, CellStyle{Fg: ColorIndexed{Index: 2}}, false,
		},
		{
			"same ColorRGB bg", CellStyle{Bg: ColorRGB{R: 1, G: 2, B: 3}}, CellStyle{Bg: ColorRGB{R: 1, G: 2, B: 3}}, true,
		},
		{
			"different Attributes", CellStyle{Attributes: AttrBold}, CellStyle{Attributes: AttrItalic}, false,
		},
		{
			// A CellStyle built without DefaultStyle() leaves Fg/Bg as the
			// nil interface value (not ColorDefault{}) — this must still
			// compare equal to itself, not fall through colorEqual's type
			// switch default case (regression: it did, until fixed).
			"both nil Fg/Bg (literal without DefaultStyle)", CellStyle{}, CellStyle{}, true,
		},
		{
			"nil Fg vs ColorDefault Fg are distinct representations", CellStyle{}, CellStyle{Fg: ColorDefault{}}, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal() = %v, want %v (a=%+v b=%+v)", got, tt.want, tt.a, tt.b)
			}
		})
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
