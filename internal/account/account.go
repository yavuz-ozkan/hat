package account

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yavuz-ozkan/hat/internal/layout"
	"github.com/yavuz-ozkan/hat/internal/share"
)

type Kind int

const (
	Main Kind = iota
	Managed
	Unmanaged
)

const (
	markerFile       = ".hat"
	legacyMarkerFile = ".claude-as"
	credentialsFile  = ".credentials.json"
)

type Account struct {
	Name            string
	Kind            Kind
	ConfigDir       string
	CredentialsFile string
	StateFile       string
}

func (a Account) SignInHint() string {
	switch a.Kind {
	case Main:
		return "claude, then /login"
	case Managed:
		return "hat " + a.Name
	default:
		return fmt.Sprintf("start Claude with CLAUDE_CONFIG_DIR=%s, then /login", a.ConfigDir)
	}
}

func (a Account) Environment() (set map[string]string, unset []string) {
	if a.Kind == Main {
		return map[string]string{}, []string{"CLAUDE_CONFIG_DIR"}
	}
	return map[string]string{"CLAUDE_CONFIG_DIR": a.ConfigDir}, nil
}

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("%q isn't a usable account name. Use lowercase letters, digits, and dashes, starting with a letter or digit, like work or client-a", name)
	}
	return nil
}

func MainAccount(l layout.Layout) Account {
	return Account{
		Name:            "main",
		Kind:            Main,
		ConfigDir:       l.MainDir,
		CredentialsFile: filepath.Join(l.MainDir, credentialsFile),
		StateFile:       l.MainStateFile,
	}
}

func Named(l layout.Layout, name string) Account {
	dir := l.AccountDir(name)
	return Account{
		Name:            name,
		Kind:            kindOf(l, dir),
		ConfigDir:       dir,
		CredentialsFile: filepath.Join(dir, credentialsFile),
		StateFile:       filepath.Join(dir, ".claude.json"),
	}
}

func All(l layout.Layout) ([]Account, error) {
	entries, err := os.ReadDir(l.Home)
	if err != nil {
		return nil, err
	}
	var found []Account
	for _, entry := range entries {
		name, ok := strings.CutPrefix(entry.Name(), layout.AccountDirPrefix)
		if !ok || !entry.IsDir() || name == "" {
			continue
		}
		acct := Named(l, name)
		if _, err := os.Stat(acct.StateFile); err != nil {
			continue
		}
		if _, listed := l.UsageFile(acct.ConfigDir); !listed {
			continue
		}
		found = append(found, acct)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Kind != found[j].Kind {
			return found[i].Kind < found[j].Kind
		}
		return found[i].Name < found[j].Name
	})
	return append([]Account{MainAccount(l)}, found...), nil
}

func MarkManaged(dir string) error {
	path := filepath.Join(dir, markerFile)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("This folder is managed by hat.\n"), 0o644)
}

// Remove deletes a managed account's folder, its marker last. A folder that
// can't be emptied, say because a running Claude holds a file open, still
// has the marker afterwards, so it stays hat's and hat rm can try again.
// CheckNotLinked refuses an account folder that is itself a link: deleting
// through it would empty the folder it points to.
func CheckNotLinked(acct Account) error {
	// Go reports a Windows junction as irregular rather than as a symlink.
	if info, err := os.Lstat(acct.ConfigDir); err == nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("%s is a link to another folder, so hat won't delete through it. Delete the link itself if you mean to; that leaves the folder it points to alone", acct.ConfigDir)
	}
	return nil
}

func Remove(acct Account) error {
	if err := CheckNotLinked(acct); err != nil {
		return err
	}
	if err := MarkManaged(acct.ConfigDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(acct.ConfigDir)
	if err != nil {
		return err
	}
	var firstFailure error
	for _, entry := range entries {
		if entry.Name() == markerFile {
			continue
		}
		if err := os.RemoveAll(filepath.Join(acct.ConfigDir, entry.Name())); err != nil && firstFailure == nil {
			firstFailure = err
		}
	}
	if firstFailure != nil {
		return firstFailure
	}
	if err := os.Remove(filepath.Join(acct.ConfigDir, markerFile)); err != nil {
		return err
	}
	if err := os.Remove(acct.ConfigDir); err != nil {
		// Something appeared in the folder, or held it open, after the sweep.
		_ = MarkManaged(acct.ConfigDir)
		return err
	}
	return nil
}

func kindOf(l layout.Layout, dir string) Kind {
	if notCreatedYet(dir) || hasMarker(dir) || madeByOldScript(l, dir) {
		return Managed
	}
	return Unmanaged
}

func notCreatedYet(dir string) bool {
	_, err := os.Stat(dir)
	return os.IsNotExist(err)
}

func hasMarker(dir string) bool {
	for _, name := range []string{markerFile, legacyMarkerFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func madeByOldScript(l layout.Layout, dir string) bool {
	return share.IsLinkTo(filepath.Join(dir, "CLAUDE.md"), filepath.Join(l.MainDir, "CLAUDE.md"))
}
