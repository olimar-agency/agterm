package vt

import "strings"

// Annotate produces a semantic-marked string from a block's cell grid,
// wrapping contiguous spans of cells classified as isErrorColor in
// «error: ...» markers. Pure, no I/O, no dependency on block/ai/tui —
// contract closed in agterm#16.
//
// Signature takes [][]Cell (matching Block.Cells' real shape) rather than
// the single-row []Cell sketched in the issue #16 contract: the contract's
// hard rule that "a marker never crosses a newline" only has a natural
// implementation when row boundaries are visible to Annotate itself, and
// Block.Cells is already [][]Cell (sealed in #6) — Block.SemanticText()
// delegates directly with no reshaping needed.
func Annotate(rows [][]Cell) string {
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = annotateRow(row)
	}
	return strings.Join(lines, "\n")
}

// annotateRow never lets an opened marker span past the end of the row —
// the caller joins rows with "\n", so closing here is what guarantees a
// marker never crosses a newline.
func annotateRow(row []Cell) string {
	var sb strings.Builder
	inSpan := false
	for _, c := range row {
		if c.Width == 0 {
			continue // control character, not renderable — matches PlainText()
		}
		isErr := isErrorCell(c)
		switch {
		case isErr && !inSpan:
			sb.WriteString(ErrorMarkerOpen)
			inSpan = true
		case !isErr && inSpan:
			sb.WriteString(ErrorMarkerClose)
			inSpan = false
		}
		sb.WriteRune(c.Rune)
	}
	if inSpan {
		sb.WriteString(ErrorMarkerClose)
	}
	return sb.String()
}

// CountErrorLines returns the number of rows containing at least one cell
// classified as isErrorColor — used to extend the AI auto-trigger beyond
// exit-code-only (agterm#16).
func CountErrorLines(rows [][]Cell) int {
	n := 0
	for _, row := range rows {
		for _, c := range row {
			if c.Width != 0 && isErrorCell(c) {
				n++
				break
			}
		}
	}
	return n
}

// ErrorMarkerOpen/Close are part of Annotate's observable output contract:
// exported so internal/ai/context.go can balance a truncated marker without
// duplicating the literal, and so its SystemPrompt's description of the
// "«error: ...»" format stays tied to a single source of truth. Changing
// either still means updating the SystemPrompt's wording to match.
const (
	ErrorMarkerOpen  = "«error: "
	ErrorMarkerClose = "»"
)

// errorRGBRedMin defines the truecolor "error" rule: the red channel must be
// at least errorRGBRedMin, AND both green and blue must be at most half of
// red (see isErrorColor).
//
// agterm#16's contract specified a fixed additive margin instead (R >
// G+40 && R > B+40), justified as excluding orange/brown — but that formula
// doesn't actually deliver on it: classic orange #FFA500 (255,165,0) still
// satisfies R>G+40 (255>205) and R>B+40 (255>40), so it would be
// misclassified as "error". Caught by TestAnnotate_OrangeIsNotError before
// this shipped. A fixed additive margin doesn't scale with R's magnitude —
// at high brightness it leaves too much room for G. The proportional form
// used here (G,B capped at R/2) preserves every example the contract cited
// (#D70000, #FF5555 both still classify as error — see
// TestAnnotate_TruecolorRedAtThresholdGetsMarker) while actually excluding
// orange. Flagged for Architect review; not reopening the rest of the
// contract, only this one formula.
const errorRGBRedMin = 160

// isErrorCell reports whether c is classified as semantic "error" per the
// agterm#16 contract: only Fg matters, Bg and Bold are ignored, and control
// cells (Width == 0) are never classified (they carry no visible color).
func isErrorCell(c Cell) bool {
	return c.Width != 0 && isErrorColor(c.Style.Fg)
}

// isErrorColor implements the closed classification rule: ANSI red/bright-red
// (indexed 1/9), or a truecolor RGB value dominated by red — see
// errorRGBRedMin's doc for why this uses a proportional dominance check
// instead of the contract's literal fixed-margin formula. Any other color,
// including ColorDefault or an unrecognized future Color variant, is not
// an error.
func isErrorColor(c Color) bool {
	switch v := c.(type) {
	case ColorIndexed:
		return v.Index == 1 || v.Index == 9
	case ColorRGB:
		return int(v.R) >= errorRGBRedMin &&
			int(v.G) <= int(v.R)/2 &&
			int(v.B) <= int(v.R)/2
	default:
		return false
	}
}
