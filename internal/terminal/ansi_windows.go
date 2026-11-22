package terminal

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableANSI switches the Windows console into the mode that understands
// color codes. Windows Terminal already is; the older console host isn't.
func enableANSI() bool {
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil {
		return false
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
