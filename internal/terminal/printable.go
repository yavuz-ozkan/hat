package terminal

import (
	"strings"
	"unicode"
)

// Printable drops what a terminal would act on instead of show: C0 and C1
// control characters, DEL, and whole escape sequences, so a name or email
// from an account's files or Anthropic's answer can't move the cursor, rewrite
// the screen, or set the window title. It also drops Unicode format
// characters, like bidi overrides and zero-width spaces, and the line and
// paragraph separators, so text can't show up reordered or split.
func Printable(s string) string {
	var out strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == escape:
			i = skipEscapeSequence(runes, i)
		case r == csi8Bit:
			i = skipControlSequence(runes, i+1)
		case isStringIntroducer8Bit(r):
			i = skipControlString(runes, i+1)
		case r < 0x20 || r == 0x7f || 0x80 <= r && r <= 0x9f:
		case unicode.Is(unicode.Cf, r) || r == lineSeparator || r == paragraphSeparator:
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

const (
	escape  = 0x1b
	bell    = 0x07
	csi8Bit = 0x9b

	lineSeparator      = 0x2028
	paragraphSeparator = 0x2029
)

// ECMA-48 control strings: DCS, SOS, OSC, PM, APC. Each runs until a string
// terminator (ST) or, by xterm's convention, a bell.
func isStringIntroducer8Bit(r rune) bool {
	return r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f
}

func isStringIntroducerAfterEscape(r rune) bool {
	return r == 'P' || r == 'X' || r == ']' || r == '^' || r == '_'
}

// skipEscapeSequence returns the index of the last rune of the sequence that
// starts with the ESC at i.
func skipEscapeSequence(runes []rune, i int) int {
	if i+1 >= len(runes) {
		return i
	}
	next := runes[i+1]
	switch {
	case next == '[':
		return skipControlSequence(runes, i+2)
	case isStringIntroducerAfterEscape(next):
		return skipControlString(runes, i+2)
	default:
		return skipIntermediatesAndFinal(runes, i+1)
	}
}

// skipControlSequence skips a CSI's parameter and intermediate bytes and its
// final byte, from 0x40 to 0x7e.
func skipControlSequence(runes []rune, i int) int {
	for ; i < len(runes); i++ {
		switch r := runes[i]; {
		case 0x40 <= r && r <= 0x7e:
			return i
		case r < 0x20 || r > 0x7e:
			return i - 1
		}
	}
	return len(runes) - 1
}

// skipIntermediatesAndFinal skips an escape sequence like ESC ( B: any
// intermediate bytes from 0x20 to 0x2f, then one final byte.
func skipIntermediatesAndFinal(runes []rune, i int) int {
	for ; i < len(runes); i++ {
		if runes[i] < 0x20 || runes[i] > 0x2f {
			return i
		}
	}
	return len(runes) - 1
}

func skipControlString(runes []rune, i int) int {
	for ; i < len(runes); i++ {
		switch {
		case runes[i] == bell, runes[i] == 0x9c:
			return i
		case runes[i] == escape && i+1 < len(runes) && runes[i+1] == '\\':
			return i + 1
		}
	}
	return len(runes) - 1
}
