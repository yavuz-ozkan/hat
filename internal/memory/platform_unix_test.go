//go:build !windows

package memory

import (
	"os"
	"testing"
)

func longPathTempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// folderLink makes link point at the folder target the way a person would
// on this system: a symlink.
func folderLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
