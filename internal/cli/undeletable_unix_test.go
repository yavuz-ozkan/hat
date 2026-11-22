//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// makeUndeletable puts a folder at path whose file can't be deleted until
// release runs: Unix won't delete from a read-only folder.
func makeUndeletable(t *testing.T, path string) (release func()) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can delete from a read-only folder")
	}
	mustWrite(t, filepath.Join(path, "in-use"), "held")
	if err := os.Chmod(path, 0o500); err != nil {
		t.Fatal(err)
	}
	release = func() { _ = os.Chmod(path, 0o700) }
	t.Cleanup(release)
	return release
}
