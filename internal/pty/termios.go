package pty

import (
	"os"

	"golang.org/x/sys/unix"
)

// disableEcho clears the pty slave's ECHO flag, leaving every other termios
// setting (canonical mode, signal generation, ...) untouched — a surgical
// fix for agterm#18, not a switch to raw mode. See New()'s call site for
// why: agterm composes each command line in its own input widget and writes
// it to the pty atomically rather than forwarding keystrokes as the user
// types, so the kernel line discipline's default ECHO makes that write come
// straight back as if it were command output, duplicating what Block's own
// header already renders from Block.Command.
func disableEcho(f *os.File) error {
	termios, err := unix.IoctlGetTermios(int(f.Fd()), ioctlReadTermios)
	if err != nil {
		return err
	}
	termios.Lflag &^= unix.ECHO
	return unix.IoctlSetTermios(int(f.Fd()), ioctlWriteTermios, termios)
}
