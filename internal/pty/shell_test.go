package pty

import (
	"os"
	"strings"
	"testing"
	"time"
)

// requireBinary skips the test when path isn't present, so these tests
// don't fail on minimal/container environments lacking a full shell set.
func requireBinary(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not available: %v", path, err)
	}
}

// readWithTimeout reads from s until data is available or the timeout
// elapses, avoiding a hung test if the shell never produces output.
func readWithTimeout(t *testing.T, s *Shell, timeout time.Duration) string {
	t.Helper()
	type result struct {
		n   int
		buf []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := s.Read(buf)
		ch <- result{n: n, buf: buf, err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read: %v", r.err)
		}
		return string(r.buf[:r.n])
	case <-time.After(timeout):
		t.Fatal("timed out waiting for shell output")
		return ""
	}
}

func TestNew_SpawnsRequestedShell(t *testing.T) {
	requireBinary(t, "/bin/sh")
	s, err := New("/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if s.cmd == nil || s.cmd.Path != "/bin/sh" {
		t.Fatalf("expected cmd.Path /bin/sh, got %+v", s.cmd)
	}
	if s.ptm == nil {
		t.Fatal("expected non-nil ptm")
	}
}

func TestNew_FallsBackToShellEnv(t *testing.T) {
	requireBinary(t, "/bin/sh")
	t.Setenv("SHELL", "/bin/sh")
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if s.cmd.Path != "/bin/sh" {
		t.Fatalf("expected fallback to $SHELL=/bin/sh, got %q", s.cmd.Path)
	}
}

func TestNew_FallsBackToBinBashWhenShellUnset(t *testing.T) {
	requireBinary(t, "/bin/bash")
	t.Setenv("SHELL", "")
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if s.cmd.Path != "/bin/bash" {
		t.Fatalf("expected fallback to /bin/bash, got %q", s.cmd.Path)
	}
}

func TestShell_WriteAndRead(t *testing.T) {
	requireBinary(t, "/bin/sh")
	s, err := New("/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	marker := "AGTERM_SHELL_TEST_MARKER"
	if _, err := s.Write([]byte("echo " + marker + "\r")); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var seen strings.Builder
	for time.Now().Before(deadline) {
		seen.WriteString(readWithTimeout(t, s, 5*time.Second))
		if strings.Contains(seen.String(), marker) {
			return
		}
	}
	t.Fatalf("expected output to contain %q, got %q", marker, seen.String())
}

func TestShell_Resize(t *testing.T) {
	requireBinary(t, "/bin/sh")
	s, err := New("/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if err := s.Resize(40, 120); err != nil {
		t.Fatalf("resize: %v", err)
	}
}

func TestShell_CloseIsIdempotent(t *testing.T) {
	requireBinary(t, "/bin/sh")
	s, err := New("/bin/sh")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close should not error, got: %v", err)
	}
}

func TestShell_CloseWithNilFields(t *testing.T) {
	s := &Shell{}
	if err := s.Close(); err != nil {
		t.Fatalf("close on zero-value Shell should not error, got: %v", err)
	}
}
