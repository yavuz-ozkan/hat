// Command fakereleases serves a GoReleaser dist folder the way github.com
// serves hat's releases, so CI can run both installers and hat upgrade
// against a release nobody published. hat never imports it, so it isn't in
// the shipped binary.
//
// Each scenario has its own releases URL, http://<host>/<scenario>/releases:
//
//	release           the dist folder as the newest published release
//	tampered          the same, with every archive altered after checksums.txt
//	no-checksum-line  checksums.txt lists none of the archives
//	prereleases-only  /latest redirects to the releases list, as GitHub does
//	                  when every release is a pre-release
//	no-releases       /latest answers 404, as for a repository with none
//
// Usage: fakereleases -dist dist [-listen 127.0.0.1:0]. Once listening, it
// prints its base URL as the first line of stdout.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type latestAnswer int

const (
	latestIsTag latestAnswer = iota
	latestIsReleasesList
	latestIsMissing
)

type scenario struct {
	latest            latestAnswer
	tamperArchives    bool
	dropChecksumLines bool
}

var scenarios = map[string]scenario{
	"release":          {},
	"tampered":         {tamperArchives: true},
	"no-checksum-line": {dropChecksumLines: true},
	"prereleases-only": {latest: latestIsReleasesList},
	"no-releases":      {latest: latestIsMissing},
}

type releases struct {
	dist string
	tag  string
}

func main() {
	dist := flag.String("dist", "dist", "the GoReleaser dist folder to serve")
	listen := flag.String("listen", "127.0.0.1:0", "address to listen on")
	flag.Parse()
	if err := serve(*dist, *listen); err != nil {
		fmt.Fprintf(os.Stderr, "fakereleases: %v\n", err)
		os.Exit(1)
	}
}

func serve(dist, listen string) error {
	tag, err := snapshotTag(dist)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	fmt.Printf("http://%s\n", ln.Addr())
	server := &http.Server{Handler: releases{dist: dist, tag: tag}.routes(), ReadHeaderTimeout: 10 * time.Second}
	return server.Serve(ln)
}

// snapshotTag is the tag a real release of dist would have. A snapshot's
// metadata.json keeps the previous tag in "tag", so it's built from
// "version".
func snapshotTag(dist string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dist, "metadata.json"))
	if err != nil {
		return "", fmt.Errorf("%v. Build one first with goreleaser release --snapshot --clean", err)
	}
	var metadata struct{ Version string }
	if err := json.Unmarshal(data, &metadata); err != nil || metadata.Version == "" {
		return "", fmt.Errorf("%s has no version (%v). Rebuild it with goreleaser release --snapshot --clean", filepath.Join(dist, "metadata.json"), err)
	}
	return "v" + metadata.Version, nil
}

func (rel releases) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{scenario}/releases/latest", rel.latest)
	mux.HandleFunc("GET /{scenario}/releases/download/{tag}/{file}", rel.download)
	mux.HandleFunc("GET /{scenario}/storage/{file}", rel.storage)
	return mux
}

func scenarioOf(w http.ResponseWriter, r *http.Request) (scenario, bool) {
	s, ok := scenarios[r.PathValue("scenario")]
	if !ok {
		http.NotFound(w, r)
	}
	return s, ok
}

// latest redirects to an absolute URL, as GitHub does: the installers
// compare it with the releases URL they were given.
func (rel releases) latest(w http.ResponseWriter, r *http.Request) {
	s, ok := scenarioOf(w, r)
	if !ok {
		return
	}
	list := "http://" + r.Host + "/" + r.PathValue("scenario") + "/releases"
	switch s.latest {
	case latestIsMissing:
		http.NotFound(w, r)
	case latestIsReleasesList:
		http.Redirect(w, r, list, http.StatusFound)
	default:
		http.Redirect(w, r, list+"/tag/"+rel.tag, http.StatusFound)
	}
}

// download redirects to storage, as GitHub sends every download to its file
// storage, so a downloader that doesn't follow redirects fails here too.
func (rel releases) download(w http.ResponseWriter, r *http.Request) {
	if _, ok := scenarioOf(w, r); !ok {
		return
	}
	if r.PathValue("tag") != rel.tag {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/"+r.PathValue("scenario")+"/storage/"+r.PathValue("file"), http.StatusFound)
}

func (rel releases) storage(w http.ResponseWriter, r *http.Request) {
	s, ok := scenarioOf(w, r)
	if !ok {
		return
	}
	name := r.PathValue("file")
	if strings.ContainsAny(name, `/\`) || name == ".." {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(rel.dist, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(s.alter(name, data))
}

func isArchive(name string) bool {
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")
}

func (s scenario) alter(name string, data []byte) []byte {
	switch {
	case s.tamperArchives && isArchive(name):
		tampered := bytes.Clone(data)
		tampered[len(tampered)/2] ^= 0xff
		return tampered
	case s.dropChecksumLines && name == "checksums.txt":
		var kept []string
		for line := range strings.Lines(string(data)) {
			fields := strings.Fields(line)
			if len(fields) != 2 || !isArchive(fields[1]) {
				kept = append(kept, line)
			}
		}
		return []byte(strings.Join(kept, ""))
	}
	return data
}
