//go:build !windows

package state

func lockBeingReleased(error) bool { return false }
