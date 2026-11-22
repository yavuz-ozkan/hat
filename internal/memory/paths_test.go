package memory

import (
	"runtime"
	"testing"
)

func TestUnsafePathRefusesOtherServersDevicesAndNetworkRoots(t *testing.T) {
	base := t.TempDir()
	cases := map[string]bool{
		"//otherhost/share/repo/.git":   true,
		`\\otherhost\share\repo\.git`:   true,
		`\\?\C:\repo\.git`:              true,
		`\\.\pipe\x`:                    true,
		`\??\C:\repo\.git`:              true,
		`//./UNC/otherhost/share`:       true,
		"/net":                          true,
		"/a/../net":                     true,
		"/network/servers/x":            true,
		"/.vol/1/2":                     true,
		"../repo/.git/worktrees/x":      false,
		"/home/someone/repo/.git":       false,
		"C:/dev/git/repo/.git":          false,
		"/networking/x":                 false,
		"//otherhost/share/./repo/.git": true,
	}
	for path, want := range cases {
		if got := unsafePath(path, base); got != want {
			t.Errorf("unsafePath(%q) = %t, want %t", path, got, want)
		}
	}
}

func TestUnsafePathAllowsTheSameServer(t *testing.T) {
	if unsafePath("//build-box/share/repo/.git", "//BUILD-BOX/share/worktree") {
		t.Error("refused a path on the base folder's own server")
	}
}

// Claude's Linux build refuses a path under an automounted host unless the
// folder it's read from is on that host too (fa in Claude Code 2.1.330).
func TestUnsafePathRefusesAnotherNetHostOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Claude's Windows build has no automounted network folders")
	}
	cases := []struct {
		path, base string
		want       bool
	}{
		{"/net/host/x", "/home/me/wt", true},
		{"/net/host/x", "/net/host/wt", false},
		{"/net/HOST/x", "/net/host/wt", false},
		{"/net/other/x", "/net/host/wt", true},
		{"/network/servers/h/x", "/home/me", true},
		{"/networking/x", "/home/me", false},
		{"../x", "/net/host/wt", false},
		{"C:/x", "/home/me", false},
	}
	for _, c := range cases {
		if got := unsafePath(c.path, c.base); got != c.want {
			t.Errorf("unsafePath(%q, %q) = %t, want %t", c.path, c.base, got, c.want)
		}
	}
}

func TestUnsafePathIgnoresNetHostsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Claude's Windows build skips the automounted-host check")
	}
	if unsafePath("/net/host/x", t.TempDir()) {
		t.Error("refused /net/host/x, which Claude's Windows build accepts")
	}
}

// What JavaScript's String.trim drops, checked against Node 22 and Bun 1.3.6.
func TestTrimJSDropsWhatJavaScriptDrops(t *testing.T) {
	for _, r := range []rune{0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0xa0, 0x1680, 0x2000, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff} {
		if got := trimJS(string(r) + "x"); got != "x" {
			t.Errorf("trimJS kept U+%04X", r)
		}
	}
	for _, r := range []rune{0x85, 0x180e, 0x200b} {
		if got := trimJS(string(r) + "x"); got == "x" {
			t.Errorf("trimJS dropped U+%04X, which JavaScript keeps", r)
		}
	}
}

// Node's path.win32.normalize, from Node 22.
func TestWindowsNormalizeMatchesNode(t *testing.T) {
	cases := map[string]string{
		`a\\b`:               `a\b`,
		`\a\\\b\.\c\..\??\x`: `\a\b\??\x`,
		`\.\??\C:\x`:         `\??\C:\x`,
		`a/./b//c/..`:        `a\b`,
		`..\..\a`:            `..\..\a`,
		`\..\..\a`:           `\a`,
		`C:a\\..\b`:          `C:b`,
		`/??/C:/x`:           `\??\C:\x`,
		`a\.\??\x`:           `a\??\x`,
	}
	for in, want := range cases {
		if got := windowsNormalize(in); got != want {
			t.Errorf("windowsNormalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// Node's path.win32.resolve, from Node 22. Bun 1.3.6 agrees on a path on the
// base's own drive.
func TestResolvePathMatchesNodeForDriveRelativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-relative paths exist only on Windows")
	}
	cases := []struct{ base, path, want string }{
		{`C:\base`, `C:foo`, `C:\base\foo`},
		{`c:\base`, `C:foo`, `C:\base\foo`},
		{`C:\base`, `C:`, `C:\base`},
		{`C:\base`, `c:..\x`, `c:\x`},
		{`C:\base`, `\x`, `C:\x`},
		{`C:\base`, `D:\x`, `D:\x`},
	}
	for _, c := range cases {
		if got := resolvePath(c.base, c.path); got != c.want {
			t.Errorf("resolvePath(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}
