package ai

import (
	"fmt"
	"strings"

	"github.com/imattos78/agterm/internal/block"
	"github.com/imattos78/agterm/internal/vt"
)

const (
	maxOutputChars = 4000

	SystemPrompt = `You are an AI assistant embedded in agterm, an agentic terminal.
Help the user understand command output, debug errors, and suggest next steps.
Be concise. Use markdown for code blocks. Reference commands with backticks.
When the situation calls for a concrete action, end your reply with a single
fenced code block containing exactly the command the user should run next.
Some output spans are wrapped as «error: ...», indicating the program styled
them as errors in the terminal. Treat these as high-signal but not authoritative.`
)

// BuildContext formats the last n completed blocks from store into a context
// string for inclusion in an AI prompt. Output per block is truncated to
// maxOutputChars to limit payload size; full output never leaves the machine.
func BuildContext(store *block.Store, n int) string {
	blocks := store.Last(n)
	if len(blocks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Recent terminal session:\n\n")

	for _, b := range blocks {
		fmt.Fprintf(&sb, "$ %s  (exit %d, %.1fs)\n", b.Command, b.ExitCode, b.Duration.Seconds())

		out := strings.TrimRight(b.SemanticText(), "\n")
		if out != "" {
			out = truncateAnnotated(out, maxOutputChars)
			sb.WriteString(out)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// truncateAnnotated cuts s to at most max chars, same as the previous plain
// truncation, but additionally closes an «error: ...» marker left open by
// the cut — otherwise the Provider could receive a span with no closing »,
// which is malformed relative to what SystemPrompt describes (agterm#16).
func truncateAnnotated(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if strings.Count(cut, vt.ErrorMarkerOpen) > strings.Count(cut, vt.ErrorMarkerClose) {
		cut += vt.ErrorMarkerClose
	}
	return cut + "\n[output truncated]"
}

// BuildQuestion combines optional context with the user's question into a
// single message body.
func BuildQuestion(ctx, question string) string {
	if ctx == "" {
		return question
	}
	return ctx + "\n\nUser question: " + question
}
