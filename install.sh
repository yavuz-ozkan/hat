#!/bin/sh
# Installs hat into ~/.local/bin. Run it with:
#   curl -fsSL https://raw.githubusercontent.com/yavuz-ozkan/hat/main/install.sh | sh
#
# Everything is inside main, called on the last line, so a download cut off
# partway runs nothing instead of half a script.

main() {
	set -eu

	repo=yavuz-ozkan/hat
	releases="https://github.com/$repo/releases"
	bin_dir=${HAT_BIN_DIR:-$HOME/.local/bin}

	fail() {
		echo "hat install: $1" >&2
		exit 1
	}

	[ "$(uname -s)" = Linux ] || fail "this script is for Linux. On Windows, run install.ps1 in PowerShell."
	for tool in curl tar sha256sum awk; do
		command -v "$tool" >/dev/null 2>&1 || fail "this needs $tool. Install it with your package manager, then run this again."
	done
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "there's no hat build for a $(uname -m) processor." ;;
	esac

	if [ -n "${HAT_VERSION:-}" ]; then
		tag="v${HAT_VERSION#v}"
	else
		# /releases/latest redirects to /releases/tag/<tag>; reading where it
		# points needs no GitHub API token. No -f: a 404 means no release yet,
		# which the checks below explain better than curl would.
		answer=$(curl -sS -I -o /dev/null -w '%{http_code} %{redirect_url}' "$releases/latest") ||
			fail "couldn't reach GitHub to look up the newest release. Check your connection, then run this again."
		status=${answer%% *}
		latest=${answer#* }
		case "$status" in
		404) fail "GitHub lists no published release at $releases yet." ;;
		3??) ;;
		*) fail "GitHub answered HTTP $status when looking up the newest release. Run this again later." ;;
		esac
		tag=${latest##*/}
		# With only pre-releases published, GitHub redirects to the releases
		# list instead of answering 404.
		case "$latest" in
		*/releases/tag/"$tag") ;;
		"$releases" | "$releases/") fail "GitHub lists no published release at $releases yet." ;;
		*) fail "GitHub sent the newest-release lookup to '$latest', which isn't a release page, so the repository may have moved. Find where hat lives now and install from there." ;;
		esac
	fi
	version=${tag#v}
	asset="hat_${version}_linux_${arch}.tar.gz"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	curl -fsSL -o "$tmp/checksums.txt" "$releases/download/$tag/checksums.txt" ||
		fail "couldn't download checksums.txt for release $tag. Check that $releases/tag/$tag exists."
	curl -fsSL -o "$tmp/$asset" "$releases/download/$tag/$asset" ||
		fail "couldn't download $asset. Release $tag may have no build for linux/$arch."
	(cd "$tmp" && awk -v name="$asset" '$2 == name' checksums.txt | sha256sum -c - >/dev/null 2>&1) ||
		fail "$asset doesn't match its published checksum, so nothing was installed. Run this again; if it keeps failing, the release is damaged."
	tar -xzf "$tmp/$asset" -C "$tmp" hat

	mkdir -p "$bin_dir"
	staged=$(mktemp "$bin_dir/.hat.XXXXXX")
	if ! install -m 755 "$tmp/hat" "$staged" || ! mv -f "$staged" "$bin_dir/hat"; then
		rm -f "$staged"
		fail "couldn't put the new hat in $bin_dir. Any hat already there is unchanged."
	fi
	echo "Installed hat $version to $bin_dir/hat."
	case ":$PATH:" in
	*":$bin_dir:"*) ;;
	*)
		echo "$bin_dir isn't on your PATH yet. Add this line to ~/.profile (or your shell's startup file), then open a new terminal:"
		echo "  export PATH=\"$bin_dir:\$PATH\""
		;;
	esac
}

main "$@"
