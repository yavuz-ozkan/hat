// Package memory points a managed account's auto-memory straight at the main
// account's folder for the project.
//
// Every managed account reaches the shared memory through its `projects` link,
// and Claude asks before every write that passes through a link into its own
// settings area. Claude's autoMemoryDirectory setting sends memory writes to a
// named folder instead, and writes to that folder need no approval. The
// setting is one folder, not one per project, so it has to be worked out at
// each launch from the folder Claude starts in.
package memory

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// maxKeyLength is where Claude stops using the plain folder name and appends
// a hash hat can't reproduce (Claude Code 2.1.329).
const maxKeyLength = 200

// ProjectRoot is the folder Claude files a project's memory under, by Claude
// Code 2.1.330's rule: the folder holding the repo's .git, so every worktree
// of a repo shares one memory. A linked worktree whose repo has no .git
// folder, like one made from a bare repo, uses the repo folder itself, unless
// that holds a .git of its own. A submodule keeps its own folder. Outside git
// it's dir itself.
func ProjectRoot(dir string) string {
	repo, err := locateRepo(dir)
	if err != nil {
		return dir
	}
	if !repo.isLinkedWorktree() {
		return repo.top
	}
	if filepath.Base(repo.commonDir) == ".git" {
		return filepath.Dir(repo.commonDir)
	}
	if _, err := os.Stat(filepath.Join(repo.commonDir, ".git")); err == nil {
		return repo.top
	}
	return repo.commonDir
}

type repoFolders struct {
	gitDir, commonDir, top string
}

// isLinkedWorktree reads what Claude reads: git writes a commondir file only
// into a linked worktree's git folder, not a submodule's or a plain repo's.
func (r repoFolders) isLinkedWorktree() bool {
	_, err := os.Stat(filepath.Join(r.gitDir, "commondir"))
	return err == nil
}

var errUnexpectedGitOutput = errors.New("git rev-parse printed something other than three folders")

// locateRepo leaves out --path-format=absolute, which needs git 2.31. Without
// it git prints --git-dir and --git-common-dir relative to dir when it can.
func locateRepo(dir string) (repoFolders, error) {
	out, err := git(dir, "rev-parse", "--git-dir", "--git-common-dir", "--show-toplevel")
	if err != nil {
		return repoFolders{}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		return repoFolders{}, errUnexpectedGitOutput
	}
	return repoFolders{
		gitDir:    resolveAgainst(dir, lines[0]),
		commonDir: resolveAgainst(dir, lines[1]),
		top:       filepath.Clean(filepath.FromSlash(strings.TrimSpace(lines[2]))),
	}, nil
}

func resolveAgainst(dir, gitPath string) string {
	path := filepath.FromSlash(strings.TrimSpace(gitPath))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path)
}

// Key is the project folder name Claude uses under projects/. Claude's
// pattern is a JavaScript regex without the u flag, so it replaces each
// UTF-16 code unit that isn't an ASCII letter or digit with "-", and a
// character outside the Basic Multilingual Plane, like an emoji, becomes two.
func Key(root string) string {
	var key strings.Builder
	for _, r := range filepath.FromSlash(root) {
		if isASCIILetterOrDigit(r) {
			key.WriteRune(r)
			continue
		}
		key.WriteString(strings.Repeat("-", utf16.RuneLen(r)))
	}
	return key.String()
}

func isASCIILetterOrDigit(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9'
}

// SettingsArgs points the account's memory at main's folder for the project
// launchDir belongs to. On Unix Claude keys by the physical folder, which is
// what Node's process.cwd() returns, while Go's Getwd returns $PWD, which can
// go through a symlink.
func SettingsArgs(mainDir, launchDir string) []string {
	if runtime.GOOS != "windows" {
		if physical, err := filepath.EvalSymlinks(launchDir); err == nil {
			launchDir = physical
		}
	}
	key := Key(ProjectRoot(launchDir))
	if len(key) > maxKeyLength {
		return nil
	}
	settings, _ := json.Marshal(map[string]string{
		"autoMemoryDirectory": filepath.Join(mainDir, "projects", key, "memory"),
	})
	return []string{"--settings", string(settings)}
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
