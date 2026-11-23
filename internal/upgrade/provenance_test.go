package upgrade

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

type ghAnswer struct {
	output   string
	exitCode int
	err      error
}

// fakeGH answers gh auth status with auth and gh attestation verify with
// verify, and records what it was asked.
type fakeGH struct {
	auth, verify ghAnswer
	calls        [][]string
	archive      []byte
}

func (f *fakeGH) run(args ...string) ([]byte, int, error) {
	f.calls = append(f.calls, args)
	answer := f.auth
	if args[0] == "attestation" {
		answer = f.verify
		f.archive, _ = os.ReadFile(args[2])
	}
	return []byte(answer.output), answer.exitCode, answer.err
}

func signedIn(verify ghAnswer) *fakeGH { return &fakeGH{verify: verify} }

func (f *fakeGH) check(t *testing.T) (Provenance, error) {
	t.Helper()
	return GitHubReleases{ReleasesURL: DefaultReleasesURL, GH: f.run}.CheckProvenance("v1.0.0", "hat_1.0.0_linux_amd64.tar.gz", []byte("archive"))
}

func TestCheckProvenanceIsSkippedWithoutGH(t *testing.T) {
	gh := &fakeGH{auth: ghAnswer{err: ErrGHNotInstalled}}

	got, err := gh.check(t)

	if err != nil || got != ProvenanceSkippedNoGH || len(gh.calls) != 1 {
		t.Errorf("got %v, %v after %d gh runs; want skipped for a missing gh", got, err, len(gh.calls))
	}
	if !strings.Contains(got.Note(), "install gh") || !strings.Contains(got.Note(), "gh auth login") {
		t.Errorf("note doesn't say how to turn the check on: %s", got.Note())
	}
}

func TestCheckProvenanceAsksGitHubAboutThisArchiveAndTag(t *testing.T) {
	gh := signedIn(ghAnswer{})

	got, err := gh.check(t)

	if err != nil || got != ProvenanceVerified {
		t.Fatalf("got %v, %v; want verified", got, err)
	}
	if len(gh.calls) != 2 {
		t.Fatalf("ran gh %v", gh.calls)
	}
	if want := []string{"auth", "status", "--hostname", "github.com"}; !slices.Equal(gh.calls[0], want) {
		t.Errorf("gh %v, want gh %v", gh.calls[0], want)
	}
	verify := gh.calls[1]
	want := []string{"attestation", "verify", verify[2], "--hostname", "github.com", "--repo", "yavuz-ozkan/hat",
		"--signer-workflow", "yavuz-ozkan/hat/.github/workflows/release.yml", "--source-ref", "refs/tags/v1.0.0"}
	if !slices.Equal(verify, want) {
		t.Errorf("gh %v, want gh %v", verify, want)
	}
	if string(gh.archive) != "archive" {
		t.Errorf("gh was handed %q, not the downloaded archive", gh.archive)
	}
	if _, err := os.Stat(verify[2]); !os.IsNotExist(err) {
		t.Errorf("left the archive copy for gh behind: %v", err)
	}
}

