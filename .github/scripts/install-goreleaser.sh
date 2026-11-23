#!/bin/sh
# Puts GoReleaser in the folder named by $1, and fails unless the archive's
# SHA-256 is the one pinned here. The pin is the v2.20.3 release's
# checksums.txt entry for goreleaser_Linux_x86_64.tar.gz; that release's
# build provenance (gh attestation verify --repo goreleaser/goreleaser) names
# the same digest, signed by its release workflow at refs/tags/v2.20.3.
set -eu
version=v2.20.3
archive=goreleaser_Linux_x86_64.tar.gz
sha256=a36d5d7d07a5e538e052cef2a9f93f1cd4af493cf5a3712b9177c1ebe360f0e7

dest=${1:?usage: install-goreleaser.sh <folder>}
mkdir -p "$dest"
curl -fsSL --retry 3 -o "$dest/$archive" "https://github.com/goreleaser/goreleaser/releases/download/$version/$archive"
actual=$(sha256sum "$dest/$archive" | cut -d' ' -f1)
if [ "$actual" != "$sha256" ]; then
	rm "$dest/$archive"
	echo "GoReleaser $version's $archive has SHA-256 $actual, not the pinned $sha256. Don't release with it: find out why the download changed, then update the pin in $0."
	exit 1
fi
tar -xzf "$dest/$archive" -C "$dest" goreleaser
rm "$dest/$archive"
"$dest/goreleaser" --version | grep GitVersion
