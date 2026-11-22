package memory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These are the path checks Claude Code 2.1.330 runs on every path it reads
// out of a .git file, a commondir or gitdir file, or a link, before it trusts
// that path for the project root. hat runs the same checks so it lands on the
// same root, including when Claude refuses a path and keeps the folder it
// started from. The Windows and Linux builds of Claude differ in what counts
// as an absolute path and a separator (platform_*.go).

// maxLinks is how many links Claude follows in one path before refusing it.
const maxLinks = 40

var (
	anySlashes         = regexp.MustCompile(`[/\\]+`)
	twoLeadingSlashes  = regexp.MustCompile(`^[\\/]{2}`)
	devicePrefix       = regexp.MustCompile(`^[\\/]{2}[?.][\\/]`)
	ntPrefix           = regexp.MustCompile(`^[\\/]\?\?[\\/]`)
	uncPrefixAfterRoot = regexp.MustCompile(`(?i)^[?.][\\/]unc[\\/]`)
	uncServer          = regexp.MustCompile(`^[^\\/]+`)
	dotsOnlySegment    = regexp.MustCompile(`^\.[. ]*$`)
	macVolumeSegment   = regexp.MustCompile(`(?i)/\.(?:vol|file|nofollow|resolve)(?:/|$)`)
	macVolumeName      = regexp.MustCompile(`(?i)^\.(?:vol|file|nofollow|resolve)$`)
)

// unsafePath is whether Claude refuses text, a path read from a file, before
// looking at the disk: a path on another network share, a Windows device or
// NT path, or a network mount root like /net or /network.
func unsafePath(text, base string) bool {
	resolved := resolvePath(base, text)
	if leavesTheShare(text, base) || leavesTheShare(resolved, base) {
		return true
	}
	for _, path := range []string{text, resolved} {
		if onAnotherNetHost(path, base) || startsInNetwork(path) || isNetFolder(path) || isMacVolumePath(path) {
			return true
		}
	}
	return false
}

func leavesTheShare(path, base string) bool {
	if !twoLeadingSlashes.MatchString(path) && !isNTPath(path) {
		return false
	}
	if devicePrefix.MatchString(path) || isNTPath(path) || hasDotsOnlySegment(path) {
		return true
	}
	server, ok := serverOf(path)
	baseServer, baseOK := serverOf(base)
	return !ok || !baseOK || server != baseServer
}

// onAnotherNetHost is whether path is under an automounted host, like
// /net/<host>, other than the one base is under.
func onAnotherNetHost(path, base string) bool {
	host, ok := netHostOf(path)
	if !ok {
		return false
	}
	baseHost, baseOK := netHostOf(base)
	return !baseOK || host != baseHost
}

func isNTPath(path string) bool {
	return ntPrefix.MatchString(path) || strings.Contains(path, "??") && ntPrefix.MatchString(windowsNormalize(path))
}

