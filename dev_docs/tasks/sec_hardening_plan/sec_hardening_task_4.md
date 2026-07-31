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

On every non-Linux platform — macOS, the primary target — the whole caller identity shown in the dialog comes from two `ps` invocations:

```go
exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid))  // caller.go:327
exec.Command("ps", "-o", "args=",       "-p", strconv.Itoa(pid))  // caller.go:344
```

Both resolve `ps` through `exec.LookPath` against the PATH opx inherited from the invoking process. That process is precisely the party the dialog exists to disclose, so it controls both the header name and the detail line. A planted `ps` that prints `1 /Applications/claude` and `claude --resume` makes the dialog attribute an arbitrary secret request to a program the user trusts.

There is no privilege escalation — the shim runs as the same uid — but the dialog is the trust boundary, and a boundary the attacker can write the text of is not a boundary.

The Linux path (`ancestorChainLinux`, `caller.go:217`) already avoids this entirely by reading `/proc`; no subprocess, no PATH. The macOS path should get as close to that as a `ps` call allows.

`ps` is at `/bin/ps` on macOS and every BSD; on Linux (where this path is only reached if `/proc` is unavailable) it is `/bin/ps` or `/usr/bin/ps`.

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
- A test asserting `Name()` returns `"unknown"` rather than panicking or hanging when no `ps` is resolvable (skip on `runtime.GOOS == "linux"`, where the `/proc` path is taken).
- No test shells out to a real `ps` in a way CI cannot satisfy — `AGENTS.md` forbids tests that depend on real external binaries.
- `make test` and `make lint` pass.

**User-run**

- On macOS, run `opx op://V/I/f` from a terminal and confirm the dialog still names the calling program correctly (the fix must not regress normal identification).
- Plant a fake `ps` early on PATH and confirm the dialog identity no longer follows it.
