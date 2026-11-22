package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRefusesScriptsThatRunThroughCmd(t *testing.T) {
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	for _, name := range []string{"claude.cmd", "claude.bat", "CLAUDE.CMD"} {
		dir := t.TempDir()
		script := filepath.Join(dir, name)
		if err := os.WriteFile(script, []byte("@echo off\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)

		for _, executable := range []string{"claude", script} {
			_, err := Find(executable)
			if err == nil || !strings.Contains(err.Error(), "native Claude Code install") || !strings.Contains(err.Error(), setupURL) {
				t.Errorf("%s: Find(%q) = %v, want a refusal that points at the native install", name, executable, err)
			}
		}
	}
}

func TestFindAcceptsANativeExe(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "claude.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got, err := Find(exe); err != nil || got != exe {
		t.Errorf("Find(%q) = %q, %v", exe, got, err)
	}
}

// Windows drops trailing dots and spaces from a file name, so claude.cmd. and
// "claude.cmd " are claude.cmd. A PATHEXT entry with a trailing dot or space
// makes Go's PATH lookup return such a name.
func TestFindRefusesABatchFileNamedWithTrailingDotsOrSpaces(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude.cmd")
	if err := os.WriteFile(script, []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	for _, pathext := range []string{".CMD.", ".CMD ", ".cmd. .", ".BAT;.CMD  "} {
		t.Setenv("PATHEXT", pathext)
		if got, err := Find("claude"); err == nil {
			t.Errorf("PATHEXT=%q: Find = %q, want a refusal", pathext, got)
		}
	}
	for _, named := range []string{script + ".", script + " ", script + ". ."} {
		if got, err := Find(named); err == nil {
			t.Errorf("Find(%q) = %q, want a refusal", named, got)
		}
	}
}
