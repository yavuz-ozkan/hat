package claude

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const setupURL = "https://code.claude.com/docs/en/setup"

// Find resolves executable to the program hat will run. On Windows it refuses
// a .cmd or .bat, which is what an npm install of Claude Code puts on PATH:
// Windows runs those through cmd.exe, and cmd.exe acts on characters like "&"
// in the arguments hat passes along, such as folder names and your own
// Claude arguments.
func Find(executable string) (string, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", fmt.Errorf("couldn't find Claude Code (%v). Install it from %s, then run hat again", err, setupURL)
	}
	if runtime.GOOS == "windows" && isBatchFile(path) {
		return "", fmt.Errorf("the Claude Code on your PATH is %s, a script that runs through cmd.exe, which could run commands hidden in the arguments hat passes to it. "+
			"hat needs the native Claude Code install: follow %s, then run hat again", path, setupURL)
	}
	return path, nil
}

// isBatchFile looks at the extension Windows acts on, which drops trailing
// dots and spaces: claude.cmd. and "claude.cmd " both run claude.cmd. A
// PATHEXT entry like ".CMD " makes Go's PATH lookup find such a name.
func isBatchFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimRight(path, ". ")))
	return ext == ".cmd" || ext == ".bat"
}
