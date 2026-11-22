package memory

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const autofsSuperMagic = 0x0187

// isAutofsMapRoot is whether dir is an autofs map root right under /, like
// /net, where looking up a name asks autofs to mount it. Claude Code 2.1.330
// doesn't look for a .git in one.
func isAutofsMapRoot(dir string) bool {
	if dir == "/" || filepath.Dir(dir) != "/" {
		return false
	}
	var stat unix.Statfs_t
	return unix.Statfs(dir, &stat) == nil && stat.Type == autofsSuperMagic
}

func isMissing(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
