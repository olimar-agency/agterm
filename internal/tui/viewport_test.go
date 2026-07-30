package tui

import (
	"fmt"
	"reflect"
	"testing"
)

// linesN returns n synthetic lines "L0".."L(n-1)", oldest first — matching
// the order flattenLines() produces (oldest block first, active block last).
func linesN(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%d", i)
	}
	return lines
}

func TestViewport_StickyBottom_ZeroOffsetShowsTail(t *testing.T) {
	var v Viewport
	if !v.AtBottom() {
		t.Fatal("zero-value Viewport must start at bottom")
	}
	got := v.Slice(linesN(10), 4)
	want := []string{"L6", "L7", "L8", "L9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slice() = %v, want %v", got, want)
	}
}

func TestViewport_Slice_PadsWhenNotEnoughHistory(t *testing.T) {
	var v Viewport
	got := v.Slice(linesN(2), 5)
	want := []string{"", "", "", "L0", "L1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slice() = %v, want %v", got, want)
	}
}

func TestViewport_PageUp_MovesByViewportMinusOneWithOverlap(t *testing.T) {
	v := Viewport{offset: 0}
	v.PageUp(100, 5) // viewportH-1 = 4, plenty of totalLines to not clamp
	if v.offset != 4 {
		t.Fatalf("offset = %d, want 4 (page size with 1-line overlap)", v.offset)
	}
}

func TestViewport_PageUp_ClampsImmediatelyAgainstTotalLines(t *testing.T) {
	// AtBottom() reports the raw offset field directly, so PageUp must not
	// leave a "phantom" unclamped offset even when there's nothing to
	// scroll into — otherwise an empty or near-empty buffer would report
	// scrolled (and show the scroll indicator) after a single PageUp.
	v := Viewport{offset: 0}
	v.PageUp(0, 23) // empty buffer
	if !v.AtBottom() {
		t.Fatalf("offset = %d, want 0: PageUp on an empty buffer has nothing to scroll into", v.offset)
	}
}

func TestViewport_PageDown_FloorsAtZero(t *testing.T) {
	v := Viewport{offset: 2}
	v.PageDown(5) // would go to -2
	if v.offset != 0 {
		t.Fatalf("offset = %d, want floored to 0", v.offset)
	}
	if !v.AtBottom() {
		t.Fatal("expected AtBottom() true after flooring to 0")
	}
}

func TestViewport_HalfUp_HalfDown(t *testing.T) {
	v := Viewport{offset: 0}
	v.HalfUp(100, 9) // 9/2 = 4
	if v.offset != 4 {
		t.Fatalf("offset after HalfUp = %d, want 4", v.offset)
	}
	v.HalfDown(9)
	if v.offset != 0 {
		t.Fatalf("offset after HalfDown = %d, want 0", v.offset)
	}
}

func TestViewport_HalfUp_HalfDown_EvenHeight(t *testing.T) {
	v := Viewport{offset: 0}
	v.HalfUp(100, 10) // 10/2 = 5, no odd-height rounding to worry about
	if v.offset != 5 {
		t.Fatalf("offset after HalfUp(10) = %d, want 5", v.offset)
	}
	v.HalfDown(10)
	if v.offset != 0 {
		t.Fatalf("offset after HalfDown(10) = %d, want 0", v.offset)
	}
}

func TestViewport_HalfStep_MinimumOfOne(t *testing.T) {
	v := Viewport{offset: 0}
	v.HalfUp(100, 1) // 1/2 = 0, must floor to at least 1 to make progress
	if v.offset != 1 {
		t.Fatalf("offset = %d, want 1 (half-step must never be zero)", v.offset)
	}
}

func TestViewport_JumpTop_ClampsToEarliestFullPage(t *testing.T) {
	v := Viewport{}
	v.JumpTop(10, 4)
	got := v.Slice(linesN(10), 4)
	want := []string{"L0", "L1", "L2", "L3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slice() after JumpTop = %v, want %v", got, want)
	}
}

func TestViewport_JumpTop_EmptyAndSingleLineBuffers(t *testing.T) {
	var v Viewport
	v.JumpTop(0, 4)
	if got := v.Slice(linesN(0), 4); !reflect.DeepEqual(got, []string{"", "", "", ""}) {
		t.Fatalf("Slice() on empty buffer after JumpTop = %v, want all blank", got)
	}

	v = Viewport{}
	v.JumpTop(1, 4)
	got := v.Slice(linesN(1), 4)
	want := []string{"", "", "", "L0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slice() on single-line buffer after JumpTop = %v, want %v", got, want)
	}
}

