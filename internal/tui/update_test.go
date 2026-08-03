package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/imattos78/agterm/internal/ai"
	"github.com/imattos78/agterm/internal/block"
	"github.com/imattos78/agterm/internal/config"
	"github.com/imattos78/agterm/internal/history"
	"github.com/imattos78/agterm/internal/pty"
)

// Compile-time assertions that the real production types still satisfy
// shellIO/recorderIO — guards against fakeShell/fakeRecorder silently
// drifting from the contract the real *pty.Shell/*history.Recorder expose.
var (
	_ shellIO    = (*pty.Shell)(nil)
	_ recorderIO = (*history.Recorder)(nil)
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakeShell is an in-memory shellIO substitute for a real *pty.Shell.
type fakeShell struct {
	mu       sync.Mutex
	writes   [][]byte
	resizes  []struct{ Rows, Cols uint16 }
	closed   bool
	writeErr error
	closeErr error
	log      *[]string
}

func (f *fakeShell) Read(p []byte) (int, error) { return 0, errors.New("fakeShell: Read not used") }

func (f *fakeShell) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]byte(nil), p...)
	f.writes = append(f.writes, cp)
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeShell) Resize(rows, cols uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizes = append(f.resizes, struct{ Rows, Cols uint16 }{rows, cols})
	return nil
}

func (f *fakeShell) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.log != nil {
		*f.log = append(*f.log, "shell.Close")
	}
	return f.closeErr
}

func (f *fakeShell) lastWrite() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) == 0 {
		return ""
	}
	return string(f.writes[len(f.writes)-1])
}

// fakeRecorder is an in-memory recorderIO substitute for *history.Recorder.
type fakeRecorder struct {
	mu       sync.Mutex
	appended []*block.Block
	closed   bool
	log      *[]string
}

func (f *fakeRecorder) Append(b *block.Block) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appended = append(f.appended, b)
	if f.log != nil {
		*f.log = append(*f.log, "recorder.Append")
	}
	return nil
}

func (f *fakeRecorder) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.log != nil {
		*f.log = append(*f.log, "recorder.Close")
	}
	return nil
}

// fakeProvider is a deterministic ai.Provider driven by a caller-controlled
// channel, so streaming tests never depend on a real network call.
type fakeProvider struct {
	name string
	ch   chan ai.StreamResult
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.StreamResult {
	return f.ch
}

// newTestModel builds a Model wired to fakes, mirroring the parts of New()
// that matter for Update/shutdown logic without touching a real PTY, disk
// config, or history file.
func newTestModel(shell shellIO, provider ai.Provider, rec recorderIO) Model {
	store := block.NewStore(500)

	cmdInput := textinput.New()
	cmdInput.CharLimit = 1024
	cmdInput.Focus()

	aiIn := textinput.New()
	aiIn.CharLimit = 512

	return Model{
		shell:    shell,
		detector: &pty.Detector{},
		parser:   block.NewParser(store),
		store:    store,
		input:    cmdInput,
		aiInput:  aiIn,
		provider: provider,
		recorder: rec,
		width:    80,
		height:   24,
	}
}

// mustModel re-asserts the tea.Model returned by Update back to our concrete
// Model type, since Update satisfies the tea.Model interface.
func mustModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	m, ok := tm.(Model)
	if !ok {
		t.Fatalf("expected tui.Model, got %T", tm)
	}
	return m
}

// runCmd executes a tea.Cmd and returns the resulting message, or nil if cmd
// is nil. Used to inspect e.g. whether a Cmd resolves to tea.QuitMsg.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// ── WindowSizeMsg ─────────────────────────────────────────────────────────────

func TestUpdate_WindowSizeMsg_ResizesShellAndInputs(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)

	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = mustModel(t, tm)

	if m.width != 100 || m.height != 40 {
		t.Fatalf("expected width/height 100/40, got %d/%d", m.width, m.height)
	}
	if len(sh.resizes) != 1 || sh.resizes[0].Rows != 40 || sh.resizes[0].Cols != 100 {
		t.Fatalf("expected shell.Resize(40,100), got %v", sh.resizes)
	}
}

// ── running command: raw key passthrough ─────────────────────────────────────

func TestUpdate_KeyMsg_RunningWritesRawBytesToShell(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.running = true

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = mustModel(t, tm)

	if !m.running {
		t.Fatalf("expected running to remain true")
	}
	if sh.lastWrite() != "a" {
		t.Fatalf("expected shell to receive %q, got %q", "a", sh.lastWrite())
	}
	if m.input.Value() != "" {
		t.Fatalf("expected the command input to NOT receive the key while a command is running, got %q", m.input.Value())
	}
}

