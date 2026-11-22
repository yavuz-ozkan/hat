package main

import "testing"

func TestVersionOfPrefersReleaseThenModuleVersion(t *testing.T) {
	cases := []struct{ ldflags, module, want string }{
		{"1.2.3", "v9.9.9", "1.2.3"},
		{"", "v0.2.0", "0.2.0"},
		{"", "v0.0.0-20261121010203-abcdef123456", "0.0.0-20261121010203-abcdef123456"},
		{"", "(devel)", "dev"},
		{"", "", "dev"},
	}
	for _, c := range cases {
		if got := versionOf(c.ldflags, c.module); got != c.want {
			t.Errorf("versionOf(%q, %q) = %q, want %q", c.ldflags, c.module, got, c.want)
		}
	}
}
