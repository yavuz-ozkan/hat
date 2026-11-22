package layout

import (
	"crypto/sha256"
	"encoding/hex"
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
	// UsageDir is in the user cache folder rather than a Claude config folder,
	// so recording usage never writes anything Claude reads.
	UsageDir string
}

// UserDirs are the per-user folders the operating system sets aside for
// settings and for caches.
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
// recorded. An empty configDir means the main account, matching how Claude
// Code treats an unset CLAUDE_CONFIG_DIR. Account folders in the home folder
// get readable names; a config folder anywhere else is named by a hash of its
// path, so two folders never share a file.
func (l Layout) UsageFile(configDir string) string {
	if configDir == "" {
		configDir = l.MainDir
	}
	dir := filepath.Clean(configDir)
	base := foldCase(filepath.Base(dir))
	var name string
	switch {
	case samePath(dir, l.MainDir):
		name = "main"
	case samePath(filepath.Dir(dir), l.Home) && strings.HasPrefix(base, AccountDirPrefix) && base != AccountDirPrefix:
		name = strings.TrimPrefix(base, ".")
	default:
		sum := sha256.Sum256([]byte(foldCase(dir)))
		name = "elsewhere-" + hex.EncodeToString(sum[:8])
	}
	return filepath.Join(l.UsageDir, name+".json")
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
