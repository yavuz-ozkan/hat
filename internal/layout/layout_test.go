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
		if got := l.UsageFile(configDir); got != filepath.Join(l.UsageDir, want) {
			t.Errorf("UsageFile(%q) = %s, want %s in %s", configDir, got, want, l.UsageDir)
		}
	}
}

func TestUsageFileKeepsFoldersOutsideHomeApart(t *testing.T) {
	l := testLayout(t)
	elsewhere := filepath.Join(t.TempDir(), ".claude-work")
	other := filepath.Join(t.TempDir(), ".claude-work")

	a, b := l.UsageFile(elsewhere), l.UsageFile(other)

	if a == b || a == l.UsageFile(l.AccountDir("work")) {
		t.Errorf("folders share a usage file: %s, %s", a, b)
	}
	if !strings.HasPrefix(filepath.Base(a), "elsewhere-") || filepath.Dir(a) != l.UsageDir {
		t.Errorf("UsageFile(%q) = %s", elsewhere, a)
	}
}

func TestUsageFileIgnoresCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows paths are case-insensitive")
	}
	l := testLayout(t)
	if got := l.UsageFile(strings.ToUpper(l.AccountDir("work"))); got != filepath.Join(l.UsageDir, "claude-work.json") {
		t.Errorf("UsageFile with other case = %s", got)
	}
}

func TestUsageDirIsOutsideEveryClaudeFolder(t *testing.T) {
	l := testLayout(t)
	if strings.HasPrefix(l.UsageDir, l.MainDir) || strings.Contains(l.UsageDir, AccountDirPrefix) {
		t.Errorf("UsageDir %s is inside a Claude config folder", l.UsageDir)
	}
}
