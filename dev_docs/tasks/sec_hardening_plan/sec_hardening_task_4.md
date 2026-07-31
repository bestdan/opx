---
title: Invoke ps by absolute path with a minimal environment
priority: medium
size: 2
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/caller/caller.go:327 # psPPIDComm
  - internal/caller/caller.go:344 # psArgv
  - internal/caller/caller_test.go
tags: [security, caller]
---

Part of [[sec_hardening_plan]]. Closes **F3**.

## Context

The whole caller identity shown in the dialog comes from two `ps` invocations:

```go
exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid))  // caller.go:327
exec.Command("ps", "-o", "args=",       "-p", strconv.Itoa(pid))  // caller.go:344
```

Both resolve `ps` through `exec.LookPath` against the PATH opx inherited from the invoking process. That process is precisely the party the dialog exists to disclose, so it controls both the header name and the detail line. A planted `ps` that prints `1 /Applications/claude` and `claude --resume` makes the dialog attribute an arbitrary secret request to a program the user trusts.

There is no privilege escalation — the shim runs as the same uid — but the dialog is the trust boundary, and a boundary the attacker can write the text of is not a boundary.

Since #17 narrowed opx to macOS, this matters more than when it was written: the `/proc` walk that used to make Linux immune to it is gone, so `ps` is now the **only** source of caller identity. There is no second path to degrade to and nothing to cross-check against.

`ps` is at `/bin/ps` on macOS. `/usr/bin/ps` is worth keeping as a second candidate purely for robustness; it costs one `stat`.

`resolveHelper` in `internal/prompt` (merged in #14) already does exactly this screening. Do **not** import it across package boundaries — `caller` importing `prompt` inverts the dependency, since `main` composes both. Duplicate the ten lines; `AGENTS.md` prefers three similar lines to a premature abstraction, and the two lists have no reason to move together.

Note while you are in here: `psArgv` splits `ps -o args=` output with `strings.Fields`, which mangles any argument containing a space (`sh -c 'a b'` becomes three tokens) and cannot be fixed without a different data source — `ps` gives no delimiter. Leave the behaviour alone in this task; [[sec_hardening_task_6]] and [[sec_hardening_task_7]] deal with faithful rendering, and this limitation belongs in the notes there.

## Task

1. Add an unexported `psPath() string` to `internal/caller` that returns the first of `/bin/ps`, `/usr/bin/ps` that stats as a regular executable file and is not group/world-writable. Return `""` when neither qualifies.
2. `psPPIDComm` and `psArgv` both use that path. When it is `""`, return `ok == false` — the existing failure path already degrades gracefully to `"unknown"` in `Name()` (`caller.go:86`), which is the honest answer.
3. Set an explicit minimal environment on both commands: `cmd.Env = []string{"PATH=/bin:/usr/bin", "LC_ALL=C"}`. `LC_ALL=C` also stabilises `ps` column formatting across locales, which the current inherited-locale behaviour does not guarantee.
4. Do not shell out through `sh`; keep the direct `exec.Command` form.

## Acceptance Criteria

**Code-enforced**

- `internal/caller/export_test.go` exposes `psPath` as `PSPathForTest`.
- `internal/caller/caller_test.go` covers `psPath` against `t.TempDir()` fixtures the same way [[sec_hardening_task_3]] covers `resolveHelper`: missing, directory, non-executable, world-writable, clean — plus a case asserting a fake `ps` reachable only via `t.Setenv("PATH", ...)` is **not** selected.
- A test asserting `Name()` returns `"unknown"` rather than panicking or hanging when no `ps` is resolvable — with `ps` now the only identity source, this degradation is the whole safety net.
- No test shells out to a real `ps` in a way CI cannot satisfy — `AGENTS.md` forbids tests that depend on real external binaries.
- `make test` and `make lint` pass.

**User-run**

- On macOS, run `opx op://V/I/f` from a terminal and confirm the dialog still names the calling program correctly (the fix must not regress normal identification).
- Plant a fake `ps` early on PATH and confirm the dialog identity no longer follows it.
