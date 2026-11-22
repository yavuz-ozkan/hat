package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func zipBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	_, _ = w.Write(content)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tarGzBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2})
	_, _ = tw.Write([]byte("hi"))
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))})
	_, _ = tw.Write(content)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func archiveForThisPlatform(t *testing.T, program []byte) []byte {
	t.Helper()
	if runtime.GOOS == "windows" {
		return zipBytes(t, binaryName(runtime.GOOS), program)
	}
	return tarGzBytes(t, binaryName(runtime.GOOS), program)
}

func shaOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestAssetNameFormat(t *testing.T) {
	if got := assetName("0.2.0", "windows", "amd64"); got != "hat_0.2.0_windows_amd64.zip" {
		t.Errorf("windows: %s", got)
	}
	if got := assetName("0.2.0", "linux", "arm64"); got != "hat_0.2.0_linux_arm64.tar.gz" {
		t.Errorf("linux: %s", got)
	}
}

func TestExtractBinaryFromBothArchiveKinds(t *testing.T) {
	zipped := zipBytes(t, "hat.exe", []byte("windows build"))
	tarred := tarGzBytes(t, "hat", []byte("linux build"))

	if got, err := extractBinary("a.zip", zipped, "hat.exe"); err != nil || string(got) != "windows build" {
		t.Errorf("zip: %q %v", got, err)
	}
	if got, err := extractBinary("a.tar.gz", tarred, "hat"); err != nil || string(got) != "linux build" {
		t.Errorf("tar.gz: %q %v", got, err)
	}
	if _, err := extractBinary("a.zip", zipped, "missing"); err == nil {
		t.Error("extracted a file that isn't in the archive")
	}
}

func TestVerifyAcceptsOnlyTheListedChecksum(t *testing.T) {
	name := "hat_1.0.0_linux_amd64.tar.gz"
	archive := tarGzBytes(t, "hat", []byte("build"))
	good := []byte(shaOf(archive) + "  " + name + "\n")
	bad := []byte(strings.Repeat("0", 64) + "  " + name + "\n")
	other := []byte(shaOf(archive) + "  something-else.tar.gz\n")

	if err := verify(name, archive, good); err != nil {
		t.Errorf("good checksum refused: %v", err)
	}
	if err := verify(name, archive, bad); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Errorf("bad checksum: %v", err)
	}
	if err := verify(name, archive, other); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("missing entry: %v", err)
	}
}

func TestReplaceSwapsTheProgramAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName(runtime.GOOS))
	_ = os.WriteFile(exe, []byte("old"), 0o755)

	if err := (swapper{rename: os.Rename}).replace(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	RemoveReplacedWindowsBinaries(exe)

	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Errorf("program = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("left behind %v", entries)
	}
}

type fakeReleases struct {
	tag     string
	files   map[string][]byte
	fetches int
}

func (f *fakeReleases) LatestTag() (string, error) { return f.tag, nil }

func (f *fakeReleases) Fetch(_ string, name string) ([]byte, error) {
	f.fetches++
	data, ok := f.files[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, ErrNotPublished)
	}
	return data, nil
}

func publishedRelease(t *testing.T, version string, program []byte) *fakeReleases {
	t.Helper()
	asset := assetName(version, runtime.GOOS, runtime.GOARCH)
	archive := archiveForThisPlatform(t, program)
	sums := fmt.Sprintf("%s  %s\n", shaOf(archive), asset)
	return &fakeReleases{tag: "v" + version, files: map[string][]byte{asset: archive, "checksums.txt": []byte(sums)}}
}

func installedProgram(t *testing.T, content string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), binaryName(runtime.GOOS))
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestRunInstallsANewerRelease(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "version 0.1.0")

	result, err := Run(releases, "0.1.0", exe)

	if err != nil || result.Outcome != Installed || result.To != "0.2.0" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.2.0" {
		t.Errorf("program = %q", got)
	}
}

