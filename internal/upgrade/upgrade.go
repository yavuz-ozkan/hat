package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

func assetName(version, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("hat_%s_%s_%s.%s", version, goos, goarch, ext)
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "hat.exe"
	}
	return "hat"
}

type Outcome int

const (
	Installed Outcome = iota
	AlreadyNewest
	AheadOfNewest
)

type Result struct {
	From, To   string
	Outcome    Outcome
	Provenance Provenance
}

// Run installs the newest release over exePath when it's newer than the
// running version. A development build (no version number) counts as older.
func Run(src Source, current, exePath string) (Result, error) {
	unlock, err := lockUpgrade(exePath)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	tag, err := src.LatestTag()
	if err != nil {
		return Result{}, err
	}
	latest := strings.TrimPrefix(tag, "v")
	switch order, err := compareVersions(current, latest); {
	case err != nil:
		return Result{}, fmt.Errorf("the newest release is tagged %q, which isn't a version number hat understands", tag)
	case order == 0:
		return Result{From: current, To: latest, Outcome: AlreadyNewest}, nil
	case order > 0:
		return Result{From: current, To: latest, Outcome: AheadOfNewest}, nil
	}

	checksums, err := src.Fetch(tag, "checksums.txt")
	if errors.Is(err, ErrNotPublished) {
		return Result{}, fmt.Errorf("release %s has no checksums.txt, so hat can't check the download and won't install it", tag)
	}
	if err != nil {
		return Result{}, err
	}
	asset := assetName(latest, runtime.GOOS, runtime.GOARCH)
	archive, err := src.Fetch(tag, asset)
	if errors.Is(err, ErrNotPublished) {
		return Result{}, fmt.Errorf("release %s has no build for %s/%s", tag, runtime.GOOS, runtime.GOARCH)
	}
	if err != nil {
		return Result{}, err
	}
	if err := verify(asset, archive, checksums); err != nil {
		return Result{}, err
	}
	provenance, err := src.CheckProvenance(tag, asset, archive)
	if err != nil {
		return Result{}, err
	}
	binary, err := extractBinary(asset, archive, binaryName(runtime.GOOS))
	if err != nil {
		return Result{}, err
	}
	if err := (swapper{rename: os.Rename}).replace(exePath, binary); err != nil {
		return Result{}, err
	}
	return Result{From: current, To: latest, Outcome: Installed, Provenance: provenance}, nil
}

func compareVersions(current, latest string) (int, error) {
	if !semver.IsValid("v" + latest) {
		return 0, fmt.Errorf("not a version: %q", latest)
	}
	if !semver.IsValid("v" + current) {
		return -1, nil
	}
	return semver.Compare("v"+current, "v"+latest), nil
}

func verify(name string, archive, checksums []byte) error {
	want, err := checksumFor(checksums, name)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("%s doesn't match its published checksum (got %s, want %s), so it wasn't installed. Run hat upgrade again; if it keeps failing, the release is damaged", name, got, want)
	}
	return nil
}

func checksumFor(checksums []byte, name string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("the release's checksums.txt has no entry for %s, so it wasn't installed", name)
}

func extractBinary(archiveName string, archive []byte, name string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return fromZip(archiveName, archive, name)
	}
	return fromTarGz(archiveName, archive, name)
}

func fromZip(archiveName string, archive []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s has no %s inside", archiveName, name)
}

func fromTarGz(archiveName string, archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s has no %s inside", archiveName, name)
		}
		if err != nil {
			return nil, err
		}
		if header.Name == name {
			return io.ReadAll(tr)
		}
	}
}
