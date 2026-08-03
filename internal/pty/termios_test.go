package pty

import (
	"testing"

	creackpty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// TestDisableEcho_ClearsOnlyTheEchoBit opens a real pty pair (not a full
// shell) and asserts disableEcho clears ECHO while leaving every other
// termios setting untouched — agterm#18 wants a surgical fix, not raw mode,
// so canonical mode / signal generation for the child must survive.
func TestDisableEcho_ClearsOnlyTheEchoBit(t *testing.T) {
	ptm, tty, err := creackpty.Open()
	if err != nil {
		t.Skipf("pty.Open unavailable in this environment: %v", err)
	}
	defer ptm.Close()
	defer tty.Close()

	before, err := unix.IoctlGetTermios(int(tty.Fd()), ioctlReadTermios)
	if err != nil {
		t.Fatalf("IoctlGetTermios: %v", err)
	}
	if before.Lflag&unix.ECHO == 0 {
		t.Fatalf("test assumption broken: a freshly opened pty should default to ECHO enabled")
	}

	if err := disableEcho(tty); err != nil {
		t.Fatalf("disableEcho: %v", err)
	}

	after, err := unix.IoctlGetTermios(int(tty.Fd()), ioctlReadTermios)
	if err != nil {
		t.Fatalf("IoctlGetTermios after disableEcho: %v", err)
	}

	if after.Lflag&unix.ECHO != 0 {
		t.Fatalf("expected ECHO cleared, Lflag = %#x", after.Lflag)
	}
	// Everything else in Lflag must be untouched — this is not raw mode.
	wantLflag := before.Lflag &^ unix.ECHO
	if after.Lflag != wantLflag {
		t.Fatalf("Lflag changed beyond ECHO: before=%#x after=%#x want=%#x", before.Lflag, after.Lflag, wantLflag)
	}
	if after.Iflag != before.Iflag {
		t.Fatalf("Iflag changed, expected untouched: before=%#x after=%#x", before.Iflag, after.Iflag)
	}
	if after.Oflag != before.Oflag {
		t.Fatalf("Oflag changed, expected untouched: before=%#x after=%#x", before.Oflag, after.Oflag)
	}
	if after.Cflag != before.Cflag {
		t.Fatalf("Cflag changed, expected untouched: before=%#x after=%#x", before.Cflag, after.Cflag)
	}
	// Canonical mode and signal generation specifically must survive —
	// these matter for interactive programs run via full PTY passthrough
	// (m.running in internal/tui/model.go).
	if after.Lflag&unix.ICANON == 0 {
		t.Fatalf("expected ICANON to survive disableEcho, Lflag = %#x", after.Lflag)
	}
	if after.Lflag&unix.ISIG == 0 {
		t.Fatalf("expected ISIG to survive disableEcho, Lflag = %#x", after.Lflag)
	}
}
