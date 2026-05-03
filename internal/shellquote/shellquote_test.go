package shellquote_test

import (
	"testing"

	"github.com/bestdan/opx/internal/shellquote"
)

func TestQuote(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `''`},
		{"plain", "hello", `'hello'`},
		{"spaces", "hello world", `'hello world'`},
		{"single quote", "it's", `'it'\''s'`},
		{"only quote", "'", `''\'''`},
		{"adjacent quotes", "''", `''\'''\'''`},
		{"newline", "a\nb", "'a\nb'"},
		{"dollar and backtick", "$x `y`", "'$x `y`'"},
		{"backslash", `\n`, `'\n'`},
		{"double quote", `"x"`, `'"x"'`},
		{"binary nul", "a\x00b", "'a\x00b'"},
		{"unicode", "héllo", `'héllo'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(shellquote.Quote([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("Quote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
