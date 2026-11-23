package upgrade

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SignerWorkflow is the only workflow whose attestations hat accepts. The
// installers pin the same one.
const SignerWorkflow = Repo + "/.github/workflows/release.yml"

// Provenance says what became of the build provenance check for the archive
// Run installed.
type Provenance int

const (
	// ProvenanceNotRun: Run downloaded nothing, so there was nothing to check.
	ProvenanceNotRun Provenance = iota
	ProvenanceVerified
	ProvenanceSkippedNotOnGitHub
	ProvenanceSkippedNoGH
	ProvenanceSkippedGHSignedOut
	ProvenanceSkippedGHTooOld
)

// Note is the line to show after an install. The installers print the same
// lines.
func (p Provenance) Note() string {
	return provenanceNotes[p]
}

var provenanceNotes = map[Provenance]string{
	ProvenanceVerified:           "gh verified its build provenance: it was built by " + SignerWorkflow + ".",
	ProvenanceSkippedNotOnGitHub: "Build provenance not checked: HAT_RELEASES_URL isn't a github.com address, and only GitHub releases carry hat's attestations.",
	ProvenanceSkippedNoGH:        "Build provenance not checked: the GitHub CLI isn't installed. To check it next time, install gh from https://cli.github.com and run gh auth login.",
	ProvenanceSkippedGHSignedOut: "Build provenance not checked: gh isn't signed in. To check it next time, run gh auth login.",
	ProvenanceSkippedGHTooOld:    "Build provenance not checked: this gh is too old to verify attestations. To check it next time, upgrade gh from https://cli.github.com.",
}

// RunGH runs the GitHub CLI with args and returns everything it printed and
// its exit code. err is ErrGHNotInstalled when gh isn't on PATH, and any
// other error means gh couldn't be started.
type RunGH func(args ...string) (output []byte, exitCode int, err error)

var ErrGHNotInstalled = errors.New("gh isn't installed")

// ghAuthRequired is the exit code gh documents (gh help exit-codes) for a
// command that needs a sign-in it doesn't have.
const ghAuthRequired = 4

func GHOnPath(args ...string) ([]byte, int, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return nil, 0, ErrGHNotInstalled
	}
	output, err := exec.Command(gh, args...).CombinedOutput()
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return output, exited.ExitCode(), nil
	}
	return output, 0, err
}

func onGitHub(releasesURL string) bool {
	return strings.HasPrefix(releasesURL, "https://github.com/")
}

// CheckProvenance asks gh whether hat's release workflow built archive.
// Without gh, or with gh signed out, it reports the check as skipped: the
// checksum already matched, and provenance is an extra the person opts into
// by installing gh.
func (g GitHubReleases) CheckProvenance(asset string, archive []byte) (Provenance, error) {
	if !onGitHub(g.ReleasesURL) {
		return ProvenanceSkippedNotOnGitHub, nil
	}
	dir, err := os.MkdirTemp("", "hat-provenance-")
	if err != nil {
		return 0, fmt.Errorf("couldn't save %s where gh can check its build provenance (%v). Check that your temp folder is writable, then run hat upgrade again", asset, err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, asset)
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		return 0, fmt.Errorf("couldn't save %s where gh can check its build provenance (%v). Check that your temp folder is writable, then run hat upgrade again", asset, err)
	}

	output, exitCode, err := g.GH("attestation", "verify", path, "--repo", Repo, "--signer-workflow", SignerWorkflow)
	switch {
	case errors.Is(err, ErrGHNotInstalled):
		return ProvenanceSkippedNoGH, nil
	case err != nil:
		return 0, fmt.Errorf("couldn't start gh to check %s's build provenance (%v), so it wasn't installed. Check that gh --version runs, then run hat upgrade again", asset, err)
	case exitCode == 0:
		return ProvenanceVerified, nil
	case exitCode == ghAuthRequired:
		return ProvenanceSkippedGHSignedOut, nil
	case bytes.Contains(output, []byte(`unknown command "attestation"`)):
		return ProvenanceSkippedGHTooOld, nil
	}
	return 0, fmt.Errorf("%s matches its checksum, but gh couldn't verify it was built by %s, so it wasn't installed. gh said: %s. "+
		"Run hat upgrade again; if gh's sign-in is the problem, run gh auth login first. If it keeps failing, report it at https://github.com/%s/security",
		asset, SignerWorkflow, oneLine(output), Repo)
}

func oneLine(output []byte) string {
	return strings.Join(strings.Fields(string(output)), " ")
}
