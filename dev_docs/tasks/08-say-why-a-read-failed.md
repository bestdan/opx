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

Grep the message strings — they are unique, and unlike line numbers they will
still be there when someone picks this up:

- `read failed: %v` — in both `readAndForget` and `runSubcommand`; the one
  that prompted this
- `op signout failed, not spawning child` — in `runSubcommand`; run mode's
  fail-closed, and invisible
- `spawn failed` — in `runSubcommand`
- `interrupted` — in both `readAndForget` and `runSubcommand`
- `error writing output` — in `readAndForget`

There is a second half, and missing it produces a fix that does not work.
`main` builds the runner as `oprunner.NewWithStderr(diag)`, so **op's own
stderr goes to the same discarded writer** — and `realRunner.ReadSecret`
returns the bare `cmd.Run()` error, which is `exit status 1` and carries no op
text. Promoting only the `diagf` call would print:

```
read failed: exit status 1
```

which does not answer the question this task exists to answer. The message the
user wants — `invalid character in secret reference: ' '` — only ever existed
on op's stderr. The comment already above that `diagf` in `readAndForget` says
as much: the one-line summary was designed to sit *on top of* op's output, not
to replace it.

Resolving that is the judgment call in this task. `oprunner.New()`'s default
forwards op's stderr straight to `os.Stderr`, precisely so users see op's
errors; `main` overrides it to `diag` to keep quiet mode quiet. Unconditionally
forwarding the stream would make op's chatter unsuppressible on success paths
too. Capturing op's stderr into the returned error — so the caller decides what
to print and when — is probably the shape, but that is `internal/oprunner`'s
contract and worth deciding deliberately.

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
  `--verbose`, and still exits 1. **The printed text contains op's own
  message, not just an exit status** — `read failed: exit status 1` satisfies
  the letter of this and none of its point.
- A run-mode `ForgetSession` failure — which is fatal and suppresses the child
  — says so on stderr.
- The same holds for the rest of the fatal set: a failed spawn and a failed
  stdout write report their cause too. None of this needs a live `op` or a
  refactor — the fake Spawner covers the spawn, and swapping `os.Stdout` for a
  pipe whose read end is already closed provokes the write error — so each
  loud path gets a test. Whether `interrupted` joins the loud set is the
  judgment call flagged above; decide it, and say which way in the PR.
- Nothing new reaches stdout; the existing stdout-atomicity tests still pass.
- `make test` and `make lint` pass.
