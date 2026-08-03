//go:build aix || linux || solaris || zos

package pty

import "golang.org/x/sys/unix"

// See termios_bsd.go for why this split exists.
const (
	ioctlReadTermios  = unix.TCGETS
	ioctlWriteTermios = unix.TCSETS
)
