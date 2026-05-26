package tui

import "github.com/charmbracelet/lipgloss"

var (
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	cmdStyle     = lipgloss.NewStyle().Bold(true)
	outputStyle  = lipgloss.NewStyle().PaddingLeft(2)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
)

func exitCodeStyle(code int) lipgloss.Style {
	if code == 0 {
		return successStyle
	}
	return errorStyle
}
