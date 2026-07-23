// Package vt implements the ANSI/VT terminal emulator core: the Cell grid
// contract and the incremental parser that produces it. It must not import
// internal/pty or internal/block — Block imports vt, never the reverse
// (contract closed in agterm#6).
package vt

// Attr is a bitset of boolean SGR attributes. Only 8 of 16 bits are used by
// this slice; the rest are headroom for Phase 8 (double underline, overline,
// framed, encircled).
type Attr uint16

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrUnderline
	AttrBlink
	AttrReverse
	AttrHidden
	AttrStrikethrough
)

// Color is a sealed interface so the color model can grow from indexed (this
// slice) to 256-color and truecolor (Phase 8) without changing Cell or Block.
type Color interface{ isColor() }

// ColorDefault means "no color set" / reset.
type ColorDefault struct{}

// ColorIndexed is the 16-color ANSI palette (0-15 in this slice; 16-255 is
// Phase 8, same type).
type ColorIndexed struct{ Index uint8 }

// ColorRGB is 24-bit truecolor, reserved for Phase 8.
type ColorRGB struct{ R, G, B uint8 }

func (ColorDefault) isColor() {}
func (ColorIndexed) isColor() {}
func (ColorRGB) isColor()     {}

// CellStyle groups the SGR attributes applicable to a cell. The zero value
// is not valid on its own — use DefaultStyle() for "no style".
type CellStyle struct {
	Fg         Color
	Bg         Color
	Attributes Attr
}

// DefaultStyle returns the terminal's reset style: default fg/bg, no attributes.
func DefaultStyle() CellStyle {
	return CellStyle{Fg: ColorDefault{}, Bg: ColorDefault{}}
}

// Cell is the minimal renderable unit produced by Parser.Feed.
type Cell struct {
	Rune  rune
	Style CellStyle
	// Width is 1 for a normal cell, 0 for a control character (not yet
	// rendered) — wide-char (CJK/emoji) measurement is a later slice.
	Width uint8
}
