package oprunner_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/bestdan/opx/internal/oprunner"
)

// withEmptyPATH points PATH at an empty directory so the `op` binary can never
// be resolved during the test, no matter what the host has installed.
func withEmptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
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
	withEmptyPATH(t)
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
	r := oprunner.NewWithStderr(io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invocation so the subprocess is killed immediately

	out, err := r.ReadSecret(ctx, "op://V/I/f")
	if err == nil {
		t.Fatalf("expected error from cancelled context, got out=%q", out)
	}
}

func TestReadSecret_StderrForwarded(t *testing.T) {
	// We can't observe op's real stderr without op installed, but we can at
	// least confirm that NewWithStderr accepts a custom writer and the
	// resulting Runner does not panic when invoked.
	var buf bytes.Buffer
	r := oprunner.NewWithStderr(&buf)
	withEmptyPATH(t)

	if _, err := r.ReadSecret(context.Background(), "op://V/I/f"); err == nil {
		t.Fatal("expected error when op is missing")
	}
}

func TestForgetSession_OpMissing(t *testing.T) {
	withEmptyPATH(t)
	r := oprunner.NewWithStderr(io.Discard)

	if err := r.ForgetSession(); err == nil {
		t.Error("expected error from ForgetSession when op is missing, got nil")
	}
}