func TestUpdate_KeyMsg_RunningCtrlCForwardsSIGINTByteWithoutQuitting(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.running = true

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = mustModel(t, tm)

	if sh.lastWrite() != "\x03" {
		t.Fatalf("expected shell to receive the raw SIGINT byte 0x03, got %q", sh.lastWrite())
	}
	if !m.running {
		t.Fatalf("expected running to remain true — Ctrl+C while running targets the child process, not agterm")
	}
	if m.err != nil {
		t.Fatalf("expected no error set, got %v", m.err)
	}
	if cmd != nil {
		if _, ok := runCmd(cmd).(tea.QuitMsg); ok {
			t.Fatalf("expected Ctrl+C while running NOT to quit agterm")
		}
	}
	if sh.closed {
		t.Fatalf("expected the shell to stay open — only the child process should see the SIGINT byte")
	}
}

func TestUpdate_KeyMsg_RunningWriteErrorShutsDownAndQuits(t *testing.T) {
	sh := &fakeShell{writeErr: errors.New("write boom")}
	m := newTestModel(sh, nil, nil)
	m.running = true

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = mustModel(t, tm)

	if m.err == nil || !strings.Contains(m.err.Error(), "write boom") {
		t.Fatalf("expected err to wrap write boom, got %v", m.err)
	}
	if !sh.closed {
		t.Fatalf("expected shutdown to close the shell")
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

// ── ptyMsg: command completion ───────────────────────────────────────────────

func TestUpdate_PtyMsg_CleanExitPersistsBlockAndRefocusesInput(t *testing.T) {
	sh := &fakeShell{}
	rec := &fakeRecorder{}
	m := newTestModel(sh, nil, rec)
	m.running = true
	m.input.Blur()
	m.parser.StartBlock("ls -la", "/tmp")

	segs := []pty.Segment{{Kind: pty.SegCommandEnd, ExitCode: 0}}
	tm, _ := m.Update(ptyMsg{segs: segs})
	m = mustModel(t, tm)

	if m.running {
		t.Fatalf("expected running to become false after clean exit")
	}
	if !m.input.Focused() {
		t.Fatalf("expected command input to refocus after clean exit")
	}
	if len(rec.appended) != 1 || rec.appended[0].Command != "ls -la" {
		t.Fatalf("expected the completed block to be recorded, got %v", rec.appended)
	}
	if m.aiOpen {
		t.Fatalf("expected AI panel to stay closed on a zero exit code")
	}
}

func TestUpdate_PtyMsg_NonZeroExitAutoOpensAIPanel(t *testing.T) {
	sh := &fakeShell{}
	ch := make(chan ai.StreamResult, 1)
	m := newTestModel(sh, &fakeProvider{name: "fake", ch: ch}, &fakeRecorder{})
	m.running = true
	m.parser.StartBlock("false", "/tmp")

	segs := []pty.Segment{{Kind: pty.SegCommandEnd, ExitCode: 1}}
	tm, _ := m.Update(ptyMsg{segs: segs})
	m = mustModel(t, tm)

	if !m.aiOpen {
		t.Fatalf("expected AI panel to auto-open on a non-zero exit code")
	}
	if m.input.Focused() {
		t.Fatalf("expected command input to blur once the AI panel opens")
	}
	if !strings.Contains(m.aiInput.Value(), "false") || !strings.Contains(m.aiInput.Value(), "1") {
		t.Fatalf("expected AI input to mention the failing command and exit code, got %q", m.aiInput.Value())
	}
}

func TestUpdate_PtyMsg_NonZeroExitDoesNotReopenAlreadyOpenPanel(t *testing.T) {
	sh := &fakeShell{}
	ch := make(chan ai.StreamResult, 1)
	m := newTestModel(sh, &fakeProvider{name: "fake", ch: ch}, &fakeRecorder{})
	m.running = true
	m.aiOpen = true
	m.aiInput.SetValue("existing question")
	m.parser.StartBlock("false", "/tmp")

	segs := []pty.Segment{{Kind: pty.SegCommandEnd, ExitCode: 1}}
	tm, _ := m.Update(ptyMsg{segs: segs})
	m = mustModel(t, tm)

	if m.aiInput.Value() != "existing question" {
		t.Fatalf("expected an already-open AI panel not to be overwritten, got %q", m.aiInput.Value())
	}
}

// ── ptyMsg: styled-error auto-trigger (agterm#16) ────────────────────────────

func TestUpdate_PtyMsg_StyledErrorsAutoOpenPanelEvenOnExitZero(t *testing.T) {
	sh := &fakeShell{}
	ch := make(chan ai.StreamResult, 1)
	m := newTestModel(sh, &fakeProvider{name: "fake", ch: ch}, &fakeRecorder{})
	m.running = true
	m.parser.StartBlock("lint", "/tmp")

	// 3 lines of red text meets autoTriggerErrorLineThreshold, exit code 0
	// (a linter that reports failures without a non-zero exit).
	red := "\x1b[31merror one\x1b[0m\n\x1b[31merror two\x1b[0m\n\x1b[31merror three\x1b[0m\n"
	segs := []pty.Segment{
		{Kind: pty.SegCommandStart},
		{Kind: pty.SegOutput, Data: []byte(red)},
		{Kind: pty.SegCommandEnd, ExitCode: 0},
	}
	tm, _ := m.Update(ptyMsg{segs: segs})
	m = mustModel(t, tm)

	if !m.aiOpen {
		t.Fatalf("expected AI panel to auto-open on styled errors despite exit 0")
	}
	if !strings.Contains(m.aiInput.Value(), "lint") || !strings.Contains(m.aiInput.Value(), "3") {
		t.Fatalf("expected AI input to mention the command and the styled-error line count, got %q", m.aiInput.Value())
	}
}

func TestUpdate_PtyMsg_BelowThresholdStyledErrorsDoNotAutoOpenPanel(t *testing.T) {
	sh := &fakeShell{}
	ch := make(chan ai.StreamResult, 1)
	m := newTestModel(sh, &fakeProvider{name: "fake", ch: ch}, &fakeRecorder{})
	m.running = true
	m.parser.StartBlock("ls", "/tmp")

	// Only 2 red lines — below autoTriggerErrorLineThreshold(3), exit 0.
	red := "\x1b[31mone\x1b[0m\n\x1b[31mtwo\x1b[0m\nplain line\n"
	segs := []pty.Segment{
		{Kind: pty.SegCommandStart},
		{Kind: pty.SegOutput, Data: []byte(red)},
		{Kind: pty.SegCommandEnd, ExitCode: 0},
	}
	tm, _ := m.Update(ptyMsg{segs: segs})
	m = mustModel(t, tm)

	if m.aiOpen {
		t.Fatalf("expected AI panel to stay closed below the styled-error threshold")
	}
}

func TestUpdate_PtyMsg_NoCellsFallbackDoesNotAutoOpenOnExitZero(t *testing.T) {
	sh := &fakeShell{}
	ch := make(chan ai.StreamResult, 1)
	m := newTestModel(sh, &fakeProvider{name: "fake", ch: ch}, &fakeRecorder{})
	m.running = true
	m.parser.StartBlock("noop", "/tmp")

	// No SegOutput at all before SegCommandEnd — the block closes with
	// Cells == nil (zero value, never touched by feedVT). ErrorLineCount()
	// must fall back to 0, not panic on a nil grid.
	segs := []pty.Segment{{Kind: pty.SegCommandEnd, ExitCode: 0}}
	tm, _ := m.Update(ptyMsg{segs: segs})
	m = mustModel(t, tm)

	if m.aiOpen {
		t.Fatalf("expected AI panel to stay closed for a Cells==nil block at exit 0")
	}
}

// ── aiChunkMsg: streaming state transitions ──────────────────────────────────

func TestUpdate_AiChunkMsg_AccumulatesTextWhileStreaming(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiStreaming = true
	m.aiCh = make(chan ai.StreamResult, 1)

	tm, _ := m.Update(aiChunkMsg{Text: "hello ", Done: false})
	m = mustModel(t, tm)

	if m.aiResponse != "hello " {
		t.Fatalf("expected aiResponse to accumulate, got %q", m.aiResponse)
	}
	if !m.aiStreaming {
		t.Fatalf("expected aiStreaming to remain true mid-stream")
	}
	if m.aiCh == nil {
		t.Fatalf("expected aiCh to remain set mid-stream")
	}
}

func TestUpdate_AiChunkMsg_DoneExtractsSuggestedCommand(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiStreaming = true
	m.aiResponse = "you should run:\n$ ls -la\n"
	m.aiCh = make(chan ai.StreamResult, 1)
	cancelled := false
	m.aiCancel = func() { cancelled = true }

	tm, _ := m.Update(aiChunkMsg{Done: true})
	m = mustModel(t, tm)

	if m.aiStreaming {
		t.Fatalf("expected aiStreaming false once Done")
	}
	if m.aiCh != nil {
		t.Fatalf("expected aiCh to be cleared once Done")
	}
	if !cancelled {
		t.Fatalf("expected the stream's cancel func to be invoked on Done")
	}
	if m.aiCancel != nil {
		t.Fatalf("expected aiCancel to be cleared after being invoked")
	}
	if m.suggestedCmd != "ls -la" {
		t.Fatalf("expected suggestedCmd %q, got %q", "ls -la", m.suggestedCmd)
	}
}

func TestUpdate_AiChunkMsg_DoneWithErrSetsAiError(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiStreaming = true
	m.aiResponse = "partial"

	tm, _ := m.Update(aiChunkMsg{Done: true, Err: errors.New("stream boom")})
	m = mustModel(t, tm)

	if m.aiError != "stream boom" {
		t.Fatalf("expected aiError %q, got %q", "stream boom", m.aiError)
	}
	if m.suggestedCmd != "" {
		t.Fatalf("expected no suggestedCmd extraction when the stream errored")
	}
}

// ── errMsg ────────────────────────────────────────────────────────────────────

func TestUpdate_ErrMsg_ShutsDownAndQuits(t *testing.T) {
	sh := &fakeShell{}
	rec := &fakeRecorder{}
	m := newTestModel(sh, nil, rec)

	tm, cmd := m.Update(errMsg{err: errors.New("pty read failed")})
	m = mustModel(t, tm)

	if m.err == nil || m.err.Error() != "pty read failed" {
		t.Fatalf("expected err to be set, got %v", m.err)
	}
	if !sh.closed || !rec.closed {
		t.Fatalf("expected shutdown to close shell and recorder, shell=%v recorder=%v", sh.closed, rec.closed)
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

// ── AI panel: key handling ────────────────────────────────────────────────────

func TestUpdate_AIPanel_EscDismissesSuggestionFirst(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.suggestedCmd = "ls -la"

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustModel(t, tm)

	if m.suggestedCmd != "" {
		t.Fatalf("expected first Esc to clear the suggestion")
	}
	if !m.aiOpen {
		t.Fatalf("expected the AI panel to stay open on the dismiss-only Esc")
	}
}

func TestUpdate_AIPanel_EscClosesPanelWhenNoSuggestion(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.aiResponse = "some response"
	m.input.Blur()
	m.aiInput.Focus()
	cancelled := false
	m.aiCancel = func() { cancelled = true }

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustModel(t, tm)

	if m.aiOpen {
		t.Fatalf("expected AI panel to close")
	}
	if !cancelled {
		t.Fatalf("expected in-flight stream to be cancelled on close")
	}
	if m.aiResponse != "" || m.aiError != "" {
		t.Fatalf("expected aiResponse/aiError to be cleared on close")
	}
	if !m.input.Focused() {
		t.Fatalf("expected command input to refocus once the AI panel closes")
	}
}

func TestUpdate_AIPanel_TabAcceptsSuggestion_ManualPath(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.aiOpen = true
	m.suggestedCmd = "rm -rf /tmp/x" // not whitelisted
	m.autoRunReadonly = true

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mustModel(t, tm)

	if m.aiOpen {
		t.Fatalf("expected AI panel to close after accepting the suggestion")
	}
	if m.running {
		t.Fatalf("expected a non-whitelisted command not to auto-run")
	}
	if m.input.Value() != "rm -rf /tmp/x" {
		t.Fatalf("expected the suggestion to land in the command input, got %q", m.input.Value())
	}
	if len(sh.writes) != 0 {
		t.Fatalf("expected nothing written to the shell for the manual path")
	}
}

func TestUpdate_AIPanel_TabAutoRunsWhitelistedSuggestion(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.aiOpen = true
	m.suggestedCmd = "ls -la" // whitelisted
	m.autoRunReadonly = true

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mustModel(t, tm)

	if m.aiOpen {
		t.Fatalf("expected AI panel to close")
	}
	if !m.running {
		t.Fatalf("expected the whitelisted suggestion to auto-run")
	}
	if sh.lastWrite() != "ls -la\r" {
		t.Fatalf("expected shell to receive %q, got %q", "ls -la\r", sh.lastWrite())
	}
	if m.parser.Active() == nil || m.parser.Active().Command != "ls -la" {
		t.Fatalf("expected parser to have started a block for the auto-run command")
	}
}

func TestUpdate_AIPanel_EnterStartsStream(t *testing.T) {
	ch := make(chan ai.StreamResult, 1)
	m := newTestModel(&fakeShell{}, &fakeProvider{name: "fake", ch: ch}, nil)
	m.aiOpen = true
	m.aiInput.SetValue("why did it fail?")

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, tm)

	if !m.aiStreaming {
		t.Fatalf("expected aiStreaming true once a question is submitted")
	}
	if m.aiInput.Value() != "" {
		t.Fatalf("expected aiInput to clear after submit, got %q", m.aiInput.Value())
	}
	if cmd == nil {
		t.Fatalf("expected a non-nil cmd to read the first stream chunk")
	}
}

func TestUpdate_AIPanel_EnterNoProviderSetsError(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil) // provider nil
	m.aiOpen = true
	m.aiInput.SetValue("why did it fail?")

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, tm)

	if m.aiStreaming {
		t.Fatalf("expected aiStreaming false with no provider configured")
	}
	if m.aiError == "" {
		t.Fatalf("expected aiError to be set when no provider is configured")
	}
}

func TestUpdate_AIPanel_CtrlCQuits(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.aiOpen = true

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	mustModel(t, tm)

	if !sh.closed {
		t.Fatalf("expected shutdown to close the shell")
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

func TestUpdate_AIPanel_DefaultForwardsKeysToAIInput(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.aiInput.Focus()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = mustModel(t, tm)

	if m.aiInput.Value() != "x" {
		t.Fatalf("expected aiInput to receive forwarded key, got %q", m.aiInput.Value())
	}
}

func TestUpdate_AIPanel_StreamingIgnoresInputKeys(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.aiStreaming = true
	m.aiInput.Focus()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = mustModel(t, tm)

	if m.aiInput.Value() != "" {
		t.Fatalf("expected aiInput to ignore keys while streaming, got %q", m.aiInput.Value())
	}
}

// ── command input (AI panel closed) ──────────────────────────────────────────

// ── scrollback (Phase 7) ─────────────────────────────────────────────────────

// fillStore adds n single-line blocks (command header only, no output) to
// m's store, giving flattenLines() exactly n lines to page through.
func fillStore(m *Model, n int) {
	for i := 0; i < n; i++ {
		m.parser.StartBlock(fmt.Sprintf("cmd%d", i), "")
		m.store.Add(m.parser.Active())
	}
}

func TestUpdate_PgUpPgDown_ScrollsBlockListAndReturnsToBottom(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100) // well beyond blockHeight() so paging has room to move

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("expected PgUp to scroll away from the bottom")
	}
	if !strings.Contains(m.View(), "scrolled") {
		t.Fatalf("expected View() to show the scroll indicator once scrolled")
	}

	// Enough PgDowns to certainly land back at (or floor to) the bottom.
	for i := 0; i < 5; i++ {
		tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = mustModel(t, tm)
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("expected repeated PgDown to floor back at the bottom")
	}
	if strings.Contains(m.View(), "scrolled") {
		t.Fatalf("expected View() to hide the scroll indicator once back at the bottom")
	}
}

func TestUpdate_CtrlU_EditsCommandInputWhenAtBottom(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.input.SetValue("hello")
	m.input.CursorEnd()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = mustModel(t, tm)

	if m.input.Value() != "" {
		t.Fatalf("expected ctrl+u to delete-before-cursor in the command input, got %q", m.input.Value())
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("expected ctrl+u to leave the viewport untouched while at bottom")
	}
}

func TestUpdate_CtrlU_ScrollsInsteadOfEditingOnceScrolled(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)
	m.input.SetValue("hello")
	m.input.CursorEnd()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	offsetAfterPgUp := m.viewport.offset

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = mustModel(t, tm)

	if m.input.Value() != "hello" {
		t.Fatalf("expected ctrl+u to leave the command input untouched once scrolled, got %q", m.input.Value())
	}
	if m.viewport.offset <= offsetAfterPgUp {
		t.Fatalf("expected ctrl+u to scroll further up once already scrolled: before=%d after=%d", offsetAfterPgUp, m.viewport.offset)
	}
}

