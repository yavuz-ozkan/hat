//go:build !windows

package memory

import "testing"

func longPathTempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}
