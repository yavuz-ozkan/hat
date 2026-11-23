package memory

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"syscall"

	"golang.org/x/sys/windows"
)

func isAutofsMapRoot(string) bool { return false }

// Claude's Windows build treats a leading slash or backslash, or a drive
// letter and one, as absolute, and either slash as a separator. When it walks
// a path it skips a UNC share, a drive, or a leading slash.
var (
	startsAbsolute = regexp.MustCompile(`^(?:[/\\]|[A-Za-z]:[/\\])`)
	walkStart      = regexp.MustCompile(`^(?:[/\\]{2}[^/\\]+[/\\][^/\\]+[/\\]?|[A-Za-z]:[/\\]|[/\\])`)
	separators     = regexp.MustCompile(`[/\\]+`)
	endsInSep      = regexp.MustCompile(`[\\/]$`)
)

// Claude's Windows build has no automounted network folders to check.
func netHostOf(string) (string, bool) { return "", false }

// isMissing counts what Claude's runtime reports as ENOENT. Bun reports a
// name Windows can't hold, like one with a colon, as missing too.
func isMissing(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, windows.ERROR_INVALID_NAME)
}

// reparsePointKind sorts an entry Go marks irregular: a reparse point that
// isn't a link to a drive, like a junction to a volume or a OneDrive file
// (tag 0x9000601a). Go leaves the folder bit off some of them, and libuv,
// which Claude's runtime follows, goes by the folder attribute alone.
func reparsePointKind(info fs.FileInfo) entryKind {
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if ok && attrs.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return folderEntry
	}
	return fileEntry
}
