package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	startMarker = "# agterm-start"
	endMarker   = "# agterm-end"
)

// shell hook snippets — use add-zsh-hook for zsh (safe with all plugin managers),
// PROMPT_COMMAND + DEBUG trap for bash, --on-event for fish (additive by design).
var hooksByShell = map[string]string{
	"zsh": `# agterm-start
autoload -Uz add-zsh-hook
_agterm_preexec() { printf '\033]133;C\007'; }
_agterm_precmd()  { printf '\033]133;D;%s\007' "$?"; }
add-zsh-hook preexec _agterm_preexec
add-zsh-hook precmd  _agterm_precmd
# agterm-end`,

	"bash": `# agterm-start
_agterm_preexec() { printf '\033]133;C\007'; }
_agterm_precmd()  { printf '\033]133;D;%s\007' "$?"; }
trap '_agterm_preexec' DEBUG
PROMPT_COMMAND="${PROMPT_COMMAND:+${PROMPT_COMMAND}; }_agterm_precmd"
# agterm-end`,

	"fish": `# agterm-start
function _agterm_preexec --on-event fish_preexec
    printf '\033]133;C\007'
end
function _agterm_postcmd --on-event fish_postexec
    printf '\033]133;D;%s\007' $status
end
# agterm-end`,
}

var rcFileByShell = map[string]string{
	"zsh":  ".zshrc",
	"bash": ".bashrc",
	"fish": ".config/fish/config.fish",
}

func shellName() (string, error) {
	shell := os.Getenv("SHELL")
	for name := range hooksByShell {
		if strings.HasSuffix(shell, name) {
			return name, nil
		}
	}
	return "", fmt.Errorf("unsupported shell %q — supported: zsh, bash, fish", shell)
}

func rcPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, rcFileByShell[name]), nil
}

func runInstall(args []string) error {
	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		}
	}

	name, err := shellName()
	if err != nil {
		return err
	}
	rc, err := rcPath(name)
	if err != nil {
		return err
	}
	hooks := hooksByShell[name]

	existing, err := os.ReadFile(rc)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", rc, err)
	}

	if strings.Contains(string(existing), startMarker) {
		fmt.Printf("agterm hooks already installed in %s\n", rc)
		return nil
	}

	newContent := strings.TrimRight(string(existing), "\n") + "\n\n" + hooks + "\n"

	if dryRun {
		fmt.Printf("-- dry-run: would append to %s --\n%s\n", rc, hooks)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		return fmt.Errorf("creating dirs: %w", err)
	}

	// backup before modifying
	if err := os.WriteFile(rc+".agterm.bak", existing, 0o644); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}
	if err := os.WriteFile(rc, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", rc, err)
	}

	fmt.Printf("agterm hooks installed in %s (backup: %s.agterm.bak)\n", rc, rc)
	fmt.Printf("Restart your shell or run:  source %s\n", rc)
	return nil
}

func runUninstall(args []string) error {
	_ = args
	name, err := shellName()
	if err != nil {
		return err
	}
	rc, err := rcPath(name)
	if err != nil {
		return err
	}

	existing, err := os.ReadFile(rc)
	if err != nil {
		return fmt.Errorf("reading %s: %w", rc, err)
	}
	content := string(existing)

	if !strings.Contains(content, startMarker) {
		fmt.Printf("agterm hooks not found in %s\n", rc)
		return nil
	}

	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start < 0 || end < 0 || end < start {
		return fmt.Errorf("malformed agterm block in %s — remove manually", rc)
	}
	end += len(endMarker)

	before := strings.TrimRight(content[:start], "\n")
	after := strings.TrimLeft(content[end:], "\n")
	var newContent string
	if after != "" {
		newContent = before + "\n" + after
	} else {
		newContent = before + "\n"
	}

	if err := os.WriteFile(rc+".agterm.bak", existing, 0o644); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}
	if err := os.WriteFile(rc, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", rc, err)
	}

	fmt.Printf("agterm hooks removed from %s (backup: %s.agterm.bak)\n", rc, rc)
	return nil
}
