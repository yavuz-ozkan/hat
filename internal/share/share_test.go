package share

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func mainWithSharedItems(t *testing.T) (mainDir, accountDir string) {
	t.Helper()
	root := t.TempDir()
	mainDir = filepath.Join(root, ".claude")
	accountDir = filepath.Join(root, ".claude-work")
	mustWrite(t, filepath.Join(mainDir, "projects", "repo", "memory", "MEMORY.md"), "shared memory")
	mustWrite(t, filepath.Join(mainDir, "CLAUDE.md"), "shared instructions")
	if err := os.MkdirAll(accountDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return mainDir, accountDir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestConnectLinksFoldersAndFilesThatExistInMain(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)

	copies, err := Connect(accountDir, mainDir, []string{"projects", "CLAUDE.md", "skills"})
	if err != nil || len(copies) != 0 {
		t.Fatalf("Connect: copies=%v err=%v", copies, err)
	}

	if got := mustRead(t, filepath.Join(accountDir, "projects", "repo", "memory", "MEMORY.md")); got != "shared memory" {
		t.Errorf("memory through link = %q", got)
	}
	if got := mustRead(t, filepath.Join(accountDir, "CLAUDE.md")); got != "shared instructions" {
		t.Errorf("instructions through link = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(accountDir, "skills")); !os.IsNotExist(err) {
		t.Errorf("skills isn't in main, so it shouldn't be linked; Lstat err = %v", err)
	}
}

func TestConnectTwiceChangesNothing(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)
	items := []string{"projects", "CLAUDE.md"}
	if _, err := Connect(accountDir, mainDir, items); err != nil {
		t.Fatal(err)
	}

	copies, err := Connect(accountDir, mainDir, items)
	if err != nil || len(copies) != 0 {
		t.Fatalf("second Connect: copies=%v err=%v", copies, err)
	}
}

func TestConnectReportsPrivateCopyWithoutOverwritingIt(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)
	mustWrite(t, filepath.Join(accountDir, "CLAUDE.md"), "edited under this account")

	copies, err := Connect(accountDir, mainDir, []string{"CLAUDE.md"})
	if err != nil {
		t.Fatal(err)
	}

	if copy, ok := firstAs[PrivateCopy](copies); !ok || len(copies) != 1 || copy.Item != "CLAUDE.md" {
		t.Fatalf("copies = %v, want one for CLAUDE.md", copies)
	}
	if got := mustRead(t, filepath.Join(accountDir, "CLAUDE.md")); got != "edited under this account" {
		t.Errorf("private copy was overwritten: %q", got)
	}
}

