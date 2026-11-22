package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInheritFromMainWaitsForClaudesLockThenGivesUp(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := writeFile(t, dir, "account.json", accountState)
	before, _ := os.ReadFile(accountPath)
	if err := os.Mkdir(accountPath+".lock", 0o755); err != nil {
		t.Fatal(err)
	}
	defer func(wait time.Duration) { lockWaitFor = wait }(lockWaitFor)
	lockWaitFor = 200 * time.Millisecond

	err := InheritFromMain(mainPath, accountPath)

	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if after, _ := os.ReadFile(accountPath); string(after) != string(before) {
		t.Error("wrote the file while Claude held the lock")
	}
	if _, err := os.Stat(accountPath + ".lock"); err != nil {
		t.Error("removed a lock it didn't own")
	}
}

func TestInheritFromMainNeverRemovesSomeoneElsesLock(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := writeFile(t, dir, "account.json", accountState)
	lock := accountPath + ".lock"
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	defer func(wait time.Duration) { lockWaitFor = wait }(lockWaitFor)
	lockWaitFor = 200 * time.Millisecond

	err := InheritFromMain(mainPath, accountPath)

	if !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("removed a lock it didn't create, even though it looked abandoned")
	}
}

func TestConcurrentLaunchesLeaveOneValidFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := filepath.Join(dir, "account.json")

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- InheritFromMain(mainPath, accountPath)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if servers := readJSON(t, accountPath)["mcpServers"].(map[string]any); len(servers) != 2 {
		t.Errorf("connectors = %v", servers)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "account.json.*"))
	if len(leftovers) != 0 {
		t.Errorf("left behind %v", leftovers)
	}
}

func TestReadNamesTheBrokenFile(t *testing.T) {
	path := writeFile(t, t.TempDir(), "state.json", `{"cut off`)

	_, err := Read(path)

	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "didn't change it") {
		t.Errorf("err = %v", err)
	}
}

func TestReadTakesAnEmptyOrNullFileForAnEmptyDocument(t *testing.T) {
	for _, content := range []string{"", " \r\n\t", "null", " null\n"} {
		doc, err := Read(writeFile(t, t.TempDir(), "state.json", content))
		if err != nil || doc == nil || len(doc) != 0 {
			t.Errorf("Read(%q) = %v, %v, want an empty document", content, doc, err)
		}
	}
}

// Claude Code offers to restore an empty state file from its backups, so hat
// leaves it for Claude.
func TestInheritFromMainLeavesAnEmptyAccountStateFileAlone(t *testing.T) {
	for _, content := range []string{"", " \r\n\t"} {
		dir := t.TempDir()
		mainPath := writeFile(t, dir, "main.json", mainState)
		accountPath := writeFile(t, dir, "account.json", content)

		if err := InheritFromMain(mainPath, accountPath); err != nil {
			t.Fatalf("%q: %v", content, err)
		}

		if after, _ := os.ReadFile(accountPath); string(after) != content {
			t.Errorf("%q: the file now reads %q", content, after)
		}
	}
}

func TestInheritFromMainReadsAnEmptyMainStateFileAsEmpty(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", "")
	accountPath := writeFile(t, dir, "account.json", accountState)

	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}
}

func TestInheritFromMainFillsAnAccountStateFileHoldingNull(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := writeFile(t, dir, "account.json", "null")

	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}

	if servers, _ := readJSON(t, accountPath)["mcpServers"].(map[string]any); len(servers) != 2 {
		t.Errorf("connectors = %v, want main's two", servers)
	}
}

func TestInheritFromMainReadsAMainStateFileHoldingNull(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", "null")
	accountPath := writeFile(t, dir, "account.json", accountState)

	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}
}

