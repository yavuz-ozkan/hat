package cli

import (
	"path/filepath"
	"syscall"
	"testing"
)

// makeUndeletable puts a file under path that can't be deleted until release
// runs: Windows won't delete a file opened without FILE_SHARE_DELETE, as a
// running Claude can hold one.
func makeUndeletable(t *testing.T, path string) (release func()) {
	t.Helper()
	file := filepath.Join(path, "in-use")
	mustWrite(t, file, "held")
	name, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	release = func() {
		if !closed {
			closed = true
			_ = syscall.CloseHandle(handle)
		}
	}
	t.Cleanup(release)
	return release
}
