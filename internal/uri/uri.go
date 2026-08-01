// Package uri provides helpers for validating 1Password op:// URIs.
package uri

import (
	"strings"
	"unicode/utf8"
)

// maxURI bounds an accepted URI in bytes.
//
// The limit is about review, not about `op`: the URI is the thing the
// confirmation dialog asks the user to approve, and a multi-kilobyte string is
// not reviewable — it either scrolls the dialog past the rest of the request or
// gets truncated, and a truncation that hides the tail is how a URI lies about
// which secret it names. Every other caller-controlled display string in opx is
// bounded for the same reason (see maxChildCommand in internal/caller). 1024 is
// far above any real vault/item/field path; a URI over it is a payload, not a
// typo.
//
// Unlike the control-character rule below, this is a hard limit on otherwise
// legitimate input. It is deliberate.
const maxURI = 1024

// IsOPURI returns true if s is an acceptable op:// URI. Two conditions, and the
// second is a security check rather than a syntax one:
//
//  1. Syntax: the op:// prefix and at least three non-empty path segments
//     (vault/item/field), within maxURI bytes.
//  2. Text safety: s is valid UTF-8 and contains no control characters — no
//     rune below 0x20, and none in 0x7f–0x9f.
//
// The second exists because the URI is interpolated into the confirmation
// dialog, which is opx's entire trust boundary, and it is attacker-controlled
// in every input mode (argv, --env, --env-file). internal/prompt escapes
// control characters on the way out; this rejects them on the way in, so a URI
// carrying terminal escapes never reaches the dialog at all and fails as a
// usage error before any prompt is drawn (see AGENTS.md invariant 3). Belt and
// braces on purpose: the sanitizer protects any future interpolation site, and
// this protects the user from having to interpret a body full of \x1b.
//
// Two things this check must NOT be turned into:
//
//   - A byte scan. The C1 test is over runes, not bytes, because bytes in
//     0x80–0x9f are ordinary UTF-8 continuation bytes: verified, "‘quoted’",
//     "日本" and "—dash" all contain them. A byte-wise rule would reject real
//     vault names — 1Password names legitimately carry spaces, punctuation and
//     unicode, which is why this rejects control characters specifically rather
//     than "anything unusual".
//   - A rune scan alone. Ranging over a string decodes an invalid byte as
//     U+FFFD, so a raw 0x9b sent as a single byte passes a rune-only test.
//     That byte cannot reach AppleScript (sanitizeDisplay also ranges over
//     runes, so it emits U+FFFD too) — but it would mean the user approves a
//     dialog showing "�" while `op` receives the original byte. The approved
//     text and the used text must be the same string, so invalid UTF-8 is
//     rejected outright.
func IsOPURI(s string) bool {
	if !strings.HasPrefix(s, "op://") {
		return false
	}
	if len(s) > maxURI {
		return false
	}
	rest := strings.TrimPrefix(s, "op://")
	// SplitN with n=4 so an optional fourth segment (e.g. section) stays intact.
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}
