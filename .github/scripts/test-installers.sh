#!/bin/sh
# Runs install.sh and hat upgrade against fake releases served from a
# GoReleaser snapshot (internal/fakereleases), then against the real v0.1.1
# on github.com with a stand-in gh (internal/fakegh) for every provenance
# outcome. Each case checks the message, that a good install replaced the
# hat already there, and that a refused one left it byte for byte. From the
# repository root:
#
#   goreleaser release --snapshot --clean
#   sh .github/scripts/test-installers.sh [--own-gh]
#
# The machine's own gh is hidden from PATH, so the result doesn't depend on
# whether it has one or is signed in. --own-gh also installs v0.1.1 and runs
# hat upgrade with this machine's gh, which verifies when it's signed in to
# github.com.
set -eu

own_gh=no
case "${1:-}" in
"") ;;
--own-gh) own_gh=yes ;;
*)
	echo "usage: sh .github/scripts/test-installers.sh [--own-gh]" >&2
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

mkdir "$work/fakegh" "$work/broken-gh"
go build -o "$work/fakereleases" ./internal/fakereleases
go build -o "$work/fakegh/gh" ./internal/fakegh
go build -ldflags "-X main.version=0.0.1" -o "$work/old-hat" .
printf 'previous hat\n' >"$work/previous"
# Found on PATH but can't be started: its interpreter doesn't exist.
printf '#!/nonexistent/interpreter\n' >"$work/broken-gh/gh"
chmod 755 "$work/broken-gh/gh"

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
fake_gh_path=$work/fakegh:$no_gh_path
broken_gh_path=$work/broken-gh:$no_gh_path

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
installed_saying() { installed "$1" && said "$2"; }
upgraded_saying() {
	[ "$code" = 0 ] && said "Upgraded hat from 0.0.1 to $1" && said "$2" &&
		"$bin/hat" version | grep -qF "hat $1"
}
refused() { [ "$code" != 0 ] && said "$1" && cmp -s "$work/previous" "$bin/hat"; }
upgrade_refused() { [ "$code" != 0 ] && said "$1" && cmp -s "$work/old-hat" "$bin/hat"; }

off_github="Build provenance not checked: HAT_RELEASES_URL isn't a github.com address"
no_release="no published release"
not_https="but it must start with https://"
no_gh="Build provenance not checked: the GitHub CLI isn't installed. To check it next time, install gh from https://cli.github.com and run gh auth login."
verified="gh verified its build provenance: it was built by yavuz-ozkan/hat/.github/workflows/release.yml."
signed_out="Build provenance not checked: gh isn't signed in to github.com. To check it next time, run gh auth login --hostname github.com."
too_old="Build provenance not checked: this gh is too old to verify attestations."
cant_verify="gh couldn't verify it was built by yavuz-ozkan/hat/.github/workflows/release.yml from tag v"
cant_start="couldn't start gh to check"

# install_fake <scenario>: install.sh from the fake releases, gh hidden.
install_fake() {
	previous_hat "install-$1"
	run env PATH="$no_gh_path" HAT_RELEASES_URL="$base/$1/releases" HAT_BIN_DIR="$bin" sh ./install.sh
}
upgrade_fake() {
	old_hat "upgrade-$1"
	run env PATH="$no_gh_path" HAT_RELEASES_URL="$base/$1/releases" "$bin/hat" upgrade
}

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
previous_hat install-plain-http
run env PATH="$no_gh_path" HAT_RELEASES_URL=http://mirror.example/releases HAT_BIN_DIR="$bin" sh ./install.sh
check "install.sh refuses a plain http:// mirror, suggesting https://" refused "$not_https"

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
old_hat upgrade-plain-http
run env PATH="$no_gh_path" HAT_RELEASES_URL=http://mirror.example/releases "$bin/hat" upgrade
check "hat upgrade refuses a plain http:// mirror, suggesting https://" upgrade_refused "$not_https"

