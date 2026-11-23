//go:build !linux && !windows

package memory

import (
	"errors"
	"os"
	"syscall"
)

func isAutofsMapRoot(string) bool { return false }

func isMissing(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
