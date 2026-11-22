package memory

import (
	"os"
	"path/filepath"
	"strings"
)

// findGitRoot walks up from dir to the first folder holding a .git file or
// folder, the way Claude Code 2.1.330 does: it checks only that the entry
// exists, not that it is a working git folder.
func findGitRoot(dir string) (string, bool) {
	at, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	top := filesystemTop(at)
	for at != top {
		if !isAutofsMapRoot(at) && isGitEntry(filepath.Join(at, ".git"), at) {
			return at, true
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	if isGitEntry(filepath.Join(top, ".git"), top) {
		return top, true
	}
	return "", false
}

// filesystemTop is everything up to the first separator, the way Claude
// finds where to stop: C:\ for C:\dev\git, / on Unix.
func filesystemTop(path string) string {
	sep := string(filepath.Separator)
	if i := strings.Index(path, sep); i >= 0 {
		return path[:i+1]
	}
	return sep
}

// isGitEntry is whether path is a .git Claude counts: a file or folder, or a
// link to one whose target passes Claude's path checks.
func isGitEntry(path, root string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	kind := kindOf(path, info)
	if kind == linkEntry {
		target, ok := linkText(path)
		if !ok || unsafePath(target, root) || unsafeWalk(target, root, root) {
			return false
		}
		followed, err := os.Stat(path)
		if err != nil {
			return false
		}
		kind = followedKindOf(followed)
	}
	return kind == fileEntry || kind == folderEntry
}

// mainRepoOf is the folder Claude files a linked worktree's memory under:
// its main repo. root is the folder holding the .git. Claude follows a .git
// pointer file only when all of this holds, and otherwise keeps root:
//   - the file says "gitdir: <folder>"
//   - that folder holds a commondir file naming the repo's git folder
//   - the folder sits directly in that git folder's worktrees folder
//   - its gitdir file points back at root's .git
//
// A submodule's git folder has no commondir, so a submodule keeps its own
// folder. A worktree of a bare repo maps to the bare repo, unless that holds
// a .git of its own.
func mainRepoOf(root string) string {
	pointer, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil {
		return root
	}
	text := trimJS(string(pointer))
	gitDirText, isPointer := strings.CutPrefix(text, "gitdir:")
	if !isPointer {
		return root
	}
	gitDirText = trimJS(gitDirText)
	if unsafePath(gitDirText, root) || unsafeWalk(gitDirText, root, root) {
		return root
	}
	gitDir := resolvePath(root, gitDirText)

	commonText, ok := readGitFile(gitDir, "commondir")
	if !ok || unsafePath(commonText, gitDir) || unsafeWalk(commonText, gitDir, gitDir) {
		return root
	}
	commonDir := resolvePath(gitDir, commonText)
	if filepath.Dir(gitDir) != filepath.Join(commonDir, "worktrees") {
		return root
	}

	backText, ok := readGitFile(gitDir, "gitdir")
	if !ok || unsafePath(backText, root) || unsafeWalk(backText, gitDir, root) {
		return root
	}
	if filepath.Separator == '/' && anyHasBackslash(root, gitDir, backText, commonDir) {
		return root
	}
	if !pointsBackAt(resolvePath(gitDir, backText), root) {
		return root
	}

	if filepath.Base(commonDir) != ".git" {
		if isGitEntry(filepath.Join(commonDir, ".git"), commonDir) {
			return root
		}
		return commonDir
	}
	return filepath.Dir(commonDir)
}

func readGitFile(gitDir, name string) (string, bool) {
	path := filepath.Join(gitDir, name)
	if !walksSafely(path, gitDir, maxLinks) {
		return "", false
	}
	if kind := entryKindOf(path); kind == linkEntry || kind == specialEntry || kind == unreadableEntry {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return trimJS(string(data)), true
}

func pointsBackAt(backPointer, root string) bool {
	target, err := realPath(backPointer)
	if err != nil {
		return false
	}
	realRoot, err := realPath(root)
	if err != nil {
		return false
	}
	return target == filepath.Join(realRoot, ".git")
}

func anyHasBackslash(paths ...string) bool {
	for _, path := range paths {
		if strings.Contains(path, `\`) {
			return true
		}
	}
	return false
}
