package claude

import (
	"slices"
	"testing"
)

func TestEnvSetsAndUnsets(t *testing.T) {
	t.Setenv("HAT_TEST_KEPT", "kept")
	t.Setenv("HAT_TEST_REPLACED", "old")
	t.Setenv("HAT_TEST_REMOVED", "gone")

	env := Env(map[string]string{"HAT_TEST_REPLACED": "new", "HAT_TEST_ADDED": "added"}, "HAT_TEST_REMOVED")

	for _, want := range []string{"HAT_TEST_KEPT=kept", "HAT_TEST_REPLACED=new", "HAT_TEST_ADDED=added"} {
		if !slices.Contains(env, want) {
			t.Errorf("env is missing %s", want)
		}
	}
	for _, kv := range env {
		if name := envName(kv); name == "HAT_TEST_REMOVED" || kv == "HAT_TEST_REPLACED=old" {
			t.Errorf("env still has %s", kv)
		}
	}
}
