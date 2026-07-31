package prompt

// ResolveHelperForTest exposes the unexported dialog-helper resolver to tests
// in the prompt_test package without making it part of the public API.
func ResolveHelperForTest(candidates []string) string { return resolveHelper(candidates) }

// OsascriptCandidatesForTest exposes the compiled-in helper location list so
// tests can assert it stays absolute.
func OsascriptCandidatesForTest() []string { return osascriptCandidates }