func TestReadRefusesDataAfterTheFirstValue(t *testing.T) {
	for _, content := range []string{"{} trailing garbage", "{}{}", `{"a": 1} 2`, "null {}"} {
		dir := t.TempDir()
		mainPath := writeFile(t, dir, "main.json", mainState)
		accountPath := writeFile(t, dir, "account.json", content)

		err := InheritFromMain(mainPath, accountPath)

		if err == nil || !strings.Contains(err.Error(), accountPath) || !strings.Contains(err.Error(), "didn't change it") {
			t.Errorf("%q: err = %v, want the broken-file message", content, err)
		}
		if after, _ := os.ReadFile(accountPath); string(after) != content {
			t.Errorf("%q: the file now reads %q", content, after)
		}
	}
}

const mainState = `{
  "oauthAccount": {"emailAddress": "main@example.com"},
  "mcpServers": {
    "linear": {"type": "http", "url": "https://mcp.linear.app/mcp"},
    "granola": {"type": "http", "url": "https://mcp.granola.ai/mcp"}
  }
}`

const accountState = `{
  "oauthAccount": {"emailAddress": "work@example.com"},
  "fetchedAtMs": 1795109518882,
  "resets_at": "2026-11-20T11:00:00.771886-08:00",
  "html": "<b>&</b>",
  "mcpServers": {
    "granola": {"type": "http", "url": "https://old.example/mcp"},
    "account-only": {"type": "http", "url": "https://example.invalid/mcp"}
  }
}`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestReadWriteKeepsContentExactly(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "state.json", accountState)
	before := readJSON(t, path)

	doc, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(path, doc); err != nil {
		t.Fatal(err)
	}

	if after := readJSON(t, path); !reflect.DeepEqual(before, after) {
		t.Errorf("content changed:\nbefore %v\nafter  %v", before, after)
	}
	if data, _ := os.ReadFile(path); !json.Valid(data) || string(data) == "" {
		t.Errorf("wrote invalid JSON: %s", data)
	}
}

func TestEmail(t *testing.T) {
	dir := t.TempDir()
	doc, _ := Read(writeFile(t, dir, "state.json", mainState))
	if doc.Email() != "main@example.com" {
		t.Errorf("Email = %q", doc.Email())
	}
	empty, _ := Read(filepath.Join(dir, "missing.json"))
	if empty.Email() != "" {
		t.Errorf("missing file Email = %q", empty.Email())
	}
}

func TestInheritFromMainNewAccountGetsMainListAndSkipsOnboarding(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := filepath.Join(dir, "new.json")

	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}

	got := readJSON(t, accountPath)
	if got["hasCompletedOnboarding"] != true {
		t.Error("new account should skip first-run screens")
	}
	if servers := got["mcpServers"].(map[string]any); len(servers) != 2 {
		t.Errorf("connectors = %v", servers)
	}
}

func TestInheritFromMainPrefersMainAndKeepsAccountExtras(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := writeFile(t, dir, "account.json", accountState)
	before := readJSON(t, accountPath)

	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}

	after := readJSON(t, accountPath)
	servers := after["mcpServers"].(map[string]any)
	if url := servers["granola"].(map[string]any)["url"]; url != "https://mcp.granola.ai/mcp" {
		t.Errorf("granola url = %v, want main's", url)
	}
	if _, ok := servers["account-only"]; !ok {
		t.Error("account-only connector was dropped")
	}
	if _, ok := servers["linear"]; !ok {
		t.Error("linear from main wasn't added")
	}
	delete(before, "mcpServers")
	delete(after, "mcpServers")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("other content changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestInheritFromMainWritesNothingWhenInSync(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeFile(t, dir, "main.json", mainState)
	accountPath := filepath.Join(dir, "account.json")
	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(accountPath)
	fileWithTrailingBlankLines := string(data) + "\n\n"
	if err := os.WriteFile(accountPath, []byte(fileWithTrailingBlankLines), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := InheritFromMain(mainPath, accountPath); err != nil {
		t.Fatal(err)
	}

	if now, _ := os.ReadFile(accountPath); string(now) != fileWithTrailingBlankLines {
		t.Error("file was rewritten although nothing changed")
	}
}
