package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/account"
	"github.com/yavuz-ozkan/hat/internal/layout"
	"github.com/yavuz-ozkan/hat/internal/share"
)

type harness struct {
	app      App
	out, err *bytes.Buffer
	layout   layout.Layout
}

func newHarness(t *testing.T) harness {
	t.Helper()
	l := testLayout(t.TempDir())
	mustWrite(t, filepath.Join(l.MainDir, "projects", "repo", "memory", "MEMORY.md"), "shared memory")
	mustWrite(t, filepath.Join(l.MainDir, "CLAUDE.md"), "shared instructions")
	mustWrite(t, l.MainStateFile, `{"mcpServers":{"linear":{"type":"http","url":"https://mcp.linear.app/mcp"}}}`)
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return harness{app: App{Layout: l, Out: out, Err: errOut, Version: "test"}, out: out, err: errOut, layout: l}
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

func TestPrepareBuildsAManagedAccount(t *testing.T) {
	h := newHarness(t)
	acct := account.Named(h.layout, "work")

	if err := h.app.prepare(acct); err != nil {
		t.Fatal(err)
	}

	if !share.IsLinkTo(filepath.Join(acct.ConfigDir, "projects"), filepath.Join(h.layout.MainDir, "projects")) {
		t.Error("projects isn't linked to main")
	}
	if data, _ := os.ReadFile(acct.StateFile); !strings.Contains(string(data), "mcp.linear.app") {
		t.Errorf("connectors not copied: %s", data)
	}
	if account.Named(h.layout, "work").Kind != account.Managed {
		t.Error("folder isn't marked managed")
	}
}

func TestPrepareLinksAlsoShareItems(t *testing.T) {
	h := newHarness(t)
	mustWrite(t, filepath.Join(h.layout.MainDir, "my-skill-data", "board.json"), "{}")
	mustWrite(t, h.layout.ConfigFile, `{"alsoShare":["my-skill-data"]}`)
	acct := account.Named(h.layout, "work")

	if err := h.app.prepare(acct); err != nil {
		t.Fatal(err)
	}

	if !share.IsLinkTo(filepath.Join(acct.ConfigDir, "my-skill-data"), filepath.Join(h.layout.MainDir, "my-skill-data")) {
		t.Error("alsoShare item isn't linked")
	}
}

func TestRemoveDeletesAccountAndKeepsSharedMemory(t *testing.T) {
	h := newHarness(t)
	acct := account.Named(h.layout, "work")
	if err := h.app.prepare(acct); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, acct.CredentialsFile, "{}")

	code := h.app.Run([]string{"rm", "work"})

	if code != exitOK {
		t.Fatalf("exit %d: %s", code, h.err)
	}
	if _, err := os.Stat(acct.ConfigDir); !os.IsNotExist(err) {
		t.Error("account folder still exists")
	}
	data, err := os.ReadFile(filepath.Join(h.layout.MainDir, "projects", "repo", "memory", "MEMORY.md"))
	if err != nil || string(data) != "shared memory" {
		t.Errorf("shared memory damaged: %q %v", data, err)
	}
}

func TestRemoveDeletesTheAccountsRecordedUsage(t *testing.T) {
	h := newHarness(t)
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(work); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", work.ConfigDir)
	h.runStatusLine(t, statusLineInput)
	recorded, _ := h.layout.UsageFile(work.ConfigDir)
	if _, err := os.Stat(recorded); err != nil {
		t.Fatalf("nothing recorded to remove: %v", err)
	}

	code := h.app.Run([]string{"rm", "work"})

	if _, err := os.Stat(recorded); code != exitOK || !os.IsNotExist(err) {
		t.Errorf("exit %d, recorded usage still at %s: %v", code, recorded, err)
	}
}

func TestRemoveThatCantFinishCanBeRunAgain(t *testing.T) {
	h := newHarness(t)
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(work); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, work.CredentialsFile, "{}")
	release := makeUndeletable(t, filepath.Join(work.ConfigDir, "session-env"))

	code := h.app.Run([]string{"rm", "work"})

	if code != exitFailed || !strings.Contains(h.err.String(), "run hat rm work again") {
		t.Fatalf("exit %d, stderr %q", code, h.err)
	}
	if account.Named(h.layout, "work").Kind != account.Managed {
		t.Fatal("the half-deleted folder no longer counts as hat's, so rm can't be retried")
	}
	release()
	h.err.Reset()
	if code := h.app.Run([]string{"rm", "work"}); code != exitOK {
		t.Errorf("second rm: exit %d, stderr %q", code, h.err)
	}
	if _, err := os.Stat(work.ConfigDir); !os.IsNotExist(err) {
		t.Error("account folder still exists after the second rm")
	}
}