func TestRemoveAllKeepsWhatLinksPointAt(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)
	if _, err := Connect(accountDir, mainDir, []string{"projects", "CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}
	if !IsLinkTo(filepath.Join(accountDir, "projects"), filepath.Join(mainDir, "projects")) ||
		!IsLinkTo(filepath.Join(accountDir, "CLAUDE.md"), filepath.Join(mainDir, "CLAUDE.md")) {
		t.Fatal("the account folder doesn't hold the links this test needs")
	}
	mustWrite(t, filepath.Join(accountDir, ".credentials.json"), "account's own sign-in")
	before := filesUnder(t, mainDir)

	if err := os.RemoveAll(accountDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(accountDir); !os.IsNotExist(err) {
		t.Errorf("account folder still exists: %v", err)
	}
	if after := filesUnder(t, mainDir); !reflect.DeepEqual(before, after) || len(after) != 2 {
		t.Errorf("main folder changed:\nbefore %v\nafter  %v", before, after)
	}
}

func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		files[path] = mustRead(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestIsLinkTo(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)
	if _, err := Connect(accountDir, mainDir, []string{"CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}

	if !IsLinkTo(filepath.Join(accountDir, "CLAUDE.md"), filepath.Join(mainDir, "CLAUDE.md")) {
		t.Error("link to main's CLAUDE.md not recognized")
	}
	if IsLinkTo(filepath.Join(mainDir, "CLAUDE.md"), filepath.Join(mainDir, "CLAUDE.md")) {
		t.Error("a plain file counted as a link")
	}
}

func TestItemsAddsExtrasWithoutDuplicates(t *testing.T) {
	items, err := Items([]string{"my-skill-data", "skills"})
	if err != nil {
		t.Fatal(err)
	}

	count := map[string]int{}
	for _, item := range items {
		count[item]++
	}
	if count["my-skill-data"] != 1 || count["skills"] != 1 || len(items) != len(SharedAllowlist)+1 {
		t.Errorf("Items = %v", items)
	}
}

func firstAs[T error](warnings []error) (T, bool) {
	var zero T
	if len(warnings) == 0 {
		return zero, false
	}
	value, ok := warnings[0].(T)
	return value, ok
}

func TestConnectReportsItemsOnlyThisAccountHas(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)
	mustWrite(t, filepath.Join(accountDir, "agents", "reviewer.md"), "made under this account")

	warnings, err := Connect(accountDir, mainDir, []string{"agents"})
	if err != nil {
		t.Fatal(err)
	}

	only, ok := firstAs[OnlyHere](warnings)
	if !ok || len(warnings) != 1 || only.Item != "agents" || !strings.Contains(only.Error(), "Move") {
		t.Errorf("warnings = %v", warnings)
	}
	if _, err := os.Stat(filepath.Join(mainDir, "agents")); !os.IsNotExist(err) {
		t.Error("hat must not create anything in the main folder")
	}
}

func TestConnectReportsEvenAnEmptyRealFolder(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)
	if err := os.Mkdir(filepath.Join(accountDir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	warnings, err := Connect(accountDir, mainDir, []string{"projects"})

	if _, ok := firstAs[PrivateCopy](warnings); err != nil || !ok {
		t.Fatalf("warnings=%v err=%v, want a PrivateCopy", warnings, err)
	}
	if IsLinkTo(filepath.Join(accountDir, "projects"), filepath.Join(mainDir, "projects")) {
		t.Error("a real folder was overwritten with a link")
	}
}

func TestConnectLeavesOnlyTheLinks(t *testing.T) {
	mainDir, accountDir := mainWithSharedItems(t)

	if _, err := Connect(accountDir, mainDir, []string{"projects", "CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(accountDir)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"CLAUDE.md", "projects"}) {
		t.Errorf("account folder holds %v, want only the two links", names)
	}
}

func TestItemsRefusesWhatBelongsToOneAccount(t *testing.T) {
	for _, name := range []string{".credentials.json", ".claude.json", ".hat", ".claude-as", "browser-profile", "browser-profile/", "", ".", "..", "../outside", "nested/file"} {
		if _, err := Items([]string{name}); err == nil {
			t.Errorf("Items accepted %q", name)
		}
	}
}

func TestConnectHandlesShellCharactersInPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "R&D %PATH% ^x")
	mainDir := filepath.Join(root, ".claude")
	accountDir := filepath.Join(root, ".claude-work")
	mustWrite(t, filepath.Join(mainDir, "projects", "a&b", "MEMORY.md"), "shared memory")
	if err := os.MkdirAll(accountDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Connect(accountDir, mainDir, []string{"projects"}); err != nil {
		t.Fatal(err)
	}

	if got := mustRead(t, filepath.Join(accountDir, "projects", "a&b", "MEMORY.md")); got != "shared memory" {
		t.Errorf("memory through link = %q", got)
	}
	if !IsLinkTo(filepath.Join(accountDir, "projects"), filepath.Join(mainDir, "projects")) {
		t.Error("projects link not recognized")
	}
}

func TestItemsRefusesWindowsNamesThatReachAnotherFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows reads these names as other files")
	}
	for _, name := range []string{".credentials.json.", ".credentials.json ", "browser-profile. ", ".claude.json:stream", "my-skill-data:x"} {
		if _, err := Items([]string{name}); err == nil || !strings.Contains(err.Error(), "different file") {
			t.Errorf("Items(%q) = %v, want a refusal", name, err)
		}
	}
	if _, err := Items([]string{"my.skill-data"}); err != nil {
		t.Errorf("a dot inside a name is fine: %v", err)
	}
}