func TestCheckProvenanceRefusesWhenGHCantVerify(t *testing.T) {
	gh := signedIn(ghAnswer{output: "Error: expected SourceRepositoryRef to be refs/tags/v1.0.0, got refs/tags/v0.9.0\n", exitCode: 1})

	_, err := gh.check(t)

	if err == nil {
		t.Fatal("a failed verification was accepted")
	}
	for _, want := range []string{"wasn't installed", "from tag v1.0.0", "gh said: Error: expected SourceRepositoryRef", "run hat upgrade again", "gh auth login --hostname github.com"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
}

func TestCheckProvenanceIsSkippedWhileGHIsSignedOutOfGitHub(t *testing.T) {
	gh := &fakeGH{auth: ghAnswer{exitCode: 1}}

	got, err := gh.check(t)

	if err != nil || got != ProvenanceSkippedGHSignedOut || len(gh.calls) != 1 {
		t.Errorf("got %v, %v after gh %v; want skipped before verifying", got, err, gh.calls)
	}
	if !strings.Contains(got.Note(), "gh auth login --hostname github.com") {
		t.Errorf("note doesn't say how to sign in to github.com: %s", got.Note())
	}
}

// gh before 2.49, such as Ubuntu 24.04's packaged 2.45, has no attestation
// command. That gh is no more able to check than a missing one.
func TestCheckProvenanceIsSkippedWhenGHIsTooOld(t *testing.T) {
	gh := signedIn(ghAnswer{output: "unknown command \"attestation\" for \"gh\"\n\nUsage:  gh <command> <subcommand> [flags]\n", exitCode: 1})

	if got, err := gh.check(t); err != nil || got != ProvenanceSkippedGHTooOld {
		t.Errorf("got %v, %v; want skipped for an old gh", got, err)
	}
}

func TestCheckProvenanceRefusesWhenGHWontStart(t *testing.T) {
	wontStart := ghAnswer{err: errors.New("exec format error")}
	for name, gh := range map[string]*fakeGH{
		"auth status":        {auth: wontStart},
		"attestation verify": signedIn(wontStart),
	} {
		_, err := gh.check(t)

		if err == nil || !strings.Contains(err.Error(), "couldn't start gh") || !strings.Contains(err.Error(), "exec format error") || !strings.Contains(err.Error(), "wasn't installed") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCheckProvenanceRefusesWhenGHRunsPastItsDeadline(t *testing.T) {
	timedOut := ghAnswer{err: ErrGHTimedOut}
	for name, gh := range map[string]*fakeGH{
		"auth status":        {auth: timedOut},
		"attestation verify": signedIn(timedOut),
	} {
		_, err := gh.check(t)

		if err == nil || !strings.Contains(err.Error(), "longer than 2 minutes") || !strings.Contains(err.Error(), "wasn't installed") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestGHDeadlineCanOnlyBeShortened(t *testing.T) {
	cases := map[string]time.Duration{
		"":     2 * time.Minute,
		"3":    3 * time.Second,
		"119":  119 * time.Second,
		"120":  2 * time.Minute,
		"999":  2 * time.Minute,
		"0":    2 * time.Minute,
		"1000": 2 * time.Minute,
		"+5":   2 * time.Minute,
		"-5":   2 * time.Minute,
		"5s":   2 * time.Minute,
		" 5":   2 * time.Minute,
	}
	for value, want := range cases {
		t.Setenv("HAT_TEST_GH_DEADLINE_SECONDS", value)
		if got := ghDeadline(); got != want {
			t.Errorf("HAT_TEST_GH_DEADLINE_SECONDS=%q: %v, want %v", value, got, want)
		}
	}
}

func TestTimeoutMessageNamesTheDeadlineInForce(t *testing.T) {
	t.Setenv("HAT_TEST_GH_DEADLINE_SECONDS", "")
	if err := ghFailed("hat.zip", ErrGHTimedOut); !strings.Contains(err.Error(), "gh took longer than 2 minutes to check hat.zip's build provenance") {
		t.Errorf("default: %v", err)
	}
	t.Setenv("HAT_TEST_GH_DEADLINE_SECONDS", "3")
	if err := ghFailed("hat.zip", ErrGHTimedOut); !strings.Contains(err.Error(), "gh took longer than 3 seconds") {
		t.Errorf("shortened: %v", err)
	}
}

func TestCheckProvenanceIsSkippedForReleasesOffGitHub(t *testing.T) {
	gh := signedIn(ghAnswer{})

	got, err := GitHubReleases{ReleasesURL: "http://127.0.0.1:8080/releases", GH: gh.run}.CheckProvenance("v1.0.0", "hat.zip", []byte("archive"))

	if err != nil || got != ProvenanceSkippedNotOnGitHub || len(gh.calls) != 0 {
		t.Errorf("got %v, %v after gh %v; want skipped without asking gh", got, err, gh.calls)
	}
}

func TestCheckProvenanceTreatsTheGitHubPrefixAsCaseSensitive(t *testing.T) {
	gh := signedIn(ghAnswer{})

	got, _ := GitHubReleases{ReleasesURL: "https://GitHub.com/yavuz-ozkan/hat/releases", GH: gh.run}.CheckProvenance("v1.0.0", "hat.zip", []byte("archive"))

	if got != ProvenanceSkippedNotOnGitHub {
		t.Errorf("got %v; the installers compare the prefix case-sensitively, so hat must too", got)
	}
}

func TestGHIsOnlyEverGHExeOnWindows(t *testing.T) {
	if ghProgram("windows") != "gh.exe" || ghProgram("linux") != "gh" {
		t.Errorf("ghProgram: windows %s, linux %s", ghProgram("windows"), ghProgram("linux"))
	}
	if runtime.GOOS != "windows" {
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh.cmd"), []byte("@exit /b 0\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if _, _, err := GHOnPath("auth", "status"); !errors.Is(err, ErrGHNotInstalled) {
		t.Errorf("err = %v; a gh.cmd should count as no gh", err)
	}
}

// TestHelperProcess is the program runWithDeadline runs in the tests below.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("HAT_UPGRADE_HELPER") {
	case "":
		return
	case "sleep":
		time.Sleep(time.Minute)
	case "exit3":
		os.Exit(3)
	}
	os.Exit(0)
}

func runHelper(t *testing.T, behavior string, deadline time.Duration) ([]byte, int, error) {
	t.Helper()
	t.Setenv("HAT_UPGRADE_HELPER", behavior)
	return runWithDeadline(deadline, os.Args[0], "-test.run=^TestHelperProcess$")
}

func TestRunWithDeadlineStopsAProgramThatRunsTooLong(t *testing.T) {
	start := time.Now()

	_, _, err := runHelper(t, "sleep", 200*time.Millisecond)

	if !errors.Is(err, ErrGHTimedOut) {
		t.Errorf("err = %v, want ErrGHTimedOut", err)
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("took %v to give up", took)
	}
}

func TestRunWithDeadlinePassesTheExitCodeThrough(t *testing.T) {
	_, code, err := runHelper(t, "exit3", time.Minute)

	if err != nil || code != 3 {
		t.Errorf("code %d, err %v; want 3, nil", code, err)
	}
}

func TestRunWithDeadlineReportsAProgramThatWontStart(t *testing.T) {
	_, _, err := runWithDeadline(time.Minute, filepath.Join(t.TempDir(), "no-such-gh"))

	if err == nil || errors.Is(err, ErrGHTimedOut) || errors.Is(err, ErrGHNotInstalled) {
		t.Errorf("err = %v, want a start failure", err)
	}
}

func TestCheckReleasesURLAllowsOnlyHTTPSOrThisMachine(t *testing.T) {
	allowed := []string{
		"https://github.com/yavuz-ozkan/hat/releases", "https://mirror.example/hat/releases",
		"http://127.0.0.1:8080/release/releases", "http://localhost/releases", "http://[::1]:9000/releases", "http://localhost",
	}
	for _, u := range allowed {
		if err := checkReleasesURL(u); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
	refused := map[string]string{
		"http://mirror.example/releases":       "https://mirror.example/releases",
		"http://localhost.evil.example/r":      "https://localhost.evil.example/r",
		"http://127.0.0.1.evil.example/r":      "https://127.0.0.1.evil.example/r",
		"http://127.0.0.1:80@evil.example/r":   "https://127.0.0.1:80@evil.example/r",
		"HTTPS://github.com/yavuz-ozkan/hat/r": "https://github.com/yavuz-ozkan/hat/r",
		"mirror.example/releases":              "https://mirror.example/releases",
	}
	for u, example := range refused {
		err := checkReleasesURL(u)
		if err == nil || !strings.Contains(err.Error(), "must start with https://") || !strings.Contains(err.Error(), "HAT_RELEASES_URL="+example) {
			t.Errorf("%s: err = %v, want a refusal suggesting %s", u, err, example)
		}
	}
}

type refuseToSend struct{ t *testing.T }

func (r refuseToSend) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Errorf("sent %s %s", req.Method, req.URL)
	return nil, errors.New("refused by the test")
}

func TestLatestTagRefusesAPlainHTTPMirrorBeforeSendingAnything(t *testing.T) {
	source := GitHubReleases{ReleasesURL: "http://mirror.example/releases", Client: &http.Client{Transport: refuseToSend{t}}}

	_, err := source.LatestTag()

	if err == nil || !strings.Contains(err.Error(), "must start with https://") {
		t.Errorf("err = %v", err)
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

func TestRunChecksProvenanceOfTheArchiveAndTagItInstalls(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	releases.provenance = ProvenanceVerified
	exe := installedProgram(t, "version 0.1.0")

	result, err := Run(releases, "0.1.0", exe)

	if err != nil || result.Provenance != ProvenanceVerified {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	asset := assetName("0.2.0", runtime.GOOS, runtime.GOARCH)
	if want := []string{"v0.2.0 " + asset}; !slices.Equal(releases.checked, want) {
		t.Errorf("checked provenance of %v, want %v", releases.checked, want)
	}
}

func TestEveryProvenanceOutcomeHasANote(t *testing.T) {
	for p := ProvenanceVerified; p <= ProvenanceSkippedGHTooOld; p++ {
		if p.Note() == "" {
			t.Errorf("Provenance %d has no note", p)
		}
	}
}

// The installers can't call Go, so they repeat the check. This only catches
// their text drifting apart; .github/scripts/test-installers.* run the
// installers' behavior against stand-in gh programs.
func TestInstallersCheckProvenanceTheSameWay(t *testing.T) {
	shared := []string{
		"https://github.com/*",
		"couldn't start gh to check",
		"gh took longer than",
		"HAT_TEST_GH_DEADLINE_SECONDS",
		"must start with https://, so nothing on the network can change what",
		"plain http:// works only for this machine (127.0.0.1, ::1, or localhost)",
	}
	ghCalls := map[string][]string{
		"install.sh": {
			`gh auth status --hostname github.com >/dev/null 2>&1`,
			`gh attestation verify "$1" --hostname github.com --repo "$repo" --signer-workflow yavuz-ozkan/hat/.github/workflows/release.yml --source-ref "refs/tags/$2"`,
		},
		"install.ps1": {
			`@('auth', 'status', '--hostname', 'github.com')`,
			`@('attestation', 'verify', $Archive, '--hostname', 'github.com', '--repo', $repo,`,
			`'--signer-workflow', 'yavuz-ozkan/hat/.github/workflows/release.yml', '--source-ref', "refs/tags/$Tag")`,
		},
	}
	for installer, calls := range ghCalls {
		script := repoFile(t, installer)
		for _, want := range append(slices.Clone(shared), calls...) {
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
	ps1 := repoFile(t, "install.ps1")
	for _, want := range []string{"-cnotlike 'https://github.com/*'", "Get-Command gh.exe -CommandType Application"} {
		if !strings.Contains(ps1, want) {
			t.Errorf("install.ps1 doesn't contain %s", want)
		}
	}
}
