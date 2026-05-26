package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func main() {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptm, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agterm: %v\n", err)
		os.Exit(1)
	}
	defer ptm.Close()

	// sync terminal size on SIGWINCH
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for range sigCh {
			if ws, err := pty.GetsizeFull(os.Stdout); err == nil {
				pty.Setsize(ptm, ws)
			}
		}
	}()
	sigCh <- syscall.SIGWINCH

	// put stdin in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "agterm: raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// pty output → stdout
	go io.Copy(os.Stdout, ptm)
	// stdin → pty input
	io.Copy(ptm, os.Stdin)
}
