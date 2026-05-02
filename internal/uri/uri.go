// Package uri provides helpers for validating 1Password op:// URIs.
package uri

import "strings"

// IsOPURI returns true if s is a syntactically valid op:// URI containing at
// least three non-empty path segments (vault/item/field).
func IsOPURI(s string) bool {
	if !strings.HasPrefix(s, "op://") {
		return false
	}
	rest := strings.TrimPrefix(s, "op://")
	// SplitN with n=4 so an optional fourth segment (e.g. section) stays intact.
	parts := strings.SplitN(rest, "/", 4)
	return len(parts) >= 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}
