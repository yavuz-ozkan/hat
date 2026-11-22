package memory

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// longPathTempDir is t.TempDir() with any 8.3 short names spelled out, as on
// a CI runner whose TEMP is C:\Users\RUNNER~1\... git writes the long path
// into a worktree's back-pointer, and Claude compares it with the launch
// folder letter for letter.
func longPathTempDir(t *testing.T) string {
	t.Helper()
	return longPathName(t, t.TempDir())
}

func longPathName(t *testing.T, path string) string {
	t.Helper()
	return convertPath(t, path, windows.GetLongPathName)
}

func shortPathName(t *testing.T, path string) string {
	t.Helper()
	return convertPath(t, path, windows.GetShortPathName)
}

func convertPath(t *testing.T, path string, convert func(*uint16, *uint16, uint32) (uint32, error)) string {
	t.Helper()
	from, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := convert(from, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		t.Fatalf("converting %s: %v", path, err)
	}
	return windows.UTF16ToString(buf[:n])
}

// Bun's realpathSync, which Claude Code runs, keeps an 8.3 short name as
// written (Bun 1.3.6; Node's JavaScript realpathSync does the same), so a
// worktree entered through its short name doesn't match the long path git
// wrote back, and keeps its own memory.
func TestProjectRootKeepsAWorktreeEnteredByItsShortName(t *testing.T) {
	base := filepath.Join(longPathTempDir(t), "long-folder-name-for-8dot3")
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	short := shortPathName(t, worktree)
	if short == worktree {
		t.Skip("this volume doesn't make 8.3 short names")
	}

	if got := ProjectRoot(worktree); got != repo {
		t.Errorf("ProjectRoot(%q) = %q, want the repo %q", worktree, got, repo)
	}
	if got := ProjectRoot(short); got != short {
		t.Errorf("ProjectRoot(%q) = %q, want the short-named worktree itself", short, got)
	}
}
