package upgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
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
	ProvenanceSkippedGHSignedOut: "Build provenance not checked: gh isn't signed in to github.com. To check it next time, run gh auth login --hostname github.com.",
	ProvenanceSkippedGHTooOld:    "Build provenance not checked: this gh is too old to verify attestations. To check it next time, upgrade gh from https://cli.github.com.",
}

// RunGH runs the GitHub CLI with args and returns everything it printed and
// its exit code. err is ErrGHNotInstalled when gh isn't on PATH,
// ErrGHTimedOut when it ran past its deadline, and any other error means gh
// couldn't be started.
type RunGH func(args ...string) (output []byte, exitCode int, err error)

var (
	ErrGHNotInstalled = errors.New("gh isn't installed")
	ErrGHTimedOut     = errors.New("gh didn't finish in time")
)

const ghDeadlineDefault = 2 * time.Minute

// ghDeadline is how long each gh run may take. HAT_TEST_GH_DEADLINE_SECONDS
// lets the installer tests watch a hung gh give up without waiting 2 minutes.
// It can only shorten the wait: anything but 1 to 119 seconds, written as
// digits, leaves the default. Both installers read it the same way.
func ghDeadline() time.Duration {
	seconds := os.Getenv("HAT_TEST_GH_DEADLINE_SECONDS")
	if seconds == "" || len(seconds) > 3 || strings.Trim(seconds, "0123456789") != "" {
		return ghDeadlineDefault
	}
	n, _ := strconv.Atoi(seconds)
	if n < 1 || n >= 120 {
		return ghDeadlineDefault
	}
	return time.Duration(n) * time.Second
}

func describeDeadline(d time.Duration) string {
	if d == ghDeadlineDefault {
		return "2 minutes"
	}
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

// ghProgram is the only name hat runs gh by. On Windows that's gh.exe: a
// gh.cmd or gh.bat would run through cmd.exe, which acts on characters in the
// archive's path, the same reason claude.Find refuses them.
func ghProgram(goos string) string {
	if goos == "windows" {
		return "gh.exe"
	}
	return "gh"
}

func GHOnPath(args ...string) ([]byte, int, error) {
	gh, err := exec.LookPath(ghProgram(runtime.GOOS))
	if err != nil {
		return nil, 0, ErrGHNotInstalled
	}
	return runWithDeadline(ghDeadline(), gh, args...)
}

func runWithDeadline(deadline time.Duration, program string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	// gh may leave a child holding its output open; without this, a killed gh
	// could still keep hat waiting.
	cmd.WaitDelay = 5 * time.Second
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, 0, ErrGHTimedOut
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return output, exited.ExitCode(), nil
	}
	return output, 0, err
}

func onGitHub(releasesURL string) bool {
	return strings.HasPrefix(releasesURL, "https://github.com/")
}

// CheckProvenance asks gh whether hat's release workflow built archive from
// tag. Without gh, or with gh signed out of github.com, it reports the check
// as skipped: the checksum already matched, and provenance is an extra the
// person opts into by installing gh. --hostname github.com keeps a GH_HOST
// pointing at GitHub Enterprise from sending the check there.
func (g GitHubReleases) CheckProvenance(tag, asset string, archive []byte) (Provenance, error) {
	if !onGitHub(g.ReleasesURL) {
		return ProvenanceSkippedNotOnGitHub, nil
	}
	// Only the exit code matters. gh masks the token it shows here, and hat
	// never reads this output.
	_, exitCode, err := g.GH("auth", "status", "--hostname", "github.com")
	switch {
	case errors.Is(err, ErrGHNotInstalled):
		return ProvenanceSkippedNoGH, nil
	case err != nil:
		return 0, ghFailed(asset, err)
	case exitCode != 0:
		return ProvenanceSkippedGHSignedOut, nil
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

	output, exitCode, err := g.GH("attestation", "verify", path, "--hostname", "github.com", "--repo", Repo,
		"--signer-workflow", SignerWorkflow, "--source-ref", "refs/tags/"+tag)
	switch {
	case err != nil:
		return 0, ghFailed(asset, err)
	case exitCode == 0:
		return ProvenanceVerified, nil
	case bytes.Contains(output, []byte(`unknown command "attestation"`)):
		return ProvenanceSkippedGHTooOld, nil
	}
	return 0, fmt.Errorf("%s matches its checksum, but gh couldn't verify it was built by %s from tag %s, so it wasn't installed. gh said: %s. "+
		"Run hat upgrade again; if gh's sign-in is the problem, run gh auth login --hostname github.com first. If it keeps failing, report it at https://github.com/%s/security",
		asset, SignerWorkflow, tag, oneLine(output), Repo)
}

func ghFailed(asset string, err error) error {
	if errors.Is(err, ErrGHTimedOut) {
		return fmt.Errorf("gh took longer than %s to check %s's build provenance, so it wasn't installed. Check your connection, then run hat upgrade again", describeDeadline(ghDeadline()), asset)
	}
	return fmt.Errorf("couldn't start gh to check %s's build provenance (%v), so it wasn't installed. Check that gh --version runs, then run hat upgrade again", asset, err)
}

func oneLine(output []byte) string {
	return strings.Join(strings.Fields(string(output)), " ")
}
