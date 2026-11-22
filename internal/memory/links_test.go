package memory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Each expectation here was checked against Claude Code 2.1.330's own
// resolver, extracted from its Windows and Linux builds and run on the same
// layouts with junctions on Windows and symlinks on Linux.

// fileLink makes a symlink to a file, which Windows allows only with
// Developer Mode or as an administrator.
func fileLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("can't make a file symlink here (Developer Mode is off?): %v", err)
		}
		t.Fatal(err)
	}
}

// linkToAServer makes link a symlink to //host/share. On Windows it uses
// mklink, because Go's Symlink first looks the target up to tell a folder
// from a file, which would go out on the network.
func linkToAServer(t *testing.T, link string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		fileLink(t, link, "//host/share")
		return
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/D", link, `\\host\share`).CombinedOutput(); err != nil {
		t.Skipf("can't make a folder symlink here (Developer Mode is off?): %v: %s", err, out)
	}
}

func pointerTo(gitDir string) string {
	return "gitdir: " + filepath.ToSlash(gitDir) + "\n"
}

func TestProjectRootRefusesACommondirThatIsALink(t *testing.T) {
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	commondir := filepath.Join(repo, ".git", "worktrees", "repo-feature", "commondir")
	writeTree(t, base, map[string]string{"real-commondir": "../..\n"})
	if err := os.Remove(commondir); err != nil {
		t.Fatal(err)
	}
	fileLink(t, commondir, filepath.Join(base, "real-commondir"))

	if got := ProjectRoot(worktree); got != worktree {
		t.Errorf("ProjectRoot = %q, want the worktree itself", got)
	}
}

// Claude keeps the path as the pointer file wrote it, so the root is the link
// to the repo, not the repo.
func TestProjectRootFollowsAGitdirThroughALinkToTheRepo(t *testing.T) {
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	viaLink := filepath.Join(base, "repo-link")
	folderLink(t, viaLink, repo)
	writeTree(t, worktree, map[string]string{".git": pointerTo(filepath.Join(viaLink, ".git", "worktrees", "repo-feature"))})

	if got := ProjectRoot(worktree); got != viaLink {
		t.Errorf("ProjectRoot = %q, want the link %q", got, viaLink)
	}
}

func TestProjectRootRefusesAGitdirThroughALinkToAnotherServer(t *testing.T) {
	base := t.TempDir()
	repo, worktree := filepath.Join(base, "repo"), filepath.Join(base, "repo-feature")
	linkedWorktree(t, repo, worktree)
	toServer := filepath.Join(base, "on-a-server")
	linkToAServer(t, toServer)
	writeTree(t, worktree, map[string]string{".git": pointerTo(filepath.Join(toServer, "repo", ".git", "worktrees", "repo-feature"))})

	if got := ProjectRoot(worktree); got != worktree {
		t.Errorf("ProjectRoot = %q, want the worktree itself", got)
	}
}

// Claude follows at most 40 links in one path: a pointer that goes through
// a chain of 39 links still reaches the repo, and one of 40 doesn't.
func TestProjectRootFollowsAtMostFortyLinks(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	for _, links := range []int{39, 40} {
		worktree := filepath.Join(base, fmt.Sprintf("chain-%d", links))
		linkedWorktree(t, repo, worktree)
		chain := filepath.Join(base, fmt.Sprintf("links-%d", links))
		if err := os.MkdirAll(chain, 0o755); err != nil {
			t.Fatal(err)
		}
		target := repo
		for i := links; i >= 1; i-- {
			link := filepath.Join(chain, fmt.Sprintf("l%d", i))
			folderLink(t, link, target)
			target = link
		}
		writeTree(t, worktree, map[string]string{".git": pointerTo(filepath.Join(target, ".git", "worktrees", filepath.Base(worktree)))})

		want := worktree
		if links < maxLinks {
			want = target
		}
		if got := ProjectRoot(worktree); got != want {
			t.Errorf("%d links: ProjectRoot = %q, want %q", links, got, want)
		}
	}
}

func TestRealPathFollowsLinksAndKeepsTheRestAsWritten(t *testing.T) {
	base := t.TempDir()
	writeTree(t, base, map[string]string{"Target/Inner/": ""})
	folderLink(t, filepath.Join(base, "Link"), filepath.Join(base, "Target"))

	got, err := realPath(filepath.Join(base, "Link", "Inner"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "Target", "Inner"); got != want {
		t.Errorf("realPath = %q, want %q", got, want)
	}
	if runtime.GOOS == "windows" {
		got, err := realPath(filepath.Join(base, "Link", "inner"))
		if want := filepath.Join(base, "Target", "inner"); err != nil || got != want {
			t.Errorf("realPath = %q, %v, want %q with the case as typed", got, err, want)
		}
	}
}
