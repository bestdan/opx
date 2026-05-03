package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/oprunner"
	"github.com/bestdan/opx/internal/prompt"
)

// fakeRunner implements oprunner.Runner for tests.
type fakeRunner struct {
	// secrets maps URI → bytes returned by ReadSecret.  If nil, every read
	// returns the legacy single-secret value.
	secrets      map[string][]byte
	secret       []byte
	readErr      error
	failOnURI    string // if non-empty, ReadSecret(URI) returns errReadFail
	forgetErr    error
	forgetCalled int
	readCalls    []string
	// If cancelOnRead is true, ReadSecret will behave as though the context
	// was already cancelled (simulating a signal).
	cancelOnRead bool
}

var errReadFail = errors.New("simulated read failure")

func (f *fakeRunner) ReadSecret(ctx context.Context, uri string) ([]byte, error) {
	f.readCalls = append(f.readCalls, uri)
	if f.cancelOnRead {
		return nil, context.Canceled
	}
	if f.failOnURI != "" && uri == f.failOnURI {
		return nil, errReadFail
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.secrets != nil {
		return f.secrets[uri], nil
	}
	return f.secret, nil
}

func (f *fakeRunner) ForgetSession() error {
	f.forgetCalled++
	return f.forgetErr
}

// compile-time check that fakeRunner satisfies the interface.
var _ oprunner.Runner = (*fakeRunner)(nil)

// fakeConfirmer implements prompt.Confirmer for tests.
type fakeConfirmer struct {
	err          error
	calls        int
	lastRequest  prompt.Request
	allRequested []prompt.Request
}

func (f *fakeConfirmer) Confirm(req prompt.Request) error {
	f.calls++
	f.lastRequest = req
	f.allRequested = append(f.allRequested, req)
	return f.err
}

// compile-time check that fakeConfirmer satisfies the interface.
var _ prompt.Confirmer = (*fakeConfirmer)(nil)

// allow is a shorthand for a confirmer that always grants access.
func allow() *fakeConfirmer { return &fakeConfirmer{} }

// deny is a shorthand for a confirmer that always denies access.
func deny() *fakeConfirmer { return &fakeConfirmer{err: prompt.ErrDenied} }

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// was written. The restore and pipe close are deferred so a panic or
// runtime.Goexit (e.g. from t.Fatal* inside fn) doesn't strand the reader
// goroutine or leave os.Stdout pointing at a closed pipe for later tests.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	os.Stdout = w
	// Restore + close on every exit path, including panic / runtime.Goexit.
	// On the happy path the close happens here first so the goroutine sees EOF;
	// the deferred close is then a harmless no-op.
	defer func() {
		os.Stdout = old
		_ = w.Close()
		_ = r.Close()
	}()
	fn()
	os.Stdout = old
	_ = w.Close()
	return <-done
}

func TestRun_NoArgs(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{}, fr, allow())
	if code != exitUsage {
		t.Errorf("got exit code %d, want %d", code, exitUsage)
	}
}

