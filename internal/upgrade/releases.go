package upgrade

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const Repo = "yavuz-ozkan/hat"

const DefaultReleasesURL = "https://github.com/" + Repo + "/releases"

// Source is where releases come from.
type Source interface {
	LatestTag() (string, error)
	// Fetch returns all of file name from release tag, or an error wrapping
	// ErrNotPublished when the release has no such file.
	Fetch(tag, name string) ([]byte, error)
	// CheckProvenance says whether archive, already matched against
	// checksums.txt, was built by hat's release workflow from tag. An error
	// means the check failed and the archive must not be installed.
	CheckProvenance(tag, asset string, archive []byte) (Provenance, error)
}

var ErrNotPublished = errors.New("not published in this release")

type GitHubReleases struct {
	ReleasesURL string
	// Client must follow redirects: GitHub answers every download with one
	// to its file storage.
	Client *http.Client
	GH     RunGH
}

// checkReleasesURL refuses a releases URL that a network could alter on the
// way: anything but https://, except plain http:// to this machine, which is
// what a test server needs. The installers apply the same rule to the same
// text, so it's matched as text rather than parsed.
func checkReleasesURL(releasesURL string) error {
	if strings.HasPrefix(releasesURL, "https://") {
		return nil
	}
	if authority, ok := strings.CutPrefix(releasesURL, "http://"); ok {
		authority, _, _ = strings.Cut(authority, "/")
		for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
			if !strings.Contains(authority, "@") && (authority == host || strings.HasPrefix(authority, host+":")) {
				return nil
			}
		}
	}
	rest := releasesURL
	if _, after, found := strings.Cut(releasesURL, "://"); found {
		rest = after
	}
	return fmt.Errorf("HAT_RELEASES_URL is %q, but it must start with https://, so nothing on the network can change what hat downloads; plain http:// works only for this machine (127.0.0.1, ::1, or localhost). For example: HAT_RELEASES_URL=https://%s", releasesURL, rest)
}

// ReleasesAt reads releases from releasesURL: DefaultReleasesURL, unless
// HAT_RELEASES_URL points at a mirror or a test server.
func ReleasesAt(releasesURL string) GitHubReleases {
	return GitHubReleases{
		ReleasesURL: strings.TrimSuffix(releasesURL, "/"),
		Client:      &http.Client{Timeout: 5 * time.Minute},
		GH:          GHOnPath,
	}
}

// LatestTag reads the tag from where /releases/latest redirects, which is
// /releases/tag/<tag>, without following it. GitHub skips pre-releases there.
// HEAD matches the installers, which need it (see install.ps1).
func (g GitHubReleases) LatestTag() (string, error) {
	if err := checkReleasesURL(g.ReleasesURL); err != nil {
		return "", err
	}
	noFollow := *g.Client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Head(g.ReleasesURL + "/latest")
	if err != nil {
		return "", fmt.Errorf("couldn't reach GitHub to look up the newest release (%v). Check your connection, then run hat upgrade again", err)
	}
	defer resp.Body.Close()
	noRelease := fmt.Errorf("GitHub lists no published release at %s yet, so there's nothing to upgrade to", g.ReleasesURL)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", noRelease
	case resp.StatusCode/100 != 3:
		return "", fmt.Errorf("GitHub answered HTTP %d when hat looked up the newest release. Run hat upgrade again later", resp.StatusCode)
	}
	if location, err := resp.Location(); err == nil {
		if tag, ok := tagOfReleasePage(location.Path); ok {
			return tag, nil
		}
		// With only pre-releases published, GitHub redirects /releases/latest
		// to the releases list instead of answering 404.
		if strings.TrimSuffix(location.String(), "/") == strings.TrimSuffix(g.ReleasesURL, "/") {
			return "", noRelease
		}
	}
	return "", fmt.Errorf("GitHub sent the newest-release lookup to %q, which isn't a release page, so the repository may have moved. Find where hat lives now and reinstall from there", resp.Header.Get("Location"))
}

func tagOfReleasePage(urlPath string) (string, bool) {
	parent, tag := path.Split(strings.TrimSuffix(urlPath, "/"))
	if !strings.HasSuffix(parent, "/releases/tag/") || tag == "" {
		return "", false
	}
	return tag, true
}

func (g GitHubReleases) Fetch(tag, name string) ([]byte, error) {
	resp, err := g.Client.Get(g.ReleasesURL + "/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name))
	if err != nil {
		return nil, fmt.Errorf("couldn't download %s from release %s (%v). Check your connection, then run hat upgrade again", name, tag, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s: %w", name, ErrNotPublished)
	default:
		return nil, fmt.Errorf("downloading %s from release %s failed with HTTP %d. Run hat upgrade again later", name, tag, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("the download of %s from release %s broke off (%v). Run hat upgrade again", name, tag, err)
	}
	return data, nil
}
