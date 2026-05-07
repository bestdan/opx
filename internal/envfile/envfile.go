// Package envfile parses dotenv-style files of NAME=VALUE pairs.
//
// The format intentionally mirrors what `op run --env-file` accepts: one
// assignment per line, blank lines and `#` comments ignored, and values
// optionally surrounded by matching single or double quotes (the quotes are
// stripped, no escape processing is performed).
//
// Parsing is deliberately strict — a malformed line is a hard error rather
// than being silently dropped, because env files feed straight into a
// security boundary and a quietly-skipped secret would surface as a missing
// variable at runtime, far from its real cause.
package envfile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Entry is a single NAME=VALUE pair as read from the file. The Line field
// records the 1-based source line number for diagnostics.
type Entry struct {
	Name  string
	Value string
	Line  int
}

// nameRE matches POSIX-portable shell variable names. Kept in sync with
// envNameRE in main.go; duplicated here so the package stays self-contained.
var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseFile reads and parses the file at path.
func ParseFile(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, path)
}

// Parse reads NAME=VALUE entries from r. The source string is included in
// error messages so callers can identify which file produced a parse error
// when several env files are loaded together.
func Parse(r io.Reader, source string) ([]Entry, error) {
	var entries []Entry
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Tolerate a leading "export " so files that double as shell
		// snippets still parse — `op run` accepts the same.
		if strings.HasPrefix(trimmed, "export ") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%s:%d: expected NAME=VALUE, got %q", source, lineNo, raw)
		}
		name := trimmed[:eq]
		value := trimmed[eq+1:]
		if !nameRE.MatchString(name) {
			return nil, fmt.Errorf("%s:%d: %q is not a valid shell variable name", source, lineNo, name)
		}
		value = stripQuotes(value)
		entries = append(entries, Entry{Name: name, Value: value, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return entries, nil
}

// stripQuotes removes a single matching pair of surrounding single or double
// quotes. Mismatched or unbalanced quotes are left as-is — the value passes
// through verbatim and any downstream validator (e.g. uri.IsOPURI) will
// reject it.
func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
