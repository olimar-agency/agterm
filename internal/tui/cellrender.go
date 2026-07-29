package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/imattos78/agterm/internal/vt"
)

// renderCells renders one parsed row of vt.Cell as a string with real ANSI
// styling (color, bold, italic, ...), grouping contiguous cells that share a
// Style into a single lipgloss run instead of styling rune by rune.
func renderCells(row []vt.Cell) string {
	var sb strings.Builder
	var run []rune
	var runStyle vt.CellStyle
	haveRun := false

	flush := func() {
		if len(run) == 0 {
			return
		}
		sb.WriteString(lipglossStyleFor(runStyle).Render(string(run)))
		run = run[:0]
	}

	for _, c := range row {
		if c.Width == 0 {
			continue // control character, not renderable — matches Block.PlainText()
		}
		newRun := !haveRun || !c.Style.Equal(runStyle)
		if newRun && haveRun {
			flush()
		}
		if newRun {
			runStyle = c.Style
			haveRun = true
		}
		run = append(run, c.Rune)
	}
	flush()
	return sb.String()
}

// lipglossStyleFor translates a vt.CellStyle into the equivalent lipgloss.Style.
func lipglossStyleFor(s vt.CellStyle) lipgloss.Style {
	ls := lipgloss.NewStyle()
	if c, ok := lipglossColor(s.Fg); ok {
		ls = ls.Foreground(c)
	}
	if c, ok := lipglossColor(s.Bg); ok {
		ls = ls.Background(c)
	}
	if s.Attributes&vt.AttrBold != 0 {
		ls = ls.Bold(true)
	}
	if s.Attributes&vt.AttrDim != 0 {
		ls = ls.Faint(true)
	}
	if s.Attributes&vt.AttrItalic != 0 {
		ls = ls.Italic(true)
	}
	if s.Attributes&vt.AttrUnderline != 0 {
		ls = ls.Underline(true)
	}
	if s.Attributes&vt.AttrBlink != 0 {
		ls = ls.Blink(true)
	}
	if s.Attributes&vt.AttrReverse != 0 {
		ls = ls.Reverse(true)
	}
	if s.Attributes&vt.AttrStrikethrough != 0 {
		ls = ls.Strikethrough(true)
	}
	// AttrHidden has no lipgloss equivalent (concealed text) and the parser
	// never sets it yet (SGR 8 isn't in the Phase 6 contract) — nothing to do.
	return ls
}

// lipglossColor maps a vt.Color to a lipgloss.Color. ok is false for
// vt.ColorDefault (or an unrecognized type), meaning: leave the terminal's
// own default, don't call Foreground/Background at all.
func lipglossColor(c vt.Color) (lipgloss.Color, bool) {
	switch v := c.(type) {
	case vt.ColorIndexed:
		return lipgloss.Color(strconv.Itoa(int(v.Index))), true
	case vt.ColorRGB:
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", v.R, v.G, v.B)), true
	default:
		return "", false
	}
}
