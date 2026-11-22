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
