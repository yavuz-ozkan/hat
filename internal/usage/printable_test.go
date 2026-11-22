package usage

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

const (
	clearScreen = "\x1b[2J\x1b[H"
	setTitle    = "\x1b]0;pwned\x07"
	csiIn8Bits  = "\xc2\x9b31m"
	oscIn8Bits  = "\xc2\x9d8;;https://evil.example\xc2\x9c"
)

func TestPrintableDropsControlsAndEscapeSequences(t *testing.T) {
	cases := map[string]string{
		"work@example.com":      "work@example.com",
		"a" + clearScreen + "b": "ab",
		"a" + setTitle + "b":    "ab",
		"a\x1b]8;;https://evil.example\x1b\\link\x1b]8;;\x1b\\b": "alinkb",
		"a\x1b(Bb":                       "ab",
		"a\x1b7b":                        "ab",
		"a" + csiIn8Bits + "b":           "ab",
		"a" + oscIn8Bits + "b":           "ab",
		"tab\there\r\nnul\x00del\x7fend": "tabherenuldelend",
		"bell\x07":                       "bell",
		"cut off \x1b[":                  "cut off ",
		"Opus 4.5 · café 🚀":              "Opus 4.5 · café 🚀",
	}
	for in, want := range cases {
		if got := printable(in); got != want {
			t.Errorf("printable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderPrintsNoEscapesFromAccountData(t *testing.T) {
	now := time.Now()
	rows := []Row{{
		Account: "work" + clearScreen,
		Email:   "work@example.com" + setTitle,
		Limits:  []Limit{{Kind: "x" + clearScreen, Label: "weekly" + csiIn8Bits, Model: "Opus" + setTitle, Percent: 5}},
	}, {
		Account: "other",
		Problem: &Problem{Code: UnexpectedResponse, Message: "odd" + clearScreen},
	}}

	var out bytes.Buffer
	Render(&out, rows, Style{}, now)

	if got := out.String(); strings.ContainsAny(got, "\x1b\x07\u009b\u009d") {
		t.Errorf("output holds control characters: %q", got)
	}
	for _, want := range []string{"work@example.com", "weekly, Opus", "odd"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lost %q: %q", want, out.String())
		}
	}
}

func TestDefaultLinePrintsNoEscapesFromTheModelName(t *testing.T) {
	line, err := ParseStatusLine([]byte(`{"model":{"display_name":"Opus\u001b]0;pwned\u0007\u001b[2J"}}`))
	if err != nil {
		t.Fatal(err)
	}

	if got := DefaultLine(line); got != "Opus" {
		t.Errorf("DefaultLine = %q, want Opus", got)
	}
}
