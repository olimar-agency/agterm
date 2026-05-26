package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/imattos78/agterm/internal/tui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := runInstall(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "agterm install:", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if err := runUninstall(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "agterm uninstall:", err)
				os.Exit(1)
			}
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\nusage: agterm [install|uninstall]\n", os.Args[1])
			os.Exit(1)
		}
	}

	m, err := tui.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agterm:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "agterm:", err)
		os.Exit(1)
	}
}
