package pty

import (
	"os"
	"os/exec"

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

	ptm, err := creackpty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return &Shell{cmd: cmd, ptm: ptm}, nil
}

func (s *Shell) Read(p []byte) (int, error)  { return s.ptm.Read(p) }
func (s *Shell) Write(p []byte) (int, error) { return s.ptm.Write(p) }

func (s *Shell) Resize(rows, cols uint16) error {
	return creackpty.Setsize(s.ptm, &creackpty.Winsize{Rows: rows, Cols: cols})
}

func (s *Shell) Close() {
	s.cmd.Process.Kill()
	s.ptm.Close()
}
