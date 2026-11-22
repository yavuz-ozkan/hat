package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/yavuz-ozkan/hat/internal/usage"
)

// statusline runs as Claude Code's statusLine command. Claude shows whatever
// it prints, so it always prints: a problem recording usage must never cost
// someone their status line. Recording is skipped quietly, because there is
// no one to tell; "hat usage" shows when the last record is old.
func (a App) statusline(command []string) int {
	input, _ := io.ReadAll(a.In)
	line, err := usage.ParseStatusLine(input)
	if path, listed := a.Layout.UsageFile(os.Getenv("CLAUDE_CONFIG_DIR")); listed && err == nil {
		_ = usage.Save(path, line, time.Now())
	}
	return showStatusLine(a.Out, a.Err, command, input, line)
}

func StatusLineWithoutRecording(in io.Reader, out, errOut io.Writer, command []string) int {
	input, _ := io.ReadAll(in)
	line, _ := usage.ParseStatusLine(input)
	return showStatusLine(out, errOut, command, input, line)
}

func showStatusLine(out, errOut io.Writer, command []string, input []byte, line usage.StatusLine) int {
	if len(command) > 0 {
		return passStatusLineTo(out, errOut, command, input)
	}
	fmt.Fprintln(out, usage.DefaultLine(line))
	return exitOK
}

func passStatusLineTo(out, errOut io.Writer, command []string, input []byte) int {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout, cmd.Stderr = out, errOut
	err := cmd.Run()
	var exited *exec.ExitError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &exited):
		return exited.ExitCode()
	default:
		// Claude Code shows a status line command's output only when it exits
		// 0, and the status line is where the person will see the problem.
		fmt.Fprintln(out, cantStartMessage(command[0], err))
		return exitOK
	}
}

// errBadExeFormat is Windows' ERROR_BAD_EXE_FORMAT ("not a valid Win32
// application"), what starting a .sh script directly gives: Windows has no
// shell to hand it to.
const errBadExeFormat = syscall.Errno(193)

func cantStartMessage(command string, err error) string {
	if runtime.GOOS == "windows" && errors.Is(err, errBadExeFormat) {
		return fmt.Sprintf("hat statusline couldn't run %s: Windows can't start it as a program. For a shell script, put sh in front of it in settings.json: hat statusline sh %s", command, command)
	}
	return fmt.Sprintf("hat statusline couldn't run %s (%v). Fix the command after \"hat statusline\" in settings.json.", command, err)
}
