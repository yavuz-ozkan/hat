package memory

import "testing"

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
		"/net/host/x":                   false,
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
