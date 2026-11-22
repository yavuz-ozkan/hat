package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/yavuz-ozkan/hat/internal/account"
	"github.com/yavuz-ozkan/hat/internal/claude"
	"github.com/yavuz-ozkan/hat/internal/config"
	"github.com/yavuz-ozkan/hat/internal/layout"
	"github.com/yavuz-ozkan/hat/internal/memory"
	"github.com/yavuz-ozkan/hat/internal/share"
	"github.com/yavuz-ozkan/hat/internal/state"
	"github.com/yavuz-ozkan/hat/internal/upgrade"
)

const (
	exitOK      = 0
	exitFailed  = 1
	exitBadArgs = 2
)

const helpText = `hat runs Claude Code as several accounts side by side.

Usage:
  hat <account> [claude arguments]   start Claude as that account
  hat usage [--json]                 every account's plan limits, as Claude last reported them
  hat usage --live [--json]          ask Anthropic right now (unofficial, see below)
  hat statusline [command ...]       record plan limits for "hat usage"; set it as
                                     Claude Code's statusLine command
  hat rm <account>                   delete an account hat made
  hat upgrade                        install the newest release
  hat version                        show the installed version

Examples:
  hat work
  hat client-a --resume
  hat usage --json
  hat statusline ~/.claude/statusline.sh
  hat statusline sh ~/.claude/statusline.sh   the same on Windows, where a script needs sh

Each account keeps its own sign-in in ~/.claude-<account>. Instructions,
settings, skills, plugins, memory, and history are shared with the main
account (~/.claude, started with plain "claude"). Other ~/.claude-* folders
are shown by "usage" and otherwise left alone.

"hat usage" reads what "hat statusline" recorded and sends nothing anywhere.
"hat usage --live" is unofficial: it sends each account's token to an
undocumented Anthropic endpoint that may change or stop working.
`

type App struct {
	Layout        layout.Layout
	In            io.Reader
	Out           io.Writer
	Err           io.Writer
	Version       string
	Releases      upgrade.Source
	InstalledPath func() (string, error)
}

func (a App) commands() map[string]func(args []string) int {
	return map[string]func([]string) int{
		"help":       a.help,
		"-h":         a.help,
		"--help":     a.help,
		"version":    a.version,
		"--version":  a.version,
		"usage":      a.usage,
		"statusline": a.statusline,
		"rm":         a.remove,
		"upgrade":    a.upgrade,
	}
}

func (a App) upgrade(args []string) int {
	if len(args) != 0 {
		return a.fail(exitBadArgs, "upgrade takes no arguments. Run hat upgrade.")
	}
	exePath, err := a.InstalledPath()
	if err != nil {
		return a.fail(exitFailed, "couldn't work out where this hat is installed (%v), so it can't replace itself. Reinstall with install.sh or install.ps1 instead.", err)
	}
	result, err := upgrade.Run(a.Releases, a.Version, exePath)
	var notRestored *upgrade.NotRestoredError
	if errors.As(err, &notRestored) {
		return a.fail(exitFailed, "%v.", err)
	}
	if err != nil {
		return a.fail(exitFailed, "%v. Your current hat (%s) is unchanged.", err, a.Version)
	}
	switch result.Outcome {
	case upgrade.AlreadyNewest:
		fmt.Fprintf(a.Out, "hat %s is the newest release.\n", result.To)
	case upgrade.AheadOfNewest:
		fmt.Fprintf(a.Out, "hat %s is newer than the newest release (%s), so nothing changed.\n", result.From, result.To)
	default:
		fmt.Fprintf(a.Out, "Upgraded hat from %s to %s.\n", result.From, result.To)
	}
	return exitOK
}

func (a App) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Err, helpText)
		return exitBadArgs
	}
	if run, isCommand := a.commands()[args[0]]; isCommand {
		return run(args[1:])
	}
	return a.launch(args[0], args[1:])
}

