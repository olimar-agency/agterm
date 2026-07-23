package vt

import (
	"strconv"
	"unicode/utf8"
)

type parserState int

const (
	stateGround parserState = iota
	stateEsc
	stateCSI
	stateString    // inside OSC/DCS/PM/APC payload, terminated by BEL or ST
	stateStringEsc // saw ESC while in stateString, expecting '\\' to close ST
)

// Parser is an incremental ANSI/SGR parser. It is stateful across calls to
// Feed so that a CSI sequence or a multibyte UTF-8 rune split across two PTY
// reads still decodes correctly. Not safe for concurrent use.
type Parser struct {
	state   parserState
	csiBuf  []byte
	utf8Buf []byte
	style   CellStyle
}

// NewParser returns a Parser starting from the default (reset) style.
func NewParser() *Parser {
	return &Parser{state: stateGround, style: DefaultStyle()}
}

// Feed consumes raw PTY bytes and returns the Cells they produce. Bytes
// belonging to an incomplete CSI sequence or an incomplete UTF-8 rune are
// retained internally and completed by a later Feed call. Unsupported or
// malformed escape sequences are silently discarded — they never leak into
// the returned Cells.
func (p *Parser) Feed(data []byte) []Cell {
	var cells []Cell
	for _, c := range data {
		switch p.state {
		case stateGround:
			cells = p.feedGround(c, cells)
		case stateEsc:
			p.feedEsc(c)
		case stateCSI:
			p.feedCSI(c)
		case stateString:
			p.feedString(c)
		case stateStringEsc:
			p.feedStringEsc(c)
		}
	}
	return cells
}

func (p *Parser) feedGround(c byte, cells []Cell) []Cell {
	if c == 0x1B {
		cells = p.flushInvalidUTF8(cells)
		p.state = stateEsc
		return cells
	}

	p.utf8Buf = append(p.utf8Buf, c)
	if !utf8.FullRune(p.utf8Buf) {
		return cells
	}
	r, size := utf8.DecodeRune(p.utf8Buf)
	p.utf8Buf = p.utf8Buf[size:]

	width := uint8(1)
	if r < 0x20 || r == 0x7F {
		width = 0
	}
	return append(cells, Cell{Rune: r, Style: p.style, Width: width})
}

// flushInvalidUTF8 drains any pending, never-completed multibyte sequence —
// this happens when a control byte like ESC interrupts a partial rune.
func (p *Parser) flushInvalidUTF8(cells []Cell) []Cell {
	for range p.utf8Buf {
		cells = append(cells, Cell{Rune: utf8.RuneError, Style: p.style, Width: 1})
	}
	p.utf8Buf = nil
	return cells
}

func (p *Parser) feedEsc(c byte) {
	switch c {
	case '[':
		p.state = stateCSI
		p.csiBuf = p.csiBuf[:0]
	case ']', 'P', '^', '_':
		// OSC / DCS / PM / APC — string-type sequences terminated by BEL or
		// ST (ESC \\). Out of scope for this slice per the contract; consumed
		// and ignored so their payload never leaks into Cells as runes.
		p.state = stateString
	default:
		// Unsupported escape — silently dropped.
		p.state = stateGround
	}
}

// feedString consumes the payload of an OSC/DCS/PM/APC sequence until its
// terminator (BEL, or ESC '\\' i.e. ST), discarding every byte silently.
func (p *Parser) feedString(c byte) {
	switch c {
	case 0x07: // BEL
		p.state = stateGround
	case 0x1B:
		p.state = stateStringEsc
	}
}

func (p *Parser) feedStringEsc(byte) {
	// ST is ESC '\\'; whether or not this byte confirms it, this slice
	// returns to ground rather than reprocessing it as a fresh escape byte.
	p.state = stateGround
}

func (p *Parser) feedCSI(c byte) {
	switch {
	case c >= 0x30 && c <= 0x3F:
		p.csiBuf = append(p.csiBuf, c)
	case c >= 0x40 && c <= 0x7E:
		if c == 'm' {
			p.applySGR(p.csiBuf)
		}
		// Any other final byte (cursor movement, erase, DECSET, ...) is
		// silently ignored in this slice — out of scope until Phase 8.
		p.csiBuf = nil
		p.state = stateGround
	default:
		// Intermediate bytes / malformed sequence — abandon it silently.
		p.csiBuf = nil
		p.state = stateGround
	}
}

// applySGR applies the SGR (Select Graphic Rendition) parameters in buf to
// the current style. Coverage matches the contract in agterm#6:
// 0,1,3,4,7,22,23,24,27,30-37,39,40-47,49. 256-color and truecolor (38/48
// with subparams 5 or 2) fall back to ColorDefault — parsed only enough to
// skip their subparams so trailing params in the same sequence still apply.
func (p *Parser) applySGR(buf []byte) {
	params := parseSGRParams(buf)
	for i := 0; i < len(params); i++ {
		n := params[i]
		switch {
		case n == 0:
			p.style = DefaultStyle()
		case n == 1:
			p.style.Attributes |= AttrBold
		case n == 3:
			p.style.Attributes |= AttrItalic
		case n == 4:
			p.style.Attributes |= AttrUnderline
		case n == 7:
			p.style.Attributes |= AttrReverse
		case n == 22:
			p.style.Attributes &^= AttrBold | AttrDim
		case n == 23:
			p.style.Attributes &^= AttrItalic
		case n == 24:
			p.style.Attributes &^= AttrUnderline
		case n == 27:
			p.style.Attributes &^= AttrReverse
		case n >= 30 && n <= 37:
			p.style.Fg = ColorIndexed{Index: uint8(n - 30)}
		case n == 39:
			p.style.Fg = ColorDefault{}
		case n >= 40 && n <= 47:
			p.style.Bg = ColorIndexed{Index: uint8(n - 40)}
		case n == 49:
			p.style.Bg = ColorDefault{}
		case n == 38 || n == 48:
			// TODO(phase6-slice2): 256-color / truecolor. Fall back to
			// default and skip the subparams so we don't misinterpret them
			// as unrelated SGR codes.
			if n == 38 {
				p.style.Fg = ColorDefault{}
			} else {
				p.style.Bg = ColorDefault{}
			}
			if i+1 < len(params) {
				switch params[i+1] {
				case 5:
					i += 2 // mode + palette index
				case 2:
					i += 4 // mode + r + g + b
				}
			}
		}
		// Any other code is ignored silently.
	}
}

// unparseableSGRParam is a sentinel for a non-empty parameter segment that
// isn't a plain decimal number (e.g. colon-separated subparams like
// "38:2:255:0:0"). It intentionally matches no case in applySGR's switch, so
// it's ignored rather than misread as an SGR reset.
const unparseableSGRParam = -1

// parseSGRParams splits a ';'-separated CSI parameter buffer into ints. An
// empty buffer (bare "\x1b[m") means reset, i.e. a single 0 parameter. An
// empty segment between semicolons (e.g. "\x1b[;1m") also defaults to 0, per
// ECMA-48. A segment that isn't a plain decimal number yields
// unparseableSGRParam instead of silently coercing to 0.
func parseSGRParams(buf []byte) []int {
	if len(buf) == 0 {
		return []int{0}
	}
	var params []int
	start := 0
	for i := 0; i <= len(buf); i++ {
		if i == len(buf) || buf[i] == ';' {
			seg := buf[start:i]
			n := 0
			if len(seg) > 0 {
				v, err := strconv.Atoi(string(seg))
				if err != nil {
					v = unparseableSGRParam
				}
				n = v
			}
			params = append(params, n)
			start = i + 1
		}
	}
	return params
}
