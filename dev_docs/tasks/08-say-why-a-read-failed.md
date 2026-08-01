# Say why a read failed, instead of just exiting 1

**Surfaced by:** smoke-testing PR #27 (task 11 of the security-hardening
plan). Reading `op://Private/<item name with an option-space>/password` drew
the dialog, was approved, and then exited 1 with no output at all. The reason
— `op`'s own `invalid character in secret reference: ' '` — existed the whole
time, and went to `io.Discard`.

## How it is

`diag` defaults to `io.Discard` and only becomes `os.Stderr` under
`--verbose` / `OPX_VERBOSE`. Every failure that is not a usage error routes
through `diagf`, so in the default quiet mode each of these produces a bare
exit code and nothing else:

- `main.go:393`, `main.go:524` — `read failed: %v` (the one that prompted this)
- `main.go:532` — `op signout failed, not spawning child` — run mode's
  fail-closed, and invisible
- `main.go:544` — `spawn failed`
- `main.go:386`, `main.go:520` — `interrupted`
- `main.go:399` — `error writing output`

Usage errors already print unconditionally (see the comment on `runWith`:
"without it a new user has no signal that they typoed a flag"). PR #27 added
`confirmFailed` on the same grounds — a dialog that could not be drawn is
reported whatever the verbosity, because the user was never asked and cannot
know why. The read path is the remaining half of that argument, and the worse
half: the user *did* answer a biometric prompt, and got silence and a `1`.

This is not "verbose mode is broken" — verbose works. It is that quiet mode
was scoped to suppress *progress* and has been suppressing *causes*.

## How it should be

A failure that ends the invocation reports its cause on stderr regardless of
verbosity. The diag writer keeps what it is actually for: the success summary,
the signout warning, and other progress chatter that is noise on a working run.

Constraints, from `AGENTS.md`:

- `os.Stdout` is the secret channel. Nothing here writes to it, and invariant
  5 (batch atomicity) is untouched — this changes reporting only.
- The diagnostic carries `op`'s error and nothing derived from the secret.
  `op`'s read failures name the URI, not the value; keep it that way, and do
  not add fetched bytes to any message.
- Exit codes do not change. This is about the reason, not the code.

One thing to decide rather than assume: whether `interrupted` belongs in the
loud set. A SIGINT the user sent themselves is closer to a denial — they
already know why — so leaving that one on `diag` may be right.

## Acceptance criteria

- Reading a URI that `op` rejects prints `op`'s reason on stderr with no
  `--verbose`, and still exits 1.
- A run-mode `ForgetSession` failure — which is fatal and suppresses the child
  — says so on stderr.
- Nothing new reaches stdout; the existing stdout-atomicity tests still pass.
- `make test` and `make lint` pass.