const macOSUnsupported = "hat doesn't support macOS yet. Claude Code keeps macOS sign-ins in the Keychain, not in each account's folder, so every account would ask you to sign in again at every start. Run Claude Code with plain claude on this Mac"

// Refusal is why hat won't run args on the operating system goos names.
// Help and version always run, so someone on an unsupported system can still
// see what hat is and which build they have.
func Refusal(goos string, args []string) (string, bool) {
	if goos != "darwin" || len(args) == 0 {
		return "", false
	}
	switch args[0] {
	case "help", "-h", "--help", "version", "--version":
		return "", false
	}
	return macOSUnsupported, true
}

func (a App) fail(code int, format string, args ...any) int {
	fmt.Fprintf(a.Err, "hat: "+format+"\n", args...)
	return code
}

func (a App) runner() (claude.Runner, error) {
	executable, err := claude.Find(a.Layout.ClaudeExecutable())
	return claude.Runner{Executable: executable}, err
}

func (a App) checkAccountName(name string) error {
	if err := account.ValidateName(name); err != nil {
		return err
	}
	if name == "main" {
		return errors.New(`"main" is the main account, which plain "claude" already runs. Pick another name for a separate account`)
	}
	if _, isCommand := a.commands()[name]; isCommand {
		return fmt.Errorf("%q is a hat command, so it can't be an account name. Pick another, like %s-account", name, name)
	}
	return nil
}

func (a App) help([]string) int {
	fmt.Fprint(a.Out, helpText)
	return exitOK
}

func (a App) version([]string) int {
	fmt.Fprintln(a.Out, "hat", a.Version)
	return exitOK
}

func (a App) launch(name string, claudeArgs []string) int {
	if err := a.checkAccountName(name); err != nil {
		return a.fail(exitBadArgs, "%v.", err)
	}
	acct := account.Named(a.Layout, name)
	if acct.Kind == account.Unmanaged {
		return a.fail(exitBadArgs, "%s wasn't made by hat, so hat leaves it alone. Start it with CLAUDE_CONFIG_DIR=%s claude.", acct.ConfigDir, acct.ConfigDir)
	}
	runner, err := a.runner()
	if err != nil {
		return a.fail(exitFailed, "%v.", err)
	}
	a.warnAboutAmbientCredentials(name)
	a.warnWhenMainHasNoSharedFolders(name)
	if err := a.prepare(acct); err != nil {
		return a.fail(exitFailed, "%v", err)
	}

	set, unset := acct.Environment()
	// agent-browser gets its own browser and saved website logins per
	// account: two Chromes can't open one profile folder at once, so
	// accounts browsing side by side would otherwise collide.
	set["AGENT_BROWSER_SESSION"] = "hat-" + name
	set["AGENT_BROWSER_PROFILE"] = filepath.Join(acct.ConfigDir, "browser-profile")
	env := claude.Env(set, unset...)
	if _, err := os.Stat(acct.CredentialsFile); os.IsNotExist(err) {
		fmt.Fprintf(a.Out, "First run for '%s'. A browser window will open: sign in with the %s account.\n", name, name)
		if err := runner.SignIn(env); err != nil {
			return a.fail(exitFailed, "Sign-in didn't finish (%v). Run hat %s again to retry.", err, name)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return a.fail(exitFailed, "couldn't read the current folder: %v", err)
	}
	args := append(memory.SettingsArgs(a.Layout.MainDir, cwd), claudeArgs...)
	code, err := runner.Interactive(env, args)
	if err != nil {
		return a.fail(exitFailed, "couldn't start Claude (%s): %v. Install Claude Code, or check that \"claude\" runs on its own.", runner.Executable, err)
	}
	return code
}

// ambientCredentials are the variables Claude Code signs in with ahead of
// the sign-in saved in its config folder.
var ambientCredentials = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

func (a App) warnAboutAmbientCredentials(name string) {
	for _, variable := range ambientCredentials {
		if os.Getenv(variable) != "" {
			fmt.Fprintf(a.Err, "hat: warning: %s is set, so Claude Code uses it instead of %s's own sign-in. To use %s's sign-in, %s.\n", variable, name, name, unsetForOneRun(variable, name))
		}
	}
}

func unsetForOneRun(variable, name string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("run Remove-Item Env:%s in PowerShell, which clears it in this terminal only, then hat %s", variable, name)
	}
	return fmt.Sprintf("start it with env -u %s hat %s", variable, name)
}

