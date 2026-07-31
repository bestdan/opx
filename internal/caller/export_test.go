package caller

import "testing"

// PSPathForTest exposes the unexported ps resolver to tests in the
// caller_test package without making it part of the public API.
func PSPathForTest(candidates []string) string { return psPath(candidates) }

// PSCandidatesForTest exposes the compiled-in ps candidate list so tests can
// assert its shape without hardcoding it.
func PSCandidatesForTest() []string { return psCandidates }

// PSEnvForTest exposes the environment both ps invocations run with.
func PSEnvForTest() []string { return psEnv }

// WithPSCandidatesForTest swaps the compiled-in ps candidate list for the
// duration of the test, restoring it afterwards. It is the only way to
// exercise the no-resolvable-ps degradation on a machine that has one.
func WithPSCandidatesForTest(t *testing.T, candidates []string) {
	t.Helper()
	saved := psCandidates
	t.Cleanup(func() { psCandidates = saved })
	psCandidates = candidates
}

// IsUninterestingForTest exposes the unexported shell/terminal/multiplexer
// skip predicate to tests in the caller_test package without making it part
// of the public API.
func IsUninterestingForTest(comm string) bool { return isUninteresting(comm) }

// RenderArgvForTest exposes the unexported argv-rendering/path-shortening
// helper to tests in the caller_test package without making it part of the
// public API.
func RenderArgvForTest(argv []string) string { return renderAncestorArgv(argv) }

// MaxChildCommandForTest exposes the child-command display budget so tests
// can build an argv that straddles it without hardcoding the number.
func MaxChildCommandForTest() int { return maxChildCommand }

// TruncateForTest exposes the unexported truncation helper to tests in the
// caller_test package without making it part of the public API.
func TruncateForTest(s string, n int) string { return truncate(s, n) }

// DescribeArgvForTest exposes the unexported describeArgv helper — the core
// of Describe()'s rendering logic decoupled from real process ancestry — to
// tests in the caller_test package without making it part of the public API.
func DescribeArgvForTest(argv []string, aboveComms []string) string {
	return describeArgv(argv, aboveComms)
}
