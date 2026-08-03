//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package pty

import "golang.org/x/sys/unix"

// BSD-family termios ioctl requests (including darwin) use the TIOCGETA /
// TIOCSETA names; the "other" unix family (linux, aix, solaris, zos) uses
// TCGETS / TCSETS instead — see termios_unix.go. Mirrors the same split
// charmbracelet/x/term (already a dependency, via bubbletea) uses for its
// own raw-mode support.
const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
)
