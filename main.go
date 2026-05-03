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
	"fmt"
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
const (
	exitSuccess = 0
	exitOpFail  = 1
	exitUsage   = 2
)

// envNameRE matches POSIX-portable shell variable names.
var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func main() {
	runner := oprunner.New()

	// Recover from panics so the session forget still runs.
	defer func() {
		if r := recover(); r != nil {
			if err := runner.ForgetSession(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: op signout failed after panic: %v\n", err)
			}
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(exitOpFail)
		}
	}()
	os.Exit(run(os.Args[1:], runner, prompt.New()))
}

// run is the main logic, separated from main() so it is testable.
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
		fmt.Fprintf(os.Stderr, "opx: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "warning: op signout failed: %v\n", ferr)
	}

	// If context was cancelled the user interrupted us; exit without output.
	if ctx.Err() != nil {
		return exitOpFail
	}
	if readErr != nil {
		// op's own error messages already went to stderr via cmd.Stderr.
		return exitOpFail
	}

	out := renderOutput(bindings, secrets, envMode)
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		return exitOpFail
	}
	return exitSuccess
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
	fmt.Fprintln(os.Stderr, "usage: opx <op://uri>")
	fmt.Fprintln(os.Stderr, "       opx --env NAME=<op://uri> [--env NAME=<op://uri> ...]")
}
