package upgrade

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const lockSuffix = ".hat-upgrade.lock"

// lockUpgrade never removes a lock it didn't create, even an old one: between
// judging a lock abandoned and removing it, another upgrade could take it.
// A lock left by a crashed upgrade is the person's to delete.
func lockUpgrade(exePath string) (unlock func(), err error) {
	path := exePath + lockSuffix
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	switch {
	case errors.Is(err, fs.ErrExist):
		return nil, fmt.Errorf("another hat upgrade is running, or one stopped partway. Wait for it to finish, then run hat upgrade again. If none is running, delete %s", path)
	case err != nil:
		return nil, fmt.Errorf("couldn't create %s, which keeps two upgrades from running at once (%v). Check that you can write to that folder", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}
