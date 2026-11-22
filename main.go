package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/yavuz-ozkan/hat/internal/cli"
	"github.com/yavuz-ozkan/hat/internal/layout"
	"github.com/yavuz-ozkan/hat/internal/upgrade"
)

// version is set at release time with -ldflags "-X main.version=1.2.3".
var version string

// versionOf is the version this build reports. A build made with go install
// has no -ldflags, but Go records the module version it fetched.
func versionOf(ldflagsVersion, moduleVersion string) string {
	if ldflagsVersion != "" {
		return ldflagsVersion
	}
	if moduleVersion == "" || moduleVersion == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(moduleVersion, "v")
}

func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

func main() {
	switch cli.HandlingFor(runtime.GOOS, os.Args[1:]) {
	case cli.RefuseUnsupported:
		fmt.Fprintf(os.Stderr, "hat: %s.\n", cli.MacOSUnsupported)
		os.Exit(1)
	case cli.StatusLineOnly:
		os.Exit(cli.StatusLineWithoutRecording(os.Stdin, os.Stdout, os.Stderr, os.Args[2:]))
	}
	l, err := layout.Current()
	if err != nil && len(os.Args) > 1 && os.Args[1] == "statusline" {
		os.Exit(cli.StatusLineWithoutRecording(os.Stdin, os.Stdout, os.Stderr, os.Args[2:]))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "hat: couldn't find your home, settings, or cache folder: %v. Check that HOME (USERPROFILE on Windows) is set\n", err)
		os.Exit(1)
	}
	if exe, err := upgrade.InstalledPath(); err == nil {
		upgrade.RemoveReplacedWindowsBinaries(exe)
	}
	app := cli.App{
		Layout:        l,
		In:            os.Stdin,
		Out:           os.Stdout,
		Err:           os.Stderr,
		Version:       versionOf(version, moduleVersion()),
		Releases:      upgrade.PublicReleases(),
		InstalledPath: upgrade.InstalledPath,
	}
	os.Exit(app.Run(os.Args[1:]))
}
