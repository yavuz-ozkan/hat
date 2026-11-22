package memory

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func isAutofsMapRoot(string) bool { return false }

// isMissing counts what Claude's runtime reports as ENOENT. Bun reports a
// name Windows can't hold, like one with a colon, as missing too.
func isMissing(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, windows.ERROR_INVALID_NAME)
}
