// opx is a 1Password CLI wrapper that provides per-call biometric authorization.
// Each invocation:
//  1. Shows a confirmation dialog disclosing the requested URI and calling process.
//  2. Runs "op read <uri>" — triggering a fresh biometric prompt.
//  3. On exit (success, failure, or signal), runs "op session forget --all"
//     to invalidate the CLI session token, leaving no residual access.
//
// Usage:
//
//	opx <op://uri>         # direct mode
//	opx get <logical-name> # allowlist mode
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bestdan/opx/internal/allowlist"
	"github.com/bestdan/opx/internal/caller"
	"github.com/bestdan/opx/internal/oprunner"
	"github.com/bestdan/opx/internal/prompt"
	"github.com/bestdan/opx/internal/uri"
)

// Exit codes.
const (
	exitSuccess = 0
	exitOpFail  = 1
	exitUsage   = 2
	exitConfig  = 3
)

func main() {
	runner := oprunner.New()

	// Recover from panics so the session forget still runs.
	defer func() {
		if r := recover(); r != nil {
			if err := runner.ForgetSession(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: op session forget failed after panic: %v\n", err)
			}
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(exitOpFail)
		}
	}()
	os.Exit(run(os.Args[1:], os.Getenv("OPX_CONFIG"), runner, prompt.New()))
}

// run is the main logic, separated from main() so it is testable.
// configPath overrides the default allowlist location (~/.config/opx/allowlist.json)
// when non-empty; tests pass a temp path here.
func run(args []string, configPath string, r oprunner.Runner, c prompt.Confirmer) int {
	if len(args) == 0 {
		printUsage()
		return exitUsage
	}

	var opURI string

	switch {
	case args[0] == "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: opx get <name>")
			return exitUsage
		}
		al, err := allowlist.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "config error: %v\n", err)
			return exitConfig
		}
		resolved, ok := al.Resolve(args[1])
		if !ok {
			fmt.Fprintf(os.Stderr, "config error: name %q not found in allowlist\n", args[1])
			return exitConfig
		}
		opURI = resolved

	case uri.IsOPURI(args[0]):
		opURI = args[0]

	default:
		fmt.Fprintf(os.Stderr, "usage error: %q is not a valid op:// URI\n", args[0])
		printUsage()
		return exitUsage
	}

	return confirmAndRead(opURI, r, c)
}

// confirmAndRead shows the confirmation dialog and, if approved, reads the secret.
func confirmAndRead(uri string, r oprunner.Runner, c prompt.Confirmer) int {
	callerName := caller.Name()
	if err := c.Confirm(uri, callerName); err != nil {
		fmt.Fprintf(os.Stderr, "opx: %v\n", err)
		return exitOpFail
	}
	return readAndForget(uri, r)
}

// readAndForget runs "op read <uri>" and always calls "op session forget --all"
// before returning, regardless of whether the read succeeded, failed, or was
// interrupted by a signal.
func readAndForget(uri string, r oprunner.Runner) int {
	// signal.NotifyContext cancels ctx when SIGINT or SIGTERM arrives, which
	// causes exec.CommandContext to kill the op subprocess cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	secret, readErr := r.ReadSecret(ctx, uri)

	// Always forget the session — even when interrupted or on error.
	if ferr := r.ForgetSession(); ferr != nil {
		fmt.Fprintf(os.Stderr, "warning: op session forget failed: %v\n", ferr)
	}

	// If context was cancelled the user interrupted us; exit without output.
	if ctx.Err() != nil {
		return exitOpFail
	}

	if readErr != nil {
		// op's own error messages already went to stderr via cmd.Stderr.
		return exitOpFail
	}

	if _, err := os.Stdout.Write(secret); err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		return exitOpFail
	}
	return exitSuccess
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: opx <op://uri>")
	fmt.Fprintln(os.Stderr, "       opx get <name>")
}
