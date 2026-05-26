package pty

import (
	"testing"
)

func segs(d *Detector, raw string) []Segment {
	return d.Process([]byte(raw))
}

func TestDetector_PlainOutput(t *testing.T) {
	d := &Detector{}
	got := segs(d, "hello world\n")
	if len(got) != 1 || got[0].Kind != SegOutput || string(got[0].Data) != "hello world\n" {
		t.Fatalf("unexpected segments: %v", got)
	}
}

func TestDetector_CommandStartEnd(t *testing.T) {
	d := &Detector{}
	input := "output\x1b]133;C\x07middle\x1b]133;D;0\x07prompt"
	got := segs(d, input)

	want := []struct {
		kind SegKind
		data string
		exit int
	}{
		{SegOutput, "output", 0},
		{SegCommandStart, "", 0},
		{SegOutput, "middle", 0},
		{SegCommandEnd, "", 0},
		{SegOutput, "prompt", 0},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d segments, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Kind != w.kind {
			t.Errorf("seg[%d] kind: got %v want %v", i, g.Kind, w.kind)
		}
		if w.data != "" && string(g.Data) != w.data {
			t.Errorf("seg[%d] data: got %q want %q", i, g.Data, w.data)
		}
		if w.kind == SegCommandEnd && g.ExitCode != w.exit {
			t.Errorf("seg[%d] exitCode: got %d want %d", i, g.ExitCode, w.exit)
		}
	}
}

func TestDetector_NonZeroExit(t *testing.T) {
	d := &Detector{}
	got := segs(d, "\x1b]133;D;127\x07")
	if len(got) != 1 || got[0].Kind != SegCommandEnd || got[0].ExitCode != 127 {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDetector_SplitBuffer(t *testing.T) {
	d := &Detector{}
	// split the OSC sequence across two reads
	p1 := segs(d, "before\x1b]133")
	p2 := segs(d, ";C\x07after")

	all := append(p1, p2...)
	kinds := make([]SegKind, len(all))
	for i, s := range all {
		kinds[i] = s.Kind
	}

	want := []SegKind{SegOutput, SegCommandStart, SegOutput}
	if len(kinds) != len(want) {
		t.Fatalf("got kinds %v, want %v (segs: %+v)", kinds, want, all)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kind[%d]: got %v want %v", i, kinds[i], want[i])
		}
	}
}

func TestDetector_UnknownVariantStripped(t *testing.T) {
	d := &Detector{}
	// OSC 133;A (prompt start) should be stripped, not emitted
	got := segs(d, "before\x1b]133;A\x07after")
	if len(got) != 2 {
		t.Fatalf("expected 2 output segments, got %d: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Kind != SegOutput {
			t.Errorf("expected SegOutput, got %v", s.Kind)
		}
	}
}
