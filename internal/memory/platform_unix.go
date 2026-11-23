//go:build !windows

package memory

import (
	"io/fs"
	"regexp"
	"strings"
)

// Claude's Linux build treats only a leading slash as absolute and only a
// slash as a separator.
var (
	startsAbsolute = regexp.MustCompile(`^/`)
	walkStart      = regexp.MustCompile(`^/`)
	separators     = regexp.MustCompile(`/+`)
	endsInSep      = regexp.MustCompile(`/$`)
)

// netHostOf is the host an automounted network path is on: <host> in
// /net/<host>/... or /network/servers/<host>/..., lowercased the way Claude's
// Linux build does it.
func netHostOf(path string) (string, bool) {
	if !strings.HasPrefix(path, "/") {
		return "", false
	}
	host, found := "", false
	walkSlashPath(path, func(kept []string) {
		if !found && isNetHostFolder(kept) {
			host, found = asciiLower(kept[len(kept)-1]), true
		}
	})
	return host, found
}

func isNetHostFolder(kept []string) bool {
	return len(kept) == 2 && foldedName(kept[0]) == "net" ||
		len(kept) == 3 && foldedName(kept[0]) == "network" && foldedName(kept[1]) == "servers"
}

// foldedName drops the invisible characters Claude drops from a folder name
// before comparing it: zero-width joiners and marks, bidi controls, and the
// byte order mark.
func foldedName(name string) string {
	return strings.ToLower(strings.ToUpper(strings.Map(func(r rune) rune {
		if 0x200c <= r && r <= 0x200f || 0x202a <= r && r <= 0x202e || 0x206a <= r && r <= 0x206f || r == 0xfeff {
			return -1
		}
		return r
	}, name)))
}

// reparsePointKind is for Windows; elsewhere Go reports an entry it can't
// type as irregular, and Claude's runtime sees it as neither file nor folder.
func reparsePointKind(fs.FileInfo) entryKind { return specialEntry }
