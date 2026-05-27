package ai

import "strings"

// ExtractCommand parses an AI response for the first suggested shell command.
// It checks, in order:
//  1. The first fenced code block (```...```) — takes its first non-empty line.
//  2. The first line starting with "$ " — returns the rest of the line.
//
// Returns "" if no command is found.
func ExtractCommand(text string) string {
	// 1. fenced code block
	if cmd := extractFenced(text); cmd != "" {
		return cmd
	}
	// 2. $ prefix
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "$ ") {
			cmd := strings.TrimSpace(strings.TrimPrefix(line, "$ "))
			if cmd != "" {
				return cmd
			}
		}
	}
	return ""
}

func extractFenced(text string) string {
	const fence = "```"
	start := strings.Index(text, fence)
	if start == -1 {
		return ""
	}
	// skip the opening fence line (may have a language tag)
	rest := text[start+len(fence):]
	newline := strings.Index(rest, "\n")
	if newline == -1 {
		return ""
	}
	rest = rest[newline+1:]

	end := strings.Index(rest, fence)
	if end == -1 {
		return ""
	}
	block := strings.TrimSpace(rest[:end])
	// take only the first line of the block — multi-line blocks may be scripts;
	// single-line blocks are the typical "run this" suggestion
	lines := strings.Split(block, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			return l
		}
	}
	return ""
}