func TestUpdate_CtrlD_QuitsWhenAtBottom(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})

	if !sh.closed {
		t.Fatalf("expected ctrl+d to shut down when at bottom (EOF convention)")
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

func TestUpdate_CtrlD_ScrollsDownInsteadOfQuittingOnceScrolled(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	offsetAfterPgUp := m.viewport.offset

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = mustModel(t, tm)

	if sh.closed {
		t.Fatalf("expected ctrl+d NOT to shut down the shell once scrolled")
	}
	if m.viewport.offset >= offsetAfterPgUp {
		t.Fatalf("expected ctrl+d to scroll back down once already scrolled: before=%d after=%d", offsetAfterPgUp, m.viewport.offset)
	}
}

func TestUpdate_Home_MovesInputCursorWhenAtBottom(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.input.SetValue("hello")
	m.input.CursorEnd()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = mustModel(t, tm)

	if m.input.Position() != 0 {
		t.Fatalf("expected home to move the command input cursor to 0, got %d", m.input.Position())
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("expected home to leave the viewport untouched while at bottom")
	}
}

func TestUpdate_Home_JumpsToTopOnceScrolled(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = mustModel(t, tm)

	if !strings.Contains(m.View(), "cmd0") {
		t.Fatalf("expected home to jump all the way back to the earliest block, view:\n%s", m.View())
	}
}

func TestUpdate_End_JumpsToBottomOnceScrolled(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("test setup broken: expected to be scrolled up before pressing End")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = mustModel(t, tm)

	if !m.viewport.AtBottom() {
		t.Fatalf("expected end to re-engage sticky-bottom")
	}
}

func TestUpdate_Esc_JumpsToBottomOnceScrolled(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustModel(t, tm)

	if !m.viewport.AtBottom() {
		t.Fatalf("expected esc to re-engage sticky-bottom as an escape hatch")
	}
}

func TestUpdate_PtyMsg_PreservesScrollAnchorAsNewOutputArrives(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	viewBefore := m.View()

	// A ptyMsg with no segments still runs through the growth-tracking path
	// (Viewport.Note); the rendered window must not move.
	tm, _ = m.Update(ptyMsg{})
	m = mustModel(t, tm)

	if m.View() != viewBefore {
		t.Fatalf("expected the scrolled view to stay anchored across an unrelated ptyMsg\nbefore:\n%s\nafter:\n%s", viewBefore, m.View())
	}
}

func TestUpdate_CtrlC_QuitsEvenWhileScrolled(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("test setup broken: expected to be scrolled up before ctrl+c")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !sh.closed {
		t.Fatalf("expected ctrl+c to quit unconditionally even while scrolled")
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

// scrollIndicatorLine returns the indicator row m expects in its own View()
// output (content is exactly m.blockHeight() lines, the indicator is the
// next one — see View()'s parts ordering). Positional, not a substring
// search, because at extreme widths MaxWidth can truncate mid-word (e.g.
// "scrolled" -> "scrolle"), which would make a text-match unreliable.
func scrollIndicatorLine(t *testing.T, m Model) string {
	t.Helper()
	if m.viewport.AtBottom() {
		t.Fatalf("no scroll indicator is rendered while at bottom")
	}
	lines := strings.Split(m.View(), "\n")
	i := m.blockHeight()
	if i >= len(lines) {
		t.Fatalf("expected an indicator line at index %d, only got %d lines:\n%s", i, len(lines), m.View())
	}
	return lines[i]
}

func TestUpdate_ScrollIndicator_ShowsCorrectLineCount(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)

	wantAbove := m.viewport.AboveBottom(len(m.flattenLines()), m.blockHeight())
	if !strings.Contains(scrollIndicatorLine(t, m), fmt.Sprintf("%d lines", wantAbove)) {
		t.Fatalf("expected indicator to report %d lines, view:\n%s", wantAbove, m.View())
	}
}

func TestUpdate_ScrollIndicator_CapsWidthOnNarrowTerminal(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.width = 10 // narrower than the indicator text itself
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)

	// Block-list content lines are a separate, pre-existing concern
	// (blockLines' own padding can already exceed very narrow widths) —
	// only the indicator this PR adds needs to respect m.width here.
	indicatorLine := scrollIndicatorLine(t, m)
	if got := lipgloss.Width(indicatorLine); got > m.width {
		t.Fatalf("indicator line exceeds terminal width %d: %q (width %d)", m.width, indicatorLine, got)
	}
}

func TestUpdate_WindowSizeMsg_WhileScrolled_ReclampsInsteadOfResetting(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	offsetBefore := m.viewport.offset

	// Grow the terminal — plenty of room, offset should NOT reset to 0.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	m = mustModel(t, tm)

	if m.viewport.AtBottom() {
		t.Fatalf("expected offset to survive a height resize, got reset to bottom")
	}
	if m.viewport.offset != offsetBefore {
		t.Fatalf("expected raw offset unchanged by a height resize alone: before=%d after=%d", offsetBefore, m.viewport.offset)
	}

	// Shrink drastically — offset must reclamp without panicking or going negative.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 3})
	m = mustModel(t, tm)
	_ = m.View() // must not panic
}

func TestUpdate_WindowSizeMsg_WhileScrolled_NarrowWidthDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)

	// Documented, accepted limitation: width resize may drift the scroll
	// position (no Cell-anchoring in this slice). It must not panic.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 5, Height: 24})
	m = mustModel(t, tm)
	_ = m.View()
}

