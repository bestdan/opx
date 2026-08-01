// Package oprunner abstracts invocations of the 1Password op CLI binary.
package oprunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner abstracts op CLI invocations so the real binary can be swapped out
// in tests.
type Runner interface {
	// ReadSecret runs "op read <uri>" and returns the secret value.
	// The context may be cancelled to abort the operation.
	ReadSecret(ctx context.Context, uri string) ([]byte, error)
	// ForgetSession runs "op signout --all" to invalidate any cached session
	// token.  The caller should always invoke this, even after errors.
	ForgetSession() error
}

// opCandidates are the only locations `op` is accepted from, tried in order.
// Absolute and compiled in by construction: PATH belongs to the process opx is
// prompting the user about, so an `op` found there is the caller choosing the
// binary that both reads the secret and ends the session.
//
// The list covers every mainstream macOS install layout — Homebrew on Apple
// Silicon and Intel, a system package, and MacPorts — because the failure mode
// of rejecting a legitimate install is not a weaker opx but *no* opx, and a user
// who works around it by going back to raw `op read` has a cached session and no
// dialog at all. That is worse than anything this rule prevents.
//
// Deliberately absent: any path derived from the environment. `~/.local/bin/op`
// is a real install location and was considered, but there is no `~` at runtime
// — it comes from `$HOME`, which is set by the same caller that sets `PATH`. A
// hostile parent exporting HOME=/tmp/evil would make /tmp/evil/.local/bin/op an
// accepted candidate, which is a PATH allowlist wearing a different variable's
// name. A user whose only `op` lives there symlinks it into /usr/local/bin once.
var opCandidates = []string{
	"/opt/homebrew/bin/op", // Homebrew, Apple Silicon
	"/usr/local/bin/op",    // Homebrew on Intel; manual installs
	"/usr/bin/op",          // system package
	"/opt/local/bin/op",    // MacPorts
}

// resolveOp returns the first candidate that is a regular, executable file
// which is not group- or world-writable, or an error naming what it looked for.
//
// **What this does and does not buy, stated plainly, because the obvious
// reading is wrong.** It does *not* establish that the binary is genuine: the
// real candidate on this platform is usually /opt/homebrew/bin/op, and a
// Homebrew prefix is owned by the invoking user (verified: /opt/homebrew/bin is
// drwxrwxr-x, and the `op` in it is a user-owned symlink). Anything already
// running as the user can replace it. The SIP-sealed-volume argument that makes
// `resolveHelper` in internal/prompt and `resolveTool` in internal/caller safe
// does **not** transfer here, and this comment exists so nobody copies that
// reasoning across.
//
// What it buys is the whole of the finding it closes: the **caller's PATH no
// longer chooses** which binary reads the secret and ends the session. That
// distinction is the one that matters, because the two attacks are not
// comparable. Injecting a PATH entry — or dropping node_modules/.bin/op, which
// npm puts first on PATH for every script it runs — is ephemeral, targeted,
// invisible, and gone by the next invocation. Overwriting the machine's real
// `op` is persistent, global, and already total victory independent of opx,
// since every raw `op read` the user runs goes through it too. This collapses
// opx's exposure to the trust the user already places in their own install.
//
// The candidate is matched **before** any symlink resolution, and must stay that
// way: /opt/homebrew/bin/op is a symlink into a versioned Caskroom path
// (.../1password-cli/2.35.0/op, verified), so canonicalizing the candidate would
// make the rule break on every `op` upgrade.
//
// The mode check is the same one the other two screeners use, and has the same
// documented limit: it examines the candidate's own mode, never its directory,
// so a clean file in a writable directory is accepted even though it could be
// replaced by unlink-and-recreate. Here that limit is not theoretical — see
// above — which is exactly why the value of this function is stated in terms of
// PATH rather than in terms of file integrity.
func resolveOp() (string, error) {
	for _, path := range opCandidates {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		mode := info.Mode().Perm()
		if mode&0o111 == 0 || mode&0o022 != 0 {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("no usable op binary: looked for %s. "+
		"If op is installed elsewhere, link it into one of those locations "+
		"(e.g. sudo ln -s \"$(command -v op)\" /usr/local/bin/op) — opx will not "+
		"take op from PATH, since PATH is set by the process it is gating",
		strings.Join(opCandidates, ", "))
}

// New returns a Runner that delegates to the real op binary.  op's own stderr
// is forwarded to os.Stderr so the user sees biometric prompts and errors.
func New() Runner {
	return newRunner(os.Stderr)
}

// NewWithStderr returns a Runner that writes op's stderr to the given writer.
// This is primarily useful in tests.
func NewWithStderr(opStderr io.Writer) Runner {
	return newRunner(opStderr)
}

// newRunner resolves `op` once and records the result — path or error — on the
// runner.
//
// Resolving once is a correctness requirement, not an optimization: ReadSecret
// and ForgetSession must run the *same* binary. Two lookups leave a window in
// which the read is served by the real `op` and the signout by something else,
// which is precisely the split that makes a shim dangerous — the user pays one
// biometric prompt and the session is never invalidated.
//
// A resolution failure is recorded rather than returned because the constructors
// have no error in their signature; it surfaces from both methods, so every call
// path fails closed. In `opx run` that means ForgetSession fails and the child is
// not spawned (see AGENTS.md invariant 2).
func newRunner(opStderr io.Writer) *realRunner {
	path, err := resolveOp()
	return &realRunner{opStderr: opStderr, opPath: path, resolveErr: err}
}

type realRunner struct {
	opStderr io.Writer
	// opPath is the absolute path resolved once at construction; resolveErr is
	// non-nil when no trusted candidate was found, and is returned by every
	// method so no call path silently proceeds without op.
	opPath     string
	resolveErr error
}

func (r *realRunner) ReadSecret(ctx context.Context, uri string) ([]byte, error) {
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	cmd := exec.CommandContext(ctx, r.opPath, "read", uri)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = r.opStderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (r *realRunner) ForgetSession() error {
	if r.resolveErr != nil {
		return r.resolveErr
	}
	cmd := exec.Command(r.opPath, "signout", "--all")
	cmd.Stderr = r.opStderr
	cmd.Stdout = io.Discard
	return cmd.Run()
}
