package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/account"
	"github.com/yavuz-ozkan/hat/internal/usage"
)

// TestMain lets a test run this test binary as someone's own status line
// command, the same on Windows and Linux.
func TestMain(m *testing.M) {
	if os.Getenv("HAT_TEST_OWN_STATUS_LINE") == "1" {
		input, _ := io.ReadAll(os.Stdin)
		var line struct {
			Model struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
		}
		_ = json.Unmarshal(input, &line)
		fmt.Printf("my own line for %s", line.Model.DisplayName)
		os.Exit(7)
	}
	os.Exit(m.Run())
}

const statusLineInput = `{"session_id":"5f1c","model":{"id":"claude-opus-4-5","display_name":"Opus"},` +
	`"workspace":{"current_dir":"/home/someone/myapp"},"version":"2.1.330",` +
	`"rate_limits":{"five_hour":{"used_percentage":23,"resets_at":4102444800},"seven_day":{"used_percentage":41.2,"resets_at":4103000000}}}`

func (h harness) runStatusLine(t *testing.T, input string, args ...string) int {
	t.Helper()
	h.app.In = strings.NewReader(input)
	return h.app.Run(append([]string{"statusline"}, args...))
}

// filesOutsideTheCache describes everything under home except hat's cache
// folder, without following links, so a test can tell whether any of it
// changed. Folders are compared by what they hold, not their times, which
// Windows updates lazily.
func filesOutsideTheCache(t *testing.T, h harness) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(h.layout.Home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(h.layout.Home, "cache") {
			return filepath.SkipDir
		}
		if path == h.layout.Home {
			return nil
		}
		if !entry.Type().IsRegular() {
			files[path] = entry.Type().String()
			return nil
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func blockUsageFolder(t *testing.T, h harness) {
	t.Helper()
	mustWrite(t, h.layout.UsageDir, "in the way")
}

func TestStatuslineRecordsTheAccountsLimitsAndPrintsADefaultLine(t *testing.T) {
	h := newHarness(t)
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(work); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", work.ConfigDir)
	before := filesOutsideTheCache(t, h)

	code := h.runStatusLine(t, statusLineInput)

	if code != exitOK || h.out.String() != "Opus · 5h 23% · week 41%\n" {
		t.Errorf("exit %d, printed %q", code, h.out)
	}
	record, err := usage.Load(h.layout.UsageFile(work.ConfigDir))
	if err != nil || !strings.Contains(string(record.RateLimits), `"five_hour"`) || record.RecordedAt.IsZero() {
		t.Errorf("record = %+v, %v", record, err)
	}
	if _, err := os.Stat(h.layout.UsageFile("")); !os.IsNotExist(err) {
		t.Error("recorded the work account's limits as main's")
	}
	if after := filesOutsideTheCache(t, h); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("statusline changed files outside its cache:\nbefore %v\nafter  %v", before, after)
	}
}

func TestStatuslineWithoutConfigDirRecordsMain(t *testing.T) {
	h := newHarness(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	h.runStatusLine(t, statusLineInput)

	if _, err := usage.Load(filepath.Join(h.layout.UsageDir, "main.json")); err != nil {
		t.Errorf("main's limits weren't recorded: %v", err)
	}
}

func TestStatuslinePassesItsInputToYourOwnCommand(t *testing.T) {
	h := newHarness(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HAT_TEST_OWN_STATUS_LINE", "1")

	code := h.runStatusLine(t, statusLineInput, os.Args[0])

	if code != 7 || h.out.String() != "my own line for Opus" {
		t.Errorf("exit %d, printed %q; want the command's own exit code and output", code, h.out)
	}
	if _, err := usage.Load(h.layout.UsageFile("")); err != nil {
		t.Errorf("wrapping a command skipped recording: %v", err)
	}
}

func TestStatuslineSaysSoWhenYourCommandWontStart(t *testing.T) {
	h := newHarness(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	code := h.runStatusLine(t, statusLineInput, filepath.Join(t.TempDir(), "no-such-command"))

	if code == exitOK || !strings.Contains(h.out.String(), "Fix the command") {
		t.Errorf("exit %d, printed %q", code, h.out)
	}
}

func TestStatuslineStillPrintsWhenItCantRecord(t *testing.T) {
	h := newHarness(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	blockUsageFolder(t, h)

	for _, input := range []string{statusLineInput, "not json", ""} {
		h.out.Reset()
		if code := h.runStatusLine(t, input); code != exitOK || !strings.HasSuffix(h.out.String(), "\n") {
			t.Errorf("input %q: exit %d, printed %q", input, code, h.out)
		}
	}
}

func TestUsageShowsRecordedLimitsWithoutSendingAnything(t *testing.T) {
	h := newHarness(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	h.runStatusLine(t, statusLineInput)
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(work); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, work.CredentialsFile, `{"claudeAiOauth":{"accessToken":"not-sent","expiresAt":1}}`)
	h.out.Reset()

	code := h.app.Run([]string{"usage", "--json"})

	var rows []usage.Row
	if err := json.Unmarshal(h.out.Bytes(), &rows); code != exitOK || err != nil {
		t.Fatalf("exit %d, %v: %s", code, err, h.out)
	}
	if len(rows) != 2 || rows[0].Account != "main" || rows[0].AsOf == nil || len(rows[0].Limits) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[1].Account != "work" || rows[1].Problem == nil || rows[1].Problem.Code != usage.NoUsageRecorded {
		t.Errorf("work = %+v", rows[1])
	}
}

func TestUsageTextSaysHowToStartRecording(t *testing.T) {
	h := newHarness(t)
	mustWrite(t, filepath.Join(h.layout.MainDir, ".credentials.json"), "{}")

	code := h.app.Run([]string{"usage"})

	if code != exitOK || !strings.Contains(h.out.String(), `"hat statusline"`) || strings.Contains(h.out.String(), "Unofficial") {
		t.Errorf("exit %d, printed %q", code, h.out)
	}
}

func TestHelpLabelsLiveUsageUnofficial(t *testing.T) {
	h := newHarness(t)

	h.app.Run([]string{"help"})

	if !strings.Contains(h.out.String(), "--live") || !strings.Contains(h.out.String(), "unofficial") {
		t.Errorf("help = %s", h.out)
	}
}

func TestStatusLineStillShowsWhenHatCantFindItsFolders(t *testing.T) {
	var out, errOut bytes.Buffer

	code := StatusLineWithoutRecording(strings.NewReader(statusLineInput), &out, &errOut, nil)

	if code != exitOK || !strings.Contains(out.String(), "Opus") {
		t.Errorf("exit %d, output %q", code, out.String())
	}
}