func TestRunRefusesWhileAnotherUpgradeIsRunning(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "version 0.1.0")
	if err := os.WriteFile(exe+lockSuffix, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Run(releases, "0.1.0", exe)

	if err == nil || !strings.Contains(err.Error(), "another hat upgrade is running") || !strings.Contains(err.Error(), exe+lockSuffix) {
		t.Errorf("err = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.1.0" || releases.fetches != 0 {
		t.Errorf("program = %q after %d downloads; want it untouched", got, releases.fetches)
	}
	if _, err := os.Stat(exe + lockSuffix); err != nil {
		t.Errorf("removed the other upgrade's lock: %v", err)
	}
}

func TestRunNeverRemovesAnOldLockItDidntCreate(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "version 0.1.0")
	if err := os.WriteFile(exe+lockSuffix, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dayOld := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(exe+lockSuffix, dayOld, dayOld); err != nil {
		t.Fatal(err)
	}

	_, err := Run(releases, "0.1.0", exe)

	if err == nil || !strings.Contains(err.Error(), "delete "+exe+lockSuffix) {
		t.Errorf("err = %v, want it to name the lock to delete", err)
	}
	if _, err := os.Stat(exe + lockSuffix); err != nil {
		t.Errorf("removed a lock it didn't create: %v", err)
	}
}

func TestRunRemovesItsOwnLock(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "version 0.1.0")

	if _, err := Run(releases, "0.1.0", exe); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(exe + lockSuffix); !os.IsNotExist(err) {
		t.Errorf("left its lock behind: %v", err)
	}
}

func TestRunDoesNothingWhenAlreadyNewest(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "version 0.2.0")

	result, err := Run(releases, "0.2.0", exe)

	if err != nil || result.Outcome != AlreadyNewest || releases.fetches != 0 {
		t.Errorf("result=%+v err=%v fetches=%d", result, err, releases.fetches)
	}
}

func TestRunSaysWhenTheReleaseHasNoBuildForThisPlatform(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	delete(releases.files, assetName("0.2.0", runtime.GOOS, runtime.GOARCH))
	exe := installedProgram(t, "version 0.1.0")

	_, err := Run(releases, "0.1.0", exe)

	if err == nil || !strings.Contains(err.Error(), "has no build for "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("err = %v", err)
	}
}

func TestRemoveReplacedWindowsBinariesDeletesOnlyWhatUpgradesCreate(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName(runtime.GOOS))
	movedAside := exe + ".moved-aside-by-hat-1795133452217000000"
	backups := []string{exe + ".old", exe + ".old-20261120", exe + ".moved-aside-by-hat-", exe + ".moved-aside-by-hat-12a"}
	_ = os.WriteFile(movedAside, []byte("replaced by an upgrade"), 0o644)
	for _, backup := range backups {
		_ = os.WriteFile(backup, []byte("someone's backup"), 0o644)
	}

	RemoveReplacedWindowsBinaries(exe)

	_, err := os.Stat(movedAside)
	if runtime.GOOS == "windows" && !os.IsNotExist(err) {
		t.Error("on Windows the copy an upgrade moved aside should be deleted")
	}
	if runtime.GOOS != "windows" && err != nil {
		t.Error("deleted a file on a system where upgrades never move one aside")
	}
	for _, backup := range backups {
		if _, err := os.Stat(backup); err != nil {
			t.Errorf("deleted %s, which no upgrade created", filepath.Base(backup))
		}
	}
}

func failingRenames(failedCalls ...int) swapper {
	calls := 0
	return swapper{rename: func(from, to string) error {
		calls++
		if slices.Contains(failedCalls, calls) {
			return fmt.Errorf("simulated failure renaming %s", filepath.Base(from))
		}
		return os.Rename(from, to)
	}}
}

func failInstallRename() swapper { return failingRenames(2) }

func failInstallAndRestore() swapper { return failingRenames(2, 3) }

func TestReplaceOnWindowsPutsTheOldProgramBackWhenTheSwapFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the move-aside swap only runs on Windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "hat.exe")
	_ = os.WriteFile(exe, []byte("old"), 0o755)

	err := failInstallRename().replace(exe, []byte("new"))

	if err == nil {
		t.Fatal("swap failure wasn't reported")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("program = %q, want the old one back", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left behind %v", entries)
	}
}

func TestReplaceOnWindowsSaysWhereTheOldProgramIsWhenItCantBePutBack(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the move-aside swap only runs on Windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "hat.exe")
	_ = os.WriteFile(exe, []byte("old"), 0o755)

	err := failInstallAndRestore().replace(exe, []byte("new"))

	notRestored, ok := err.(*NotRestoredError)
	if !ok {
		t.Fatalf("err = %v, want NotRestoredError", err)
	}
	if got, _ := os.ReadFile(notRestored.MovedTo); string(got) != "old" {
		t.Errorf("%s holds %q, want the old program", notRestored.MovedTo, got)
	}
	if !strings.Contains(err.Error(), "rename it to hat.exe") {
		t.Errorf("message doesn't say how to recover: %v", err)
	}
}

