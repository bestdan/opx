package prompt_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bestdan/opx/internal/prompt"
)

// fakeConfirmer is a test double for Confirmer.
type fakeConfirmer struct {
	err error
}

func (f *fakeConfirmer) Confirm(req prompt.Request) error {
	return f.err
}

// compile-time check.
var _ prompt.Confirmer = (*fakeConfirmer)(nil)

func req(uri string) prompt.Request {
	return prompt.Request{
		Bindings: []prompt.Binding{{URI: uri}},
		Caller:   "bash",
	}
}

func TestFakeConfirmer_Allow(t *testing.T) {
	fc := &fakeConfirmer{}
	if err := fc.Confirm(req("op://V/I/f")); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestFakeConfirmer_Deny(t *testing.T) {
	fc := &fakeConfirmer{err: prompt.ErrDenied}
	err := fc.Confirm(req("op://V/I/f"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, prompt.ErrDenied) {
		t.Errorf("expected errors.Is(err, ErrDenied) to be true, got %v", err)
	}
}

func TestNew_ReturnsConfirmer(t *testing.T) {
	c := prompt.New()
	if c == nil {
		t.Error("prompt.New() returned nil")
	}
}

// writeHelper creates a file at dir/name with the given permission bits and
// returns its path. Used to stand in for the osascript binary.
func writeHelper(t *testing.T, dir, name string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), perm); err != nil {
		t.Fatalf("writing helper: %v", err)
	}
	// WriteFile's perm is masked by umask; set the bits we actually asked for.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod helper: %v", err)
	}
	return path
}

// TestResolveHelper_Rejects covers every way a candidate fails to qualify. A
// dialog helper that exits 0 *is* an approval, so anything questionable must
// resolve to "" and let confirmDarwin deny.
func TestResolveHelper_Rejects(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "adir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases := []struct {
		name      string
		candidate string
	}{
		{"missing", filepath.Join(dir, "nope")},
		{"directory", subdir},
		{"not executable", writeHelper(t, dir, "noexec", 0o644)},
		{"group writable", writeHelper(t, dir, "grpw", 0o775)},
		{"world writable", writeHelper(t, dir, "worldw", 0o757)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prompt.ResolveHelperForTest([]string{tc.candidate}); got != "" {
				t.Errorf("resolveHelper accepted %s candidate: %q", tc.name, got)
			}
		})
	}
}

// TestResolveHelper_AcceptsCleanExecutable is the positive case: a regular
// 0755 file is what the packaged osascript looks like.
func TestResolveHelper_AcceptsCleanExecutable(t *testing.T) {
	dir := t.TempDir()
	path := writeHelper(t, dir, "osascript", 0o755)

	if got := prompt.ResolveHelperForTest([]string{path}); got != path {
		t.Errorf("resolveHelper() = %q, want %q", got, path)
	}
}

// TestResolveHelper_FirstMatchWins pins the preference order: candidates are
// tried in list order, and an unusable earlier entry is skipped rather than
// aborting the search.
func TestResolveHelper_FirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	bad := writeHelper(t, dir, "bad", 0o666)
	first := writeHelper(t, dir, "first", 0o755)
	second := writeHelper(t, dir, "second", 0o755)

	if got := prompt.ResolveHelperForTest([]string{first, second}); got != first {
		t.Errorf("resolveHelper() = %q, want first candidate %q", got, first)
	}
	if got := prompt.ResolveHelperForTest([]string{bad, second}); got != second {
		t.Errorf("resolveHelper() skipped past unusable candidate to %q, want %q", got, second)
	}
}

// TestResolveHelper_IgnoresPATH is the finding this whole change exists for:
// a helper reachable only through PATH must never be selected. PATH is set by
// the process opx is prompting the user about, so an "osascript" found there
// is the caller answering its own confirmation dialog.
func TestResolveHelper_IgnoresPATH(t *testing.T) {
	dir := t.TempDir()
	writeHelper(t, dir, "osascript", 0o755)
	t.Setenv("PATH", dir)

	if got := prompt.ResolveHelperForTest([]string{"/nonexistent/osascript"}); got != "" {
		t.Errorf("resolveHelper consulted PATH and returned %q, want \"\"", got)
	}
}

// TestResolveHelper_RequiresAbsoluteCandidates guards the compiled-in list:
// a bare name would reintroduce PATH resolution at the exec.Command call.
func TestResolveHelper_RequiresAbsoluteCandidates(t *testing.T) {
	candidates := prompt.OsascriptCandidatesForTest()
	if len(candidates) == 0 {
		t.Fatal("candidate list is empty")
	}
	for _, c := range candidates {
		if !filepath.IsAbs(c) {
			t.Errorf("candidate %q is not absolute", c)
		}
	}
}
