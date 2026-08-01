package oprunner_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bestdan/opx/internal/oprunner"
)

// withNoOp points the candidate list at a path that does not exist, so `op` can
// never be resolved during the test regardless of what the host has installed.
func withNoOp(t *testing.T) {
	t.Helper()
	oprunner.WithOpCandidatesForTest(t, []string{filepath.Join(t.TempDir(), "op")})
}

// withFakeOp writes a shell-script `op` into a fresh temp dir and makes it the
// sole candidate for the duration of the test. The script body is appended
// after the `#!/bin/sh` shebang, which every macOS runner has.
//
// It installs a *candidate*, not a PATH entry: opx no longer consults PATH, so
// a PATH-based fake would leave these tests exercising nothing.
func withFakeOp(t *testing.T, script string) string {
	t.Helper()
	return withFakeOpMode(t, script, 0o755)
}

// withFakeOpMode is withFakeOp with an explicit file mode, for the cases that
// need a candidate the resolver must reject.
func withFakeOpMode(t *testing.T, script string, perm os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "op")
	body := "#!/bin/sh\n" + script
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatalf("write fake op: %v", err)
	}
	// WriteFile's perm is masked by umask; set the bits we actually asked for.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod fake op: %v", err)
	}
	oprunner.WithOpCandidatesForTest(t, []string{path})
	return path
}

func TestNew_ReturnsRunner(t *testing.T) {
	if r := oprunner.New(); r == nil {
		t.Fatal("oprunner.New() returned nil")
	}
}

func TestNewWithStderr_ReturnsRunner(t *testing.T) {
	if r := oprunner.NewWithStderr(io.Discard); r == nil {
		t.Fatal("oprunner.NewWithStderr() returned nil")
	}
}

func TestReadSecret_OpMissing(t *testing.T) {
	withNoOp(t)
	r := oprunner.NewWithStderr(io.Discard)

	out, err := r.ReadSecret(context.Background(), "op://V/I/f")
	if err == nil {
		t.Fatalf("expected error when op is missing, got out=%q", out)
	}
	if out != nil {
		t.Errorf("expected nil output on error, got %q", out)
	}
}

func TestReadSecret_CancelledContext(t *testing.T) {
	// A resolvable `op` is required so the resolution error does not fire
	// before the context check; otherwise we'd get the resolver's error
	// instead of context.Canceled. Use a fake op that would sleep if it ever
	// ran.
	withFakeOp(t, "sleep 30\n")
	r := oprunner.NewWithStderr(io.Discard)

	// Bound the test so a regression in cancellation propagation fails in
	// seconds rather than hanging until Go's default 10-minute test timeout.
	parent, cancelParent := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelParent()
	ctx, cancel := context.WithCancel(parent)
	cancel()

	_, err := r.ReadSecret(ctx, "op://V/I/f")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
	}
}

func TestReadSecret_Success(t *testing.T) {
	const wantSecret = "sekret-value"
	withFakeOp(t, "printf '"+wantSecret+"'\n")

	r := oprunner.NewWithStderr(io.Discard)
	out, err := r.ReadSecret(context.Background(), "op://V/I/f")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != wantSecret {
		t.Errorf("ReadSecret = %q, want %q", out, wantSecret)
	}
}

func TestReadSecret_StderrForwarded(t *testing.T) {
	// Fake op writes a known message to its stderr and exits non-zero. The
	// real Runner is expected to forward that stderr to the writer passed to
	// NewWithStderr.
	const wantMsg = "fake op stderr marker"
	withFakeOp(t, "echo '"+wantMsg+"' >&2\nexit 1\n")

	var buf bytes.Buffer
	r := oprunner.NewWithStderr(&buf)

	if _, err := r.ReadSecret(context.Background(), "op://V/I/f"); err == nil {
		t.Fatal("expected error from failing op")
	}
	if !strings.Contains(buf.String(), wantMsg) {
		t.Errorf("stderr writer did not receive op's stderr; got %q", buf.String())
	}
}

func TestForgetSession_OpMissing(t *testing.T) {
	withNoOp(t)
	r := oprunner.NewWithStderr(io.Discard)

	if err := r.ForgetSession(); err == nil {
		t.Error("expected error from ForgetSession when op is missing, got nil")
	}
}

// TestResolveOp_Rejects covers every way a candidate fails to qualify. `op` is
// the binary that both reads the secret and ends the session, so an
// unqualified candidate must resolve to an error rather than being used.
func TestResolveOp_Rejects(t *testing.T) {
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
		{"not executable", writeOpFile(t, dir, "noexec", 0o644)},
		{"group writable", writeOpFile(t, dir, "grpw", 0o775)},
		{"world writable", writeOpFile(t, dir, "worldw", 0o757)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oprunner.WithOpCandidatesForTest(t, []string{tc.candidate})
			got, err := oprunner.ResolveOpForTest()
			if err == nil {
				t.Errorf("resolveOp accepted %s candidate: %q", tc.name, got)
			}
		})
	}
}

// writeOpFile creates a file at dir/name with the given permission bits and
// returns its path. Used to stand in for the op binary.
func writeOpFile(t *testing.T, dir, name string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), perm); err != nil {
		t.Fatalf("write op: %v", err)
	}
	// WriteFile's perm is masked by umask; set the bits we actually asked for.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod op: %v", err)
	}
	return path
}

