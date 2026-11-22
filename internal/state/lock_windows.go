package state

import (
	"errors"
	"syscall"
)

// lockBeingReleased: while another process deletes the lock folder, Windows
// answers a create with "access denied" instead of "already exists".
func lockBeingReleased(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}
