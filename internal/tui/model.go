package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/imattos78/agterm/internal/ai"
	_ "github.com/imattos78/agterm/internal/ai/anthropic"
	_ "github.com/imattos78/agterm/internal/ai/gemini"
	_ "github.com/imattos78/agterm/internal/ai/ollama"
	_ "github.com/imattos78/agterm/internal/ai/openai"
	"github.com/imattos78/agterm/internal/ai/tools"
	"github.com/imattos78/agterm/internal/block"
	"github.com/imattos78/agterm/internal/config"
	"github.com/imattos78/agterm/internal/history"
	ptyPkg "github.com/imattos78/agterm/internal/pty"
)

// ── tea messages ─────────────────────────────────────────────────────────────

type ptyMsg struct{ segs []ptyPkg.Segment }
type errMsg struct{ err error }
type aiChunkMsg ai.StreamResult

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	// shell
	shell    *ptyPkg.Shell
	detector *ptyPkg.Detector
	parser   *block.Parser
	store    *block.Store

	// terminal input
	input   textinput.Model
	running bool

	// AI panel
	provider     ai.Provider
	sendContext  bool
	aiOpen       bool
	aiInput      textinput.Model
	aiResponse   string
	aiStreaming  bool
	aiError      string
	aiCh         <-chan ai.StreamResult
	aiCancel     context.CancelFunc
	suggestedCmd string // non-empty when AI has proposed a command

	// history
	recorder *history.Recorder

	autoRunReadonly bool

	// layout
	width  int
	height int
	err    error
}

const aiPanelHeight = 12 // lines reserved for AI panel when open

// New constructs the Model, loading config and wiring the AI provider.
func New() (Model, error) {
	sh, err := ptyPkg.New("")
	if err != nil {
		return Model{}, fmt.Errorf("spawn shell: %w", err)
	}

	store := block.NewStore(500)

	// command input
	cmdInput := textinput.New()
	cmdInput.Placeholder = "type a command…"
	cmdInput.Focus()
	cmdInput.CharLimit = 1024
	cmdInput.PromptStyle = promptStyle
	cmdInput.Prompt = "❯ "

	// AI question input
	aiIn := textinput.New()
	aiIn.Placeholder = "ask AI…"
	aiIn.CharLimit = 512
	aiIn.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	aiIn.Prompt = "AI ❯ "

	m := Model{
		shell:    sh,
		detector: &ptyPkg.Detector{},
		parser:   block.NewParser(store),
		store:    store,
		input:    cmdInput,
		aiInput:  aiIn,
	}

	// load config and wire provider (non-fatal if config missing or provider unavailable)
	cfg, err := config.Load()
	if err == nil {
		m.provider, m.sendContext = buildProvider(cfg)
		m.autoRunReadonly = cfg.AutoRunReadonly
	}

	// load persistent history into the in-memory store (non-fatal)
	if historyPath, err := history.DefaultPath(); err == nil {
		if blocks, err := history.Load(historyPath); err == nil {
			for _, b := range blocks {
				store.Add(b)
			}
		}

		// open history recorder for append (non-fatal)
		if rec, err := history.Open(historyPath); err == nil {
			m.recorder = rec
		}
	}

	return m, nil
}

// buildProvider returns the configured Provider and sendContext flag.
// Returns nil provider if the active provider can't be initialised.
func buildProvider(cfg config.Config) (ai.Provider, bool) {
	if cfg.LocalOnly {
		return nil, false
	}
	name, pcfg, ok := cfg.ActiveProvider()
	if !ok {
		return nil, false
	}
	p, err := ai.Build(name, pcfg.APIKey, pcfg.BaseURL, pcfg.Model)
	if err != nil {
		return nil, false
	}
	return p, pcfg.SendContext
}

// switchProvider changes the active provider at runtime without restarting.
// It returns an error string suitable for display, or "" on success.
func (m *Model) switchProvider(name string) string {
	cfg, err := config.Load()
	if err != nil {
		return "could not reload config: " + err.Error()
	}
	pcfg := cfg.Providers[name]
	p, err := ai.Build(name, pcfg.APIKey, pcfg.BaseURL, pcfg.Model)
	if err != nil {
		return err.Error()
	}
	m.provider = p
	m.sendContext = pcfg.SendContext
	return ""
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.readPTY())
}

// ── PTY read ──────────────────────────────────────────────────────────────────

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

// ── AI streaming ──────────────────────────────────────────────────────────────

func readNextAI(ch <-chan ai.StreamResult) tea.Cmd {
	return func() tea.Msg {
		return aiChunkMsg(<-ch)
	}
}

func (m *Model) startStream(question string) tea.Cmd {
	if m.provider == nil {
		m.aiError = "No AI provider configured — add one to ~/.config/agterm/config.json"
		m.aiStreaming = false
		return nil
	}

	m.aiStreaming = true
	m.aiResponse = ""
	m.aiError = ""

	var ctx string
	if m.sendContext {
		ctx = ai.BuildContext(m.store, 10)
	}

	req := ai.Request{
		System: ai.SystemPrompt,
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: ai.BuildQuestion(ctx, question)},
		},
	}

	cancelCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	m.aiCh = m.provider.Stream(cancelCtx, req)
	m.aiCancel = cancel
	return readNextAI(m.aiCh)
}

