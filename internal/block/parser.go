package block

import (
	"fmt"
	"strings"
	"time"

	"github.com/imattos78/agterm/internal/pty"
)

// Parser assembles PTY segments into Blocks. Call StartBlock before sending a
// command to the PTY so the block carries the command text; the OSC 133;C
// segment that arrives shortly after confirms execution started.
type Parser struct {
	store   *Store
	active  *Block
	counter int
}

// NewParser creates a Parser backed by the given Store.
func NewParser(store *Store) *Parser {
	return &Parser{store: store}
}

// Active returns the block currently accumulating output, or nil when idle.
func (p *Parser) Active() *Block {
	return p.active
}

// StartBlock opens a new block for the given command and working directory.
// Call this just before writing the command bytes to the PTY.
func (p *Parser) StartBlock(cmd, workDir string) {
	p.counter++
	p.active = &Block{
		ID:        fmt.Sprintf("b%d", p.counter),
		Command:   cmd,
		WorkDir:   workDir,
		StartedAt: time.Now(),
	}
}

// Feed processes an ordered slice of segments from the Detector.
// Segments are consumed in order so output before SegCommandEnd is captured.
func (p *Parser) Feed(segs []pty.Segment) {
	for _, seg := range segs {
		switch seg.Kind {
		case pty.SegOutput:
			if p.active != nil {
				// normalise CR LF → LF and strip bare CR to avoid display artifacts
				out := strings.ReplaceAll(string(seg.Data), "\r\n", "\n")
				out = strings.ReplaceAll(out, "\r", "")
				p.active.Output += out
			}

		case pty.SegCommandStart:
			// OSC 133;C is a confirmation signal; open a block only if none is open
			if p.active == nil {
				p.StartBlock("", "")
			}

		case pty.SegCommandEnd:
			if p.active != nil {
				p.active.ExitCode = seg.ExitCode
				p.active.Duration = time.Since(p.active.StartedAt)
				p.store.Add(p.active)
				p.active = nil
			}
		}
	}
}

// Flush closes any open block without a clean exit (e.g. on session close).
func (p *Parser) Flush() {
	if p.active != nil {
		p.active.ExitCode = -1
		p.active.Duration = time.Since(p.active.StartedAt)
		p.store.Add(p.active)
		p.active = nil
	}
}
