package terminal

import "testing"

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
		if got := Printable(in); got != want {
			t.Errorf("Printable(%q) = %q, want %q", in, got, want)
		}
	}
}

// An email holding a right-to-left override, U+202E, shows its tail reversed:
// "work@moc.elpmaxe-live" reads as an address at evil-example.com.
func TestPrintableDropsUnicodeFormatCharacters(t *testing.T) {
	const (
		rightToLeftOverride = "\xe2\x80\xae"
		leftToRightIsolate  = "\xe2\x81\xa6"
		popIsolate          = "\xe2\x81\xa9"
		zeroWidthSpace      = "\xe2\x80\x8b"
		zeroWidthJoiner     = "\xe2\x80\x8d"
		byteOrderMark       = "\xef\xbb\xbf"
		lineSeparator       = "\xe2\x80\xa8"
		paragraphSeparator  = "\xe2\x80\xa9"
	)
	cases := map[string]string{
		"work@" + rightToLeftOverride + "moc.elpmaxe-live":          "work@moc.elpmaxe-live",
		"a" + leftToRightIsolate + "b" + popIsolate + "c":           "abc",
		"admin" + zeroWidthSpace + "@example.com":                   "admin@example.com",
		"a" + zeroWidthJoiner + "b" + byteOrderMark + "c":           "abc",
		"line" + lineSeparator + "break" + paragraphSeparator + "!": "linebreak!",
	}
	for in, want := range cases {
		if got := Printable(in); got != want {
			t.Errorf("Printable(%q) = %q, want %q", in, got, want)
		}
	}
}