func TestUpdate_PgUp_ScrollsWithinASingleBlockLargerThanTheViewport(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.parser.StartBlock("cmd0", "")
	active := m.parser.Active()
	for i := 0; i < 200; i++ {
		active.Output += fmt.Sprintf("line%d\n", i)
	}
	m.store.Add(active)
	// Close the block out of "active" state — it's now a single completed
	// Block in the Store whose own output (201 lines) outgrows the
	// viewport by itself, proving scroll is line-granular, not block-
	// granular (a Block-level scroll couldn't page into this at all).
	m.parser = block.NewParser(m.store)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("expected PgUp to scroll within a single oversized block")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = mustModel(t, tm)
	if !strings.Contains(m.View(), "line0") {
		t.Fatalf("expected Home to reach the start of the oversized block's own output, view:\n%s", m.View())
	}
}

func TestUpdate_PgUp_EmptyBufferDoesNotPanicOrMoveOffset(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)

	if !m.viewport.AtBottom() {
		t.Fatalf("expected PgUp on an empty buffer to leave the viewport at bottom (nothing to scroll into), offset=%d", m.viewport.offset)
	}
	_ = m.View() // must not panic
}

func TestUpdate_PtyMsg_StoreEvictionWhileScrolledDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 500) // at the Store's cap (block.NewStore(500) in newTestModel)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("test setup broken: expected to be scrolled up before eviction")
	}

	// Pushes the Store past its cap, evicting the oldest blocks — exercises
	// syncViewport()/Clamp with a total that can now shrink from the front.
	fillStore(&m, 50)

	tm, _ = m.Update(ptyMsg{})
	m = mustModel(t, tm)
	_ = m.View() // must not panic, offset must not go invalid
	if m.viewport.offset < 0 {
		t.Fatalf("offset went negative after eviction: %d", m.viewport.offset)
	}
}

