#!/bin/sh
# Runs install.sh and hat upgrade against fake releases served from a
# GoReleaser snapshot (internal/fakereleases), and checks that each installs
# the good release and refuses the bad ones without touching the hat already
# there. From the repository root:
#
#   goreleaser release --snapshot --clean
#   sh .github/scripts/test-installers.sh [--real-release]
#
# gh is hidden from PATH, so the result doesn't depend on whether this
# machine has it. --real-release also installs v0.1.1 from github.com with gh
# hidden, with stand-in gh programs, and with this machine's own gh.
set -eu

real_release=no
case "${1:-}" in
"") ;;
--real-release) real_release=yes ;;
*)
	echo "usage: sh .github/scripts/test-installers.sh [--real-release]" >&2
	exit 2
	;;
esac
unset HAT_VERSION HAT_RELEASES_URL HAT_BIN_DIR

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "There's no hat build for a $(uname -m) processor to test." >&2
	exit 2
	;;
esac
set -- dist/hat_*_linux_"$arch".tar.gz
if [ ! -f "$1" ]; then
	echo "dist/ has no linux/$arch snapshot. Build one first with goreleaser release --snapshot --clean." >&2
	exit 2
fi
version=${1#dist/hat_}
version=${version%_linux_"$arch".tar.gz}

work=$(mktemp -d)
server=
cleanup() {
	if [ -n "$server" ]; then kill "$server" 2>/dev/null || true; fi
	rm -rf "$work"
}
trap cleanup EXIT

go build -o "$work/fakereleases" ./internal/fakereleases
go build -ldflags "-X main.version=0.0.1" -o "$work/old-hat" .
printf 'previous hat\n' >"$work/previous"

"$work/fakereleases" -dist dist >"$work/server.out" 2>"$work/server.err" &
server=$!
tries=0
until base=$(head -n 1 "$work/server.out") && [ -n "$base" ]; do
	tries=$((tries + 1))
	if [ "$tries" -gt 100 ]; then
		echo "The fake releases server didn't start:" >&2
		cat "$work/server.err" >&2
		exit 1
	fi
	sleep 0.1
done

# gh sits in /usr/bin on GitHub's runners, beside tools the installer needs,
# so each folder holding gh is swapped for one linking everything else in it.
no_gh_path=
shadows=0
saved_ifs=$IFS
IFS=:
for dir in $PATH; do
	if [ -n "$dir" ] && [ -e "$dir/gh" ]; then
		shadows=$((shadows + 1))
		shadow=$work/no-gh-$shadows
		mkdir "$shadow"
		for entry in "$dir"/*; do
			[ "${entry##*/}" = gh ] || ln -s "$entry" "$shadow/"
		done
		dir=$shadow
	fi
	no_gh_path=${no_gh_path:+$no_gh_path:}$dir
done
IFS=$saved_ifs
if PATH=$no_gh_path command -v gh >/dev/null 2>&1; then
	echo "Couldn't hide gh from PATH." >&2
	exit 1
fi

passed=0
failed=0
check() {
	name=$1
	shift
	if "$@"; then
		passed=$((passed + 1))
		echo "PASS  $name"
	else
		failed=$((failed + 1))
		echo "FAIL  $name (exit $code). Output:"
		sed 's/^/      /' "$work/out"
	fi
}
run() { "$@" >"$work/out" 2>&1 && code=0 || code=$?; }
said() { grep -qF -- "$1" "$work/out"; }
previous_hat() {
	bin=$work/$1
	mkdir -p "$bin"
	cp "$work/previous" "$bin/hat"
	chmod 755 "$bin/hat"
}
old_hat() {
	bin=$work/$1
	mkdir -p "$bin"
	cp "$work/old-hat" "$bin/hat"
}
installed() { [ "$code" = 0 ] && said "Installed hat $1 to $bin/hat." && [ "$("$bin/hat" version)" = "hat $1" ]; }
upgraded() { [ "$code" = 0 ] && said "Upgraded hat from 0.0.1 to $1." && [ "$("$bin/hat" version)" = "hat $1" ]; }
installed_saying() { installed "$1" && said "$2"; }
upgraded_saying() { upgraded "$1" && said "$2"; }
refused() { [ "$code" != 0 ] && said "$1" && cmp -s "$work/previous" "$bin/hat"; }
upgrade_refused() { [ "$code" != 0 ] && said "$1" && cmp -s "$work/old-hat" "$bin/hat"; }

