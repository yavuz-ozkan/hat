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

// Source is where releases come from.
type Source interface {
	LatestTag() (string, error)
	// Fetch returns all of file name from release tag, or an error wrapping
	// ErrNotPublished when the release has no such file.
	Fetch(tag, name string) ([]byte, error)
}

var ErrNotPublished = errors.New("not published in this release")

type GitHubReleases struct {
	ReleasesURL string
	// Client must follow redirects: GitHub answers every download with one
	// to its file storage.
	Client *http.Client
}

func PublicReleases() GitHubReleases {
	return GitHubReleases{
		ReleasesURL: "https://github.com/" + Repo + "/releases",
		Client:      &http.Client{Timeout: 5 * time.Minute},
	}
}

// LatestTag reads the tag from where /releases/latest redirects, which is
// /releases/tag/<tag>, without following it. GitHub skips pre-releases there.
// HEAD matches the installers, which need it (see install.ps1).
func (g GitHubReleases) LatestTag() (string, error) {
	noFollow := *g.Client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Head(g.ReleasesURL + "/latest")
	if err != nil {
		return "", fmt.Errorf("couldn't reach GitHub to look up the newest release (%v). Check your connection, then run hat upgrade again", err)
	}
	defer resp.Body.Close()
	noRelease := fmt.Errorf("GitHub lists no published release at %s yet, so there's nothing to upgrade to", g.ReleasesURL)
	location, err := resp.Location()
	if resp.StatusCode/100 != 3 || err != nil {
		return "", noRelease
	}
	parent, tag := path.Split(strings.TrimSuffix(location.Path, "/"))
	if path.Base(parent) != "tag" || tag == "" {
		return "", noRelease
	}
	return tag, nil
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
