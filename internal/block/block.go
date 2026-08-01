package block

import (
	"regexp"
	"strings"
	"time"

	"github.com/imattos78/agterm/internal/vt"
)

type Block struct {
	ID        string
	Command   string
	Output    string
	ExitCode  int
	Duration  time.Duration
	WorkDir   string
	StartedAt time.Time

	// Cells is the styled cell grid, populated by the VT parser (Phase 6+).
	// Coexists with Output during the transition (agterm#6); nil for blocks
	// loaded from history written before this field existed. Not persisted —
	// history.jsonl stores Output only.
	Cells [][]vt.Cell `json:"-"`
}

// ansiEscapeRE matches a CSI sequence per ECMA-48: parameter bytes 0-9:;<=>?
// (0x30-0x3F) followed by any final byte (0x40-0x7E). The parameter class
// must include '?' etc. — private-mode sequences like "\x1b[?25h" — or they
// leak through untouched instead of being stripped, unlike what vt.Parser
// does for freshly-parsed blocks.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-?]*[@-~]`)

// oscDCSRE matches OSC/DCS/PM/APC string-type sequences (ESC ] | ESC P |
// ESC ^ | ESC _ ... terminated by BEL or ST), e.g. an xterm title-set
// "\x1b]0;title\x07". vt.Parser drops these silently for freshly-parsed
// blocks (Cells != nil); this mirrors that for the legacy fallback.
var oscDCSRE = regexp.MustCompile(`\x1b[\]P^_].*?(\x07|\x1b\\)`)

// bareControlRE matches standalone C0 control bytes and DEL that aren't
// part of an escape sequence (e.g. a lone BEL or backspace some program
// wrote to stdout directly) — everything vt.Parser marks Width 0 for the
// same reason (agterm#6). '\n' (0x0A) is excluded: it's the line separator,
// not junk to strip.
var bareControlRE = regexp.MustCompile(`[\x00-\x09\x0B-\x1F\x7F]`)

// PlainText is the only surface the AI Provider layer should read from a
// Block — it is deterministic and strips all style/color information.
// Truncation policy is the Provider's responsibility, applied to this
// result, never to Cells.
func (b *Block) PlainText() string {
	if b.Cells == nil {
		s := ansiEscapeRE.ReplaceAllString(b.Output, "")
		s = oscDCSRE.ReplaceAllString(s, "")
		return bareControlRE.ReplaceAllString(s, "")
	}
	rows := make([]string, len(b.Cells))
	for i, row := range b.Cells {
		var sb strings.Builder
		for _, c := range row {
			if c.Width == 0 {
				// Control character (BEL, backspace, ...) — not a
				// renderable rune; never leak it into AI-facing text.
				continue
			}
			sb.WriteRune(c.Rune)
		}
		rows[i] = sb.String()
	}
	return strings.Join(rows, "\n")
}

// SemanticText returns PlainText's content with spans the terminal styled
// as errors wrapped in «error: ...» markers — an opt-in surface alongside
// PlainText for the AI context builder (agterm#16). PlainText itself is
// untouched and remains the canonical, style-free surface.
//
// Falls back to PlainText() when Cells is nil (e.g. a block loaded from
// history, which persists Output only, not Cells — agterm#6): there is no
// color information left to classify, so no annotation is possible.
func (b *Block) SemanticText() string {
	if b.Cells == nil {
		return b.PlainText()
	}
	return vt.Annotate(b.Cells)
}

// ErrorLineCount returns the number of lines containing at least one cell
// classified as semantic "error" (agterm#16) — used to extend the AI
// auto-trigger beyond exit-code-only. 0 when Cells is nil: no color
// information survives for a block loaded from history.
func (b *Block) ErrorLineCount() int {
	if b.Cells == nil {
		return 0
	}
	return vt.CountErrorLines(b.Cells)
}

type Store struct {
	blocks []*Block
	limit  int
}

func NewStore(limit int) *Store {
	return &Store{limit: limit}
}

func (s *Store) Add(b *Block) {
	s.blocks = append(s.blocks, b)
	if len(s.blocks) > s.limit {
		s.blocks = s.blocks[1:]
	}
}

func (s *Store) Last(n int) []*Block {
	if n >= len(s.blocks) {
		return s.blocks
	}
	return s.blocks[len(s.blocks)-n:]
}

func (s *Store) All() []*Block {
	return s.blocks
}
