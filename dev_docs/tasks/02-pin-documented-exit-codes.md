# Pin the documented exit codes so they can't drift

**Surfaced by:** co-review of PR #11 — `exitDenied = 3` existed in `main.go`
while `README.md`, `AGENTS.md`, and two skills all documented three codes and
folded denial into exit 1.

## How it is

Four places describe `opx`'s exit codes, all by hand:

- `main.go` — the `const` block (ground truth)
- `README.md` — the "Exit codes" table
- `AGENTS.md` — the coding-conventions bullet and the "Things to avoid" bullet
- `skills/1password-audit/1password-local-dev-best-practices.md` §4, plus the
  `opx` troubleshooting entry in `skills/1password-onboard/SKILL.md`

Nothing connects them. Adding `exitDenied` updated one and left three wrong for
however long, and the wrong ones were the ones users read. For a tool whose
whole contract is "denial is distinguishable from failure", that's the worst
place to have drift.

## How it should be

A test that fails when the docs and the binary disagree. Cheapest honest
version: a table-driven test in `main_test.go` that asserts each exit constant's
value, plus a check that `README.md`'s exit-code table contains a row for every
constant. That catches a new code being added without a doc update, which is the
failure mode that actually happened.

The skills are prose and can't be pinned as tightly — but they can be added to
the `AGENTS.md` "keep consistent when editing `skills/`" list, which currently
names only the `op run`/`op inject` rule and the `--account` rule.

## Acceptance

- `make test` fails if a new exit constant is added without a README row.
- `AGENTS.md` lists exit codes as a third skills-consistency invariant.
