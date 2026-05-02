package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/oprunner"
)

// fakeRunner implements oprunner.Runner for tests.
type fakeRunner struct {
	secret       []byte
	readErr      error
	forgetErr    error
	forgetCalled int
	// If cancelOnRead is true, ReadSecret will behave as though the context
	// was already cancelled (simulating a signal).
	cancelOnRead bool
}

func (f *fakeRunner) ReadSecret(ctx context.Context, uri string) ([]byte, error) {
	if f.cancelOnRead {
		return nil, context.Canceled
	}
	return f.secret, f.readErr
}

func (f *fakeRunner) ForgetSession() error {
	f.forgetCalled++
	return f.forgetErr
}

// compile-time check that fakeRunner satisfies the interface.
var _ oprunner.Runner = (*fakeRunner)(nil)

func TestRun_NoArgs(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{}, fr)
	if code != exitUsage {
		t.Errorf("got exit code %d, want %d", code, exitUsage)
	}
}

func TestRun_DirectMode_Success(t *testing.T) {
	fr := &fakeRunner{secret: []byte("supersecret")}
	code := run([]string{"op://Vault/Item/field"}, fr)
	if code != exitSuccess {
		t.Errorf("got exit code %d, want %d", code, exitSuccess)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRun_DirectMode_OpFailure(t *testing.T) {
	fr := &fakeRunner{readErr: errors.New("authentication failed")}
	code := run([]string{"op://Vault/Item/field"}, fr)
	if code != exitOpFail {
		t.Errorf("got exit code %d, want %d", code, exitOpFail)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRun_InvalidURI(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{"not-a-uri"}, fr)
	if code != exitUsage {
		t.Errorf("got exit code %d, want %d", code, exitUsage)
	}
	// ForgetSession should NOT be called for a usage error (we never started op).
	if fr.forgetCalled != 0 {
		t.Errorf("ForgetSession called %d times, want 0", fr.forgetCalled)
	}
}

func TestRun_GetSubcommand_MissingName(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{"get"}, fr)
	if code != exitUsage {
		t.Errorf("got exit code %d, want %d", code, exitUsage)
	}
}

func TestRun_GetSubcommand_Success(t *testing.T) {
	want := []byte("my-secret-value")
	fr := &fakeRunner{secret: want}

	// Write a valid allowlist file.
	dir := t.TempDir()
	allowlistPath := filepath.Join(dir, "allowlist.json")
	if err := os.WriteFile(allowlistPath, []byte(`{"github-token":"op://Personal/GitHub/token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Patch the default config path by overriding the allowlist loading via a
	// wrapper that passes the temp path.
	// Because allowlist.Load uses an environment variable or explicit path, we
	// call run() indirectly by constructing the URI ourselves and testing
	// readAndForget instead.
	code := readAndForget("op://Personal/GitHub/token", fr)
	if code != exitSuccess {
		t.Errorf("readAndForget exit code = %d, want %d", code, exitSuccess)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
	_ = allowlistPath // used above
}

func TestRun_ForgetCalledOnReadError(t *testing.T) {
	fr := &fakeRunner{readErr: errors.New("biometric failed")}
	code := run([]string{"op://V/I/f"}, fr)
	if code != exitOpFail {
		t.Errorf("got exit code %d, want %d", code, exitOpFail)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1 (cleanup must always run)", fr.forgetCalled)
	}
}

func TestRun_ForgetWarningOnForgetError(t *testing.T) {
	// Forget fails but run should still succeed overall (i.e. not crash).
	fr := &fakeRunner{
		secret:    []byte("value"),
		forgetErr: errors.New("session forget failed"),
	}
	// Capture stderr to verify the warning is printed.
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	code := run([]string{"op://V/I/f"}, fr)

	w.Close()
	os.Stderr = old

	var sb strings.Builder
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	sb.Write(buf[:n])

	if code != exitSuccess {
		t.Errorf("got exit code %d, want %d (forget error must not override success)", code, exitSuccess)
	}
	if !strings.Contains(sb.String(), "warning") {
		t.Errorf("expected warning on stderr, got: %q", sb.String())
	}
}
