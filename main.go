// opx is a 1Password CLI wrapper that provides per-call biometric authorization.
// Each invocation:
//  1. Shows a confirmation dialog disclosing the requested URI(s) and calling process.
//  2. Runs "op read <uri>" — triggering a fresh biometric prompt.
//  3. On exit (success, failure, or signal), runs "op signout --all" to
//     invalidate the CLI session token, leaving no residual access.
//
// Usage:
//
//	opx <op://uri>
//	opx --env NAME=<op://uri> [--env NAME=<op://uri> ...]
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/bestdan/opx/internal/caller"
	"github.com/bestdan/opx/internal/oprunner"
	"github.com/bestdan/opx/internal/prompt"
	"github.com/bestdan/opx/internal/shellquote"
	"github.com/bestdan/opx/internal/uri"
)

// Exit codes.
//
// exitDenied is reserved for "user said no" — the dialog was denied, timed
// out, or no UI was available. This is distinct from exitOpFail (the tool
// itself broke) so callers can branch on user intent vs. infrastructure
// error even when stderr is silent.
const (
	exitSuccess = 0
	exitOpFail  = 1
	exitUsage   = 2
	exitDenied  = 3
)

// version is set at build time via `-ldflags "-X main.version=..."` (see
// Makefile). The default makes `go run .` and unstripped builds report
// something useful instead of an empty string.
var version = "dev"

// diag is the destination for diagnostic stderr output. It defaults to
// io.Discard (silent) and is flipped to os.Stderr when --verbose / OPX_VERBOSE
// is set. Subprocess stderr (op, osascript, zenity) and our own logging both
// route through this writer. main() is the only place that mutates it.
//
// Set-once-in-main, then read-only for the rest of execution — no races.
var diag io.Writer = io.Discard

// diagf writes a verbose-only diagnostic line. No-op in quiet mode because
// diag is io.Discard.
func diagf(format string, a ...any) { fmt.Fprintf(diag, format, a...) }

// envNameRE matches POSIX-portable shell variable names.
var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func main() {
	if wantVersion(os.Args[1:]) {
		printVersion(os.Stdout, parseVersion(version), wantCheck(os.Args[1:]))
		os.Exit(exitSuccess)
	}
	verbose, args := extractVerbose(os.Args[1:])
	if verbose {
		diag = os.Stderr
	}
	runner := oprunner.NewWithStderr(diag)
	confirmer := prompt.NewWithStderr(diag)

	// Recover from panics so the session forget still runs. Panic output
	// always goes to os.Stderr (not diag) — a panic is an invariant
	// violation and silencing it would hide real bugs.
	defer func() {
		if r := recover(); r != nil {
			if err := runner.ForgetSession(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: op signout failed after panic: %v\n", err)
			}
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(exitOpFail)
		}
	}()
	os.Exit(run(args, runner, confirmer))
}

// wantVersion reports whether --version / -V appears anywhere in args.
// Checked before extractVerbose so the version path can short-circuit even
// when combined with other flags.
func wantVersion(args []string) bool {
	for _, a := range args {
		if a == "--version" || a == "-V" {
			return true
		}
	}
	return false
}

// wantCheck reports whether --check appears in args. Only meaningful when
// --version is also present; a bare --check is treated as an unknown flag
// by parseArgs and yields a usage error.
func wantCheck(args []string) bool {
	for _, a := range args {
		if a == "--check" {
			return true
		}
	}
	return false
}

// extractVerbose pulls --verbose / -v out of args and returns the filtered
// argument list along with the resulting verbosity. OPX_VERBOSE=1 in the
// environment is equivalent to passing --verbose.
func extractVerbose(args []string) (verbose bool, rest []string) {
	if v := os.Getenv("OPX_VERBOSE"); v != "" && v != "0" {
		verbose = true
	}
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == "--verbose" || a == "-v" {
			verbose = true
			continue
		}
		rest = append(rest, a)
	}
	return verbose, rest
}

