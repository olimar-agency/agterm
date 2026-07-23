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

// PlainText is the only surface the AI Provider layer should read from a
// Block — it is deterministic and strips all style/color information.
// Truncation policy is the Provider's responsibility, applied to this
// result, never to Cells.
func (b *Block) PlainText() string {
	if b.Cells == nil {
		return ansiEscapeRE.ReplaceAllString(b.Output, "")
	}
	rows := make([]string, len(b.Cells))
	for i, row := range b.Cells {
		var sb strings.Builder
		for _, c := range row {
			sb.WriteRune(c.Rune)
		}
		rows[i] = sb.String()
	}
	return strings.Join(rows, "\n")
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
