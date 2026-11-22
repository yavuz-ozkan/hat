package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/layout"
)

// TestMain lets a test run this test binary as one of three programs, the
// same on Windows and Linux: someone's own status line command, hat itself,
// or, through a copy named claude, Claude Code.
func TestMain(m *testing.M) {
	switch {
	case strings.HasPrefix(filepath.Base(os.Args[0]), "claude"):
		actAsClaude()
	case os.Getenv("HAT_TEST_OWN_STATUS_LINE") == "1":
		actAsOwnStatusLine()
	case os.Getenv("HAT_TEST_HOME") != "":
		actAsHat(os.Getenv("HAT_TEST_HOME"))
	}
	os.Exit(m.Run())
}

func actAsOwnStatusLine() {
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

func actAsHat(home string) {
	app := App{Layout: testLayout(home), In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Version: "test"}
	os.Exit(app.Run(os.Args[1:]))
}

func testLayout(home string) layout.Layout {
	return layout.ForHome(home, layout.UserDirs{Config: filepath.Join(home, "config"), Cache: filepath.Join(home, "cache")})
}

// claudeCall is what the stand-in Claude saw when hat started it.
type claudeCall struct {
	Args           []string
	ConfigDir      string
	BrowserSession string
	BrowserProfile string
}

const claudeExitCode = 3

// actAsClaude signs in by writing a credentials file, as Claude Code does,
// and otherwise exits with claudeExitCode, logging every call.
func actAsClaude() {
	call := claudeCall{
		Args:           os.Args[1:],
		ConfigDir:      os.Getenv("CLAUDE_CONFIG_DIR"),
		BrowserSession: os.Getenv("AGENT_BROWSER_SESSION"),
		BrowserProfile: os.Getenv("AGENT_BROWSER_PROFILE"),
	}
	line, _ := json.Marshal(call)
	log, err := os.OpenFile(os.Getenv("HAT_TEST_CLAUDE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(100)
	}
	_, _ = log.Write(append(line, '\n'))
	_ = log.Close()
	if len(call.Args) == 0 || call.Args[0] != "auth" {
		os.Exit(claudeExitCode)
	}
	if call.Args[1] == "login" {
		_ = os.WriteFile(filepath.Join(call.ConfigDir, ".credentials.json"), []byte("{}"), 0o600)
	}
	os.Exit(0)
}