func TestRemoveRefusesFoldersClaudeAsDidNotMake(t *testing.T) {
	h := newHarness(t)
	personal := h.layout.AccountDir("personal")
	mustWrite(t, filepath.Join(personal, ".claude.json"), "{}")

	code := h.app.Run([]string{"rm", "personal"})

	if code != exitBadArgs || !strings.Contains(h.err.String(), "wasn't made by hat") {
		t.Errorf("exit %d, stderr %q", code, h.err)
	}
	if _, err := os.Stat(personal); err != nil {
		t.Error("unmanaged folder was touched")
	}
}

func TestLaunchRefusesFoldersClaudeAsDidNotMake(t *testing.T) {
	h := newHarness(t)
	mustWrite(t, filepath.Join(h.layout.AccountDir("personal"), ".claude.json"), "{}")

	code := h.app.Run([]string{"personal"})

	if code != exitBadArgs || !strings.Contains(h.err.String(), "leaves it alone") {
		t.Errorf("exit %d, stderr %q", code, h.err)
	}
}

func TestBadArgumentsExplainTheFix(t *testing.T) {
	cases := map[string][]string{
		"isn't a usable account name": {"Bad Name"},
		"rm takes one account name":   {"rm"},
		"there's no account named":    {"rm", "ghost"},
		"usage doesn't take":          {"usage", "--yaml"},
	}
	for want, args := range cases {
		h := newHarness(t)
		if code := h.app.Run(args); code != exitBadArgs || !strings.Contains(h.err.String(), want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", args, code, h.err, want)
		}
	}
}

func TestCommandNameIsRefusedAsAccountName(t *testing.T) {
	h := newHarness(t)

	code := h.app.Run([]string{"rm", "usage"})

	if code != exitBadArgs || !strings.Contains(h.err.String(), "is a hat command") || !strings.Contains(h.err.String(), "usage-account") {
		t.Errorf("exit %d, stderr %q", code, h.err)
	}
}

func TestUpgradeSaysSoWhenItCantFindItself(t *testing.T) {
	h := newHarness(t)
	h.app.InstalledPath = func() (string, error) { return "", errors.New("executable path unavailable") }

	code := h.app.Run([]string{"upgrade"})

	if code != exitFailed || !strings.Contains(h.err.String(), "couldn't work out where this hat is installed (executable path unavailable)") {
		t.Errorf("exit %d, stderr %q", code, h.err)
	}
}

func TestMainIsRefusedAsAccountName(t *testing.T) {
	h := newHarness(t)

	code := h.app.Run([]string{"main"})

	if code != exitBadArgs || !strings.Contains(h.err.String(), `plain "claude"`) {
		t.Errorf("exit %d, stderr %q", code, h.err)
	}
	if _, err := os.Stat(h.layout.AccountDir("main")); !os.IsNotExist(err) {
		t.Error("created a folder for main")
	}
}

func TestRemoveRefusesAnAccountFolderThatIsALink(t *testing.T) {
	h := newHarness(t)
	moved := filepath.Join(t.TempDir(), "work-moved")
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(account.Account{Name: "work", Kind: account.Managed, ConfigDir: moved, CredentialsFile: filepath.Join(moved, ".credentials.json"), StateFile: filepath.Join(moved, ".claude.json")}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(moved, "session-notes.md"), "the account's own files")
	if err := os.Symlink(moved, work.ConfigDir); err != nil {
		t.Skipf("can't create a folder link here: %v", err)
	}

	code := h.app.Run([]string{"rm", "work"})

	if code != exitBadArgs || !strings.Contains(h.err.String(), "won't delete through it") {
		t.Errorf("exit %d, stderr %q", code, h.err)
	}
	if got, err := os.ReadFile(filepath.Join(moved, "session-notes.md")); err != nil || string(got) != "the account's own files" {
		t.Errorf("deleted through the link: %q %v", got, err)
	}
}

func TestMacOSRunsHelpVersionAndAStatusLineThatRecordsNothing(t *testing.T) {
	cases := map[Handling][][]string{
		RunNormally:       {{}, {"help"}, {"-h"}, {"--help"}, {"version"}, {"--version"}},
		StatusLineOnly:    {{"statusline"}, {"statusline", "sh", "~/.claude/statusline.sh"}},
		RefuseUnsupported: {{"work"}, {"usage"}, {"usage", "--live"}, {"rm", "work"}, {"upgrade"}, {"work", "--help"}},
	}
	for want, commandLines := range cases {
		for _, args := range commandLines {
			if got := HandlingFor("darwin", args); got != want {
				t.Errorf("darwin %v: HandlingFor = %d, want %d", args, got, want)
			}
		}
	}
	for _, goos := range []string{"windows", "linux"} {
		for _, args := range [][]string{{"work"}, {"statusline"}, {"usage"}} {
			if got := HandlingFor(goos, args); got != RunNormally {
				t.Errorf("%s %v: HandlingFor = %d, want it to run normally", goos, args, got)
			}
		}
	}
	if !strings.Contains(MacOSUnsupported, "doesn't support macOS") || !strings.Contains(MacOSUnsupported, "Keychain") {
		t.Errorf("MacOSUnsupported = %q", MacOSUnsupported)
	}
}
