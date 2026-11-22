package layout

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testLayout(t *testing.T) Layout {
	t.Helper()
	home := t.TempDir()
	return ForHome(home, UserDirs{Config: filepath.Join(home, "config"), Cache: filepath.Join(home, "cache")})
}

func TestUsageFileIsNamedForTheConfigFolder(t *testing.T) {
	l := testLayout(t)
	cases := map[string]string{
		"":                   "main.json",
		l.MainDir:            "main.json",
		l.MainDir + "/":      "main.json",
		l.AccountDir("work"): "claude-work.json",
	}
	for configDir, want := range cases {
		if got, listed := l.UsageFile(configDir); !listed || got != filepath.Join(l.UsageDir, want) {
			t.Errorf("UsageFile(%q) = %s, %t, want %s in %s", configDir, got, listed, want, l.UsageDir)
		}
	}
}

func TestUsageFileIsNotListedForFoldersHatUsageNeverShows(t *testing.T) {
	l := testLayout(t)
	for _, configDir := range []string{filepath.Join(t.TempDir(), ".claude-work"), filepath.Join(l.Home, "claude-work"), l.AccountDir("")} {
		if got, listed := l.UsageFile(configDir); listed {
			t.Errorf("UsageFile(%q) = %s, want it not listed", configDir, got)
		}
	}
}

func TestUsageFileIgnoresCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows paths are case-insensitive")
	}
	l := testLayout(t)
	if got, _ := l.UsageFile(strings.ToUpper(l.AccountDir("work"))); got != filepath.Join(l.UsageDir, "claude-work.json") {
		t.Errorf("UsageFile with other case = %s", got)
	}
}

func TestUsageDirIsOutsideEveryClaudeFolder(t *testing.T) {
	l := testLayout(t)
	if strings.HasPrefix(l.UsageDir, l.MainDir) || strings.Contains(l.UsageDir, AccountDirPrefix) {
		t.Errorf("UsageDir %s is inside a Claude config folder", l.UsageDir)
	}
}
