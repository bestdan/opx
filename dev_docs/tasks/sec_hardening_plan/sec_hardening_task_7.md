---
title: Base caller identity on the executable path, not the self-asserted comm
priority: medium
size: 3
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
is_blocked_by: sec_hardening_task_4
related_files:
  - internal/caller/caller.go:78 # Name()
  - internal/caller/caller.go:99 # Describe()
  - internal/caller/caller.go:313 # psPPIDComm — basenames away the full path
  - internal/caller/caller.go:61 # isUninteresting — the skip heuristic
  - internal/caller/caller_test.go
  - main.go:280 # Caller: caller.Name()
tags: [security, caller]
---

Part of [[sec_hardening_plan]]. Closes **F6**. Blocked by [[sec_hardening_task_4]] — both rewrite `psPPIDComm`, and landing them in parallel would conflict.

> **Revised after #17 narrowed opx to macOS only.** The `/proc/<pid>/exe`
> half of this task is gone with the Linux support, along with
> `ancestorChainLinux` and `readLinuxComm`. Size drops 5 → 3. What is left is
> the macOS side, which was always the important one — and the removal makes
> it sharper, not easier: `ps` is now the *only* source of caller identity,
> with no `/proc` path to fall back on when it fails or lies.

## Context

`Name()` returns `comm`, and `comm` is a string **the requesting process chooses for itself**: on Linux via `prctl(PR_SET_NAME)` or simply by being exec'd from a file with that basename; on macOS by the same basename mechanism. `main.go:280` feeds it straight into the dialog header. Nothing verifies it against anything.

Two exploit shapes, both cheap:

1. **Direct impersonation.** Copy the malware to `~/.cache/claude`, exec it, and the dialog header reads `"claude" wants to read: op://Personal/AWS/secret_key`. The user, who genuinely runs claude, approves.
2. **Attribution laundering.** Name yourself `bash`. `isUninteresting` (`caller.go:61`) skips you, `firstInteresting` walks past, and the dialog attributes your request to whatever genuinely-trusted program is above you in the chain. You do not even have to pick a good name — you have to pick a boring one.

The verifiable data is available and currently thrown away. macOS `ps -o comm=` returns the **full executable path**, and `psPPIDComm` basenames it at `caller.go:339` before storing. Linux has `/proc/<pid>/exe`, a kernel-maintained symlink that a process cannot forge for itself, and `ancestorChainLinux` does not read it at all.

Design tension worth naming: the short name is genuinely better UX in the dialog header — `"claude"` reads cleanly, `"/usr/local/bin/claude"` does not, and a noisy dialog is a dialog people stop reading. The plan's position (open question 3 in [[sec_hardening_plan]]) is to keep the short name in the header and put the verified path in the detail line, where there is room. Confirm before implementing.

## Task

1. Add `exe string` to the `process` struct, populated by `psPPIDComm`: stop discarding the full path macOS `ps -o comm=` already returns. Keep `comm` as the basename so `isUninteresting` matching is unaffected, and store the full path in `exe`. It may fail (permissions, race, a process that exited) — failure degrades to an empty `exe`, never an abort, since the existing walk is deliberately tolerant and must stay that way.
2. `Name()` keeps returning the short name for the header. Derive it from `exe`'s basename when `exe` is available, falling back to `comm` — so the header stops being purely self-asserted even though it stays short.
3. `Describe()` includes the resolved `exe` of the subject process in the detail line, so the user can distinguish `/usr/local/bin/claude` from `~/.cache/claude`. Fold this in without making the line unreadable — the existing `X › ` ancestor prefix and the 120-rune budget both still apply.
4. Mark the anomaly rather than only showing the path. A path outside the conventional install locations (`/usr/bin`, `/usr/local/bin`, `/opt/homebrew/bin`, `/bin`, `/Applications`) is the signal a hurried user will otherwise miss. A short marker — `⚠` or `(unverified location)` — makes the interesting case visible without asking the user to parse paths. Keep the compiled-in location list as internal display logic, not user-facing configuration.
5. Re-document `isUninteresting` (`caller.go:58`) as an **advisory display heuristic, not an identity or a security control**. The current comment reads as though skipping shells is neutral; it is the mechanism of exploit shape 2 above, and the next person to touch it needs to know that.
6. Do not attempt to verify the binary's signature or hash. That is a much larger project (`codesign`, notarization, a trust store) and out of scope; showing the path is the proportionate fix.

## Acceptance Criteria

**Code-enforced**

- `internal/caller/export_test.go` exposes whatever new display helper this introduces (e.g. `FormatIdentityForTest`), so the rendering is testable without real process ancestry — follow the existing `DescribeArgvForTest` pattern, which decouples logic from the live process tree.
- `internal/caller/caller_test.go` asserts: a `process` with `exe: "/usr/local/bin/claude"` renders without the anomaly marker; one with `exe: "/Users/x/.cache/claude"` renders with it; one with an empty `exe` degrades to the `comm`-only rendering and does not panic; a `(deleted)` suffix survives into the display.
- A test asserting `Name()` prefers `exe`'s basename over `comm` when they disagree — that disagreement is the impersonation signal.
- Existing `caller_test.go` assertions updated where the display genuinely changed; do not delete a test to make the suite pass (`AGENTS.md`).
- Assertions state what the identity line *renders as*, not merely that the bad value is absent — an implementation that dropped the name entirely must fail. (Both follow-up fixes on this plan, `0bb2506` and #19, slipped past absence-only assertions.)
- `make test`, `make lint`, `make cross` pass.

**User-run**

- Run `opx op://V/I/f` from a terminal on macOS and confirm the dialog identifies the caller with a full path in the detail line and still reads cleanly.
- Copy a benign binary to `~/.cache/` under a trusted-looking name, run opx from it, and confirm the dialog shows the anomalous path and the marker.
