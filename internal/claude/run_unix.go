//go:build !windows

package claude

import (
	"os/exec"
	"syscall"
)

func (r Runner) Interactive(env []string, args []string) (int, error) {
	path, err := exec.LookPath(r.Executable)
	if err != nil {
		return 1, err
	}
	err = syscall.Exec(path, append([]string{path}, args...), env)
	return exitCode(err)
}