// run is the main logic, separated from main() so it is testable.
//
// Usage output (parse errors, zero-args help) always prints to os.Stderr
// regardless of verbosity — without it a new user has no signal that they
// typoed a flag. Everything else routes through the diag writer.
func run(args []string, r oprunner.Runner, c prompt.Confirmer) int {
	bindings, envMode, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage error: %v\n", err)
		printUsage()
		return exitUsage
	}
	if len(bindings) == 0 {
		printUsage()
		return exitUsage
	}
	return confirmAndRead(bindings, envMode, r, c)
}

// parseArgs accepts either a single positional op:// URI or one or more
// --env NAME=op://... pairs.  The two modes cannot be mixed.  envMode is true
// when at least one --env flag was provided.
func parseArgs(args []string) (bindings []prompt.Binding, envMode bool, err error) {
	var positional []string
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case a == "--env":
			if i+1 >= len(args) {
				return nil, false, fmt.Errorf("--env requires a NAME=op://... argument")
			}
			i++
			b, err := parseEnvPair(args[i])
			if err != nil {
				return nil, false, err
			}
			if seen[b.Name] {
				return nil, false, fmt.Errorf("--env: duplicate name %q", b.Name)
			}
			seen[b.Name] = true
			bindings = append(bindings, b)
			envMode = true
		case strings.HasPrefix(a, "--env="):
			b, err := parseEnvPair(strings.TrimPrefix(a, "--env="))
			if err != nil {
				return nil, false, err
			}
			if seen[b.Name] {
				return nil, false, fmt.Errorf("--env: duplicate name %q", b.Name)
			}
			seen[b.Name] = true
			bindings = append(bindings, b)
			envMode = true
		case strings.HasPrefix(a, "-"):
			return nil, false, fmt.Errorf("unknown flag %q", a)
		default:
			positional = append(positional, a)
		}
	}

	if envMode && len(positional) > 0 {
		return nil, false, fmt.Errorf("cannot mix --env with positional URIs")
	}
	if envMode {
		return bindings, true, nil
	}

	switch len(positional) {
	case 0:
		return nil, false, nil
	case 1:
		if !uri.IsOPURI(positional[0]) {
			return nil, false, fmt.Errorf("%q is not a valid op:// URI", positional[0])
		}
		return []prompt.Binding{{URI: positional[0]}}, false, nil
	default:
		return nil, false, fmt.Errorf("too many positional arguments; use --env to read multiple secrets")
	}
}

// parseEnvPair splits a "NAME=op://..." string into a Binding, validating
// both halves before returning.
func parseEnvPair(pair string) (prompt.Binding, error) {
	eq := strings.IndexByte(pair, '=')
	if eq <= 0 {
		return prompt.Binding{}, fmt.Errorf("--env: expected NAME=op://..., got %q", pair)
	}
	name, val := pair[:eq], pair[eq+1:]
	if !envNameRE.MatchString(name) {
		return prompt.Binding{}, fmt.Errorf("--env: %q is not a valid shell variable name", name)
	}
	if !uri.IsOPURI(val) {
		return prompt.Binding{}, fmt.Errorf("--env %s: %q is not a valid op:// URI", name, val)
	}
	return prompt.Binding{Name: name, URI: val}, nil
}

// confirmAndRead shows the confirmation dialog covering every binding and,
// if approved, reads each secret atomically.
func confirmAndRead(bindings []prompt.Binding, envMode bool, r oprunner.Runner, c prompt.Confirmer) int {
	req := prompt.Request{
		Bindings: bindings,
		Caller:   caller.Name(),
	}
	if err := c.Confirm(req); err != nil {
		// Denial / dialog timeout / no UI all collapse to ErrDenied — that's
		// the user-intent path (exit 3). Anything else is treated as a tool
		// failure (exit 1). Today only ErrDenied flows here, but the split
		// makes the boundary explicit if Confirm grows new error modes.
		if errors.Is(err, prompt.ErrDenied) {
			diagf("denied (%v)\n", err)
			return exitDenied
		}
		diagf("confirm failed: %v\n", err)
		return exitOpFail
	}
	return readAndForget(bindings, envMode, r)
}

