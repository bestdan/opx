package oprunner

import (
	"io"
	"testing"
)

// ResolveOpForTest exposes the unexported candidate resolver to tests in the
// oprunner_test package without making it part of the public API.
func ResolveOpForTest() (string, error) { return resolveOp() }

// OpCandidatesForTest exposes the compiled-in candidate list so tests can
// assert its shape — absolute, and free of environment-derived paths — without
// hardcoding it.
func OpCandidatesForTest() []string { return opCandidates }

// WithOpCandidatesForTest swaps the compiled-in candidate list for the duration
// of the test, restoring it afterwards. It is the only way to exercise
// resolution against a fake `op`: the real list is absolute by design, and a
// test that reached the host's actual /opt/homebrew/bin/op would trigger
// biometric prompts and depend on what the machine happens to have installed.
func WithOpCandidatesForTest(t *testing.T, candidates []string) {
	t.Helper()
	saved := opCandidates
	t.Cleanup(func() { opCandidates = saved })
	opCandidates = candidates
}

// ResolvedPathForTest reports the path a Runner recorded at construction, so a
// test can assert the resolution happened once and is not redone per call.
func ResolvedPathForTest(r Runner) string {
	rr, ok := r.(*realRunner)
	if !ok {
		return ""
	}
	return rr.opPath
}

// NewForTest constructs the real Runner against whatever candidate list is
// currently installed, returning the concrete type so tests can inspect it.
func NewForTest(stderr io.Writer) Runner { return newRunner(stderr) }
