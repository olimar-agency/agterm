package tui

// Viewport tracks how far the block-list panel is scrolled up from the
// bottom of the flattened line history that View() renders. offset is the
// number of lines above the live tail the view is currently positioned at;
// offset == 0 means pinned to the bottom, auto-following new output as it
// arrives (sticky-bottom).
//
// State-mutating methods (PageUp, PageDown, HalfUp, HalfDown, JumpTop,
// JumpBottom, Note) use a pointer receiver and must only be called from
// Update(), whose returned value-receiver Model is what Bubbletea persists.
// PageUp/HalfUp/JumpTop — the "move away from bottom" operations — take
// totalLines so they can clamp offset immediately: AtBottom() reports the
// raw offset field directly (not a render-time projection of it), so an
// unclamped offset would report "scrolled" even when there's nothing to
// scroll into (e.g. PageUp on an empty buffer) and Slice() would render an
// indistinguishable-from-bottom view under a spurious "scrolled" indicator.
//
// Slice, AtBottom, AboveBottom use a value receiver deliberately: View()
// also has a value-receiver Model, so any mutation performed there would
// be silently discarded when the render returns — these read the current
// offset and project a result without needing to persist anything back.
// They still defensively re-clamp against whatever totalLines/viewportH
// are current at call time (which can differ from what a mutator saw —
// e.g. the AI panel toggling blockH, or a Store eviction, with no
// intervening scroll keypress), so an offset that was valid when set stays
// valid to render even if the world moved under it.
type Viewport struct {
	offset    int
	lastTotal int
}

// AtBottom reports whether the view is pinned to the live tail.
func (v Viewport) AtBottom() bool { return v.offset == 0 }

// PageUp scrolls up by a full page (with a 1-line overlap so a page flip
// doesn't lose context), clamped to what totalLines/viewportH allow.
func (v *Viewport) PageUp(totalLines, viewportH int) {
	v.offset += pageStep(viewportH)
	v.clampInPlace(totalLines, viewportH)
}

// HalfUp scrolls up by half a page, clamped like PageUp.
func (v *Viewport) HalfUp(totalLines, viewportH int) {
	v.offset += halfStep(viewportH)
	v.clampInPlace(totalLines, viewportH)
}

// PageDown/HalfDown move back toward the bottom. They never need a
// totalLines-aware clamp: moving toward 0 can't overshoot past valid
// range, only below it, which floor() already guards.
func (v *Viewport) PageDown(viewportH int) { v.offset -= pageStep(viewportH); v.floor() }
func (v *Viewport) HalfDown(viewportH int) { v.offset -= halfStep(viewportH); v.floor() }

// JumpBottom re-engages sticky-bottom auto-follow.
func (v *Viewport) JumpBottom() { v.offset = 0 }

// JumpTop scrolls as far back as totalLines/viewportH allow.
func (v *Viewport) JumpTop(totalLines, viewportH int) {
	v.offset = totalLines
	v.clampInPlace(totalLines, viewportH)
}

func (v *Viewport) floor() {
	if v.offset < 0 {
		v.offset = 0
	}
}

// clampInPlace bounds offset to [0, totalLines-viewportH] and persists the
// result — used by the mutators above, which run only from Update() where
// persisting matters. The value-receiver clamp() further down serves a
// different purpose: it re-projects an offset that may have gone stale
// due to a state change with no accompanying mutator call (e.g. the AI
// panel toggling blockHeight, or a Store eviction), on every read, without
// ever touching a value-receiver Model's persisted state.
func (v *Viewport) clampInPlace(totalLines, viewportH int) {
	max := totalLines - viewportH
	if max < 0 {
		max = 0
	}
	if v.offset > max {
		v.offset = max
	}
	v.floor()
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
