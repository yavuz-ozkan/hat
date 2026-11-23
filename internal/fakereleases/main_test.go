package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	archive  = "hat_1.2.3-SNAPSHOT-abc_linux_amd64.tar.gz"
	checksum = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  " + archive + "\n"
)

func fakeDist(t *testing.T) string {
	t.Helper()
	dist := t.TempDir()
	files := map[string]string{
		"metadata.json": `{"tag":"v1.2.2","version":"1.2.3-SNAPSHOT-abc"}`,
		archive:         "archive bytes",
		"checksums.txt": checksum,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dist, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dist
}

func startServer(t *testing.T) *httptest.Server {
	t.Helper()
	dist := fakeDist(t)
	tag, err := snapshotTag(dist)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(releases{dist: dist, tag: tag}.routes())
	t.Cleanup(srv.Close)
	return srv
}

func latestOf(t *testing.T, srv *httptest.Server, scenario string) *http.Response {
	t.Helper()
	noFollow := *srv.Client()
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Head(srv.URL + "/" + scenario + "/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func download(t *testing.T, srv *httptest.Server, scenario, tag, name string) (int, []byte) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/" + scenario + "/releases/download/" + tag + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func TestLatestRedirectsToTheSnapshotsTag(t *testing.T) {
	srv := startServer(t)

	resp := latestOf(t, srv, "release")

	if want := srv.URL + "/release/releases/tag/v1.2.3-SNAPSHOT-abc"; resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
		t.Errorf("HTTP %d to %q, want 302 to %q", resp.StatusCode, resp.Header.Get("Location"), want)
	}
}

func TestLatestWithOnlyPreReleasesRedirectsToTheList(t *testing.T) {
	srv := startServer(t)

	resp := latestOf(t, srv, "prereleases-only")

	if want := srv.URL + "/prereleases-only/releases"; resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
		t.Errorf("HTTP %d to %q, want 302 to %q", resp.StatusCode, resp.Header.Get("Location"), want)
	}
}

func TestLatestWithNoReleasesIsNotFound(t *testing.T) {
	if resp := latestOf(t, startServer(t), "no-releases"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("HTTP %d, want 404", resp.StatusCode)
	}
}

func TestDownloadServesTheDistFileThroughARedirect(t *testing.T) {
	status, body := download(t, startServer(t), "release", "v1.2.3-SNAPSHOT-abc", archive)

	if status != http.StatusOK || string(body) != "archive bytes" {
		t.Errorf("HTTP %d %q", status, body)
	}
}

func TestDownloadOfAnotherTagIsNotFound(t *testing.T) {
	if status, _ := download(t, startServer(t), "release", "v9.9.9", archive); status != http.StatusNotFound {
		t.Errorf("HTTP %d, want 404", status)
	}
}

func TestTamperedServesADifferentArchiveAndTheRealChecksums(t *testing.T) {
	srv := startServer(t)

	_, body := download(t, srv, "tampered", "v1.2.3-SNAPSHOT-abc", archive)
	_, sums := download(t, srv, "tampered", "v1.2.3-SNAPSHOT-abc", "checksums.txt")

	if len(body) != len("archive bytes") || bytes.Equal(body, []byte("archive bytes")) {
		t.Errorf("archive = %q, want the same length with a byte changed", body)
	}
	if string(sums) != checksum {
		t.Errorf("checksums.txt = %q", sums)
	}
}

func TestNoChecksumLineDropsTheArchivesLines(t *testing.T) {
	_, sums := download(t, startServer(t), "no-checksum-line", "v1.2.3-SNAPSHOT-abc", "checksums.txt")

	if len(sums) != 0 {
		t.Errorf("checksums.txt = %q, want no archive lines", sums)
	}
}

func TestUnknownScenarioIsNotFound(t *testing.T) {
	if resp := latestOf(t, startServer(t), "nonsense"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("HTTP %d, want 404", resp.StatusCode)
	}
}

func TestStorageServesOnlyFilesDirectlyInDist(t *testing.T) {
	srv := startServer(t)
	for _, name := range []string{"..%2Fsecret", "..%5Csecret", ".."} {
		resp, err := srv.Client().Get(srv.URL + "/release/storage/" + name)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: HTTP %d, want 404", name, resp.StatusCode)
		}
	}
}
