package pty

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	creackpty "github.com/creack/pty"
)

type Shell struct {
	cmd *exec.Cmd
	ptm *os.File
}

func New(shellPath string) (*Shell, error) {
	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
		if shellPath == "" {
			shellPath = "/bin/bash"
		}
	}

	cmd := exec.Command(shellPath)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Open (rather than creackpty.Start, which would leave the slave's
	// termios at its default) so disableEcho can run before the shell
	// starts reading from it — see termios.go and agterm#18.
	ptm, tty, err := creackpty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close() //nolint:errcheck // best-effort; the parent's copy of the fd is no longer needed once cmd.Start() has dup'd it into the child

	if err := disableEcho(tty); err != nil {
		ptm.Close() //nolint:errcheck
		return nil, err
	}

	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	// Setsid + Setctty: replicates what creackpty.Start does internally
	// (via StartWithSize), starting the shell in a new session with tty as
	// its controlling terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	if err := cmd.Start(); err != nil {
		ptm.Close() //nolint:errcheck
		return nil, err
	}

	return &Shell{cmd: cmd, ptm: ptm}, nil
}

func (s *Shell) Read(p []byte) (int, error)  { return s.ptm.Read(p) }
func (s *Shell) Write(p []byte) (int, error) { return s.ptm.Write(p) }

func (s *Shell) Resize(rows, cols uint16) error {
	return creackpty.Setsize(s.ptm, &creackpty.Winsize{Rows: rows, Cols: cols})
}

func (s *Shell) Close() error {
	var firstErr error

	if s.cmd != nil && s.cmd.Process != nil {
		if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			firstErr = err
		}
	}

	if s.ptm != nil {
		if err := s.ptm.Close(); err != nil && !errors.Is(err, os.ErrClosed) && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