func TestUpdate_TinyTerminalHeight_DoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 20)
	m.height = 1
	m.width = 20

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.blockHeight() < 1 {
		t.Fatalf("blockHeight() = %d, want >= 1 even on a 1-row terminal", m.blockHeight())
	}
	_ = m.View() // must not panic
}

// TestTextinputKeymapAssumptions locks in the Bubbles textinput behavior the
// scroll-vs-edit gating in Update() depends on: ctrl+u/home edit the command
// line, esc is a no-op. If a future bubbles bump changes any of these, this
// fails loudly instead of the offset==0 gating silently breaking underneath
// PageUp/Ctrl+U/Home/Esc.
func TestTextinputKeymapAssumptions(t *testing.T) {
	fresh := func() textinput.Model {
		ti := textinput.New()
		ti.Focus()
		ti.SetValue("hello")
		ti.CursorEnd()
		return ti
	}

	ti, _ := fresh().Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if ti.Value() != "" {
		t.Fatalf("assumption broken: ctrl+u no longer deletes before cursor in textinput, got %q", ti.Value())
	}

	ti, _ = fresh().Update(tea.KeyMsg{Type: tea.KeyHome})
	if ti.Position() != 0 {
		t.Fatalf("assumption broken: home no longer moves the cursor to line start in textinput, got position %d", ti.Position())
	}

	before := fresh()
	after, _ := before.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if after.Value() != before.Value() || after.Position() != before.Position() {
		t.Fatalf("assumption broken: esc now mutates textinput (value=%q pos=%d, want value=%q pos=%d) — the Esc-jumps-to-bottom gating in Update() needs revisiting",
			after.Value(), after.Position(), before.Value(), before.Position())
	}
}