func TestRun_Success(t *testing.T) {
	fr := &fakeRunner{secret: []byte("supersecret")}
	out := captureStdout(t, func() {
		code := run([]string{"op://Vault/Item/field"}, fr, allow())
		if code != exitSuccess {
			t.Errorf("got exit code %d, want %d", code, exitSuccess)
		}
	})
	if out != "supersecret" {
		t.Errorf("stdout = %q, want raw secret unchanged in legacy mode", out)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRun_OpFailure(t *testing.T) {
	fr := &fakeRunner{readErr: errors.New("authentication failed")}
	code := run([]string{"op://Vault/Item/field"}, fr, allow())
	if code != exitOpFail {
		t.Errorf("got exit code %d, want %d", code, exitOpFail)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRun_InvalidURI(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{"not-a-uri"}, fr, allow())
	if code != exitUsage {
		t.Errorf("got exit code %d, want %d", code, exitUsage)
	}
	// ForgetSession should NOT be called for a usage error (we never started op).
	if fr.forgetCalled != 0 {
		t.Errorf("ForgetSession called %d times, want 0", fr.forgetCalled)
	}
}

func TestRun_ForgetCalledOnReadError(t *testing.T) {
	fr := &fakeRunner{readErr: errors.New("biometric failed")}
	code := run([]string{"op://V/I/f"}, fr, allow())
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

	code := captureStdoutCode(t, func() int {
		return run([]string{"op://V/I/f"}, fr, allow())
	})

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

// captureStdoutCode is captureStdout for code-returning fns; it discards the
// stdout content because we only need the exit code in the caller.
func captureStdoutCode(t *testing.T, fn func() int) int {
	t.Helper()
	var code int
	_ = captureStdout(t, func() { code = fn() })
	return code
}

func TestRun_ConfirmDeny_NoOpRead(t *testing.T) {
	// When the user denies the dialog, op should never be called and the exit
	// code must be exitOpFail.
	fr := &fakeRunner{secret: []byte("should-not-be-returned")}
	code := run([]string{"op://V/I/f"}, fr, deny())
	if code != exitOpFail {
		t.Errorf("got exit code %d, want %d after deny", code, exitOpFail)
	}
	if len(fr.readCalls) != 0 {
		t.Errorf("ReadSecret called %d times after deny; want 0", len(fr.readCalls))
	}
	if fr.forgetCalled != 0 {
		t.Errorf("ForgetSession called %d times after deny; want 0 (op was never started)", fr.forgetCalled)
	}
}

func TestRun_ConfirmCalledWithCorrectURI(t *testing.T) {
	const wantURI = "op://MyVault/MyItem/password"
	fr := &fakeRunner{secret: []byte("val")}
	fc := allow()
	_ = captureStdoutCode(t, func() int { return run([]string{wantURI}, fr, fc) })
	if fc.calls != 1 {
		t.Fatalf("Confirm calls = %d, want 1", fc.calls)
	}
	if len(fc.lastRequest.Bindings) != 1 || fc.lastRequest.Bindings[0].URI != wantURI {
		t.Errorf("Confirm bindings = %+v, want single URI %s", fc.lastRequest.Bindings, wantURI)
	}
	if fc.lastRequest.Bindings[0].Name != "" {
		t.Errorf("legacy mode binding name = %q, want empty", fc.lastRequest.Bindings[0].Name)
	}
}

// --- batch / --env mode ---

func TestRun_EnvMultipleSucceeds(t *testing.T) {
	fr := &fakeRunner{secrets: map[string][]byte{
		"op://V/A/f": []byte("alpha"),
		"op://V/B/f": []byte("beta"),
		"op://V/C/f": []byte("gamma"),
	}}
	fc := allow()
	out := captureStdout(t, func() {
		code := run([]string{
			"--env", "A=op://V/A/f",
			"--env", "B=op://V/B/f",
			"--env", "C=op://V/C/f",
		}, fr, fc)
		if code != exitSuccess {
			t.Errorf("exit code = %d, want %d", code, exitSuccess)
		}
	})
	want := "export A='alpha';\nexport B='beta';\nexport C='gamma';\n"
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
	if fc.calls != 1 {
		t.Errorf("Confirm calls = %d, want 1 (one approval covers the batch)", fc.calls)
	}
	if len(fc.lastRequest.Bindings) != 3 {
		t.Errorf("Confirm bindings count = %d, want 3", len(fc.lastRequest.Bindings))
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
	if len(fr.readCalls) != 3 {
		t.Errorf("ReadSecret calls = %d, want 3", len(fr.readCalls))
	}
}

func TestRun_EnvSecondReadFails_AtomicNoOutput(t *testing.T) {
	fr := &fakeRunner{
		secrets: map[string][]byte{
			"op://V/A/f": []byte("alpha"),
			"op://V/C/f": []byte("gamma"),
		},
		failOnURI: "op://V/B/f",
	}
	out := captureStdout(t, func() {
		code := run([]string{
			"--env", "A=op://V/A/f",
			"--env", "B=op://V/B/f",
			"--env", "C=op://V/C/f",
		}, fr, allow())
		if code != exitOpFail {
			t.Errorf("exit code = %d, want %d", code, exitOpFail)
		}
	})
	if out != "" {
		t.Errorf("stdout = %q, want empty (atomic: one failure → no output)", out)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
	// We should have stopped after the failed read; C should not have been attempted.
	if len(fr.readCalls) != 2 {
		t.Errorf("ReadSecret calls = %d (%v), want 2 — must stop on first failure", len(fr.readCalls), fr.readCalls)
	}
}

func TestRun_EnvDeniedNoReads(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{
		"--env", "A=op://V/A/f",
		"--env", "B=op://V/B/f",
	}, fr, deny())
	if code != exitOpFail {
		t.Errorf("exit code = %d, want %d", code, exitOpFail)
	}
	if len(fr.readCalls) != 0 {
		t.Errorf("ReadSecret called %d times after deny; want 0", len(fr.readCalls))
	}
	if fr.forgetCalled != 0 {
		t.Errorf("ForgetSession called %d times after deny; want 0", fr.forgetCalled)
	}
}

func TestRun_EnvSIGINT(t *testing.T) {
	fr := &fakeRunner{cancelOnRead: true}
	out := captureStdout(t, func() {
		code := run([]string{
			"--env", "A=op://V/A/f",
			"--env", "B=op://V/B/f",
		}, fr, allow())
		if code != exitOpFail {
			t.Errorf("exit code = %d, want %d", code, exitOpFail)
		}
	})
	if out != "" {
		t.Errorf("stdout = %q, want empty after cancellation", out)
	}
	if fr.forgetCalled != 1 {
		t.Errorf("ForgetSession called %d times, want 1", fr.forgetCalled)
	}
}

func TestRun_EnvDuplicateName(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{
		"--env", "FOO=op://V/A/f",
		"--env", "FOO=op://V/B/f",
	}, fr, allow())
	if code != exitUsage {
		t.Errorf("exit code = %d, want %d (duplicate name)", code, exitUsage)
	}
	if fr.forgetCalled != 0 {
		t.Errorf("ForgetSession should not run on usage error; got %d", fr.forgetCalled)
	}
}

func TestRun_EnvInvalidName(t *testing.T) {
	cases := []string{
		"1FOO=op://V/I/f",
		"FOO-BAR=op://V/I/f",
		"=op://V/I/f",
	}
	for _, pair := range cases {
		t.Run(pair, func(t *testing.T) {
			fr := &fakeRunner{}
			code := run([]string{"--env", pair}, fr, allow())
			if code != exitUsage {
				t.Errorf("exit code = %d, want %d", code, exitUsage)
			}
		})
	}
}

func TestRun_EnvInvalidURI(t *testing.T) {
	fr := &fakeRunner{}
	fc := allow()
	code := run([]string{"--env", "FOO=https://evil.example/x"}, fr, fc)
	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if fc.calls != 0 {
		t.Errorf("Confirm called %d times; want 0 (validation must fail before prompting)", fc.calls)
	}
}

func TestRun_EnvMixedWithPositional(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{"--env", "FOO=op://V/A/f", "op://V/B/f"}, fr, allow())
	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
}

func TestRun_TooManyPositional(t *testing.T) {
	fr := &fakeRunner{}
	code := run([]string{"op://V/A/f", "op://V/B/f"}, fr, allow())
	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
}

func TestRun_EnvShellQuoting(t *testing.T) {
	// Secret containing a single quote must round-trip through eval safely.
	fr := &fakeRunner{secrets: map[string][]byte{
		"op://V/A/f": []byte("it's \"tricky\"\n$x"),
	}}
	out := captureStdout(t, func() {
		code := run([]string{"--env", "FOO=op://V/A/f"}, fr, allow())
		if code != exitSuccess {
			t.Errorf("exit code = %d, want %d", code, exitSuccess)
		}
	})
	want := "export FOO='it'\\''s \"tricky\"\n$x';\n"
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
}

func TestRun_EnvEqualsForm(t *testing.T) {
	// --env=NAME=URI should be accepted in addition to --env NAME=URI.
	fr := &fakeRunner{secrets: map[string][]byte{"op://V/A/f": []byte("v")}}
	out := captureStdout(t, func() {
		code := run([]string{"--env=FOO=op://V/A/f"}, fr, allow())
		if code != exitSuccess {
			t.Errorf("exit code = %d, want %d", code, exitSuccess)
		}
	})
	if out != "export FOO='v';\n" {
		t.Errorf("stdout = %q, want export FOO='v';\\n", out)
	}
}
