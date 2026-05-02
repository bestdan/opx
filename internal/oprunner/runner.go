// Package oprunner abstracts invocations of the 1Password op CLI binary.
package oprunner

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
)

// Runner abstracts op CLI invocations so the real binary can be swapped out
// in tests.
type Runner interface {
	// ReadSecret runs "op read <uri>" and returns the secret value.
	// The context may be cancelled to abort the operation.
	ReadSecret(ctx context.Context, uri string) ([]byte, error)
	// ForgetSession runs "op session forget --all" to invalidate any cached
	// session token.  The caller should always invoke this, even after errors.
	ForgetSession() error
}

// New returns a Runner that delegates to the real op binary.  op's own stderr
// is forwarded to os.Stderr so the user sees biometric prompts and errors.
func New() Runner {
	return &realRunner{opStderr: os.Stderr}
}

// NewWithStderr returns a Runner that writes op's stderr to the given writer.
// This is primarily useful in tests.
func NewWithStderr(opStderr io.Writer) Runner {
	return &realRunner{opStderr: opStderr}
}

type realRunner struct {
	opStderr io.Writer
}

func (r *realRunner) ReadSecret(ctx context.Context, uri string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "op", "read", uri)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = r.opStderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (r *realRunner) ForgetSession() error {
	return exec.Command("op", "session", "forget", "--all").Run()
}
