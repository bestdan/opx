---
title: Base caller identity on the executable path, not the self-asserted comm
priority: medium
size: 3
status: done
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/caller/caller.go:77 # Name()
  - internal/caller/caller.go:98 # Describe()
  - internal/caller/caller.go:354 # psPPIDComm — basenames away the full path
  - internal/caller/caller.go:60 # isUninteresting — the skip heuristic
  - internal/caller/caller_test.go
  - main.go:295 # Caller: caller.Name()
tags: [security, caller]
---

Part of [[sec_hardening_plan]]. Closes **F6**. Was blocked by [[sec_hardening_task_4]] — both rewrite `psPPIDComm` — which merged as #22, so this is unblocked. `psPPIDComm` now invokes a resolved absolute `ps` with a minimal environment; build on that rather than reinstating `exec.Command("ps", …)`.

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

> [!WARNING]
> **The premise below this line was wrong, and the task as originally written would have made opx less safe.** Corrected after implementation; see "What the premise got wrong". Read that section before reusing anything here.

The verifiable data is available and currently thrown away. macOS `ps -o comm=` returns the **full executable path**, and `psPPIDComm` basenames it at `caller.go:339` before storing. Linux has `/proc/<pid>/exe`, a kernel-maintained symlink that a process cannot forge for itself, and `ancestorChainLinux` does not read it at all.

Design tension worth naming: the short name is genuinely better UX in the dialog header — `"claude"` reads cleanly, `"/usr/local/bin/claude"` does not, and a noisy dialog is a dialog people stop reading. The plan's position (open question 3 in [[sec_hardening_plan]]) is to keep the short name in the header and put the verified path in the detail line, where there is room. Confirm before implementing.

## What the premise got wrong

**macOS `ps -o comm=` is not a kernel fact.** It prints what looks like a full path, but `ps` derives that string from the process's own arguments, so it is forgeable by exactly the party the dialog exists to disclose. Demonstrated directly during implementation — a binary at `/var/folders/.../forge`, exec'd with `argv[0] = "/usr/local/bin/claude"`:

```
ps  -o comm=          -> "/usr/local/bin/claude"        <- the forgery
lsof -F fn -a -d txt  -> "/private/var/.../exe/forge"   <- the truth
```

Task step 1 above ("stop discarding the full path macOS `ps -o comm=` already returns") therefore describes storing an attacker-chosen string and presenting it as verified. Implementing it literally would have been **worse than leaving F6 open**: today's dialog shows a forgeable name and claims nothing about location, whereas the specified version would have vouched for a location it never checked. An attacker would set `argv[0]` and earn an affirmatively clean dialog.

Two lessons, both already the plan's recurring theme:

- The Linux half of the original task rested on `/proc/<pid>/exe`, which genuinely is kernel-maintained and unforgeable. The macOS half was written by analogy and never checked. **The analogy did not hold, and nothing in the task said it had been verified.**
- The check that caught it was running the thing, not reading it. `ps -o comm=` *looks* authoritative in every terminal you try it in, because nothing you normally run forges its `argv[0]`.

### What was implemented instead (#23)

- **`caller.exePath`** reads the kernel's text vnode via `lsof -p <pid> -F fn -a -d txt`. `proc_pidpath()` would be the direct route but needs cgo, and `make cross` must stay CGO-free.
- `ps` is still used for the ppid walk and for the basename fed to `isUninteresting` — both tolerate a self-asserted source; a **displayed path** does not.
- Step 4's anomaly marker was **dropped**, not deferred. The specified location list marks real Claude Code as suspicious: it installs under `~/.local/share/claude/versions/`, npm tools under `~/.npm-global`, cargo under `~/.cargo/bin`. A marker that fires on the ordinary case trains the user to click through the one dialog that matters. The path is the disclosure.
- Step 2's "`Name()` prefers the basename of the verified path" survived intact — it just draws on the kernel path rather than the `ps` one.
- Only the **subject** process gets its path resolved (one `lsof` call), since only the subject is named in the dialog.
- The acceptance criterion asking for a `(deleted)` suffix test was dropped: that suffix is a Linux `/proc` artifact and macOS `lsof` does not emit it, so the test would have pinned behaviour that cannot occur.

### What is still open

**Exploit shape 2 (attribution laundering) is not closed.** `isUninteresting` still matches the `ps`-reported basename, so a process naming itself `bash` is still skipped and its request still attributed to a trusted ancestor — whose real path is then shown beside it, honestly describing the wrong process. Closing it means resolving every ancestor's path (6 `lsof` calls, ~120 ms) and matching on the kernel basename instead. Recorded as invariant 9 in `AGENTS.md` rather than left implicit.

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
