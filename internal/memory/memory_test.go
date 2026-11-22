package memory

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyMatchesClaudeFolderNames(t *testing.T) {
	cases := map[string]string{
		`C:\Windows\System32`:       "C--Windows-System32",
		`C:\dev\git\myapp`:          "C--dev-git-myapp",
		"/home/someone/src/my.repo": "-home-someone-src-my-repo",
		`c:\Users\someone\jolie`:    "c--Users-someone-jolie",
	}
	for root, want := range cases {
		if got := Key(root); got != want {
			t.Errorf("Key(%q) = %q, want %q", root, got, want)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func sameFolder(a, b string) bool {
	ca, _ := filepath.EvalSymlinks(a)
	cb, _ := filepath.EvalSymlinks(b)
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

func TestProjectRootOutsideGitIsTheFolder(t *testing.T) {
	dir := t.TempDir()
	if got := ProjectRoot(dir); !sameFolder(got, dir) {
		t.Errorf("ProjectRoot = %q, want %q", got, dir)
	}
}

func TestProjectRootMapsSubfoldersAndWorktreesToTheRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "start")
	worktree := filepath.Join(base, "repo-feature")
	runGit(t, repo, "worktree", "add", "-q", worktree)

	for _, dir := range []string{repo, filepath.Join(repo, "sub"), worktree} {
		if got := ProjectRoot(dir); !sameFolder(got, repo) {
			t.Errorf("ProjectRoot(%q) = %q, want the repo %q", dir, got, repo)
		}
	}
}

func TestSettingsArgsPointAtMainMemoryFolder(t *testing.T) {
	dir := t.TempDir()
	mainDir := filepath.Join("home", ".claude")

	args := SettingsArgs(mainDir, dir)

	if len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("args = %v", args)
	}
	var settings map[string]string
	if err := json.Unmarshal([]byte(args[1]), &settings); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(mainDir, "projects", Key(ProjectRoot(dir)), "memory")
	if settings["autoMemoryDirectory"] != want {
		t.Errorf("autoMemoryDirectory = %q, want %q", settings["autoMemoryDirectory"], want)
	}
}

func TestSettingsArgsSkipFolderNamesClaudeWouldHash(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("a", 220))
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Skip("filesystem refused a long folder name")
	}
	if args := SettingsArgs("main", long); len(args) != 0 {
		t.Errorf("args = %v, want none past %d characters", args, maxKeyLength)
	}
}
