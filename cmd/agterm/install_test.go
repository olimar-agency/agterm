package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempRC(t *testing.T, content string) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// point SHELL and HOME so install/uninstall pick up our temp file
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", dir)
	return rc, func() {}
}

func TestInstall_InjectsHooks(t *testing.T) {
	rc, cleanup := writeTempRC(t, "# existing config\n")
	defer cleanup()

	if err := runInstall(nil); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, _ := os.ReadFile(rc)
	if !strings.Contains(string(data), startMarker) {
		t.Error("startMarker not found after install")
	}
	if !strings.Contains(string(data), endMarker) {
		t.Error("endMarker not found after install")
	}
	// original content preserved
	if !strings.Contains(string(data), "# existing config") {
		t.Error("original content was lost")
	}
}

func TestInstall_Idempotent(t *testing.T) {
	rc, cleanup := writeTempRC(t, "")
	defer cleanup()

	if err := runInstall(nil); err != nil {
		t.Fatal(err)
	}
	if err := runInstall(nil); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(rc)
	count := strings.Count(string(data), startMarker)
	if count != 1 {
		t.Errorf("startMarker appears %d times after two installs, want 1", count)
	}
}

func TestInstall_DryRun(t *testing.T) {
	rc, cleanup := writeTempRC(t, "# original\n")
	defer cleanup()

	if err := runInstall([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(rc)
	if strings.Contains(string(data), startMarker) {
		t.Error("dry-run should not modify the file")
	}
}

func TestUninstall_RemovesHooks(t *testing.T) {
	rc, cleanup := writeTempRC(t, "# before\n")
	defer cleanup()

	if err := runInstall(nil); err != nil {
		t.Fatal(err)
	}
	if err := runUninstall(nil); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}

	data, _ := os.ReadFile(rc)
	if strings.Contains(string(data), startMarker) {
		t.Error("hooks still present after uninstall")
	}
	if !strings.Contains(string(data), "# before") {
		t.Error("content before hooks was lost on uninstall")
	}
}

func TestUninstall_Idempotent(t *testing.T) {
	writeTempRC(t, "")
	cleanup := func() {}
	defer cleanup()

	// uninstall on a file with no hooks should not error
	if err := runUninstall(nil); err != nil {
		t.Fatalf("uninstall on clean file should not error: %v", err)
	}
}

func TestInstall_BackupCreated(t *testing.T) {
	rc, cleanup := writeTempRC(t, "# content\n")
	defer cleanup()

	if err := runInstall(nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(rc + ".agterm.bak"); os.IsNotExist(err) {
		t.Error("backup file not created")
	}
}
