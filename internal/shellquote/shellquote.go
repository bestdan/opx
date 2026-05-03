// Package shellquote escapes byte strings for safe inclusion inside POSIX
// shell single-quoted literals.
//
// The output is always wrapped in single quotes.  Any embedded single quote
// is rendered as the four-byte sequence '\'' (close-quote, escaped quote,
// reopen-quote), which is portable across bash, zsh, dash, and ash.  All
// other bytes — including newlines, dollars, backslashes, and arbitrary
// binary — pass through unchanged because no expansion happens inside
// single quotes.
package shellquote

import "bytes"

// Quote returns s wrapped in single quotes with embedded single quotes
// escaped, suitable for `eval` consumption by a POSIX shell.
func Quote(s []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(s) + 2)
	buf.WriteByte('\'')
	for _, b := range s {
		if b == '\'' {
			buf.WriteString(`'\''`)
			continue
		}
		buf.WriteByte(b)
	}
	buf.WriteByte('\'')
	return buf.Bytes()
}
