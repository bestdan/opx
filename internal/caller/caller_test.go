package caller_test

import (
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/caller"
)

func TestName_NonEmpty(t *testing.T) {
	name := caller.Name()
	if name == "" {
		t.Error("caller.Name() returned empty string, want a process name")
	}
}

func TestName_NoSlash(t *testing.T) {
	// The returned name must be a plain executable name, not a path.
	name := caller.Name()
	if strings.Contains(name, "/") {
		t.Errorf("caller.Name() = %q, must not contain a path separator", name)
	}
}
