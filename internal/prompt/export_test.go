package prompt

// PangoEscapeForTest exposes the unexported pangoEscape helper to tests in
// the prompt_test package without making it part of the public API.
func PangoEscapeForTest(s string) string { return pangoEscape(s) }
