package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const movedAsideMarker = ".moved-aside-by-hat-"

// movedAsidePath gives each replaced Windows program its own name. Windows
// keeps hat running for a whole Claude session, so a copy moved aside by
// an earlier upgrade can still be in use when the next upgrade runs.
func movedAsidePath(exePath string) string {
	return fmt.Sprintf("%s%s%d", exePath, movedAsideMarker, time.Now().UnixNano())
}

func InstalledPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

type NotRestoredError struct {
	ExePath, MovedTo string
	Install, Restore error
}

func (e *NotRestoredError) Error() string {
	return fmt.Sprintf("couldn't put the new hat in place (%v), and couldn't put the old one back (%v). "+
		"Your previous hat is at %s: rename it to %s to keep using it", e.Install, e.Restore, e.MovedTo, filepath.Base(e.ExePath))
}

type swapper struct {
	rename func(from, to string) error
}

// replace writes the new program beside the old one before swapping, so a
// failed write leaves the old one working. Windows won't overwrite a running
// program but will rename it, so there the old one moves aside first and is
// deleted on a later start.
func (s swapper) replace(exePath string, binary []byte) error {
	next, err := stage(exePath, binary)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := s.rename(next, exePath); err != nil {
			_ = os.Remove(next)
			return err
		}
		return nil
	}
	old := movedAsidePath(exePath)
	if err := s.rename(exePath, old); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("couldn't move the running hat aside: %w", err)
	}
	if err := s.rename(next, exePath); err != nil {
		_ = os.Remove(next)
		if restoreErr := s.rename(old, exePath); restoreErr != nil {
			return &NotRestoredError{ExePath: exePath, MovedTo: old, Install: err, Restore: restoreErr}
		}
		return err
	}
	return nil
}

func stage(exePath string, binary []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(exePath), filepath.Base(exePath)+".new-*")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(binary); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// RemoveReplacedWindowsBinaries deletes the copies earlier upgrades moved
// aside. One still running a Claude session can't be deleted yet and stays
// until a later start.
func RemoveReplacedWindowsBinaries(exePath string) {
	if runtime.GOOS != "windows" {
		return
	}
	entries, err := os.ReadDir(filepath.Dir(exePath))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if isMovedAside(filepath.Base(exePath), entry.Name()) {
			_ = os.Remove(filepath.Join(filepath.Dir(exePath), entry.Name()))
		}
	}
}

func isMovedAside(exeName, name string) bool {
	suffix, ok := strings.CutPrefix(name, exeName+movedAsideMarker)
	if !ok || suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
