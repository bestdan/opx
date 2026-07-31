---
title: Resolve op once, reject untrusted locations, fail closed
priority: high
size: 3
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/oprunner/runner.go:40 # ReadSecret execs "op"
  - internal/oprunner/runner.go:51 # ForgetSession execs "op signout --all"
  - internal/oprunner/runner_test.go
  - internal/oprunner/integration_test.go
  - main.go # ForgetSession failure is fatal in run mode
tags: [security, oprunner]
---

Part of [[sec_hardening_plan]]. Closes **F5**.

## Context

`realRunner` execs `op` by bare name in both methods (`runner.go:40`, `runner.go:51`), resolved through the PATH opx inherited from the caller. The consequence is worse for `ForgetSession` than for `ReadSecret`:

- A shim that forwards `read` to the real `op` but no-ops on `signout` leaves the biometric-unlocked session **live**. That voids the single property the tool exists to enforce (`AGENTS.md` invariant 2, and the README's whole pitch). The user pays one biometric prompt; the attacker gets an un-prompted vault for as long as the session lasts.
- The shim returns exit 0, so `ForgetSession` returns nil, so run mode's fail-closed check (`AGENTS.md` invariant 2: a `ForgetSession` failure means the child is not spawned) is satisfied and opx reports success. The failure is completely silent.

The classic delivery is `node_modules/.bin/op` plus an `npm run dev` script that calls `opx run` — npm puts `node_modules/.bin` first on PATH for every script it runs, so no attacker-set PATH is even required.

**This is the one task where the fix has real UX risk.** `op` lives in different places per platform and install method (`/opt/homebrew/bin/op` on Apple Silicon Homebrew, `/usr/local/bin/op` on Intel, `/usr/bin/op` on Debian packages, `~/.local/bin` for some manual installs, and the 1Password desktop app's own bundled path on macOS). A strict allowlist that rejects a legitimate install turns opx into a tool that "just doesn't work", and the failure mode of that is people going back to raw `op read`. See open question 2 in [[sec_hardening_plan]] — the plan currently specifies the looser rule below; confirm before implementing.

## Task

1. Add an unexported `resolveOp() (string, error)` to `internal/oprunner`:
   - Call `exec.LookPath("op")` as today.
   - `filepath.Abs` and `filepath.EvalSymlinks` the result.
   - **Reject** when the resolved path is inside the current working directory tree, or contains a `node_modules` path component, or the PATH entry it came from was relative (`.`, `..`, or any non-absolute entry).
   - **Reject** when the file is group- or world-writable.
   - Return a descriptive error naming the rejected path when any rule fires.
2. Resolve **once** per process and reuse. Add the resolved path to `realRunner` and populate it in `New` / `NewWithStderr`. Resolving twice invites a TOCTOU gap between the `read` and the `signout` — the same binary must serve both.
3. On rejection: this is fatal, not a warning. `ReadSecret` and `ForgetSession` both return the error; `main.go` maps it to `exitOpFail`, and run mode must not spawn the child (`AGENTS.md` invariant 2 already requires exactly this for a `ForgetSession` failure — verify the resolution error takes that same path).
4. The error message must be actionable and must not leak anything: name the rejected path and why (`refusing to use op at ./node_modules/.bin/op: untrusted location`). No secret ever appears in an error — `AGENTS.md` invariant 4.
5. `internal/oprunner/integration_test.go` hits the real `op`; make sure the new resolution does not break it on a normal Homebrew install.

## Acceptance Criteria

**Code-enforced**

- `internal/oprunner/export_test.go` (new) exposes `resolveOp` for testing.
- `internal/oprunner/runner_test.go` covers, with `t.TempDir()` + `t.Setenv("PATH", ...)`: an `op` under a `node_modules/.bin` component → error; an `op` inside the working directory → error; a world-writable `op` → error; a clean `op` in an unrelated absolute temp dir → accepted; a relative PATH entry → error.
- A test asserting the resolved path is captured once at construction, not per call (e.g. construct, then move/replace the binary, and assert the recorded path is unchanged).
- `main_test.go` / `run_subcommand_test.go`: a fake Runner returning a resolution error causes `exitOpFail` **and** the fake Spawner is never invoked.
- `make test` and `make lint` pass. Run `make test-integration` too — `AGENTS.md` calls for it when touching `internal/oprunner`.

**User-run**

- With a normal install (`which op` → `/opt/homebrew/bin/op` or equivalent), confirm `opx op://V/I/f` still reads and still forces the biometric prompt.
- Create `./node_modules/.bin/op` as an `exit 0` script, put it first on PATH, and confirm opx now refuses with the descriptive error instead of using it.
