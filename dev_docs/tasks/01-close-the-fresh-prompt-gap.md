# Close the "fresh biometric prompt" gap in the binary

**Surfaced by:** co-review of PR #11 (reviewers flagged the docs as overclaiming).
**Status:** open — docs were softened; the underlying gap is unfixed.

## How it is

`readAndForget` in `main.go` reads first and calls `ForgetSession` afterwards.
So `opx` guarantees two things unconditionally — the confirmation dialog, and
`op signout --all` on every exit path — but it does **not** guarantee a
biometric prompt. If any other `op` invocation established a session inside the
inactivity window (a `from_op` on `cd`, an `op run`, a bare `op item get`),
`opx`'s own read silently reuses it and no Touch ID prompt fires.

In opx-only usage this never shows up, because step 3 leaves nothing cached.
It shows up in exactly the mixed workflow section 5 of the best-practices doc
recommends (direnv + `from_op` alongside `opx`).

## How it should be

Sign out **before** the read as well as after, so the read always starts from a
known-clean session state. Then the docs' original claim becomes true and the
`opx` value proposition is one sentence instead of three with a caveat.

Cost: one extra `op signout --all` per invocation (~100ms), and it tears down a
session the user may have legitimately established for an unrelated foreground
task. That trade is the actual decision to make — the current behaviour is
defensible, it is just not what the docs claimed.

If the answer is "no, leave it" — record that reasoning in `AGENTS.md` under the
security invariants, so the next person who reads the docs doesn't re-open this.

## Acceptance

- Either a pre-read `ForgetSession` with a test in `main_test.go` asserting the
  call order, or a written decision in `AGENTS.md` explaining why not.
- If changed, revert the hedged wording in
  `skills/1password-audit/1password-local-dev-best-practices.md:84-86` and the
  four one-line echoes in the skills.