func TestReplaceOnWindowsWorksWhileAnEarlierCopyIsStillAside(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the move-aside swap only runs on Windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "hat.exe")
	_ = os.WriteFile(exe, []byte("v2"), 0o755)
	stillRunning, err := os.Create(exe + ".moved-aside-by-hat-1")
	if err != nil {
		t.Fatal(err)
	}
	defer stillRunning.Close()

	if err := (swapper{rename: os.Rename}).replace(exe, []byte("v3")); err != nil {
		t.Fatalf("a copy still in use blocked the upgrade: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "v3" {
		t.Errorf("program = %q", got)
	}
}

func TestRunNeverDowngradesANewerBuild(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "version 0.3.0")

	result, err := Run(releases, "0.3.0", exe)

	if err != nil || result.Outcome != AheadOfNewest || releases.fetches != 0 {
		t.Errorf("result=%+v err=%v fetches=%d", result, err, releases.fetches)
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.3.0" {
		t.Errorf("program changed to %q", got)
	}
}

func TestRunReplacesADevelopmentBuild(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	exe := installedProgram(t, "dev build")

	result, err := Run(releases, "dev", exe)

	if err != nil || result.Outcome != Installed {
		t.Errorf("result=%+v err=%v", result, err)
	}
}

func TestCompareVersionsOrdersLikeSemver(t *testing.T) {
	cases := []struct {
		current, latest string
		sign            int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"dev", "0.1.0", -1},
		{"0.0.0-test", "0.0.0-test", 0},
		{"1.0.0-rc.9", "1.0.0-rc.10", -1},
		{"1.0.0-rc.10", "1.0.0-rc.9", 1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
	}
	for _, c := range cases {
		got, err := compareVersions(c.current, c.latest)
		if err != nil || sign(got) != c.sign {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want sign %d", c.current, c.latest, got, err, c.sign)
		}
	}
	if _, err := compareVersions("0.1.0", "nightly"); err == nil {
		t.Error("accepted a release tag that isn't a version")
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func repoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEveryPlaceAgreesOnTheArchiveName(t *testing.T) {
	goreleaser := repoFile(t, ".goreleaser.yaml")
	template := `name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`
	if !strings.Contains(goreleaser, template) || !strings.Contains(goreleaser, "project_name: hat") {
		t.Errorf(".goreleaser.yaml no longer names archives %s", template)
	}
	if want := "hat_1.2.3_linux_amd64.tar.gz"; assetName("1.2.3", "linux", "amd64") != want {
		t.Errorf("assetName for linux = %s, want %s", assetName("1.2.3", "linux", "amd64"), want)
	}
	if want := "hat_1.2.3_windows_arm64.zip"; assetName("1.2.3", "windows", "arm64") != want {
		t.Errorf("assetName for windows = %s, want %s", assetName("1.2.3", "windows", "arm64"), want)
	}
	if !strings.Contains(repoFile(t, "install.sh"), `asset="hat_${version}_linux_${arch}.tar.gz"`) {
		t.Error("install.sh builds a different archive name")
	}
	if !strings.Contains(repoFile(t, "install.ps1"), `$asset = "hat_${version}_windows_$arch.zip"`) {
		t.Error("install.ps1 builds a different archive name")
	}
}

func TestInstallPs1AndUpgradeAgreeOnTheMovedAsideName(t *testing.T) {
	installer := repoFile(t, "install.ps1")
	builds := `$movedAside = "$target` + movedAsideMarker + `" + [DateTime]::UtcNow.Ticks`
	filter := `-Filter 'hat.exe` + movedAsideMarker + `*'`
	pattern := `^` + regexp.QuoteMeta("hat.exe"+movedAsideMarker) + `\d+$`
	for _, want := range []string{builds, filter, `-match '` + pattern + `'`} {
		if !strings.Contains(installer, want) {
			t.Errorf("install.ps1 no longer contains %s", want)
		}
	}
	if upgradeName := filepath.Base(movedAsidePath("hat.exe")); !regexp.MustCompile(pattern).MatchString(upgradeName) {
		t.Errorf("install.ps1 wouldn't clean up %s, which upgrade creates", upgradeName)
	}
	if installerName := "hat.exe" + movedAsideMarker + "638675712000000000"; !isMovedAside("hat.exe", installerName) {
		t.Errorf("upgrade wouldn't clean up %s, which install.ps1 creates", installerName)
	}
}

func TestRunKeepsTheOldProgramWhenTheChecksumIsWrong(t *testing.T) {
	releases := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	releases.files["checksums.txt"] = []byte(strings.Repeat("0", 64) + "  " + assetName("0.2.0", runtime.GOOS, runtime.GOARCH) + "\n")
	exe := installedProgram(t, "version 0.1.0")

	_, err := Run(releases, "0.1.0", exe)

	if err == nil {
		t.Fatal("installed a release whose checksum didn't match")
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.1.0" {
		t.Errorf("program changed to %q", got)
	}
}

// githubLike serves a release the way github.com does: /releases/latest
// redirects to the tag's page, and each download redirects to file storage.
func githubLike(t *testing.T, release *fakeReleases) GitHubReleases {
	t.Helper()
	const releases = "/yavuz-ozkan/hat/releases"
	mux := http.NewServeMux()
	mux.HandleFunc(releases+"/latest", func(w http.ResponseWriter, r *http.Request) {
		if release.tag == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, releases+"/tag/"+release.tag, http.StatusFound)
	})
	mux.HandleFunc(releases+"/tag/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("LatestTag followed the redirect to %s", r.URL.Path)
	})
	mux.HandleFunc(releases+"/download/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, releases+"/download/"+release.tag+"/") {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/storage/"+filepath.Base(r.URL.Path), http.StatusFound)
	})
	mux.HandleFunc("/storage/", func(w http.ResponseWriter, r *http.Request) {
		data, ok := release.files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return GitHubReleases{ReleasesURL: srv.URL + releases, Client: srv.Client()}
}

func TestGitHubReleasesInstallsThroughRedirects(t *testing.T) {
	source := githubLike(t, publishedRelease(t, "0.2.0", []byte("version 0.2.0")))
	exe := installedProgram(t, "version 0.1.0")

	result, err := Run(source, "0.1.0", exe)

	if err != nil || result.Outcome != Installed || result.To != "0.2.0" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "version 0.2.0" {
		t.Errorf("program = %q", got)
	}
}

func TestGitHubReleasesReportsAMissingBuild(t *testing.T) {
	release := publishedRelease(t, "0.2.0", []byte("version 0.2.0"))
	delete(release.files, assetName("0.2.0", runtime.GOOS, runtime.GOARCH))
	exe := installedProgram(t, "version 0.1.0")

	_, err := Run(githubLike(t, release), "0.1.0", exe)

	if err == nil || !strings.Contains(err.Error(), "has no build for") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubReleasesWithNoReleaseYet(t *testing.T) {
	source := githubLike(t, &fakeReleases{})

	_, err := source.LatestTag()

	if err == nil || !strings.Contains(err.Error(), "no published release") {
		t.Errorf("err = %v", err)
	}
}

func latestAnsweredWith(t *testing.T, answer http.HandlerFunc) GitHubReleases {
	t.Helper()
	srv := httptest.NewServer(answer)
	t.Cleanup(srv.Close)
	return GitHubReleases{ReleasesURL: srv.URL + "/yavuz-ozkan/hat/releases", Client: srv.Client()}
}

func TestGitHubReleasesSaysToTryAgainWhenGitHubFails(t *testing.T) {
	source := latestAnsweredWith(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := source.LatestTag()

	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || !strings.Contains(err.Error(), "again later") || strings.Contains(err.Error(), "no published release") {
		t.Errorf("err = %v", err)
	}
}

// What github.com does while a repository has only pre-releases.
func TestGitHubReleasesWithOnlyPreReleases(t *testing.T) {
	source := latestAnsweredWith(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/yavuz-ozkan/hat/releases", http.StatusFound)
	})

	_, err := source.LatestTag()

	if err == nil || !strings.Contains(err.Error(), "no published release") || strings.Contains(err.Error(), "moved") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubReleasesSaysTheRepositoryMayHaveMoved(t *testing.T) {
	source := latestAnsweredWith(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/someone-else/hat/releases/latest", http.StatusMovedPermanently)
	})

	_, err := source.LatestTag()

	if err == nil || !strings.Contains(err.Error(), "may have moved") || !strings.Contains(err.Error(), "/someone-else/hat/releases/latest") {
		t.Errorf("err = %v", err)
	}
}
