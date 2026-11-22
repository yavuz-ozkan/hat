package memory

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Bun and Node call a junction a link only when it points at a drive path. A
// junction to a volume, \\?\Volume{...}\, is a plain folder to them, though
// Go's Readlink reads it (checked with Bun 1.3.6 and Node 22).
func TestKindOfTellsDriveJunctionsFromVolumeJunctions(t *testing.T) {
	base := t.TempDir()
	toDrive, toVolume := filepath.Join(base, "to-drive"), filepath.Join(base, "to-volume")
	folderLink(t, toDrive, filepath.Join(base))
	volume := make([]uint16, windows.MAX_PATH)
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(base) + `\`)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.GetVolumeNameForVolumeMountPoint(root, &volume[0], uint32(len(volume))); err != nil {
		t.Skipf("no volume name for %s: %v", base, err)
	}
	folderLink(t, toVolume, windows.UTF16ToString(volume))

	if got := entryKindOf(toDrive); got != linkEntry {
		t.Errorf("a junction to a drive path is kind %d, want a link", got)
	}
	if got := entryKindOf(toVolume); got != folderEntry {
		t.Errorf("a junction to a volume is kind %d, want a folder", got)
	}
}

// irregularInfo is what Go's Lstat gives for a reparse point that isn't a
// link, like a OneDrive file (tag 0x9000601a): no type bits but irregular.
type irregularInfo struct{ attributes uint32 }

func (i irregularInfo) Name() string       { return "file" }
func (i irregularInfo) Size() int64        { return 0 }
func (i irregularInfo) Mode() fs.FileMode  { return fs.ModeIrregular | 0o666 }
func (i irregularInfo) ModTime() time.Time { return time.Time{} }
func (i irregularInfo) IsDir() bool        { return false }
func (i irregularInfo) Sys() any {
	return &syscall.Win32FileAttributeData{FileAttributes: i.attributes}
}

func TestKindOfTakesAOneDriveFileForAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-link")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := kindOf(path, irregularInfo{attributes: 0x420}); got != fileEntry {
		t.Errorf("a hydrated OneDrive file (attributes 0x420) is kind %d, want a file", got)
	}
	if got := kindOf(path, irregularInfo{attributes: 0x430}); got != folderEntry {
		t.Errorf("a OneDrive folder (attributes 0x430) is kind %d, want a folder", got)
	}
}

// longPathTempDir is t.TempDir() with any 8.3 short names spelled out, as on
// a CI runner whose TEMP is C:\Users\RUNNER~1\... git writes the long path
// into a worktree's back-pointer, and Claude compares it with the launch
// folder letter for letter.
func longPathTempDir(t *testing.T) string {
	t.Helper()
	return longPathName(t, t.TempDir())
}

// folderLink makes link point at the folder target the way a person would
// on this system: a junction, which needs no Developer Mode.
func folderLink(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
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
