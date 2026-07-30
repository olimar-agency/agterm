package tui

import "math"

// Viewport tracks how far the block-list panel is scrolled up from the
// bottom of the flattened line history that View() renders. offset is the
// number of lines above the live tail the view is currently positioned at;
// offset == 0 means pinned to the bottom, auto-following new output as it
// arrives (sticky-bottom).
//
// State-mutating methods (PageUp, PageDown, HalfUp, HalfDown, JumpTop,
// JumpBottom, Note) use a pointer receiver and must only be called from
// Update(), whose returned value-receiver Model is what Bubbletea persists.
// Slice and AtBottom use a value receiver deliberately: View() also has a
// value-receiver Model, so any mutation performed there would be silently
// discarded when the render returns — Slice reads the current offset and
// projects a window without needing to persist anything back.
type Viewport struct {
	offset    int
	lastTotal int
}

// AtBottom reports whether the view is pinned to the live tail.
func (v Viewport) AtBottom() bool { return v.offset == 0 }

// PageUp/PageDown move by a full page with a 1-line overlap so a page flip
// doesn't lose context. HalfUp/HalfDown move by half a page.
func (v *Viewport) PageUp(viewportH int)   { v.offset += pageStep(viewportH) }
func (v *Viewport) PageDown(viewportH int) { v.offset -= pageStep(viewportH); v.floor() }
func (v *Viewport) HalfUp(viewportH int)   { v.offset += halfStep(viewportH) }
func (v *Viewport) HalfDown(viewportH int) { v.offset -= halfStep(viewportH); v.floor() }

// JumpBottom re-engages sticky-bottom auto-follow.
func (v *Viewport) JumpBottom() { v.offset = 0 }

// JumpTop scrolls as far back as the current history allows. The exact
// bound depends on total line count and viewport height, both only known
// at render time, so this sets an oversized offset that Slice's own
// clamping brings back into range on the next render.
func (v *Viewport) JumpTop() { v.offset = math.MaxInt }

func (v *Viewport) floor() {
	if v.offset < 0 {
		v.offset = 0
	}
}

// Note records the current total flattened line count so growth while
// scrolled up can grow the offset by the same amount, keeping the same
// absolute lines anchored on screen (tmux/less scrollback convention)
// instead of drifting forward to stay a fixed distance behind the live
// tail — which would make it impossible to read output in peace while a
// command keeps streaming.
//
// Required on every ptyMsg (the only event that appends to history) and
// also called before any scroll-away-from-bottom key action, so lastTotal
// is already in sync by the time the next ptyMsg observes real growth —
// otherwise a stale lastTotal makes that growth check see the entire
// existing history as "new" and blow the offset out. See syncViewport in
// model.go, the single call site all of this routes through.
func (v *Viewport) Note(totalLines int) {
	if v.offset > 0 {
		if grew := totalLines - v.lastTotal; grew > 0 {
			v.offset += grew
		}
	}
	v.lastTotal = totalLines
}

// clamp returns the offset bounded to [0, totalLines-viewportH] so the
// view never scrolls past the point where the earliest content fills the
// viewport. It returns a value rather than mutating v so it stays usable
// from a value receiver.
func (v Viewport) clamp(totalLines, viewportH int) int {
	max := totalLines - viewportH
	if max < 0 {
		max = 0
	}
	o := v.offset
	if o > max {
		o = max
	}
	if o < 0 {
		o = 0
	}
	return o
}

// AboveBottom reports how many lines above the live tail the view is
// currently positioned at, clamped to what totalLines/viewportH actually
// allow. Exposed so callers (e.g. the scroll indicator in View()) depend
// only on this stable concept instead of reaching into clamp's internals.
func (v Viewport) AboveBottom(totalLines, viewportH int) int {
	return v.clamp(totalLines, viewportH)
}

// Slice returns the viewportH lines visible at the current scroll
// position, given the full flattened line history, left-padded with blank
// lines when there isn't enough history yet to fill the viewport.
func (v Viewport) Slice(all []string, viewportH int) []string {
	if viewportH <= 0 {
		return nil
	}
	offset := v.clamp(len(all), viewportH)
	end := len(all) - offset
	start := end - viewportH
	if start < 0 {
		start = 0
	}

	out := make([]string, viewportH)
	pad := viewportH - (end - start)
	copy(out[pad:], all[start:end])
	return out
}

func pageStep(viewportH int) int {
	n := viewportH - 1
	if n < 1 {
		n = 1
	}
	return n
}

func halfStep(viewportH int) int {
	n := viewportH / 2
	if n < 1 {
		n = 1
	}
	return n
}
