//go:build !windows

package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Windows can't hold control characters in a folder name, so only Unix can
// have an account named with an escape sequence.
func TestUsageLivePrintsNoEscapesFromAnAccountFolderName(t *testing.T) {
	const escapes = "\x1b]0;pwned\x07\x1b[2J"
	for name, claudeInstalled := range map[string]bool{"renewing": true, "can't find claude": false} {
		h := newHarness(t)
		t.Setenv("HAT_TEST_CLAUDE_LOG", filepath.Join(t.TempDir(), "claude-calls.jsonl"))
		t.Setenv("PATH", t.TempDir())
		if claudeInstalled {
			installStandInClaude(t, h)
		}
		dir := filepath.Join(h.layout.Home, ".claude-evil"+escapes)
		state, _ := json.Marshal(map[string]any{"oauthAccount": map[string]string{"emailAddress": "evil@example.com" + escapes}})
		mustWrite(t, filepath.Join(dir, ".claude.json"), string(state))
		mustWrite(t, filepath.Join(dir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"expired-token","expiresAt":1000}}`)

		h.app.Run([]string{"usage", "--live"})

		for stream, text := range map[string]string{"stdout": h.out.String(), "stderr": h.err.String()} {
			if strings.ContainsAny(text, "\x1b\x07") {
				t.Errorf("%s: %s holds control characters: %q", name, stream, text)
			}
		}
		if !strings.Contains(h.err.String(), "evil's expired sign-in") {
			t.Errorf("%s: stderr doesn't name the account: %q", name, h.err.String())
		}
	}
}
