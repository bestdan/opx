// Package allowlist loads and validates the opx allowlist config file, which
// maps user-friendly logical names to op:// URIs.
package allowlist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bestdan/opx/internal/uri"
)

// Allowlist maps logical names to op:// URIs.
type Allowlist struct {
	entries map[string]string
}

// Load reads the JSON allowlist config from path.  If path is empty the
// default location (~/.config/opx/allowlist.json) is used.
//
// Load validates:
//   - the file exists and is readable
//   - the file is not world-readable (mode & 0o004 == 0)
//   - every value is a syntactically valid op:// URI
func Load(path string) (*Allowlist, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		path = filepath.Join(home, ".config", "opx", "allowlist.json")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot stat allowlist file %s: %w", path, err)
	}
	if info.Mode().Perm()&0o004 != 0 {
		return nil, fmt.Errorf(
			"allowlist file %s is world-readable; fix with: chmod 600 %s",
			path, path,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read allowlist file: %w", err)
	}

	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("allowlist JSON parse error: %w", err)
	}

	for name, uriVal := range raw {
		if !uri.IsOPURI(uriVal) {
			return nil, fmt.Errorf(
				"allowlist entry %q has invalid op:// URI: %q", name, uriVal,
			)
		}
	}

	return &Allowlist{entries: raw}, nil
}

// Resolve returns the op:// URI mapped to name, and whether it was found.
func (a *Allowlist) Resolve(name string) (string, bool) {
	u, ok := a.entries[name]
	return u, ok
}