install_fake() {
	previous_hat "install-$1"
	run env PATH="$no_gh_path" HAT_RELEASES_URL="$base/$1/releases" HAT_BIN_DIR="$bin" sh ./install.sh
}
upgrade_fake() {
	old_hat "upgrade-$1"
	run env PATH="$no_gh_path" HAT_RELEASES_URL="$base/$1/releases" "$bin/hat" upgrade
}
off_github="Build provenance not checked: HAT_RELEASES_URL isn't a github.com address"
no_release="no published release"

install_fake release
check "install.sh replaces the previous hat with $version" installed_saying "$version" "$off_github"
install_fake tampered
check "install.sh refuses a tampered archive and keeps the previous hat" refused "doesn't match its published checksum"
install_fake no-checksum-line
check "install.sh refuses an archive checksums.txt doesn't list and keeps the previous hat" refused "doesn't match its published checksum"
install_fake prereleases-only
check "install.sh says there's no published release when all are pre-releases" refused "$no_release"
install_fake no-releases
check "install.sh says there's no published release when there are none" refused "$no_release"

upgrade_fake release
check "hat upgrade replaces 0.0.1 with $version" upgraded_saying "$version" "$off_github"
upgrade_fake tampered
check "hat upgrade refuses a tampered archive and keeps the old hat" upgrade_refused "doesn't match its published checksum"
upgrade_fake no-checksum-line
check "hat upgrade refuses an archive checksums.txt doesn't list and keeps the old hat" upgrade_refused "has no entry for"
upgrade_fake prereleases-only
check "hat upgrade says there's no published release when all are pre-releases" upgrade_refused "$no_release"
upgrade_fake no-releases
check "hat upgrade says there's no published release when there are none" upgrade_refused "$no_release"

if [ "$real_release" = yes ]; then
	real_gh=$(command -v gh || true)
	if [ -z "$real_gh" ]; then
		echo "--real-release needs gh on PATH. Install it from https://cli.github.com, then run this again." >&2
		exit 2
	fi
	# What gh prints for an archive GitHub has no attestation for, and what gh
	# before 2.49 prints for any attestation command.
	mkdir "$work/failing-gh" "$work/old-gh"
	printf '#!/bin/sh\necho "Error: HTTP 404: Not Found (stand-in gh from test-installers.sh)" >&2\nexit 1\n' >"$work/failing-gh/gh"
	printf '#!/bin/sh\necho "unknown command \\"attestation\\" for \\"gh\\"" >&2\nexit 1\n' >"$work/old-gh/gh"
	chmod 755 "$work/failing-gh/gh" "$work/old-gh/gh"
	if gh auth status >/dev/null 2>&1; then
		real_gh_says="gh verified its build provenance: it was built by yavuz-ozkan/hat/.github/workflows/release.yml."
	else
		real_gh_says="Build provenance not checked: gh isn't signed in. To check it next time, run gh auth login."
	fi
	install_real() {
		previous_hat "real-$1"
		run env PATH="$2" HAT_VERSION=0.1.1 HAT_BIN_DIR="$bin" sh ./install.sh
	}
	upgrade_real() {
		old_hat "real-upgrade-$1"
		run env PATH="$2" "$bin/hat" upgrade
	}
	upgraded_to_newest_saying() { [ "$code" = 0 ] && said "Upgraded hat from 0.0.1 to " && said "$1"; }

	install_real no-gh "$no_gh_path"
	check "install.sh installs v0.1.1 from GitHub without gh, saying how to check provenance" installed_saying 0.1.1 "Build provenance not checked: the GitHub CLI isn't installed."
	install_real failing-gh "$work/failing-gh:$no_gh_path"
	check "install.sh refuses v0.1.1 when gh can't verify it and keeps the previous hat" refused "gh couldn't verify it was built by"
	install_real old-gh "$work/old-gh:$no_gh_path"
	check "install.sh installs v0.1.1 with a gh too old to verify, saying so" installed_saying 0.1.1 "this gh is too old to verify attestations"
	install_real gh "$PATH"
	check "install.sh installs v0.1.1 with this machine's gh: $real_gh_says" installed_saying 0.1.1 "$real_gh_says"
	upgrade_real gh "$PATH"
	check "hat upgrade installs the newest GitHub release with this machine's gh" upgraded_to_newest_saying "$real_gh_says"
	upgrade_real failing-gh "$work/failing-gh:$no_gh_path"
	check "hat upgrade refuses the newest GitHub release when gh can't verify it and keeps the old hat" upgrade_refused "gh couldn't verify it was built by"
fi

echo "$passed passed, $failed failed."
[ "$failed" = 0 ]
