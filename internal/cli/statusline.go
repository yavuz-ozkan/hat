package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	if err == nil {
		_ = usage.Save(a.Layout.UsageFile(os.Getenv("CLAUDE_CONFIG_DIR")), line, time.Now())
	}
	return showStatusLine(a.Out, a.Err, command, input, line)
}

// StatusLineWithoutRecording is the status line for when hat can't find its
// own folders: there's nowhere to record usage, but the line still shows.
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

// passStatusLineTo runs someone's own status line command with the input
// Claude gave hat, so wrapping it in "hat statusline" changes nothing they see.
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
		// This line is the status line, so it's where the person will see it.
		fmt.Fprintf(out, "hat statusline couldn't run %s (%v). Fix the command after \"hat statusline\" in settings.json.\n", command[0], err)
		return exitFailed
	}
}