func TestViewport_JumpBottom_ResetsToSticky(t *testing.T) {
	v := Viewport{offset: 50}
	v.JumpBottom()
	if !v.AtBottom() {
		t.Fatal("expected AtBottom() true after JumpBottom")
	}
}

func TestViewport_Clamp_WhenTotalLinesShrinksBelowOffset(t *testing.T) {
	// Simulates Store eviction: the user was scrolled up into history that
	// no longer exists. Must not panic and must degrade to showing what's
	// left, not crash on a negative slice index.
	v := Viewport{offset: 1000}
	got := v.Slice(linesN(3), 5)
	want := []string{"", "", "L0", "L1", "L2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slice() = %v, want %v", got, want)
	}
}

func TestViewport_Clamp_DoesNotMutateReceiver(t *testing.T) {
	// Slice uses a value receiver on purpose (see viewport.go doc) so that
	// calling it from View() — which itself has a value-receiver Model —
	// never silently drops a clamp back into persisted state.
	v := Viewport{offset: 1000}
	_ = v.Slice(linesN(3), 5)
	if v.offset != 1000 {
		t.Fatalf("offset = %d, want unchanged 1000 (Slice must not mutate)", v.offset)
	}
}

func TestViewport_AboveBottom(t *testing.T) {
	var v Viewport
	if got := v.AboveBottom(100, 10); got != 0 {
		t.Fatalf("AboveBottom() at bottom = %d, want 0", got)
	}

	v.PageUp(100, 10) // offset = 9
	if got := v.AboveBottom(100, 10); got != 9 {
		t.Fatalf("AboveBottom() after PageUp = %d, want 9", got)
	}

	// Must reflect the same clamp Slice() applies, not the raw offset.
	v = Viewport{offset: 1000}
	if got := v.AboveBottom(20, 10); got != 10 {
		t.Fatalf("AboveBottom() with oversized offset = %d, want clamped to 10", got)
	}
}

func TestViewport_Note_NoOpWhenAtBottom(t *testing.T) {
	var v Viewport
	v.Note(10)
	v.Note(20)
	if v.offset != 0 {
		t.Fatalf("offset = %d, want 0 (growth must not move the view while at bottom)", v.offset)
	}
}

func TestViewport_Note_GrowsOffsetToKeepAbsoluteAnchorWhileScrolled(t *testing.T) {
	all := linesN(20)
	v := Viewport{}
	v.Note(len(all))     // baseline total before scrolling
	v.PageUp(len(all), 5) // offset = 4

	before := v.Slice(all, 5)

	// New output streams in: 3 more lines appended at the tail.
	grown := append(append([]string(nil), all...), "L20", "L21", "L22")
	v.Note(len(grown))

	after := v.Slice(grown, 5)

	if !reflect.DeepEqual(before, after) {
		t.Fatalf("view drifted while scrolled: before=%v after=%v, want identical (anchor preserved)", before, after)
	}
}

func TestViewport_Clamp_NegativeOffsetFloorsToZero(t *testing.T) {
	// offset should never go negative through the public API (every mutator
	// floors it), but clamp defends against it directly in case a future
	// caller constructs a Viewport literal with a bad value.
	v := Viewport{offset: -5}
	got := v.Slice(linesN(10), 4)
	want := []string{"L6", "L7", "L8", "L9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slice() with negative offset = %v, want %v (floored to bottom)", got, want)
	}
}

func TestViewport_Slice_ZeroHeightReturnsNil(t *testing.T) {
	var v Viewport
	if got := v.Slice(linesN(5), 0); got != nil {
		t.Fatalf("Slice() with viewportH=0 = %v, want nil", got)
	}
}

func TestViewport_PageStep_MinimumOfOne(t *testing.T) {
	v := Viewport{offset: 0}
	v.PageUp(100, 1) // 1-1 = 0, must floor to at least 1 to make progress
	if v.offset != 1 {
		t.Fatalf("offset = %d, want 1 (page-step must never be zero)", v.offset)
	}
}

func TestViewport_Note_ThenClampHandlesEvictionGracefully(t *testing.T) {
	// Growth compensation (Note) and shrink-clamping (Slice) must compose
	// without panicking even if a Store eviction happens between them.
	v := Viewport{}
	v.Note(500)
	v.PageUp(500, 10)
	v.Note(3) // buffer shrank drastically (eviction), fewer lines than offset
	got := v.Slice(linesN(3), 10)
	if len(got) != 10 {
		t.Fatalf("Slice() returned %d lines, want padded to viewportH=10", len(got))
	}
}
