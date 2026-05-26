package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/imattos78/agterm/internal/block"
	ptyPkg "github.com/imattos78/agterm/internal/pty"
)

// ptyMsg carries ordered segments from one PTY read.
type ptyMsg struct{ segs []ptyPkg.Segment }

// errMsg signals a fatal PTY error.
type errMsg struct{ err error }

// Model is the root Bubbletea model for agterm.
type Model struct {
	shell    *ptyPkg.Shell
	detector *ptyPkg.Detector
	parser   *block.Parser
	store    *block.Store

	input   textinput.Model
	running bool // true while a command is executing in the PTY

	width  int
	height int
	err    error
}

// New constructs a Model, spawning the user's $SHELL.
func New() (Model, error) {
	sh, err := ptyPkg.New("")
	if err != nil {
		return Model{}, fmt.Errorf("spawn shell: %w", err)
	}

	store := block.NewStore(500)
	parser := block.NewParser(store)

	input := textinput.New()
	input.Placeholder = "type a command…"
	input.Focus()
	input.CharLimit = 1024
	input.PromptStyle = promptStyle
	input.Prompt = "❯ "

	return Model{
		shell:    sh,
		detector: &ptyPkg.Detector{},
		parser:   parser,
		store:    store,
		input:    input,
	}, nil
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.readPTY())
}

// readPTY blocks on one PTY read and returns a ptyMsg.
func (m Model) readPTY() tea.Cmd {
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := m.shell.Read(buf)
		if err != nil {
			return errMsg{err}
		}
		return ptyMsg{segs: m.detector.Process(buf[:n])}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 4
		m.shell.Resize(uint16(msg.Height), uint16(msg.Width))

	case ptyMsg:
		m.parser.Feed(msg.segs)
		// detect block completion
		if m.running && m.parser.Active() == nil {
			m.running = false
			m.input.Focus()
		}
		cmds = append(cmds, m.readPTY())

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case tea.KeyMsg:
		if m.running {
			// pass all keys directly to PTY while a command is running
			if b := keyBytes(msg); b != nil {
				m.shell.Write(b) //nolint:errcheck
			}
		} else {
			switch msg.Type {
			case tea.KeyCtrlC, tea.KeyCtrlD:
				m.parser.Flush()
				m.shell.Close()
				return m, tea.Quit

			case tea.KeyEnter:
				cmd := strings.TrimSpace(m.input.Value())
				if cmd != "" {
					wd, _ := os.Getwd()
					m.parser.StartBlock(cmd, wd)
					m.running = true
					m.shell.Write([]byte(cmd + "\r")) //nolint:errcheck
					m.input.SetValue("")
				}

			default:
				var tiCmd tea.Cmd
				m.input, tiCmd = m.input.Update(msg)
				cmds = append(cmds, tiCmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.err != nil {
		return errorStyle.Render("agterm error: "+m.err.Error()) + "\n"
	}
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// bottom bar: 1 line for input / status
	contentLines := m.height - 1

	// render all completed blocks
	var lines []string
	for _, b := range m.store.All() {
		lines = append(lines, blockLines(b, m.width)...)
	}
	// render the active (running) block
	if active := m.parser.Active(); active != nil {
		lines = append(lines, activeLines(active, m.width)...)
	}

	// keep only the last contentLines lines so we don't overflow the screen
	if len(lines) > contentLines {
		lines = lines[len(lines)-contentLines:]
	}
	// pad top with blank lines so the content stays pinned to the bottom
	for len(lines) < contentLines {
		lines = append([]string{""}, lines...)
	}

	content := strings.Join(lines, "\n")

	var bar string
	if m.running {
		bar = statusStyle.Render("running…")
	} else {
		bar = m.input.View()
	}

	return content + "\n" + bar
}

// blockLines renders a completed Block into display lines.
func blockLines(b *block.Block, width int) []string {
	exit := exitCodeStyle(b.ExitCode).Render(fmt.Sprintf("[%d]", b.ExitCode))
	dur := dimStyle.Render(fmt.Sprintf("%.1fs", b.Duration.Seconds()))
	left := promptStyle.Render("❯ ") + cmdStyle.Render(b.Command)
	right := exit + " " + dur

	pad := width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if pad < 1 {
		pad = 1
	}
	header := left + strings.Repeat(" ", pad) + right

	if b.Output == "" {
		return []string{header}
	}
	out := strings.TrimRight(b.Output, "\n")
	outputLines := strings.Split(outputStyle.Render(out), "\n")
	return append([]string{header}, outputLines...)
}

// activeLines renders the in-progress block.
func activeLines(b *block.Block, width int) []string {
	header := promptStyle.Render("❯ ") + cmdStyle.Render(b.Command) + " " + dimStyle.Render("…")
	_ = width
	if b.Output == "" {
		return []string{header}
	}
	out := strings.TrimRight(b.Output, "\n")
	outputLines := strings.Split(outputStyle.Render(out), "\n")
	return append([]string{header}, outputLines...)
}

// keyBytes translates a Bubbletea key message into the bytes the PTY expects.
func keyBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{127}
	case tea.KeyDelete:
		return []byte{27, '[', '3', '~'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyUp:
		return []byte{27, '[', 'A'}
	case tea.KeyDown:
		return []byte{27, '[', 'B'}
	case tea.KeyRight:
		return []byte{27, '[', 'C'}
	case tea.KeyLeft:
		return []byte{27, '[', 'D'}
	case tea.KeyHome:
		return []byte{27, '[', 'H'}
	case tea.KeyEnd:
		return []byte{27, '[', 'F'}
	case tea.KeyCtrlA:
		return []byte{1}
	case tea.KeyCtrlC:
		return []byte{3}
	case tea.KeyCtrlD:
		return []byte{4}
	case tea.KeyCtrlE:
		return []byte{5}
	case tea.KeyCtrlK:
		return []byte{11}
	case tea.KeyCtrlL:
		return []byte{12}
	case tea.KeyCtrlU:
		return []byte{21}
	case tea.KeyCtrlW:
		return []byte{23}
	case tea.KeyEsc:
		return []byte{27}
	default:
		return nil
	}
}