func TestUpdate_CtrlA_OpensAIPanel(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.input.Focus()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = mustModel(t, tm)

	if !m.aiOpen {
		t.Fatalf("expected Ctrl+A to open the AI panel")
	}
	if m.input.Focused() {
		t.Fatalf("expected command input to blur once the AI panel opens")
	}
	if !m.aiInput.Focused() {
		t.Fatalf("expected AI input to focus once the panel opens")
	}
}

func TestUpdate_CtrlC_QuitsFromCommandInput(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if !sh.closed {
		t.Fatalf("expected shutdown to close the shell")
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

func TestUpdate_CtrlD_QuitsFromCommandInput(t *testing.T) {
	sh := &fakeShell{}
	rec := &fakeRecorder{}
	m := newTestModel(sh, nil, rec)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})

	if !sh.closed || !rec.closed {
		t.Fatalf("expected shutdown to close shell and recorder, shell=%v recorder=%v", sh.closed, rec.closed)
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

func TestUpdate_Enter_StartsCommandBlock(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.input.SetValue("ls -la")

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, tm)

	if !m.running {
		t.Fatalf("expected running true once a command is submitted")
	}
	if sh.lastWrite() != "ls -la\r" {
		t.Fatalf("expected shell to receive %q, got %q", "ls -la\r", sh.lastWrite())
	}
	if m.input.Value() != "" {
		t.Fatalf("expected input to clear after submit, got %q", m.input.Value())
	}
	if m.parser.Active() == nil || m.parser.Active().Command != "ls -la" {
		t.Fatalf("expected parser to have opened a block for the command")
	}
}

func TestUpdate_Enter_EmptyCommandIsNoOp(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.input.SetValue("   ")

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, tm)

	if m.running {
		t.Fatalf("expected running to stay false for a blank command")
	}
	if len(sh.writes) != 0 {
		t.Fatalf("expected nothing written to the shell for a blank command")
	}
}

