---
title: Resolve zenity and osascript from a trusted absolute-path allowlist
priority: high
size: 3
status: done
created: 2026-07-31
completed: 2026-07-31
completed_by: "#14"
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/prompt/prompt.go:211 # confirmLinux — the LookPath decision
  - internal/prompt/prompt.go:223 # confirmZenity — the exec
  - internal/prompt/prompt.go:193 # confirmDarwin — osascript, same class
  - internal/prompt/prompt.go:254 # confirmTTY — the safe fallback
  - internal/prompt/prompt_test.go
tags: [security, prompt, high]
---

Part of [[sec_hardening_plan]]. Closes **F1 (HIGH)**, and pre-empts the same class on darwin.

> **Done — merged as #14.** Landed before #17 narrowed opx to macOS only. The
> zenity and `/dev/tty` code this task describes no longer exists in the tree,
> so F1's Linux half is moot on top of being fixed; what survives and matters
> is `resolveHelper` plus the `osascript` hardening, which is where the whole
> class actually bites on the supported platform. `resolveHelper` has since
> picked up a third caller (the `defaults` appearance query), which is the
> pattern working as intended. The task body below is left as written for the
> record; read it as history, not as instructions.

## Context

This is the highest-severity finding in the scan, and the mechanism is two lines:

```go
if _, err := exec.LookPath("zenity"); err == nil {   // prompt.go:211
    return confirmZenity(req, stderr)                 // execs "zenity" at :223
}
return confirmTTY(req)
```

`LookPath` searches the PATH opx inherited **from the process it is prompting about**. So the caller chooses which backend runs, and since `confirmZenity` treats `cmd.Run() == nil` as Allow (`prompt.go:233-236`), a planted `zenity` that does nothing but `exit 0` *is* the approval. There is no user interaction at all.

The second half is worse than the first: a planted `zenity` also satisfies the `LookPath` check, so `confirmTTY` — which would have prompted on the terminal — is never reached. On a headless box that would otherwise have asked, no prompt appears.

`confirmDarwin` execs `osascript` by bare name at `prompt.go:193` with the identical pattern. The scan did not file it separately (its exit-code semantics make a naive shim deny rather than allow — `cancel button "Deny"` means a shim exiting 0 without the `gave up:true` marker *would* read as Allow, so it is in fact exploitable too). Fix both here; they are the same five lines of code.

`make cross` ships linux-amd64, so the Linux path is a supported target, not hypothetical.

## Task

1. Add an unexported `resolveHelper(name string, candidates []string) string` to `internal/prompt`. For each candidate absolute path, `os.Stat` it and accept the first that is a regular file with an executable bit set and is **not** group- or world-writable (`mode&0o022 == 0`). Return `""` when none qualifies. No PATH consultation anywhere in this function.
2. Compile in the candidate lists:
   - `zenity`: `/usr/bin/zenity`, `/usr/local/bin/zenity`, `/bin/zenity`
   - `osascript`: `/usr/bin/osascript`
3. `confirmLinux` calls `resolveHelper` instead of `exec.LookPath`, and passes the resolved absolute path into `confirmZenity`, which execs that path rather than the bare name. Empty result → `confirmTTY`, which is the correct degradation: a terminal prompt the user actually answers beats a GUI helper of unknown provenance.
4. `confirmDarwin` does the same for `osascript`. There is no TTY fallback on darwin today; when `osascript` cannot be resolved, return `ErrDenied` — `ErrDenied` is already documented (`prompt.go:22-26`) as covering "no UI available to ask", and fail-closed is the established behaviour. Do **not** silently fall through to `confirmTTY` on darwin without saying so in the PR; it would be a behaviour change beyond this finding's scope.
5. Document the invariant in the package comment: the dialog backend is never selected or located via PATH, because PATH belongs to the process being prompted about.

Residual risk to state in the PR description: a candidate at a *user-writable absolute* path (e.g. `/usr/local/bin` on a single-user Homebrew macOS) is still plantable by an attacker running as the user. The mode check catches group/world-writable directories' contents but not files the user owns. This closes the PATH-injection vector, not the compromised-user-account one.

## Acceptance Criteria

**Code-enforced**

- `internal/prompt/export_test.go` exposes `resolveHelper` as `ResolveHelperForTest`.
- `internal/prompt/prompt_test.go` covers, using `t.TempDir()` fixtures: a candidate that does not exist → `""`; a directory at the candidate path → `""`; a non-executable regular file → `""`; a world-writable executable (`0o777`) → `""`; a clean `0o755` executable → its path; and first-match-wins ordering when two candidates qualify.
- A test asserting `resolveHelper` ignores PATH entirely: set `t.Setenv("PATH", <dir containing a fake zenity>)` with no candidate present at any allowlisted path, and assert the result is still `""`.
- `make test`, `make lint`, and `make cross` pass (the last because this is the platform-branching file).

**User-run**

- On Linux with the real `zenity` installed, confirm the dialog still appears and Allow/Deny still work.
- Plant an `exit 0` script named `zenity` in a temp dir, put it first on PATH, run `opx op://V/I/f`, and confirm you now get the TTY prompt (or the real zenity dialog) instead of a silent approval.
- On macOS, confirm the dialog still appears and `giving up after 60` still denies on timeout.
