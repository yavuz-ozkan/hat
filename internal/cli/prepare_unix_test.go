//go:build !windows

package cli

import (
	"os"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/account"
)

func TestPrepareMakesTheAccountFolderPrivate(t *testing.T) {
	h := newHarness(t)
	acct := account.Named(h.layout, "work")

	if err := h.app.prepare(acct); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(acct.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("%s has mode %o, want 700", acct.ConfigDir, perm)
	}
}
