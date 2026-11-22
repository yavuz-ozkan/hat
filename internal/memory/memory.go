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
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// maxKeyLength is where Claude stops using the plain folder name and appends
// a hash hat can't reproduce (Claude Code 2.1.329).
const maxKeyLength = 200

// ProjectRoot is the folder Claude files a project's memory under (Claude
// Code 2.1.330). Claude doesn't ask git: it reads files, and so does hat, so a
// folder can't steer it with git settings. The root is the nearest folder at
// or above dir that holds a .git, so every subfolder of a repo shares one
// memory. When that .git is a linked worktree's pointer file, and the
// worktree's git folder points back at it, the root moves to the main repo, so
// its worktrees share one memory too. Outside git it's dir itself.
func ProjectRoot(dir string) string {
	root, found := findGitRoot(dir)
	if !found {
		return dir
	}
	return mainRepoOf(root)
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
