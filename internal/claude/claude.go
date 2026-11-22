package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
)

type Runner struct {
	Executable string
}

func Env(set map[string]string, unset ...string) []string {
	replaced := slices.Clone(unset)
	for name := range set {
		replaced = append(replaced, name)
	}
	var env []string
	for _, kv := range os.Environ() {
		if !isOneOf(envName(kv), replaced) {
			env = append(env, kv)
		}
	}
	for name, value := range set {
		env = append(env, name+"="+value)
	}
	return env
}

// RenewSignIn gets Claude to renew an expired sign-in itself, by running the
// cheapest command that has to call Anthropic with it: listing connectors
// fetches the account's claude.ai connectors. `claude auth status` doesn't
// work for this; it answers from the file without renewing (Claude Code
// 2.1.329). Renewing outside Claude would race any open window of the account
// over the same refresh token, and the loser is signed out.
//
// It runs in the account's own folder: `mcp list` also health-checks the
// current folder's project connectors, which renewal has no business starting.
func (r Runner) RenewSignIn(env []string, accountDir string) {
	ctx, cancel := context.WithTimeout(context.Background(), renewTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Executable, "mcp", "list")
	cmd.Env = env
	cmd.Dir = accountDir
	_ = cmd.Run()
}

const renewTimeout = 45 * time.Second

func (r Runner) SignIn(env []string) error {
	for _, args := range [][]string{{"auth", "login", "--claudeai"}, {"auth", "status"}} {
		cmd := exec.Command(r.Executable, args...)
		cmd.Env = env
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// envName is the variable name in a NAME=value entry. Windows keeps hidden
// per-drive entries like "=C:=C:\dev" whose name starts with "=".
func envName(kv string) string {
	start := 0
	if strings.HasPrefix(kv, "=") {
		start = 1
	}
	if i := strings.Index(kv[start:], "="); i >= 0 {
		return kv[:start+i]
	}
	return kv
}

func isOneOf(name string, names []string) bool {
	for _, candidate := range names {
		if candidate == name || (runtime.GOOS == "windows" && strings.EqualFold(candidate, name)) {
			return true
		}
	}
	return false
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 1, err
}
