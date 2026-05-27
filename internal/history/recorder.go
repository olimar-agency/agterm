package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/imattos78/agterm/internal/block"
)

const (
	maxBlocks  = 10_000
	maxAgeDays = 30
)

// Entry is the JSONL schema for one persisted block.
type Entry struct {
	V     int          `json:"v"`
	Block *block.Block `json:"block"`
}

// Recorder appends completed blocks to a JSONL file.
type Recorder struct {
	mu sync.Mutex
	f  *os.File
	w  *bufio.Writer
}

// DefaultPath returns ~/.local/share/agterm/history.jsonl.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agterm", "history.jsonl")
}

// Open creates (or appends to) the history file at path.
func Open(path string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Recorder{f: f, w: bufio.NewWriter(f)}, nil
}

// Append writes a single block as a JSON line.
func (r *Recorder) Append(b *block.Block) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := Entry{V: 1, Block: b}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := r.w.Write(append(data, '\n')); err != nil {
		return err
	}
	return r.w.Flush()
}

// Close flushes and closes the underlying file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.w.Flush() //nolint:errcheck
	return r.f.Close()
}

// Load reads the history file and returns blocks that pass the retention
// policy: not older than maxAgeDays and capped at maxBlocks most recent entries.
// Missing file returns an empty slice without error.
func Load(path string) ([]*block.Block, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)

	var all []*block.Block
	scanner := bufio.NewScanner(f)
	// large output lines can exceed default 64 KiB scanner buffer
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil || e.Block == nil {
			continue
		}
		if e.Block.StartedAt.Before(cutoff) {
			continue
		}
		all = append(all, e.Block)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// keep only the most recent maxBlocks
	if len(all) > maxBlocks {
		all = all[len(all)-maxBlocks:]
	}
	return all, nil
}
