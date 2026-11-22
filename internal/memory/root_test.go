package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeTree makes files under root. A name ending in / is a folder.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// linkedWorktree lays out what git worktree add makes: the worktree's .git
// pointer file, and its git folder inside repo's .git/worktrees.
func linkedWorktree(t *testing.T, repo, worktree string) {
	t.Helper()
	name := filepath.Base(worktree)
	gitDir := filepath.Join(repo, ".git", "worktrees", name)
	writeTree(t, repo, map[string]string{
		".git/HEAD":                             "ref: refs/heads/main\n",
		".git/worktrees/" + name + "/HEAD":      "ref: refs/heads/" + name + "\n",
		".git/worktrees/" + name + "/commondir": "../..\n",
		".git/worktrees/" + name + "/gitdir":    filepath.ToSlash(filepath.Join(worktree, ".git")) + "\n",
	})
	writeTree(t, worktree, map[string]string{".git": "gitdir: " + filepath.ToSlash(gitDir) + "\n"})
}

func TestProjectRootOfALinkedWorktreeIsItsRepo(t *testing.T) {
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	writeTree(t, worktree, map[string]string{"src/": ""})

	for _, dir := range []string{worktree, filepath.Join(worktree, "src")} {
		if got := ProjectRoot(dir); got != repo {
			t.Errorf("ProjectRoot(%q) = %q, want the repo %q", dir, got, repo)
		}
	}
}

func TestProjectRootKeepsAWorktreeWhoseRepoDoesNotPointBack(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	linkedWorktree(t, repo, filepath.Join(base, "moved-from"))
	moved := filepath.Join(base, "moved-to")
	if err := os.Rename(filepath.Join(base, "moved-from"), moved); err != nil {
		t.Fatal(err)
	}

	if got := ProjectRoot(moved); got != moved {
		t.Errorf("ProjectRoot(%q) = %q, want the worktree itself, since its repo points at where it used to be", moved, got)
	}
}

// Each folder here is one a cloned or downloaded project could contain. With
// git rev-parse, every one of them made hat file its memory under victim's.
func TestProjectRootCantBeSteeredIntoAnotherRepo(t *testing.T) {
	needGit(t)
	base := t.TempDir()
	victim := filepath.Join(base, "victim")
	repoWithACommit(t, victim)
	runGit(t, victim, "worktree", "add", "-q", filepath.Join(base, "victim-feature"))
	victimGit := filepath.ToSlash(filepath.Join(victim, ".git"))
	workTreeIsVictim := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n\tworktree = " + filepath.ToSlash(victim) + "\n"

	attacks := map[string]map[string]string{
		"work-tree-in-config": {
			".git/HEAD":     "ref: refs/heads/main\n",
			".git/objects/": "",
			".git/refs/":    "",
			".git/config":   workTreeIsVictim,
		},
		"pointer-with-a-commondir": {
			".git":              "gitdir: fakegit\n",
			"fakegit/HEAD":      "ref: refs/heads/main\n",
			"fakegit/commondir": victimGit + "\n",
		},
		"pointer-into-a-made-up-worktrees-folder": {
			".git":                    "gitdir: x/worktrees/w\n",
			"x/worktrees/w/HEAD":      "ref: refs/heads/main\n",
			"x/worktrees/w/commondir": victimGit + "\n",
			"x/worktrees/w/gitdir":    filepath.ToSlash(filepath.Join(base, "pointer-into-a-made-up-worktrees-folder", ".git")) + "\n",
		},
		"pointer-at-the-victims-own-worktree": {
			".git": "gitdir: " + victimGit + "/worktrees/victim-feature\n",
		},
		"pointer-on-another-server": {
			".git": "gitdir: //otherhost/share/victim/.git/worktrees/victim-feature\n",
		},
		// git takes a folder holding HEAD, objects, and refs for a git
		// folder, with no .git in sight. Claude looks only for .git.
		"laid-out-like-a-git-folder": {
			"HEAD":     "ref: refs/heads/main\n",
			"objects/": "",
			"refs/":    "",
			"config":   workTreeIsVictim,
		},
	}
	for name, files := range attacks {
		dir := filepath.Join(base, name)
		writeTree(t, dir, files)
		if got := ProjectRoot(dir); got != dir {
			t.Errorf("%s: ProjectRoot = %q, want the folder itself", name, got)
		}
	}
}

func TestProjectRootReadsAPointerFileWithCRLFAndAByteOrderMark(t *testing.T) {
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	gitDir := filepath.ToSlash(filepath.Join(repo, ".git", "worktrees", "repo-feature"))
	const byteOrderMark = "\xef\xbb\xbf"
	writeTree(t, worktree, map[string]string{".git": byteOrderMark + "gitdir: " + gitDir + "\r\n"})

	if got := ProjectRoot(worktree); got != repo {
		t.Errorf("ProjectRoot = %q, want the repo %q", got, repo)
	}
}

// Claude compares the worktree's back-pointer with the folder it started in
// letter for letter, after following links, so a drive letter typed in
// another case keeps the worktree's own memory.
func TestProjectRootComparesTheBackPointerLetterForLetter(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows paths can differ in case and name the same folder")
	}
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	otherCase := strings.ToLower(worktree[:1]) + worktree[1:]
	if otherCase == worktree {
		otherCase = strings.ToUpper(worktree[:1]) + worktree[1:]
	}

	if got := ProjectRoot(otherCase); got != otherCase {
		t.Errorf("ProjectRoot(%q) = %q, want the worktree as typed", otherCase, got)
	}
}

func TestProjectRootNeverRunsGit(t *testing.T) {
	bin := t.TempDir()
	proof := filepath.Join(t.TempDir(), "git-ran")
	t.Setenv("HAT_GIT_PROOF", proof)
	if runtime.GOOS == "windows" {
		writeTree(t, bin, map[string]string{"git.cmd": "@echo off\r\necho ran > \"%HAT_GIT_PROOF%\"\r\n"})
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	} else {
		writeTree(t, bin, map[string]string{"git": "#!/bin/sh\necho ran > \"$HAT_GIT_PROOF\"\n"})
		if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	dir := filepath.Join(t.TempDir(), "x&md %HAT_GIT_PROOF%&rem")
	writeTree(t, dir, map[string]string{".git/HEAD": "ref: refs/heads/main\n"})

	SettingsArgs(filepath.Join(t.TempDir(), "main"), dir)

	if _, err := os.Stat(proof); err == nil {
		t.Error("finding the project root ran the git on PATH")
	}
}
