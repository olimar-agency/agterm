package vt

import "testing"

// cell builds a single-width Cell with the given rune and foreground color,
// bg/attributes left zero-valued (irrelevant to error classification).
func cell(r rune, fg Color) Cell {
	return Cell{Rune: r, Style: CellStyle{Fg: fg}, Width: 1}
}

func plainRow(s string) []Cell {
	row := make([]Cell, len(s))
	for i, r := range s {
		row[i] = cell(r, ColorDefault{})
	}
	return row
}

func TestAnnotate_UnstyledCellsProduceNoMarkers(t *testing.T) {
	got := Annotate([][]Cell{plainRow("hello world")})
	want := "hello world"
	if got != want {
		t.Fatalf("Annotate() = %q, want %q", got, want)
	}
}

func TestAnnotate_IndexedRedSpanGetsOneMarker(t *testing.T) {
	row := []Cell{
		cell('o', ColorDefault{}),
		cell('k', ColorDefault{}),
		cell(' ', ColorDefault{}),
		cell('b', ColorIndexed{Index: 1}),
		cell('a', ColorIndexed{Index: 1}),
		cell('d', ColorIndexed{Index: 1}),
	}
	got := Annotate([][]Cell{row})
	want := "ok " + ErrorMarkerOpen + "bad" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want %q", got, want)
	}
}

func TestAnnotate_BrightRedIndexAlsoClassifiesAsError(t *testing.T) {
	row := []Cell{cell('x', ColorIndexed{Index: 9})}
	got := Annotate([][]Cell{row})
	want := ErrorMarkerOpen + "x" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want %q", got, want)
	}
}

func TestAnnotate_TruecolorRedAtThresholdGetsMarker(t *testing.T) {
	// R=200 dominates G=10,B=10 by well over errorRGBDominance(40), and
	// R >= errorRGBRedMin(160).
	row := []Cell{cell('x', ColorRGB{R: 200, G: 10, B: 10})}
	got := Annotate([][]Cell{row})
	want := ErrorMarkerOpen + "x" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want %q", got, want)
	}
}

func TestAnnotate_TruecolorJustBelowThresholdGetsNoMarker(t *testing.T) {
	tests := []struct {
		name string
		c    ColorRGB
	}{
		{"red channel below errorRGBRedMin", ColorRGB{R: 159, G: 0, B: 0}},
		{"not enough dominance over green", ColorRGB{R: 200, G: 165, B: 10}},
		{"not enough dominance over blue", ColorRGB{R: 200, G: 10, B: 165}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := []Cell{cell('x', tt.c)}
			got := Annotate([][]Cell{row})
			if got != "x" {
				t.Fatalf("Annotate() = %q, want unmarked %q", got, "x")
			}
		})
	}
}

func TestAnnotate_OrangeIsNotError(t *testing.T) {
	// Classic orange: high red, but green isn't suppressed enough to read
	// as "red-dominant" — must not be classified as error.
	row := []Cell{cell('x', ColorRGB{R: 255, G: 165, B: 0})}
	got := Annotate([][]Cell{row})
	if got != "x" {
		t.Fatalf("Annotate() = %q, want unmarked %q (orange is not error)", got, "x")
	}
}

func TestAnnotate_BoldDoesNotChangeClassification(t *testing.T) {
	plain := Cell{Rune: 'x', Style: CellStyle{Fg: ColorIndexed{Index: 1}}, Width: 1}
	bold := Cell{Rune: 'x', Style: CellStyle{Fg: ColorIndexed{Index: 1}, Attributes: AttrBold}, Width: 1}

	want := ErrorMarkerOpen + "x" + ErrorMarkerClose
	if got := Annotate([][]Cell{{plain}}); got != want {
		t.Fatalf("plain red Annotate() = %q, want %q", got, want)
	}
	if got := Annotate([][]Cell{{bold}}); got != want {
		t.Fatalf("bold red Annotate() = %q, want %q (bold must not change classification)", got, want)
	}
}

