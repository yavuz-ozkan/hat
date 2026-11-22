package cli

import (
	"os"
	"testing"

	"github.com/yavuz-ozkan/hat/internal/account"
)

func TestRemoveKeepsTheFolderHatsWhenTheLastStepFails(t *testing.T) {
	h := newHarness(t)
	work := account.Named(h.layout, "work")
	if err := h.app.prepare(work); err != nil {
		t.Fatal(err)
	}
	// Windows won't delete a folder that is some process's current folder,
	// so everything inside goes and only the final removal fails.
	t.Chdir(work.ConfigDir)

	if code := h.app.Run([]string{"rm", "work"}); code != exitFailed {
		t.Fatalf("exit %d, stderr %q", code, h.err)
	}

	if account.Named(h.layout, "work").Kind != account.Managed {
		t.Fatal("the folder no longer counts as hat's, so rm can't be retried")
	}
	if err := os.Chdir(os.TempDir()); err != nil {
		t.Fatal(err)
	}
	h.err.Reset()
	if code := h.app.Run([]string{"rm", "work"}); code != exitOK {
		t.Errorf("second rm: exit %d, stderr %q", code, h.err)
	}
}
