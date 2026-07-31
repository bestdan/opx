package prompt

// ResolveHelperForTest exposes the unexported dialog-helper resolver to tests
// in the prompt_test package without making it part of the public API.
func ResolveHelperForTest(candidates []string) string { return resolveHelper(candidates) }

// OsascriptCandidatesForTest and DefaultsCandidatesForTest expose the
// compiled-in helper location lists so tests can assert they stay absolute.
func OsascriptCandidatesForTest() []string { return osascriptCandidates }
func DefaultsCandidatesForTest() []string  { return defaultsCandidates }
