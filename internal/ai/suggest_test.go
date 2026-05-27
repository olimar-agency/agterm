package ai

import "testing"

func TestExtractCommand_FencedBlock(t *testing.T) {
	text := "You can check file contents with:\n\n```\ncat main.go\n```\n\nThat will show you the file."
	got := ExtractCommand(text)
	if got != "cat main.go" {
		t.Errorf("got %q", got)
	}
}

func TestExtractCommand_FencedBlockWithLanguage(t *testing.T) {
	text := "Run this:\n```bash\ngit status\n```"
	got := ExtractCommand(text)
	if got != "git status" {
		t.Errorf("got %q", got)
	}
}

func TestExtractCommand_DollarPrefix(t *testing.T) {
	text := "Try running:\n$ ls -la\nThis will list all files."
	got := ExtractCommand(text)
	if got != "ls -la" {
		t.Errorf("got %q", got)
	}
}

func TestExtractCommand_NoCommand(t *testing.T) {
	text := "The error is because of a missing dependency. You should install it first."
	got := ExtractCommand(text)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractCommand_FencedPreferredOverDollar(t *testing.T) {
	text := "$ ls\n\n```\ngit log --oneline\n```"
	got := ExtractCommand(text)
	// fenced block is checked first
	if got != "git log --oneline" {
		t.Errorf("got %q", got)
	}
}

func TestExtractCommand_SkipsCommentLines(t *testing.T) {
	text := "```bash\n# install deps\nnpm install\n```"
	got := ExtractCommand(text)
	if got != "npm install" {
		t.Errorf("got %q", got)
	}
}

func TestExtractCommand_EmptyText(t *testing.T) {
	if got := ExtractCommand(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