# install_real <name> <PATH> <FAKEGH> <GH_HOST>: install.sh of v0.1.1 from
# github.com. An empty GH_HOST is the same as none to fakegh.
install_real() {
	previous_hat "real-$1"
	run env PATH="$2" FAKEGH="$3" GH_HOST="$4" HAT_VERSION=0.1.1 HAT_BIN_DIR="$bin" sh ./install.sh
}
upgrade_real() {
	old_hat "real-upgrade-$1"
	run env PATH="$2" FAKEGH="$3" GH_HOST="$4" "$bin/hat" upgrade
}

install_real no-gh "$no_gh_path" "" ""
check "install.sh installs v0.1.1 without gh, saying how to check provenance" installed_saying 0.1.1 "$no_gh"
install_real verifies "$fake_gh_path" verifies ""
check "install.sh installs v0.1.1 when gh verifies it" installed_saying 0.1.1 "$verified"
install_real enterprise "$fake_gh_path" verifies ghe.example.com
check "install.sh still asks github.com when GH_HOST is an enterprise host" installed_saying 0.1.1 "$verified"
install_real fails "$fake_gh_path" fails ""
check "install.sh refuses v0.1.1 when gh can't verify it and keeps the previous hat" refused "$cant_verify"
install_real signed-out "$fake_gh_path" signed-out ""
check "install.sh installs v0.1.1 when gh isn't signed in to github.com, saying how to sign in" installed_saying 0.1.1 "$signed_out"
install_real too-old "$fake_gh_path" too-old ""
check "install.sh installs v0.1.1 with a gh too old to verify, saying so" installed_saying 0.1.1 "$too_old"
install_real cant-start "$broken_gh_path" "" ""
check "install.sh refuses v0.1.1 when gh can't start and keeps the previous hat" refused "$cant_start"

upgrade_real no-gh "$no_gh_path" "" ""
check "hat upgrade installs the newest release without gh, saying how to check provenance" upgraded_saying "" "$no_gh"
upgrade_real verifies "$fake_gh_path" verifies ""
check "hat upgrade installs the newest release when gh verifies it" upgraded_saying "" "$verified"
upgrade_real enterprise "$fake_gh_path" verifies ghe.example.com
check "hat upgrade still asks github.com when GH_HOST is an enterprise host" upgraded_saying "" "$verified"
upgrade_real fails "$fake_gh_path" fails ""
check "hat upgrade refuses the newest release when gh can't verify it and keeps the old hat" upgrade_refused "$cant_verify"
upgrade_real signed-out "$fake_gh_path" signed-out ""
check "hat upgrade installs the newest release when gh isn't signed in to github.com, saying how to sign in" upgraded_saying "" "$signed_out"
upgrade_real too-old "$fake_gh_path" too-old ""
check "hat upgrade installs the newest release with a gh too old to verify, saying so" upgraded_saying "" "$too_old"
upgrade_real cant-start "$broken_gh_path" "" ""
check "hat upgrade refuses the newest release when gh can't start and keeps the old hat" upgrade_refused "$cant_start"

if [ "$own_gh" = yes ]; then
	if ! command -v gh >/dev/null 2>&1; then
		echo "--own-gh needs gh on PATH. Install it from https://cli.github.com, then run this again." >&2
		exit 2
	fi
	if gh auth status --hostname github.com >/dev/null 2>&1; then own_gh_says=$verified; else own_gh_says=$signed_out; fi
	previous_hat own-gh
	run env HAT_VERSION=0.1.1 HAT_BIN_DIR="$bin" sh ./install.sh
	check "install.sh installs v0.1.1 with this machine's gh: $own_gh_says" installed_saying 0.1.1 "$own_gh_says"
	old_hat own-gh-upgrade
	run "$bin/hat" upgrade
	check "hat upgrade installs the newest release with this machine's gh" upgraded_saying "" "$own_gh_says"
fi

echo "$passed passed, $failed failed."
[ "$failed" = 0 ]
