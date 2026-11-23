package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveOnDisk(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hat_0.1.1_linux_amd64.tar.gz")
	if err := os.WriteFile(path, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func verifyArgs(archive, sourceRef string) []string {
	return []string{"attestation", "verify", archive, "--hostname", "github.com", "--repo", "yavuz-ozkan/hat",
		"--signer-workflow", "yavuz-ozkan/hat/.github/workflows/release.yml", "--source-ref", sourceRef}
}

func TestVerifiesOnlyWhatHatAsks(t *testing.T) {
	archive := archiveOnDisk(t)
	cases := map[string]struct {
		args []string
		want int
	}{
		"hat's question":      {verifyArgs(archive, "refs/tags/v0.1.1"), 0},
		"another tag":         {verifyArgs(archive, "refs/tags/v0.1.0"), 1},
		"no --hostname":       {[]string{"attestation", "verify", archive, "--repo", "yavuz-ozkan/hat"}, 1},
		"no such archive":     {verifyArgs(archive+".missing", "refs/tags/v0.1.1"), 1},
		"signed in to github": {[]string{"auth", "status", "--hostname", "github.com"}, 0},
	}
	for name, c := range cases {
		if got := answer("verifies", "", c.args, io.Discard); got != c.want {
			t.Errorf("%s: exit %d, want %d", name, got, c.want)
		}
	}
}

func TestGHHostWithoutHostnameGoesToThatHost(t *testing.T) {
	archive := archiveOnDisk(t)

	if got := answer("verifies", "ghe.example.com", []string{"auth", "status"}, io.Discard); got != 1 {
		t.Errorf("auth status on GH_HOST: exit %d, want 1", got)
	}
	if got := answer("verifies", "ghe.example.com", []string{"auth", "status", "--hostname", "github.com"}, io.Discard); got != 0 {
		t.Errorf("auth status --hostname github.com: exit %d, want 0", got)
	}
	if got := answer("verifies", "ghe.example.com", verifyArgs(archive, "refs/tags/v0.1.1"), io.Discard); got != 0 {
		t.Errorf("verify --hostname github.com: exit %d, want 0", got)
	}
}

func TestEachModeAnswersAsDocumented(t *testing.T) {
	archive := archiveOnDisk(t)
	verify := verifyArgs(archive, "refs/tags/v0.1.1")
	auth := []string{"auth", "status", "--hostname", "github.com"}
	cases := []struct {
		mode       string
		args       []string
		want       int
		wantOutput string
	}{
		{"fails", auth, 0, ""},
		{"fails", verify, 1, "HTTP 404"},
		{"signed-out", auth, 1, "not logged into github.com"},
		{"too-old", auth, 0, ""},
		{"too-old", verify, 1, `unknown command "attestation"`},
	}
	for _, c := range cases {
		var out strings.Builder
		if got := answer(c.mode, "", c.args, &out); got != c.want || !strings.Contains(out.String(), c.wantOutput) {
			t.Errorf("%s %v: exit %d %q, want %d with %q", c.mode, c.args[:2], got, out.String(), c.want, c.wantOutput)
		}
	}
}
