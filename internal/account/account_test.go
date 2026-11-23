package account

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/layout"
	"github.com/yavuz-ozkan/hat/internal/share"
)

func homeWithAccounts(t *testing.T) layout.Layout {
	t.Helper()
	home := t.TempDir()
	l := layout.ForHome(home, layout.UserDirs{Config: filepath.Join(home, "config"), Cache: filepath.Join(home, "cache")})
	write := func(path string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(l.MainDir, "CLAUDE.md"))
	write(filepath.Join(l.AccountDir("made-by-hat"), ".claude.json"))
	if err := MarkManaged(l.AccountDir("made-by-hat")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(l.AccountDir("made-before-the-rename"), ".claude.json"))
	write(filepath.Join(l.AccountDir("made-before-the-rename"), ".claude-as"))
	write(filepath.Join(l.AccountDir("made-by-old-script"), ".claude.json"))
	if _, err := share.Connect(l.AccountDir("made-by-old-script"), l.MainDir, []string{"CLAUDE.md"}); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(l.AccountDir("kept-separate"), ".claude.json"))
	if err := os.MkdirAll(l.AccountDir("no-state-file"), 0o755); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAllOrdersMainThenManagedThenUnmanaged(t *testing.T) {
	l := homeWithAccounts(t)

	accounts, err := All(l)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, a := range accounts {
		got = append(got, a.Name)
	}
	want := []string{"main", "made-before-the-rename", "made-by-hat", "made-by-old-script", "kept-separate"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("accounts = %v, want %v", got, want)
	}
	if accounts[0].Kind != Main || accounts[0].StateFile != l.MainStateFile {
		t.Errorf("main account = %+v", accounts[0])
	}
	if accounts[4].Kind != Unmanaged {
		t.Errorf("kept-separate kind = %v, want Unmanaged", accounts[4].Kind)
	}
}

func TestNamedNewFolderIsManaged(t *testing.T) {
	l := homeWithAccounts(t)
	if Named(l, "brand-new").Kind != Managed {
		t.Error("a folder hat is about to create should count as managed")
	}
}

func TestEnvironmentUnsetsConfigDirOnlyForMain(t *testing.T) {
	l := homeWithAccounts(t)

	set, unset := MainAccount(l).Environment()
	if len(set) != 0 || !reflect.DeepEqual(unset, []string{"CLAUDE_CONFIG_DIR"}) {
		t.Errorf("main: set=%v unset=%v, want CLAUDE_CONFIG_DIR unset", set, unset)
	}

	work := Named(l, "work")
	set, unset = work.Environment()
	if set["CLAUDE_CONFIG_DIR"] != work.ConfigDir || len(unset) != 0 {
		t.Errorf("work: set=%v unset=%v, want CLAUDE_CONFIG_DIR=%s", set, unset, work.ConfigDir)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"work", "client-a", "a", "2nd-team"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Work", "-x", "a b", "../x"} {
		if ValidateName(bad) == nil {
			t.Errorf("ValidateName(%q) accepted a bad name", bad)
		}
	}
}

// "hat usage" lists exactly the accounts whose usage hat statusline records,
// so no row points at a record file that can't exist.
func TestAllListsOnlyAccountsWhoseUsageIsRecorded(t *testing.T) {
	l := homeWithAccounts(t)
	odd := []string{"work.", "work "}
	if runtime.GOOS != "windows" {
		odd = append(odd, "a:b")
	}
	for _, name := range odd {
		dir := filepath.Join(l.Home, layout.AccountDirPrefix+name)
		if runtime.GOOS == "windows" {
			dir = `\\?\` + dir
		}
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+string(filepath.Separator)+".claude.json", []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	accounts, err := All(l)
	if err != nil {
		t.Fatal(err)
	}

	listed := map[string]bool{}
	for _, acct := range accounts {
		listed[acct.Name] = true
		if _, recorded := l.UsageFile(acct.ConfigDir); !recorded {
			t.Errorf("All lists %q, whose usage is never recorded", acct.Name)
		}
	}
	for _, name := range odd {
		if want := runtime.GOOS != "windows"; listed[name] != want {
			t.Errorf("%q listed = %t, want %t", name, listed[name], want)
		}
	}
}
