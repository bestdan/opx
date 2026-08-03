package caller

import "testing"

// ResolveToolForTest exposes the unexported helper-binary resolver to tests in
// the caller_test package without making it part of the public API.
func ResolveToolForTest(candidates []string) string { return resolveTool(candidates) }

// PSCandidatesForTest and LsofCandidatesForTest expose the compiled-in
// candidate lists so tests can assert their shape without hardcoding them.
func PSCandidatesForTest() []string   { return psCandidates }
func LsofCandidatesForTest() []string { return lsofCandidates }

// ToolEnvForTest exposes the environment every identity lookup runs with.
func ToolEnvForTest() []string { return toolEnv }

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
func DescribeArgvForTest(exe string, argv []string, aboveComms []string) string {
	return describeArgv(exe, argv, aboveComms)
}

// IdentityForTest builds the Identity a subject process would produce, without
// a live process tree. It is the whole rendering path Current() runs — name
// selection, anomaly classification, and detail line — so tests can assert
// what the dialog says about a process at a given path.
func IdentityForTest(comm, exe string, argv []string, aboveComms []string) Identity {
	subject := process{pid: 1, comm: comm, exe: exe, argv: argv}
	chain := []process{subject}
	for i, c := range aboveComms {
		chain = append(chain, process{pid: i + 2, comm: c})
	}
	return identityOf(subject, chain, 0)
}

// ProcessForTest describes one ancestor of opx for IdentityFromChainForTest.
type ProcessForTest struct {
	Comm string   // what `ps -o comm=` reports — self-asserted
	Exe  string   // what the kernel reports; "" when unreadable
	Argv []string // only ever consulted for the subject
}

// IdentityFromChainForTest runs the whole selection-and-rendering path over a
// synthetic ancestor chain, ordered nearest-to-opx first. It is what Current()
// calls once the live walk is done, so a test can assert which process the
// dialog names — and what it discloses walking past — without a process tree.
func IdentityFromChainForTest(specs []ProcessForTest) Identity {
	chain := make([]process, len(specs))
	for i, s := range specs {
		chain[i] = process{pid: i + 1, comm: s.Comm, exe: s.Exe, argv: s.Argv}
	}
	return identityFromChain(chain)
}

// SkippableForTest exposes the subject-walk skip gate: uninteresting comm plus
// kernel corroboration, rather than comm alone.
func SkippableForTest(comm, exe string) bool {
	return process{comm: comm, exe: exe}.skippable()
}

// IsSIPSealedForTest exposes the through-line suppression predicate.
func IsSIPSealedForTest(path string) bool { return isSIPSealed(path) }

// IdentityNameForTest exposes the header-label choice between the verified
// executable path and the self-asserted comm.
func IdentityNameForTest(exe, comm string) string { return identityName(exe, comm) }

// ParseLsofTxtForTest exposes the lsof field-output parser, so the extraction
// of the kernel-reported executable path is testable without a real lsof.
func ParseLsofTxtForTest(out string) map[int]string { return parseLsofTxt(out) }

// ParsePPIDCommForTest exposes the `ps -o ppid=,comm=` line parser so the
// ppid/basename extraction is testable without a real ps.
func ParsePPIDCommForTest(out string) (ppid int, comm string, ok bool) {
	return parsePPIDComm(out)
}
