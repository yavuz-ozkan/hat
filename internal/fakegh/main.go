// Command fakegh stands in for the GitHub CLI in
// .github/scripts/test-installers.*, so CI can drive every provenance outcome
// in both installers and hat upgrade without a signed-in gh. hat never
// imports it, so it isn't in the shipped binary.
//
// FAKEGH picks how it answers:
//
//	verifies    signed in to github.com only; attestation verify passes when
//	            it's asked exactly what hat asks, for the tag the archive's
//	            name carries
//	fails       signed in; attestation verify fails as for an archive GitHub
//	            has no attestation for
//	signed-out  gh auth status fails
//	too-old     signed in; no attestation command, like gh before 2.49
//
// Like gh, it sends a command without --hostname to GH_HOST, or github.com
// when that's unset.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

const (
	repo           = "yavuz-ozkan/hat"
	signerWorkflow = repo + "/.github/workflows/release.yml"
	exitAuth       = 4
)

func main() {
	os.Exit(answer(os.Getenv("FAKEGH"), os.Getenv("GH_HOST"), os.Args[1:], os.Stderr))
}

func answer(mode, ghHost string, args []string, stderr io.Writer) int {
	host := hostOf(ghHost, args)
	switch {
	case slices.Equal(args, []string{"auth", "status", "--hostname", host}) || slices.Equal(args, []string{"auth", "status"}):
		if mode == "signed-out" || host != "github.com" {
			fmt.Fprintf(stderr, "You are not logged into %s. To log in, run: gh auth login --hostname %s\n", host, host)
			return 1
		}
		fmt.Fprintln(stderr, "github.com\n  Logged in to github.com account fakegh (keyring)\n  Token: gho_************************************")
		return 0
	case len(args) >= 2 && args[0] == "attestation" && args[1] == "verify":
		return verify(mode, host, args, stderr)
	}
	fmt.Fprintf(stderr, "fakegh: hat isn't meant to run gh %q\n", args)
	return 1
}

func hostOf(ghHost string, args []string) string {
	if i := slices.Index(args, "--hostname"); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	if ghHost != "" {
		return ghHost
	}
	return "github.com"
}

var archiveVersion = regexp.MustCompile(`^hat_(.+)_(linux|windows)_(amd64|arm64)\.(tar\.gz|zip)$`)

func verify(mode, host string, args []string, stderr io.Writer) int {
	switch {
	case mode == "too-old":
		fmt.Fprintln(stderr, "unknown command \"attestation\" for \"gh\"\n\nUsage:  gh <command> <subcommand> [flags]")
		return 1
	case mode == "signed-out" || host != "github.com":
		fmt.Fprintf(stderr, "To get started with GitHub CLI, please run:  gh auth login --hostname %s\n", host)
		return exitAuth
	case mode == "fails":
		fmt.Fprintln(stderr, "Error: HTTP 404: Not Found (fakegh has no attestation for this archive)")
		return 1
	case mode != "verifies":
		fmt.Fprintf(stderr, "fakegh: FAKEGH=%q isn't one of verifies, fails, signed-out, too-old\n", mode)
		return 1
	}
	archive := args[2]
	version := archiveVersion.FindStringSubmatch(filepath.Base(archive))
	if info, err := os.Stat(archive); err != nil || info.Size() == 0 || version == nil {
		fmt.Fprintf(stderr, "fakegh: %s isn't a hat archive on disk\n", archive)
		return 1
	}
	want := []string{"attestation", "verify", archive, "--hostname", "github.com", "--repo", repo,
		"--signer-workflow", signerWorkflow, "--source-ref", "refs/tags/v" + version[1]}
	if !slices.Equal(args, want) {
		fmt.Fprintf(stderr, "fakegh: asked gh %q, want gh %q\n", args, want)
		return 1
	}
	return 0
}