// TestResolveOp_FirstMatchWins pins the preference order: candidates are tried
// in list order, and an unusable earlier entry is skipped rather than aborting
// the search.
func TestResolveOp_FirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	bad := writeOpFile(t, dir, "bad", 0o666)
	first := writeOpFile(t, dir, "first", 0o755)
	second := writeOpFile(t, dir, "second", 0o755)

	oprunner.WithOpCandidatesForTest(t, []string{first, second})
	if got, _ := oprunner.ResolveOpForTest(); got != first {
		t.Errorf("resolveOp() = %q, want first candidate %q", got, first)
	}

	oprunner.WithOpCandidatesForTest(t, []string{bad, second})
	if got, _ := oprunner.ResolveOpForTest(); got != second {
		t.Errorf("resolveOp() skipped past unusable candidate to %q, want %q", got, second)
	}
}

// TestResolveOp_IgnoresPATH is the finding this change exists for. `op` must
// never come from PATH: PATH is set by the process opx is gating, and a shim
// that forwards `read` to the real op but no-ops on `signout` leaves the
// biometric-unlocked session live while opx reports success. npm makes this
// free — it puts node_modules/.bin first on PATH for every script it runs.
func TestResolveOp_IgnoresPATH(t *testing.T) {
	dir := t.TempDir()
	nm := filepath.Join(dir, "node_modules", ".bin")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeOpFile(t, nm, "op", 0o755)
	t.Setenv("PATH", nm)

	oprunner.WithOpCandidatesForTest(t, []string{filepath.Join(dir, "absent", "op")})
	if got, err := oprunner.ResolveOpForTest(); err == nil {
		t.Errorf("resolveOp consulted PATH and returned %q, want an error", got)
	}
}

// TestOpCandidates_AreAbsoluteAndEnvironmentFree guards the compiled-in list.
// A bare name would reintroduce PATH resolution at the exec.Command call, and a
// path built from $HOME would be no better: $HOME is set by the same caller
// that sets PATH, so a HOME-derived candidate is a PATH allowlist under another
// variable's name.
func TestOpCandidates_AreAbsoluteAndEnvironmentFree(t *testing.T) {
	candidates := oprunner.OpCandidatesForTest()
	if len(candidates) == 0 {
		t.Fatal("candidate list is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	for _, c := range candidates {
		if !filepath.IsAbs(c) {
			t.Errorf("candidate %q is not absolute", c)
		}
		if strings.ContainsAny(c, "$~") {
			t.Errorf("candidate %q looks environment-derived", c)
		}
		if strings.HasPrefix(c, home+string(filepath.Separator)) {
			t.Errorf("candidate %q is under $HOME, which the caller controls", c)
		}
	}
}

// TestResolveOp_ErrorNamesTheAcceptedLocations: rejecting a legitimate install
// must not read as "opx is broken". The failure mode that matters is the user
// giving up and going back to raw `op read`, which has a cached session and no
// dialog at all — so the error has to say where opx looked and how to fix it.
func TestResolveOp_ErrorNamesTheAcceptedLocations(t *testing.T) {
	// Two absent candidates rather than the compiled-in list, since on a
	// machine with op installed the real list resolves successfully. The
	// assertion is that the message names whatever it looked for — all of it,
	// not just the first — plus the remedy.
	dir := t.TempDir()
	absent := []string{filepath.Join(dir, "a", "op"), filepath.Join(dir, "b", "op")}
	oprunner.WithOpCandidatesForTest(t, absent)

	_, err := oprunner.ResolveOpForTest()
	if err == nil {
		t.Fatal("expected an error when no candidate qualifies")
	}
	for _, want := range append(absent, "ln -s") {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestRunner_ResolvesOnceAtConstruction is a security property, not an
// optimization. ReadSecret and ForgetSession must run the same binary: two
// lookups leave a window where the read is served by the real op and the
// signout by something else, which is exactly the split that makes a shim
// dangerous — one biometric prompt paid, session never invalidated.
func TestRunner_ResolvesOnceAtConstruction(t *testing.T) {
	first := withFakeOp(t, "exit 0\n")
	r := oprunner.NewForTest(io.Discard)
	if got := oprunner.ResolvedPathForTest(r); got != first {
		t.Fatalf("resolved path = %q, want %q", got, first)
	}

	// Swap the candidate list afterwards: a runner that re-resolved per call
	// would pick this up, which is the divergence being ruled out.
	second := writeOpFile(t, t.TempDir(), "op", 0o755)
	oprunner.WithOpCandidatesForTest(t, []string{second})

	if got := oprunner.ResolvedPathForTest(r); got != first {
		t.Errorf("resolved path changed to %q after construction, want %q — read and signout must run the same binary", got, first)
	}
}

// TestRunner_ResolutionFailureSurfacesFromBothMethods: the constructors have no
// error in their signature, so a resolution failure is recorded and returned by
// every method. Both matter, and ForgetSession matters more: in `opx run` its
// failure is what stops the child being spawned (AGENTS.md invariant 2).
func TestRunner_ResolutionFailureSurfacesFromBothMethods(t *testing.T) {
	withNoOp(t)
	r := oprunner.NewForTest(io.Discard)

	if _, err := r.ReadSecret(context.Background(), "op://V/I/f"); err == nil {
		t.Error("ReadSecret returned nil error with no usable op")
	}
	if err := r.ForgetSession(); err == nil {
		t.Error("ForgetSession returned nil error with no usable op")
	}
}
