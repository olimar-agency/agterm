package tools

import "strings"

// safeCommands is the exact-name whitelist of commands the AI may auto-run
// when auto_run_readonly is enabled in config. Only read-only, non-destructive
// commands are listed. New entries must be manually reviewed.
var safeCommands = map[string]bool{
	"cat":      true,
	"echo":     true,
	"find":     true,
	"git":      true, // git sub-commands are checked separately
	"grep":     true,
	"head":     true,
	"ls":       true,
	"pwd":      true,
	"tail":     true,
	"wc":       true,
	"which":    true,
	"whoami":   true,
	"hostname": true,
	"uname":    true,
	"env":      true,
	"printenv": true,
}

// safeGitSubcmds is the explicit allowlist of git sub-commands that are
// read-only and safe to auto-run. Anything not listed here is blocked.
var safeGitSubcmds = map[string]bool{
	"log":      true,
	"status":   true,
	"diff":     true,
	"show":     true,
	"blame":    true,
	"branch":   true, // read-only listing
	"tag":      true, // read-only listing (no -a, -d etc — those still won't cause harm but block for safety)
	"describe": true,
	"shortlog": true,
	"stash":    true, // stash list is safe; stash pop/drop are not — checked by sub-sub-cmd below
	"ls-files": true,
	"ls-tree":  true,
	"rev-parse": true,
	"config":   true, // read-only with --get; blocked for --set, but config --get is safe
}

// IsWhitelisted reports whether cmd is safe for autonomous execution.
// It checks the first token (binary name) against safeCommands and, for git,
// also validates that the sub-command is not in dangerousGitSubcmds.
func IsWhitelisted(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	parts := strings.Fields(cmd)
	bin := parts[0]

	// strip path prefix (e.g. /usr/bin/ls → ls)
	if idx := strings.LastIndex(bin, "/"); idx >= 0 {
		bin = bin[idx+1:]
	}

	if !safeCommands[bin] {
		return false
	}

	// for git, only allow explicitly whitelisted sub-commands.
	// We scan all tokens after "git" and take the first one that doesn't look
	// like a flag (no leading "-") and isn't obviously a flag argument (i.e., not
	// preceded by a known path-taking flag like -C or --git-dir).
	if bin == "git" {
		sub := gitSubcmd(parts[1:])
		if sub == "" || !safeGitSubcmds[sub] {
			return false
		}
	}

	return true
}

// gitSubcmd finds the git sub-command token from the args after "git".
// It skips flags (leading "-") and their arguments for known path-taking flags
// (-C, --git-dir, --work-tree, --namespace).
func gitSubcmd(args []string) string {
	pathFlags := map[string]bool{
		"-C":          true,
		"--git-dir":   true,
		"--work-tree": true,
		"--namespace": true,
		"--exec-path": true,
	}
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if pathFlags[a] {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}