func (m *Model) shutdown() {
	if m.aiCancel != nil {
		m.aiCancel()
		m.aiCancel = nil
	}
	m.parser.Flush()
	if m.recorder != nil {
		if err := m.recorder.Close(); err != nil && m.err == nil {
			m.err = fmt.Errorf("close history recorder: %w", err)
		}
		m.recorder = nil
	}
	if m.shell != nil {
		if err := m.shell.Close(); err != nil && m.err == nil {
			m.err = fmt.Errorf("close shell: %w", err)
		}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 4
		m.aiInput.Width = msg.Width - 8
		m.shell.Resize(uint16(msg.Height), uint16(msg.Width))

	case ptyMsg:
		m.parser.Feed(msg.segs)
		if m.running && m.parser.Active() == nil {
			m.running = false
			if !m.aiOpen {
				m.input.Focus()
			}
			// persist the just-completed block
			allBlocks := m.store.All()
			if len(allBlocks) > 0 {
				last := allBlocks[len(allBlocks)-1]
				if m.recorder != nil {
					m.recorder.Append(last) //nolint:errcheck
				}
				// auto-trigger AI on non-zero exit
				if last.ExitCode != 0 && m.provider != nil && !m.aiOpen {
					m.aiOpen = true
					m.input.Blur()
					m.aiInput.Focus()
					m.aiInput.SetValue(fmt.Sprintf("'%s' failed (exit %d) — what went wrong?", last.Command, last.ExitCode))
				}
			}
		}
		cmds = append(cmds, m.readPTY())

	case aiChunkMsg:
		r := ai.StreamResult(msg)
		if r.Done {
			m.aiStreaming = false
			m.aiCh = nil
			if r.Err != nil {
				m.aiError = r.Err.Error()
			}
			if m.aiCancel != nil {
				m.aiCancel()
				m.aiCancel = nil
			}
			// extract command suggestion from completed response
			if r.Err == nil && m.aiResponse != "" {
				m.suggestedCmd = ai.ExtractCommand(m.aiResponse)
			}
		} else {
			m.aiResponse += r.Text
			if m.aiCh != nil {
				cmds = append(cmds, readNextAI(m.aiCh))
			}
		}

	case errMsg:
		m.err = msg.err
		m.shutdown()
		return m, tea.Quit

	case tea.KeyMsg:
		if m.running {
			if b := keyBytes(msg); b != nil {
				if _, err := m.shell.Write(b); err != nil {
					m.err = fmt.Errorf("write to shell: %w", err)
					m.shutdown()
					return m, tea.Quit
				}
			}
			break
		}

		if m.aiOpen {
			switch msg.Type {
			case tea.KeyEsc:
				if m.suggestedCmd != "" {
					// first Esc dismisses suggestion only
					m.suggestedCmd = ""
					break
				}
				// close panel; cancel any in-progress stream
				if m.aiCancel != nil {
					m.aiCancel()
					m.aiCancel = nil
				}
				m.aiOpen = false
				m.aiStreaming = false
				m.aiResponse = ""
				m.aiError = ""
				m.suggestedCmd = ""
				m.aiInput.SetValue("")
				m.aiInput.Blur()
				m.input.Focus()

			case tea.KeyTab:
				// accept the AI's suggested command: move it to the input bar
				// (or auto-run it if it's whitelisted and auto_run_readonly is enabled)
				if m.suggestedCmd != "" && !m.aiStreaming {
					accepted := m.suggestedCmd
					m.suggestedCmd = ""
					m.aiOpen = false
					m.aiInput.Blur()
					m.input.Focus()
					if m.autoRunReadonly && tools.IsWhitelisted(accepted) {
						// auto-run whitelisted read-only command directly
						wd, _ := os.Getwd()
						m.parser.StartBlock(accepted, wd)
						m.running = true
						if _, err := m.shell.Write([]byte(accepted + "\r")); err != nil {
							m.err = fmt.Errorf("write to shell: %w", err)
							m.shutdown()
							return m, tea.Quit
						}
					} else {
						m.input.SetValue(accepted)
						m.input.CursorEnd()
					}
				}

			case tea.KeyEnter:
				if !m.aiStreaming {
					q := strings.TrimSpace(m.aiInput.Value())
					if q != "" {
						m.aiInput.SetValue("")
						m.suggestedCmd = ""
						cmd := m.startStream(q)
						if cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}

			case tea.KeyCtrlC, tea.KeyCtrlD:
				m.shutdown()
				return m, tea.Quit

			default:
				if !m.aiStreaming {
					var tiCmd tea.Cmd
					m.aiInput, tiCmd = m.aiInput.Update(msg)
					cmds = append(cmds, tiCmd)
				}
			}
			break
		}

		// AI panel closed — command input active
		switch msg.Type {
		case tea.KeyCtrlA:
			m.aiOpen = true
			m.aiResponse = ""
			m.aiError = ""
			m.aiInput.SetValue("")
			m.input.Blur()
			m.aiInput.Focus()

		case tea.KeyCtrlC, tea.KeyCtrlD:
			m.shutdown()
			return m, tea.Quit

		case tea.KeyEnter:
			cmd := strings.TrimSpace(m.input.Value())
			if cmd != "" {
				// :provider <name> — hot-switch AI provider without restart
				if strings.HasPrefix(cmd, ":provider ") {
					name := strings.TrimSpace(strings.TrimPrefix(cmd, ":provider "))
					m.input.SetValue("")
					if errStr := m.switchProvider(name); errStr != "" {
						m.aiOpen = true
						m.aiError = errStr
						m.aiResponse = ""
						m.input.Blur()
						m.aiInput.Focus()
					} else {
						m.aiOpen = true
						m.aiResponse = fmt.Sprintf("Switched to provider: %s", name)
						m.aiError = ""
						m.input.Blur()
						m.aiInput.Focus()
					}
					break
				}

				wd, _ := os.Getwd()
				m.parser.StartBlock(cmd, wd)
				m.running = true
				if _, err := m.shell.Write([]byte(cmd + "\r")); err != nil {
					m.err = fmt.Errorf("write to shell: %w", err)
					m.shutdown()
					return m, tea.Quit
				}
				m.input.SetValue("")
			}

		default:
			var tiCmd tea.Cmd
			m.input, tiCmd = m.input.Update(msg)
			cmds = append(cmds, tiCmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.err != nil {
		return errorStyle.Render("agterm: "+m.err.Error()) + "\n"
	}
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// allocate vertical space
	bottomBarH := 1
	panelH := 0
	if m.aiOpen {
		panelH = aiPanelHeight
	}
	blockH := m.height - bottomBarH - panelH
	if blockH < 1 {
		blockH = 1
	}

	// ── block list ────────────────────────────────────────────────────────────
	var lines []string
	for _, b := range m.store.All() {
		lines = append(lines, blockLines(b, m.width)...)
	}
	if active := m.parser.Active(); active != nil {
		lines = append(lines, activeLines(active, m.width)...)
	}
	if len(lines) > blockH {
		lines = lines[len(lines)-blockH:]
	}
	for len(lines) < blockH {
		lines = append([]string{""}, lines...)
	}
	content := strings.Join(lines, "\n")

	// ── AI panel ──────────────────────────────────────────────────────────────
	var panel string
	if m.aiOpen {
		panel = m.renderAIPanel()
	}

	// ── bottom bar ────────────────────────────────────────────────────────────
	var bar string
	if m.aiOpen {
		bar = m.aiInput.View()
	} else if m.running {
		bar = statusStyle.Render("running…")
	} else {
		bar = m.input.View()
	}

	parts := []string{content}
	if panel != "" {
		parts = append(parts, panel)
	}
	parts = append(parts, bar)
	return strings.Join(parts, "\n")
}

func (m Model) renderAIPanel() string {
	divider := dimStyle.Render(strings.Repeat("─", m.width))

	// context status
	var status string
	if m.aiStreaming {
		status = statusStyle.Render("streaming…")
	} else if m.aiError != "" {
		status = errorStyle.Render("error: " + m.aiError)
	} else if m.sendContext {
		status = dimStyle.Render(fmt.Sprintf("[context: %d blocks]", len(m.store.All())))
	} else {
		status = dimStyle.Render("[no context — set send_context: true in config to enable]")
	}

	// response area — shrink by 1 if there's a suggestion bar
	suggestionBar := ""
	if m.suggestedCmd != "" && !m.aiStreaming {
		suggestionBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2")).Bold(true).
			Render(fmt.Sprintf("  ❯ %s", m.suggestedCmd)) +
			dimStyle.Render("  [Tab to run · Esc to dismiss]")
	}

	responseH := aiPanelHeight - 3
	if suggestionBar != "" {
		responseH--
	}
	if responseH < 1 {
		responseH = 1
	}
	var respLines []string
	if m.aiResponse != "" {
		respLines = strings.Split(m.aiResponse, "\n")
	}
	if len(respLines) > responseH {
		respLines = respLines[len(respLines)-responseH:]
	}
	for len(respLines) < responseH {
		respLines = append([]string{""}, respLines...)
	}
	response := strings.Join(respLines, "\n")

	parts := []string{divider, response}
	if suggestionBar != "" {
		parts = append(parts, suggestionBar)
	}
	parts = append(parts, status)
	return strings.Join(parts, "\n")
}

// ── block rendering ───────────────────────────────────────────────────────────

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
	return append([]string{header}, strings.Split(outputStyle.Render(out), "\n")...)
}

func activeLines(b *block.Block, _ int) []string {
	header := promptStyle.Render("❯ ") + cmdStyle.Render(b.Command) + " " + dimStyle.Render("…")
	if b.Output == "" {
		return []string{header}
	}
	out := strings.TrimRight(b.Output, "\n")
	return append([]string{header}, strings.Split(outputStyle.Render(out), "\n")...)
}

// ── key translation ───────────────────────────────────────────────────────────

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