// windowsNormalize is Node's path.win32.normalize for paths that don't start
// with two slashes: those can't become an NT path, which starts with one.
func windowsNormalize(path string) string {
	path = strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(path, `\\`) {
		return path
	}
	drive := ""
	if len(path) >= 2 && path[1] == ':' && isASCIILetter(path[0]) {
		drive, path = path[:2], path[2:]
	}
	rooted := strings.HasPrefix(path, `\`)
	var kept []string
	for _, segment := range strings.Split(path, `\`) {
		switch {
		case segment == "" || segment == ".":
		case segment == ".." && len(kept) > 0 && kept[len(kept)-1] != "..":
			kept = kept[:len(kept)-1]
		case segment == ".." && rooted:
		default:
			kept = append(kept, segment)
		}
	}
	joined := strings.Join(kept, `\`)
	if rooted {
		return drive + `\` + joined
	}
	if joined == "" {
		joined = "."
	}
	return drive + joined
}

func isASCIILetter(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

func serverOf(path string) (string, bool) {
	if !twoLeadingSlashes.MatchString(path) {
		return "", false
	}
	rest := path[2:]
	if devicePrefix.MatchString(path) {
		if !uncPrefixAfterRoot.MatchString(rest) {
			return "", false
		}
		rest = rest[len("?/unc/"):]
	}
	server := uncServer.FindString(rest)
	if server == "" {
		return "", false
	}
	return asciiLower(server), true
}

func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

func hasDotsOnlySegment(path string) bool {
	for _, segment := range anySlashes.Split(path, -1) {
		if dotsOnlySegment.MatchString(segment) {
			return true
		}
	}
	return false
}

func walkSlashPath(path string, visit func(kept []string)) []string {
	var kept []string
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "", ".":
		case "..":
			if len(kept) > 0 {
				kept = kept[:len(kept)-1]
			}
		default:
			kept = append(kept, segment)
			visit(kept)
		}
	}
	return kept
}

func passesThroughTop(path string, isTop func(string) bool) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	passes := false
	walkSlashPath(path, func(kept []string) {
		passes = passes || len(kept) == 1 && isTop(kept[0])
	})
	return passes
}

func startsInNetwork(path string) bool {
	return passesThroughTop(path, func(top string) bool { return strings.ToLower(top) == "network" })
}

func isNetFolder(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	kept := walkSlashPath(path, func([]string) {})
	return len(kept) == 1 && strings.ToLower(kept[0]) == "net"
}

func isMacVolumePath(path string) bool {
	return macVolumeSegment.MatchString(path) && passesThroughTop(path, macVolumeName.MatchString)
}

// unsafeWalk is whether Claude refuses text, read from a file in base, after
// walking it on disk both as written and as resolved against base. Network
// hosts are compared with checkBase's.
func unsafeWalk(text, base, checkBase string) bool {
	written := text
	if !startsAbsolute.MatchString(text) {
		written = base + string(filepath.Separator) + text
	}
	if !walksSafely(written, checkBase, maxLinks) {
		return true
	}
	resolved := resolvePath(base, text)
	return resolved != written && !walksSafely(resolved, checkBase, maxLinks)
}

// walksSafely is whether every part of path that exists is a plain file or
// folder, or a link whose target passes the same checks, within linksLeft
// links. Missing parts are fine. Each part is checked by name before it is
// looked at, so a path into another network host is refused without asking
// the automounter for it.
func walksSafely(path, checkBase string, linksLeft int) bool {
	if linksLeft <= 0 {
		return false
	}
	at := walkStart.FindString(path)
	segments := separators.Split(path[len(at):], -1)
	for i, segment := range segments {
		switch segment {
		case "", ".":
			continue
		case "..":
			at = filepath.Dir(at)
			continue
		}
		at = filepath.Join(at, segment)
		if isMacVolumePath(at) || onAnotherNetHost(at, checkBase) || startsInNetwork(at) {
			return false
		}
		switch entryKindOf(at) {
		case unreadableEntry, specialEntry:
			return false
		case linkEntry:
			target, ok := linkText(at)
			if !ok || unsafePath(target, filepath.Dir(at)) {
				return false
			}
			next := target
			if !startsAbsolute.MatchString(target) {
				next = filepath.Dir(at) + string(filepath.Separator) + target
			}
			if rest := strings.Join(segments[i+1:], string(filepath.Separator)); rest != "" {
				if !endsInSep.MatchString(next) {
					next += string(filepath.Separator)
				}
				next += rest
			}
			return walksSafely(next, checkBase, linksLeft-1)
		}
	}
	return true
}

type entryKind int

const (
	missingEntry entryKind = iota
	fileEntry
	folderEntry
	linkEntry
	specialEntry
	unreadableEntry
)

func entryKindOf(path string) entryKind {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		return kindOf(path, info)
	case isMissing(err):
		return missingEntry
	default:
		return unreadableEntry
	}
}

// kindOf sorts what Lstat found the way Claude's runtime does. Go reports a
// Windows junction as irregular, where libuv calls it a link only when it
// points at a drive path (\??\C:\...): a junction to a volume
// (\??\Volume{...}\) is a plain folder there.
func kindOf(path string, info fs.FileInfo) entryKind {
	if info.Mode()&fs.ModeSymlink != 0 {
		return linkEntry
	}
	if runtime.GOOS == "windows" && info.Mode()&fs.ModeIrregular != 0 {
		target, err := os.Readlink(path)
		if err == nil && pointsAtADrive(target) {
			return linkEntry
		}
	}
	return followedKindOf(info)
}

// pointsAtADrive is whether a junction target, as os.Readlink gives it, is a
// drive path.
func pointsAtADrive(target string) bool {
	return len(target) >= 3 && isASCIILetter(target[0]) && target[1] == ':' && os.IsPathSeparator(target[2])
}

// followedKindOf sorts what Stat found, through any link.
func followedKindOf(info fs.FileInfo) entryKind {
	mode := info.Mode()
	switch {
	case mode.IsDir():
		return folderEntry
	case mode.IsRegular():
		return fileEntry
	case mode.Type() == fs.ModeIrregular:
		return reparsePointKind(info)
	default:
		return specialEntry
	}
}

// linkText is a link's target as Claude reads it: refused when it holds a
// NUL or isn't valid UTF-8.
func linkText(path string) (string, bool) {
	target, err := os.Readlink(path)
	if err != nil || strings.Contains(target, "\x00") || !utf8.ValidString(target) {
		return "", false
	}
	return target, true
}

// maxRealPathLinks stands in for the loop error the operating system gives
// a path that links to itself.
const maxRealPathLinks = 255

var errTooManyLinks = errors.New("too many links")

// realPath is Node's JavaScript fs.realpathSync, which Claude's runtime uses
// on Windows too: it follows links, and keeps every other part of the path
// as written, so c:\dev stays c:\dev where Go's EvalSymlinks would give C:\dev,
// and an 8.3 name like RUNNER~1 stays short.
func realPath(path string) (string, error) {
	path = filepath.Clean(path)
	for followed := 0; ; followed++ {
		if followed > maxRealPathLinks {
			return "", errTooManyLinks
		}
		next, linked, err := followFirstLink(path)
		if err != nil || !linked {
			return next, err
		}
		path = next
	}
}

func followFirstLink(path string) (string, bool, error) {
	volume := filepath.VolumeName(path)
	at := volume + string(filepath.Separator)
	if _, err := os.Lstat(at); err != nil {
		return "", false, err
	}
	segments := strings.FieldsFunc(path[len(volume):], func(r rune) bool { return r < utf8.RuneSelf && os.IsPathSeparator(uint8(r)) })
	for i, segment := range segments {
		next := filepath.Join(at, segment)
		info, err := os.Lstat(next)
		if err != nil {
			return "", false, err
		}
		if kindOf(next, info) != linkEntry {
			at = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", false, err
		}
		rest := strings.Join(segments[i+1:], string(filepath.Separator))
		return resolvePath(resolvePath(at, target), rest), true, nil
	}
	return at, false, nil
}

// resolvePath is Node's path.resolve(base, path) for an absolute base.
func resolvePath(base, path string) string {
	switch {
	case filepath.IsAbs(path):
		return filepath.Clean(path)
	case runtime.GOOS == "windows" && isDriveRelative(path):
		return filepath.Clean(filepath.Join(driveFolder(base, path[:2]), path[2:]))
	case runtime.GOOS == "windows" && path != "" && os.IsPathSeparator(path[0]):
		return filepath.Clean(filepath.VolumeName(base) + path)
	default:
		return filepath.Join(base, path)
	}
}

// isDriveRelative is whether path is like C:foo, relative to a drive's
// current folder.
func isDriveRelative(path string) bool {
	return len(path) >= 2 && isASCIILetter(path[0]) && path[1] == ':' && !(len(path) >= 3 && os.IsPathSeparator(path[2]))
}

// driveFolder is the folder Node's path.win32.resolve resolves a drive's
// relative path against: base when it is on that drive, else the drive's
// current folder from the hidden =C: variable, else the process's current
// folder when it is on that drive, else the drive's root. The drive letter
// keeps the case it has in the path.
func driveFolder(base, drive string) string {
	sameDrive := func(folder string) bool { return strings.EqualFold(filepath.VolumeName(folder), drive) }
	if sameDrive(base) {
		return drive + base[len(drive):]
	}
	if current := os.Getenv("=" + strings.ToUpper(drive)); current != "" && sameDrive(current) {
		return drive + current[len(drive):]
	}
	if cwd, err := os.Getwd(); err == nil && sameDrive(cwd) {
		return drive + cwd[len(drive):]
	}
	return drive + `\`
}

// trimJS is JavaScript's String.trim: Unicode white space and a byte order
// mark, but not U+0085, which Go counts as space and JavaScript doesn't.
func trimJS(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) && r != '\u0085' || r == '\uFEFF'
	})
}
