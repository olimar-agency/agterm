package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imattos78/agterm/internal/block"
)

func makeBlock(cmd string, ago time.Duration) *block.Block {
	return &block.Block{
		ID:        cmd,
		Command:   cmd,
		Output:    "output of " + cmd,
		ExitCode:  0,
		Duration:  100 * time.Millisecond,
		StartedAt: time.Now().Add(-ago),
	}
}

func TestRecorder_AppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	rec, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	b1 := makeBlock("ls", 0)
	b2 := makeBlock("git status", 0)
	if err := rec.Append(b1); err != nil {
		t.Fatalf("Append b1: %v", err)
	}
	if err := rec.Append(b2); err != nil {
		t.Fatalf("Append b2: %v", err)
	}
	rec.Close()

	blocks, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Command != "ls" || blocks[1].Command != "git status" {
		t.Errorf("unexpected commands: %q %q", blocks[0].Command, blocks[1].Command)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	blocks, err := Load(filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("expected empty, got %d blocks", len(blocks))
	}
}

func TestLoad_RetentionFiltersByAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	rec, _ := Open(path)
	rec.Append(makeBlock("old-cmd", 31*24*time.Hour)) // older than 30 days
	rec.Append(makeBlock("new-cmd", 1*time.Hour))
	rec.Close()

	blocks, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Command != "new-cmd" {
		t.Errorf("retention filter failed: got %v", blocks)
	}
}

func TestLoad_SkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	// write one valid and one corrupt line
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	f.WriteString(`{"v":1,"block":{"Command":"ls","StartedAt":"` + time.Now().Format(time.RFC3339) + `"}}` + "\n")
	f.WriteString("not valid json\n")
	f.Close()

	blocks, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(blocks) != 1 {
		t.Errorf("expected 1 valid block, got %d", len(blocks))
	}
}
