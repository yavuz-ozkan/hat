package upgrade

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type ghCall struct {
	args    []string
	archive []byte
}

// fakeGH answers every gh run with output and exitCode, or with err, and
// records what it was asked to check.
func fakeGH(output string, exitCode int, err error) (RunGH, *[]ghCall) {
	var calls []ghCall
	run := func(args ...string) ([]byte, int, error) {
		call := ghCall{args: args}
		if len(args) > 2 {
			call.archive, _ = os.ReadFile(args[2])
		}
		calls = append(calls, call)
		return []byte(output), exitCode, err
	}
	return run, &calls
}

func onGitHubWith(gh RunGH) GitHubReleases {
	return GitHubReleases{ReleasesURL: DefaultReleasesURL, GH: gh}
}

func TestCheckProvenanceIsSkippedWithoutGH(t *testing.T) {
	gh, _ := fakeGH("", 0, ErrGHNotInstalled)

	got, err := onGitHubWith(gh).CheckProvenance("hat.zip", []byte("archive"))

	if err != nil || got != ProvenanceSkippedNoGH {
		t.Errorf("got %v, %v; want skipped for a missing gh", got, err)
	}
	if !strings.Contains(got.Note(), "install gh") || !strings.Contains(got.Note(), "gh auth login") {
		t.Errorf("note doesn't say how to turn the check on: %s", got.Note())
	}
}

func TestCheckProvenanceAsksGHAboutTheArchiveAndTheReleaseWorkflow(t *testing.T) {
	gh, calls := fakeGH("", 0, nil)

	got, err := onGitHubWith(gh).CheckProvenance("hat_1.0.0_linux_amd64.tar.gz", []byte("archive"))

	if err != nil || got != ProvenanceVerified {
		t.Fatalf("got %v, %v; want verified", got, err)
	}
	if len(*calls) != 1 {
		t.Fatalf("ran gh %d times", len(*calls))
	}
	call := (*calls)[0]
	want := []string{"attestation", "verify", call.args[2], "--repo", "yavuz-ozkan/hat", "--signer-workflow", "yavuz-ozkan/hat/.github/workflows/release.yml"}
	if !slices.Equal(call.args, want) {
		t.Errorf("gh %v, want gh %v", call.args, want)
	}
	if string(call.archive) != "archive" {
		t.Errorf("gh was handed %q, not the downloaded archive", call.archive)
	}
	if _, err := os.Stat(call.args[2]); !os.IsNotExist(err) {
		t.Errorf("left the archive copy for gh behind: %v", err)
	}
}

func TestCheckProvenanceRefusesWhenGHCantVerify(t *testing.T) {
	gh, _ := fakeGH("Error: HTTP 404: Not Found (https://api.github.com/repos/yavuz-ozkan/hat/attestations/sha256:3e79)\n", 1, nil)

	_, err := onGitHubWith(gh).CheckProvenance("hat_1.0.0_linux_amd64.tar.gz", []byte("archive"))

	if err == nil {
		t.Fatal("a failed verification was accepted")
	}
	for _, want := range []string{"wasn't installed", "gh said: Error: HTTP 404", "run hat upgrade again", "gh auth login"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
}

func TestCheckProvenanceIsSkippedWhileGHIsSignedOut(t *testing.T) {
	gh, _ := fakeGH("To get started with GitHub CLI, please run:  gh auth login\n", ghAuthRequired, nil)

	got, err := onGitHubWith(gh).CheckProvenance("hat.zip", []byte("archive"))

	if err != nil || got != ProvenanceSkippedGHSignedOut || !strings.Contains(got.Note(), "gh auth login") {
		t.Errorf("got %v (%s), %v; want skipped with how to sign in", got, got.Note(), err)
	}
}

// gh before 2.49, such as Ubuntu 24.04's packaged 2.45, has no attestation
// command. That gh is no more able to check than a missing one.
func TestCheckProvenanceIsSkippedWhenGHIsTooOld(t *testing.T) {
	gh, _ := fakeGH("unknown command \"attestation\" for \"gh\"\n\nUsage:  gh <command> <subcommand> [flags]\n", 1, nil)

	got, err := onGitHubWith(gh).CheckProvenance("hat.zip", []byte("archive"))

	if err != nil || got != ProvenanceSkippedGHTooOld {
		t.Errorf("got %v, %v; want skipped for an old gh", got, err)
	}
}

func TestCheckProvenanceRefusesWhenGHWontStart(t *testing.T) {
	gh, _ := fakeGH("", 0, errors.New("permission denied"))

	_, err := onGitHubWith(gh).CheckProvenance("hat.zip", []byte("archive"))

	if err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "wasn't installed") {
		t.Errorf("err = %v", err)
	}
}

func TestCheckProvenanceIsSkippedForReleasesOffGitHub(t *testing.T) {
	gh, calls := fakeGH("", 0, nil)

	got, err := GitHubReleases{ReleasesURL: "http://127.0.0.1:8080/releases", GH: gh}.CheckProvenance("hat.zip", []byte("archive"))

	if err != nil || got != ProvenanceSkippedNotOnGitHub || len(*calls) != 0 {
		t.Errorf("got %v, %v after %d gh runs; want skipped without asking gh", got, err, len(*calls))
	}
}

func TestReleasesAtDropsATrailingSlash(t *testing.T) {
	if got := ReleasesAt("https://example.com/hat/releases/").ReleasesURL; got != "https://example.com/hat/releases" {
		t.Errorf("ReleasesURL = %s", got)
	}
}

func TestRunKeepsTheOldProgramWhenProvenanceFails(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	releases.provenanceErr = errors.New("gh couldn't verify it")
	exe := installedProgram(t, "version 0.1.0")

	_, err := Run(releases, "0.1.0", exe)

	if err == nil {
		t.Fatal("installed a release whose provenance failed")
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.1.0" {
		t.Errorf("program changed to %q", got)
	}
}

func TestRunChecksProvenanceOfTheArchiveItInstalls(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	releases.provenance = ProvenanceVerified
	exe := installedProgram(t, "version 0.1.0")

	result, err := Run(releases, "0.1.0", exe)

	if err != nil || result.Provenance != ProvenanceVerified {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	asset := assetName("0.2.0", runtime.GOOS, runtime.GOARCH)
	if !slices.Equal(releases.checked, []string{asset}) {
		t.Errorf("checked provenance of %v, want [%s]", releases.checked, asset)
	}
}

func TestEveryProvenanceOutcomeHasANote(t *testing.T) {
	for p := ProvenanceVerified; p <= ProvenanceSkippedGHTooOld; p++ {
		if p.Note() == "" {
			t.Errorf("Provenance %d has no note", p)
		}
	}
}

// The installers can't call Go, so they repeat the check; this keeps the
// three from drifting apart.
func TestInstallersCheckProvenanceTheSameWay(t *testing.T) {
	for _, installer := range []string{"install.sh", "install.ps1"} {
		script := repoFile(t, installer)
		for _, want := range []string{"attestation verify", "--signer-workflow", SignerWorkflow, "https://github.com/*"} {
			if !strings.Contains(script, want) {
				t.Errorf("%s doesn't contain %s", installer, want)
			}
		}
		for p := ProvenanceVerified; p <= ProvenanceSkippedGHTooOld; p++ {
			if !strings.Contains(script, p.Note()) {
				t.Errorf("%s doesn't print %q", installer, p.Note())
			}
		}
	}
}
