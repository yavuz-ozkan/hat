//go:build !windows

package share

import (
	"os"
	"path/filepath"
)

func makeLink(link, target string, _ bool) error {
	return os.Symlink(target, link)
}

func isLink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

func readLink(path string) (string, error) {
	dest, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(path), dest)
	}
	return dest, nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
