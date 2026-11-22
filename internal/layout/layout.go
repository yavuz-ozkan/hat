package layout

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const AccountDirPrefix = ".claude-"

type Layout struct {
	Home    string
	MainDir string
	// MainStateFile is the main account's state file. It sits beside MainDir,
	// not inside it, because the main account runs with CLAUDE_CONFIG_DIR unset.
	MainStateFile string
	ConfigFile    string
	UsageDir      string
}

type UserDirs struct {
	Config string
	Cache  string
}

func ForHome(home string, dirs UserDirs) Layout {
	return Layout{
		Home:          home,
		MainDir:       filepath.Join(home, ".claude"),
		MainStateFile: filepath.Join(home, ".claude.json"),
		ConfigFile:    filepath.Join(dirs.Config, "hat", "config.json"),
		UsageDir:      filepath.Join(dirs.Cache, "hat", "usage"),
	}
}

func Current() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Layout{}, err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return Layout{}, err
	}
	return ForHome(home, UserDirs{Config: configDir, Cache: cacheDir}), nil
}

func (l Layout) AccountDir(name string) string {
	return filepath.Join(l.Home, AccountDirPrefix+name)
}

// UsageFile is where the plan limits seen by the account in configDir are
// recorded, for the folders "hat usage" lists: the main one and each
// ~/.claude-<name>. For any other folder listed is false and nothing is
// recorded, since nothing would ever show it. An empty configDir means the
// main account, matching how Claude Code treats an unset CLAUDE_CONFIG_DIR.
func (l Layout) UsageFile(configDir string) (path string, listed bool) {
	if configDir == "" {
		configDir = l.MainDir
	}
	dir := filepath.Clean(configDir)
	base := foldCase(filepath.Base(dir))
	switch {
	case samePath(dir, l.MainDir):
		return filepath.Join(l.UsageDir, "main.json"), true
	case samePath(filepath.Dir(dir), l.Home) && strings.HasPrefix(base, AccountDirPrefix) && base != AccountDirPrefix && isPlainName(base):
		return filepath.Join(l.UsageDir, strings.TrimPrefix(base, ".")+".json"), true
	default:
		return "", false
	}
}

// isPlainName is whether Windows opens the folder by exactly this name. It
// drops trailing dots and spaces, so ".claude-work." is .claude-work, and a
// colon names a stream inside a file, so neither may pick a record file.
func isPlainName(name string) bool {
	return !strings.Contains(name, ":") && strings.TrimRight(name, ". ") == name
}

func samePath(a, b string) bool {
	return foldCase(filepath.Clean(a)) == foldCase(filepath.Clean(b))
}

// foldCase lowers paths on Windows, where C:\Users\Me and c:\users\me name the
// same folder.
func foldCase(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// ClaudeExecutable prefers the native installer's copy over whatever PATH
// finds first. On Windows an older npm install often sits earlier in PATH.
func (l Layout) ClaudeExecutable() string {
	name := "claude"
	if runtime.GOOS == "windows" {
		name = "claude.exe"
	}
	native := filepath.Join(l.Home, ".local", "bin", name)
	if _, err := os.Stat(native); err == nil {
		return native
	}
	return "claude"
}
