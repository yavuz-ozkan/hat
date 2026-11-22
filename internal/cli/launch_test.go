package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/account"
)

// launched is one run of hat in its own process. On Unix hat replaces itself
// with Claude, which would end a test running it in-process.
type launched struct {
	code   int
	stderr string
	calls  []claudeCall
}

func installStandInClaude(t *testing.T, h harness) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
	}
	mustWrite(t, filepath.Join(h.layout.Home, ".local", "bin", name), string(program))
	if err := os.Chmod(filepath.Join(h.layout.Home, ".local", "bin", name), 0o755); err != nil {
		t.Fatal(err)
	}
}

// launchHat runs hat with args in a folder outside git, with no ambient
// credentials unless extraEnv sets them.
func launchHat(t *testing.T, h harness, extraEnv []string, args ...string) launched {
	t.Helper()
	claudeLog := filepath.Join(t.TempDir(), "claude-calls.jsonl")
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = t.TempDir()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(ambientCredentials, strings.ToUpper(name)) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "HAT_TEST_HOME="+h.layout.Home, "HAT_TEST_CLAUDE_LOG="+claudeLog)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr

	err := cmd.Run()

	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) {
		t.Fatal(err)
	}
	return launched{code: cmd.ProcessState.ExitCode(), stderr: stderr.String(), calls: readClaudeCalls(t, claudeLog)}
}

func readClaudeCalls(t *testing.T, path string) []claudeCall {
	t.Helper()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var calls []claudeCall
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var call claudeCall
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func checkStartedAsTheAccount(t *testing.T, h harness, call claudeCall) {
	t.Helper()
	work := account.Named(h.layout, "work")
	if call.ConfigDir != work.ConfigDir {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", call.ConfigDir, work.ConfigDir)
	}
	if call.BrowserSession != "hat-work" || call.BrowserProfile != filepath.Join(work.ConfigDir, "browser-profile") {
		t.Errorf("agent-browser session %q, profile %q", call.BrowserSession, call.BrowserProfile)
	}
	if len(call.Args) != 4 || call.Args[0] != "--settings" || !strings.Contains(call.Args[1], "autoMemoryDirectory") || !slices.Equal(call.Args[2:], []string{"--resume", "a b&c"}) {
		t.Errorf("args = %q, want hat's --settings, then the person's own arguments", call.Args)
	}
}

func TestLaunchSignsANewAccountInThenStartsClaudeAsIt(t *testing.T) {
	h := newHarness(t)
	installStandInClaude(t, h)

	run := launchHat(t, h, nil, "work", "--resume", "a b&c")

	if run.code != claudeExitCode {
		t.Fatalf("exit %d, want Claude's %d; stderr %s", run.code, claudeExitCode, run.stderr)
	}
	if len(run.calls) != 3 || !slices.Equal(run.calls[0].Args, []string{"auth", "login", "--claudeai"}) || !slices.Equal(run.calls[1].Args, []string{"auth", "status"}) {
		t.Fatalf("calls = %+v, want sign-in, then a check, then Claude", run.calls)
	}
	for _, call := range run.calls[:2] {
		if call.ConfigDir != account.Named(h.layout, "work").ConfigDir {
			t.Errorf("signed in with CLAUDE_CONFIG_DIR = %q", call.ConfigDir)
		}
	}
	checkStartedAsTheAccount(t, h, run.calls[2])
}

func TestLaunchStartsAnExistingAccountWithoutSigningIn(t *testing.T) {
	h := newHarness(t)
	installStandInClaude(t, h)
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(work); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, work.CredentialsFile, "{}")

	run := launchHat(t, h, nil, "work", "--resume", "a b&c")

	if run.code != claudeExitCode || len(run.calls) != 1 {
		t.Fatalf("exit %d, calls %+v; stderr %s", run.code, run.calls, run.stderr)
	}
	checkStartedAsTheAccount(t, h, run.calls[0])
	if strings.Contains(run.stderr, "warning") {
		t.Errorf("warned with nothing wrong: %s", run.stderr)
	}
}

func TestLaunchWarnsThatAmbientCredentialsWinOverTheAccount(t *testing.T) {
	h := newHarness(t)
	installStandInClaude(t, h)

	run := launchHat(t, h, []string{"ANTHROPIC_API_KEY=sk-test"}, "work")

	if !strings.Contains(run.stderr, "ANTHROPIC_API_KEY is set, so Claude Code uses it instead of work's own sign-in") || strings.Contains(run.stderr, "sk-test") {
		t.Errorf("stderr = %s", run.stderr)
	}
	if run.code != claudeExitCode {
		t.Errorf("exit %d; the warning shouldn't stop Claude", run.code)
	}
}

func TestLaunchSaysToRunClaudeOnceWhenMainIsFresh(t *testing.T) {
	h := newHarness(t)
	installStandInClaude(t, h)
	if err := os.RemoveAll(filepath.Join(h.layout.MainDir, "projects")); err != nil {
		t.Fatal(err)
	}

	run := launchHat(t, h, nil, "work")

	if !strings.Contains(run.stderr, "run plain claude once first") || run.code != claudeExitCode {
		t.Errorf("exit %d, stderr = %s", run.code, run.stderr)
	}
}
