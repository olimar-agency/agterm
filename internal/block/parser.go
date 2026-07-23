package block

import (
	"fmt"
	"strings"
	"time"

	"github.com/imattos78/agterm/internal/pty"
	"github.com/imattos78/agterm/internal/vt"
)

// Parser assembles PTY segments into Blocks. Call StartBlock before sending a
// command to the PTY so the block carries the command text; the OSC 133;C
// segment that arrives shortly after confirms execution started.
type Parser struct {
	store   *Store
	active  *Block
	counter int

	// vtParser and rowBuf accumulate the active block's Cells. One vt.Parser
	// per block, so SGR state never leaks across commands (agterm#6).
	vtParser *vt.Parser
	rowBuf   []vt.Cell
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
	p.vtParser = vt.NewParser()
	p.rowBuf = nil
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

				p.feedVT(seg.Data)
			}

		case pty.SegCommandStart:
			// OSC 133;C is a confirmation signal; open a block only if none is open
			if p.active == nil {
				p.StartBlock("", "")
			}

		case pty.SegCommandEnd:
			if p.active != nil {
				p.flushVTRow()
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
		p.flushVTRow()
		p.active.ExitCode = -1
		p.active.Duration = time.Since(p.active.StartedAt)
		p.store.Add(p.active)
		p.active = nil
	}
}

// feedVT feeds raw (pre-normalisation) bytes to the active block's VT
// parser and groups the resulting cells into rows, splitting on '\n' —
// consistent with the CRLF→LF normalisation already applied to Output.
// Bare '\r' is dropped for the same reason.
func (p *Parser) feedVT(data []byte) {
	for _, c := range p.vtParser.Feed(data) {
		switch c.Rune {
		case '\r':
			continue
		case '\n':
			p.active.Cells = append(p.active.Cells, p.rowBuf)
			p.rowBuf = nil
		default:
			p.rowBuf = append(p.rowBuf, c)
		}
	}
}

// flushVTRow appends any pending, not-yet-newline-terminated row to Cells —
// covers output whose last line has no trailing '\n' before the block closes.
func (p *Parser) flushVTRow() {
	if len(p.rowBuf) > 0 {
		p.active.Cells = append(p.active.Cells, p.rowBuf)
		p.rowBuf = nil
	}
}
