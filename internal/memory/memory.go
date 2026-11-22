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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// maxKeyLength is where Claude stops using the plain folder name and appends
// a hash hat can't reproduce (Claude Code 2.1.329).
const maxKeyLength = 200

var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ProjectRoot is the folder Claude files a project's memory under: the main
// folder of the git repo containing dir, so every worktree of a repo shares
// one memory, or dir itself outside git.
func ProjectRoot(dir string) string {
	commonDir, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || commonDir == "" {
		return dir
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir)
	}
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return dir
	}
	return top
}

// Key is the project folder name Claude uses under projects/: every character
// that isn't a letter or digit becomes "-".
func Key(root string) string {
	return nonAlphanumeric.ReplaceAllString(filepath.FromSlash(root), "-")
}

func SettingsArgs(mainDir, launchDir string) []string {
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