func TestAnnotate_BackgroundColorIsIgnored(t *testing.T) {
	// Red background, default (unset) foreground — must NOT classify as
	// error; the contract says only Fg matters.
	c := Cell{Rune: 'x', Style: CellStyle{Fg: ColorDefault{}, Bg: ColorIndexed{Index: 1}}, Width: 1}
	got := Annotate([][]Cell{{c}})
	if got != "x" {
		t.Fatalf("Annotate() = %q, want unmarked %q (Bg must be ignored)", got, "x")
	}
}

func TestAnnotate_ContiguousErrorCellsFuseIntoOneMarker(t *testing.T) {
	row := []Cell{
		cell('a', ColorIndexed{Index: 1}),
		cell('b', ColorIndexed{Index: 1}),
		cell('c', ColorIndexed{Index: 9}), // still "error", different exact color
	}
	got := Annotate([][]Cell{row})
	want := ErrorMarkerOpen + "abc" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want a single fused marker %q", got, want)
	}
}

func TestAnnotate_SeparateErrorSpansProduceSeparateMarkers(t *testing.T) {
	row := []Cell{
		cell('a', ColorIndexed{Index: 1}),
		cell(' ', ColorDefault{}),
		cell('b', ColorIndexed{Index: 1}),
	}
	got := Annotate([][]Cell{row})
	want := ErrorMarkerOpen + "a" + ErrorMarkerClose + " " + ErrorMarkerOpen + "b" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want %q", got, want)
	}
}

func TestAnnotate_NewlineWithinASpanProducesTwoMarkers(t *testing.T) {
	// Two rows, both entirely red — a marker must never cross the "\n"
	// that joins rows, per the closed contract (agterm#16).
	row1 := []Cell{cell('a', ColorIndexed{Index: 1})}
	row2 := []Cell{cell('b', ColorIndexed{Index: 1})}

	got := Annotate([][]Cell{row1, row2})
	want := ErrorMarkerOpen + "a" + ErrorMarkerClose + "\n" + ErrorMarkerOpen + "b" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want two separate markers across the newline: %q", got, want)
	}
}

func TestAnnotate_ControlCellsAreSkippedLikePlainText(t *testing.T) {
	row := []Cell{
		cell('a', ColorIndexed{Index: 1}),
		{Rune: 0, Style: CellStyle{Fg: ColorIndexed{Index: 1}}, Width: 0}, // control cell
		cell('b', ColorIndexed{Index: 1}),
	}
	got := Annotate([][]Cell{row})
	want := ErrorMarkerOpen + "ab" + ErrorMarkerClose
	if got != want {
		t.Fatalf("Annotate() = %q, want control cell skipped and span still fused: %q", got, want)
	}
}

func TestCountErrorLines(t *testing.T) {
	mixed := []Cell{cell('o', ColorDefault{}), cell('k', ColorIndexed{Index: 1})}
	pureRed := []Cell{cell('x', ColorIndexed{Index: 9}), cell('y', ColorRGB{R: 200, G: 0, B: 0})}
	clean := plainRow("no errors here")

	got := CountErrorLines([][]Cell{mixed, pureRed, clean, mixed})
	want := 3 // mixed, pureRed, mixed — clean doesn't count
	if got != want {
		t.Fatalf("CountErrorLines() = %d, want %d", got, want)
	}
}

func TestCountErrorLines_NoErrorLines(t *testing.T) {
	got := CountErrorLines([][]Cell{plainRow("ok"), plainRow("also ok")})
	if got != 0 {
		t.Fatalf("CountErrorLines() = %d, want 0", got)
	}
}

func TestCountErrorLines_EmptyInput(t *testing.T) {
	if got := CountErrorLines(nil); got != 0 {
		t.Fatalf("CountErrorLines(nil) = %d, want 0", got)
	}
}