// readAndForget runs "op read" for each binding sequentially and always calls
// "op signout --all" before returning, regardless of whether reads
// succeeded, failed, or were interrupted by a signal.  Output is atomic: if
// any read fails, nothing is written to stdout.
func readAndForget(bindings []prompt.Binding, envMode bool, r oprunner.Runner) int {
	// signal.NotifyContext cancels ctx when SIGINT or SIGTERM arrives, which
	// causes exec.CommandContext to kill the op subprocess cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	secrets := make([][]byte, len(bindings))
	var readErr error
	for i, b := range bindings {
		s, err := r.ReadSecret(ctx, b.URI)
		if err != nil {
			readErr = err
			break
		}
		secrets[i] = s
	}

	// Always forget the session — even when interrupted or on error.
	if ferr := r.ForgetSession(); ferr != nil {
		diagf("warning: op signout failed: %v\n", ferr)
	}

	// If context was cancelled the user interrupted us; exit without output.
	if ctx.Err() != nil {
		diagf("interrupted\n")
		return exitOpFail
	}
	if readErr != nil {
		// op's own stderr was routed to diag (visible only in verbose mode);
		// add our own one-line summary so the user sees a clear top-level
		// reason without scanning op's diagnostic output.
		diagf("read failed: %v\n", readErr)
		return exitOpFail
	}

	out := renderOutput(bindings, secrets, envMode)
	if _, err := os.Stdout.Write(out); err != nil {
		diagf("error writing output: %v\n", err)
		return exitOpFail
	}
	diagf("%s\n", successSummary(bindings, envMode))
	return exitSuccess
}

// successSummary returns the verbose-mode confirmation line printed after a
// successful read. Format mirrors the dialog title: caller name, then either
// the URI (single mode) or the count plus bound variable list (--env mode).
func successSummary(bindings []prompt.Binding, envMode bool) string {
	c := caller.Name()
	if !envMode {
		return fmt.Sprintf("%q → %s", c, bindings[0].URI)
	}
	names := make([]string, len(bindings))
	for i, b := range bindings {
		names[i] = "$" + b.Name
	}
	noun := "secret"
	if len(bindings) != 1 {
		noun = "secrets"
	}
	return fmt.Sprintf("%q → %d %s (%s)", c, len(bindings), noun, strings.Join(names, ", "))
}

// renderOutput returns the bytes to write to stdout.  In single-URI legacy
// mode it returns the raw secret unchanged.  In --env mode it returns one
// `export NAME='value';\n` line per binding, with values shell-quoted.
func renderOutput(bindings []prompt.Binding, secrets [][]byte, envMode bool) []byte {
	if !envMode {
		return secrets[0]
	}
	var buf bytes.Buffer
	for i, b := range bindings {
		buf.WriteString("export ")
		buf.WriteString(b.Name)
		buf.WriteByte('=')
		buf.Write(shellquote.Quote(secrets[i]))
		buf.WriteString(";\n")
	}
	return buf.Bytes()
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: opx [--verbose] <op://uri>")
	fmt.Fprintln(os.Stderr, "       opx [--verbose] --env NAME=<op://uri> [--env NAME=<op://uri> ...]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  --verbose, -v    write diagnostics to stderr (default: silent)")
	fmt.Fprintln(os.Stderr, "                   OPX_VERBOSE=1 in the environment is equivalent")
	fmt.Fprintln(os.Stderr, "  --version, -V    print version and exit")
	fmt.Fprintln(os.Stderr, "                   add --check to compare against the latest GitHub release")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Exit codes: 0 ok, 1 op/tool failure, 2 usage error, 3 user denied")
}
