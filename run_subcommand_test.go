package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/prompt"
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
var _ = func() prompt.Confirmer { return allow() }
