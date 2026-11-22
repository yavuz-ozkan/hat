package terminal

import (
	"os"

	"golang.org/x/term"
)

// NO_COLOR (https://no-color.org) turns color off.
func ColorOK() bool {
	if os.Getenv("NO_COLOR") != "" || !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	return enableANSI()
}