func TestUpdate_Enter_WriteErrorShutsDownAndQuits(t *testing.T) {
	sh := &fakeShell{writeErr: errors.New("write boom")}
	m := newTestModel(sh, nil, nil)
	m.input.SetValue("ls")

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustModel(t, tm)

	if m.err == nil {
		t.Fatalf("expected err to be set on write failure")
	}
	if !sh.closed {
		t.Fatalf("expected shutdown to close the shell")
	}
	if _, ok := runCmd(cmd).(tea.QuitMsg); !ok {
		t.Fatalf("expected returned cmd to resolve to tea.QuitMsg")
	}
}

func TestUpdate_Default_ForwardsKeysToCommandInput(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil)
	m.input.Focus()

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = mustModel(t, tm)

	if m.input.Value() != "l" {
		t.Fatalf("expected input to receive forwarded key, got %q", m.input.Value())
	}
	if len(sh.writes) != 0 {
		t.Fatalf("expected the key NOT to be written to the shell while no command is running, got %v", sh.writes)
	}
}

func TestUpdate_Enter_ProviderHotSwitch(t *testing.T) {
	if path, err := config.DefaultPath(); err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			t.Skipf("skipping: a real config file exists at %s, hot-switch outcome would depend on it", path)
		}
	}

	t.Run("success falls back to default ollama provider", func(t *testing.T) {
		m := newTestModel(&fakeShell{}, nil, nil)
		m.input.SetValue(":provider ollama")

		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = mustModel(t, tm)

		if !m.aiOpen {
			t.Fatalf("expected the AI panel to open to show the switch result")
		}
		if m.aiError != "" {
			t.Fatalf("expected no error switching to the default ollama provider, got %q", m.aiError)
		}
		if !strings.Contains(m.aiResponse, "ollama") {
			t.Fatalf("expected confirmation to mention ollama, got %q", m.aiResponse)
		}
	})

	t.Run("unknown provider name reports an error", func(t *testing.T) {
		m := newTestModel(&fakeShell{}, nil, nil)
		m.input.SetValue(":provider this-provider-does-not-exist-xyz")

		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = mustModel(t, tm)

		if !m.aiOpen {
			t.Fatalf("expected the AI panel to open to show the switch result")
		}
		if m.aiError == "" {
			t.Fatalf("expected an error for an unregistered provider name")
		}
	})
}

// ── shutdown ordering ────────────────────────────────────────────────────────