func (a App) warnWhenMainHasNoSharedFolders(name string) {
	if _, err := os.Stat(filepath.Join(a.Layout.MainDir, "projects")); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(a.Err, "hat: warning: run plain claude once first, so the shared folders exist in %s. Otherwise %s keeps its own copies instead of sharing main's.\n", a.Layout.MainDir, name)
	}
}

const accountDirMode = 0o700

func (a App) prepare(acct account.Account) error {
	cfg, err := config.Load(a.Layout.ConfigFile)
	if err != nil {
		return err
	}
	items, err := share.Items(cfg.AlsoShare)
	if err != nil {
		return fmt.Errorf("%s: %w", a.Layout.ConfigFile, err)
	}
	if err := os.MkdirAll(acct.ConfigDir, accountDirMode); err != nil {
		return err
	}
	// MkdirAll leaves an existing folder's mode alone, and folders made by
	// earlier versions are 0755. Windows ignores these mode bits.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(acct.ConfigDir, accountDirMode); err != nil {
			return err
		}
	}
	if err := account.MarkManaged(acct.ConfigDir); err != nil {
		return err
	}
	warnings, err := share.Connect(acct.ConfigDir, a.Layout.MainDir, items)
	for _, warning := range warnings {
		fmt.Fprintf(a.Err, "hat: warning: %v\n", warning)
	}
	if err != nil {
		return err
	}
	err = state.InheritFromMain(a.Layout.MainStateFile, acct.StateFile)
	if errors.Is(err, state.ErrBusy) {
		fmt.Fprintf(a.Err, "hat: warning: %v\n", err)
		return nil
	}
	return err
}

func (a App) remove(args []string) int {
	if len(args) != 1 {
		return a.fail(exitBadArgs, "rm takes one account name, like: hat rm work")
	}
	name := args[0]
	if err := a.checkAccountName(name); err != nil {
		return a.fail(exitBadArgs, "%v.", err)
	}
	acct := account.Named(a.Layout, name)
	if _, err := os.Stat(acct.ConfigDir); errors.Is(err, os.ErrNotExist) {
		return a.fail(exitBadArgs, "there's no account named %s (%s doesn't exist). Run hat usage to see your accounts.", name, acct.ConfigDir)
	}
	if acct.Kind == account.Unmanaged {
		return a.fail(exitBadArgs, "%s wasn't made by hat, so hat won't delete it. Delete it yourself if you mean to.", acct.ConfigDir)
	}
	if err := account.CheckNotLinked(acct); err != nil {
		return a.fail(exitBadArgs, "%v.", err)
	}

	usageFile, _ := a.Layout.UsageFile(acct.ConfigDir)
	if err := os.Remove(usageFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return a.fail(exitFailed, "couldn't delete %s's recorded usage in %s: %v. Nothing was removed. Fix that file's permissions and run hat rm %s again.", name, usageFile, err, name)
	}
	if err := account.Remove(acct); err != nil {
		return a.fail(exitFailed, "couldn't delete all of %s: %v. Shared files are untouched. Close any Claude window running as %s and run hat rm %s again.", acct.ConfigDir, err, name, name)
	}
	fmt.Fprintf(a.Out, "Removed %s: its sign-in, connector sign-ins, saved browser logins, and recorded usage.\n", name)
	fmt.Fprintf(a.Out, "Shared memory, settings, and skills are untouched in %s.\n", a.Layout.MainDir)
	return exitOK
}
