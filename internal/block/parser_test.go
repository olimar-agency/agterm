package block

import (
	"testing"

	"github.com/imattos78/agterm/internal/pty"
)

func feed(p *Parser, segs ...pty.Segment) { p.Feed(segs) }

func TestParser_BasicBlock(t *testing.T) {
	store := NewStore(10)
	p := NewParser(store)

	p.StartBlock("ls", "/tmp")
	feed(p,
		pty.Segment{Kind: pty.SegCommandStart},
		pty.Segment{Kind: pty.SegOutput, Data: []byte("file1\nfile2\n")},
		pty.Segment{Kind: pty.SegCommandEnd, ExitCode: 0},
	)

	blocks := store.All()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Command != "ls" {
		t.Errorf("command: got %q want %q", b.Command, "ls")
	}
	if b.ExitCode != 0 {
		t.Errorf("exit code: got %d want 0", b.ExitCode)
	}
	if b.Output != "file1\nfile2\n" {
		t.Errorf("output: got %q", b.Output)
	}
	if b.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

func TestParser_OutputBeforeEndCaptured(t *testing.T) {
	// output that arrives in the same Feed as CommandEnd must be captured
	store := NewStore(10)
	p := NewParser(store)

	p.StartBlock("echo hi", "/")
	feed(p,
		pty.Segment{Kind: pty.SegOutput, Data: []byte("hi\n")},
		pty.Segment{Kind: pty.SegCommandEnd, ExitCode: 0},
		pty.Segment{Kind: pty.SegOutput, Data: []byte("prompt$ ")}, // should be discarded
	)

	blocks := store.All()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Output != "hi\n" {
		t.Errorf("output: got %q want %q", blocks[0].Output, "hi\n")
	}
}

func TestParser_NonZeroExit(t *testing.T) {
	store := NewStore(10)
	p := NewParser(store)

	p.StartBlock("false", "/")
	feed(p, pty.Segment{Kind: pty.SegCommandEnd, ExitCode: 1})

	blocks := store.All()
	if len(blocks) != 1 || blocks[0].ExitCode != 1 {
		t.Fatalf("expected exit 1, got: %+v", blocks)
	}
}

func TestParser_FlushOpenBlock(t *testing.T) {
	store := NewStore(10)
	p := NewParser(store)

	p.StartBlock("sleep 100", "/")
	feed(p, pty.Segment{Kind: pty.SegOutput, Data: []byte("partial")})
	p.Flush()

	blocks := store.All()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 flushed block, got %d", len(blocks))
	}
	if blocks[0].ExitCode != -1 {
		t.Errorf("flushed block should have exit -1, got %d", blocks[0].ExitCode)
	}
}

func TestParser_StoreLimit(t *testing.T) {
	store := NewStore(3)
	p := NewParser(store)

	for i := 0; i < 5; i++ {
		p.StartBlock("cmd", "/")
		feed(p, pty.Segment{Kind: pty.SegCommandEnd, ExitCode: 0})
	}

	if len(store.All()) != 3 {
		t.Errorf("store should cap at 3, got %d", len(store.All()))
	}
}

func TestParser_CRLFNormalised(t *testing.T) {
	store := NewStore(10)
	p := NewParser(store)

	p.StartBlock("cmd", "/")
	feed(p,
		pty.Segment{Kind: pty.SegOutput, Data: []byte("line1\r\nline2\r\n")},
		pty.Segment{Kind: pty.SegCommandEnd, ExitCode: 0},
	)

	if store.All()[0].Output != "line1\nline2\n" {
		t.Errorf("CRLF not normalised: %q", store.All()[0].Output)
	}
}