func TestShutdown_CancelsRecorderThenShellInOrder(t *testing.T) {
	var log []string
	sh := &fakeShell{log: &log}
	rec := &fakeRecorder{log: &log}
	m := newTestModel(sh, nil, rec)
	m.parser.StartBlock("sleep 5", "/tmp") // left running — Flush() must close it out
	cancelled := false
	m.aiCancel = func() {
		cancelled = true
		log = append(log, "aiCancel")
	}

	m.shutdown()

	if !cancelled {
		t.Fatalf("expected aiCancel to be invoked")
	}
	want := []string{"aiCancel", "recorder.Close", "shell.Close"}
	if len(log) != len(want) {
		t.Fatalf("expected log %v, got %v", want, log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("expected log %v, got %v", want, log)
		}
	}
	blocks := m.store.All()
	if len(blocks) != 1 || blocks[0].ExitCode != -1 {
		t.Fatalf("expected the interrupted block to be flushed with ExitCode -1, got %v", blocks)
	}
}

func TestShutdown_NilAICancelAndRecorderAreSafe(t *testing.T) {
	sh := &fakeShell{}
	m := newTestModel(sh, nil, nil) // no recorder, no aiCancel

	m.shutdown()

	if !sh.closed {
		t.Fatalf("expected shell to be closed even with no recorder/aiCancel")
	}
}

// ── renderAIPanel ─────────────────────────────────────────────────────────────

func TestRenderAIPanel_StreamingShowsStatus(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.aiStreaming = true

	out := m.renderAIPanel()
	if !strings.Contains(out, "streaming") {
		t.Fatalf("expected streaming status in panel, got %q", out)
	}
}

func TestRenderAIPanel_ErrorShowsMessage(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.aiError = "boom"

	out := m.renderAIPanel()
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected error message in panel, got %q", out)
	}
}

func TestRenderAIPanel_WithContextShowsBlockCount(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.sendContext = true
	m.store.Add(&block.Block{Command: "ls"})

	out := m.renderAIPanel()
	if !strings.Contains(out, "context: 1 blocks") {
		t.Fatalf("expected context block count in panel, got %q", out)
	}
}

func TestRenderAIPanel_NoContextShowsHint(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.sendContext = false

	out := m.renderAIPanel()
	if !strings.Contains(out, "no context") {
		t.Fatalf("expected no-context hint in panel, got %q", out)
	}
}

func TestRenderAIPanel_SuggestionBarShownWhenNotStreaming(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.suggestedCmd = "ls -la"

	out := m.renderAIPanel()
	if !strings.Contains(out, "ls -la") || !strings.Contains(out, "Tab to run") {
		t.Fatalf("expected suggestion bar with command and hint, got %q", out)
	}
}

func TestRenderAIPanel_SuggestionBarHiddenWhileStreaming(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	m.aiOpen = true
	m.aiStreaming = true
	m.suggestedCmd = "ls -la"

	out := m.renderAIPanel()
	if strings.Contains(out, "Tab to run") {
		t.Fatalf("expected suggestion bar to be hidden while streaming, got %q", out)
	}
}

// TestUpdate_CtrlD_ThenPtyMsg_StillPreservesAnchor is a regression probe
// for an OCR finding claiming HalfDown (Ctrl+D while scrolled) needs its
// own syncViewport() call because it can leave offset > 0 without
// resyncing lastTotal. That's only a real problem if totalLines can drift
// without going through ptyMsg (the only place lines are appended), and
// ptyMsg always calls syncViewport() itself — so HalfDown/PageDown (which
// never change totalLines, only the viewing position) shouldn't need it.
// This proves it empirically rather than by argument alone.
func TestUpdate_CtrlD_ThenPtyMsg_StillPreservesAnchor(t *testing.T) {
	m := newTestModel(&fakeShell{}, nil, nil)
	fillStore(&m, 100)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("test setup broken: expected scrolled after PgUp")
	}

	// Ctrl+D/HalfDown while scrolled — leaves offset > 0 (half-page from a
	// full page up), no syncViewport() call on this path.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = mustModel(t, tm)
	if m.viewport.AtBottom() {
		t.Fatalf("test setup broken: expected still scrolled after a single HalfDown from a PageUp")
	}
	// Compare only the visible content lines, not the full View() — the
	// indicator's own "N lines" text is *expected* to grow as new output
	// appends below an anchored position, that's not drift.
	contentBefore := strings.Join(m.viewport.Slice(m.flattenLines(), m.blockHeight()), "\n")

	// New output streams in — the same anchor-preservation ptyMsg exercises
	// elsewhere, but now with a HalfDown in between the last sync and here.
	fillStore(&m, 5)
	tm, _ = m.Update(ptyMsg{})
	m = mustModel(t, tm)
	contentAfter := strings.Join(m.viewport.Slice(m.flattenLines(), m.blockHeight()), "\n")

	if contentAfter != contentBefore {
		t.Fatalf("visible content drifted after Ctrl+D then new output, despite no growth having occurred at HalfDown time\nbefore:\n%s\nafter:\n%s", contentBefore, contentAfter)
	}
}
