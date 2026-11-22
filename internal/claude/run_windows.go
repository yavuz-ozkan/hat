package claude

import (
	"os"
	"os/exec"
	"os/signal"
)

// Interactive runs Claude attached to this terminal and returns its exit code.
// Windows can't hand the process over the way Unix exec does, so hat
// waits. Ctrl+C reaches every process on the console, and Windows ends any
// process that doesn't claim it. signal.Ignore doesn't claim it on Windows
// (Go's sigignore is empty there); only signal.Notify does. So hat takes
// the signal and drops it, and Claude alone decides what Ctrl+C means.
func (r Runner) Interactive(env []string, args []string) (int, error) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	cmd := exec.Command(r.Executable, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return exitCode(cmd.Run())
}
