// Package spawn abstracts launching the child process in `opx run`.
//
// The interface exists so tests can verify the env vars passed to the child,
// and the chosen exit-code propagation, without ever exec-ing a real process.
package spawn

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// Spawner runs a child command and returns its exit code.
//
// argv[0] is the program to run; argv[1:] are its arguments. env is the
// complete environment for the child (callers are responsible for merging
// the parent environment if they want it inherited). Stdin/stdout/stderr
// are wired to os.Stdin/os.Stdout/os.Stderr by the real implementation.
//
// Spawn returns the child's exit code on normal exit. A non-nil error
// indicates that the child could not be started or was terminated by a
// signal — in those cases the returned exit code is unspecified and
// callers should treat the call as a tool failure.
type Spawner interface {
	Spawn(ctx context.Context, argv []string, env []string) (exitCode int, err error)
}

// New returns a Spawner backed by os/exec.
func New() Spawner { return realSpawner{} }

type realSpawner struct{}

func (realSpawner) Spawn(ctx context.Context, argv []string, env []string) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("spawn: empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// ExitError covers both normal non-zero exits and signal terminations.
		// ExitCode returns -1 for signal termination; surface that as a tool
		// failure rather than masquerading as a successful run.
		code := exitErr.ExitCode()
		if code < 0 {
			return 0, err
		}
		return code, nil
	}
	return 0, err
}
