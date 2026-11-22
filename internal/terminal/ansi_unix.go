//go:build !windows

package terminal

func enableANSI() bool { return true }
