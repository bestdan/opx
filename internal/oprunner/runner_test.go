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

	"github.com/bestdan/opx/internal/oprunner"
)

// withEmptyPATH points PATH at an empty directory so the `op` binary can never
// be resolved during the test, no matter what the host has installed.
func withEmptyPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// withFakeOp installs a shell-script `op` in a fresh temp dir and prepends that
// dir to PATH for the duration of the test. The script body is appended after
// the `#!/bin/sh` shebang. macOS and Linux runners both have /bin/sh; the
// project does not target Windows.
func withFakeOp(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "op")
	body := "#!/bin/sh\n" + script
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake op: %v", err)
	}
	t.Setenv("PATH", dir)
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
	// A real `op` binary must be resolvable so the LookPath check in
	// exec.Cmd.Start does not fire before the context check; otherwise we'd
	// get an exec.Error instead of context.Canceled. Use a fake op that
	// would sleep if it ever ran.
	withFakeOp(t, "sleep 30\n")
	r := oprunner.NewWithStderr(io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.ReadSecret(ctx, "op://V/I/f")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
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
	withEmptyPATH(t)
	r := oprunner.NewWithStderr(io.Discard)

	if err := r.ForgetSession(); err == nil {
		t.Error("expected error from ForgetSession when op is missing, got nil")
	}
}
