---
type: epic
title: Remediate the 2026-07-31 security scan findings
status: active
created: 2026-07-31
---

# Remediate the 2026-07-31 security scan findings

## Goal

Close the ten verified findings from `CLAUDE-SECURITY-20260731-081020/CLAUDE-SECURITY-RESULTS.md` (1 HIGH, 8 MEDIUM, 1 LOW). All ten attack the same thing from three directions: **the confirmation dialog is opx's entire trust boundary, and the untrusted caller currently gets to influence what that dialog says and which binaries implement it.**

## Root causes

The ten findings collapse into four groups, which is how the tasks are sliced:

| # | Root cause | Findings | Tasks | Status |
|---|---|---|---|---|
| A | Dialog interpolates caller-controlled text without sanitization — `sanitizeCallerDetail` from `a58bd99` was never applied to `Binding.URI` / `Binding.Name` | F2, F4, F8 | [[sec_hardening_task_1]], [[sec_hardening_task_2]] | task 1 **merged** (#15); task 2 open |
| B | Security-critical helper binaries (`osascript`, `ps`, `op`) resolved by bare name through caller-controlled PATH | **F1 (HIGH)**, F3, F5 | [[sec_hardening_task_3]], [[sec_hardening_task_4]], [[sec_hardening_task_5]] | task 3 **merged** (#14); tasks 4, 5 open |
| C | Run-mode dialog understates the child that receives the secrets — `renderArgv` basenames path-like tokens, `RenderCommand` cuts at 120 runes | F7, F9, F10 | [[sec_hardening_task_6]] | **merged** (#16, + #19) |
| D | Caller identity is the self-asserted `comm` string; the verifiable executable path is fetched and then discarded | F6 | [[sec_hardening_task_7]] | open |

Group B contained the only HIGH: a planted `zenity` that exits `0` **was** the approval, and its presence also suppressed the `/dev/tty` fallback, so no prompt appeared at all.

## Revised 2026-07-31, after #17 narrowed opx to macOS only

The Linux and `/dev/tty` code paths are gone from the tree, which changes three things:

- **F1's Linux half is moot** as well as fixed. What survives from task 3 and still matters is `resolveHelper` and the `osascript` hardening — the same class on the one supported platform.
- **Task 7 shrinks** (size 5 → 3): the `/proc/<pid>/exe` half went with the Linux support. The macOS side was always the important one.
- **Tasks 4 and 7 get sharper, not easier.** `ps` is now the *only* source of caller identity, with no `/proc` walk to fall back on or cross-check against.

Task 8 was **unblocked and promoted** on those grounds — three security properties were merged and undocumented — and has since landed (#20, #21).

## Scope / non-goals

In scope: `internal/prompt`, `internal/caller`, `internal/oprunner`, `internal/uri`, and the two `prompt.Request` construction sites in `main.go` (`confirmAndRead` at `main.go:278`, `runSubcommand` at `main.go:418`).

Explicitly **not** in scope, per `AGENTS.md`:

- No new flags, config files, allowlists of *URIs*, nicknames, or env-var knobs. (Allowlisting *helper binary paths* is different — it is a fixed, compiled-in set with no user-facing surface, and that distinction is the one judgment call worth confirming; see Open questions.)
- No third-party dependencies, no cgo (`make cross` must stay CGO-free).
- No change to the exit-code set (`exitSuccess`, `exitOpFail`, `exitUsage`, `exitDenied`).
- No change to where secrets go: stdout in single/`--env` mode, child environment in run mode.
- The two candidate sites the scan's panel left unreviewed are not covered here; they are unknown, not cleared.

## Approach

Three principles, applied in this order:

1. **Nothing the caller controls reaches the terminal unescaped.** One sanitizer (`sanitizeDisplay`) on every dialog-body interpolation, plus rejection of control bytes at the `uri.IsOPURI` validator so a control-laden URI is a usage error before any prompt is drawn. Belt and braces, because this is the boundary.
2. **Security-critical subprocesses are resolved from a compiled-in absolute-path allowlist, not PATH.** Each helper gets a `resolve` step that checks the candidate is a regular file at a known system location and is not group/world-writable. If no trusted candidate exists, degrade to the safe option (TTY prompt for the dialog; "unknown" for identity) or fail closed (`op`).
3. **The dialog describes the child faithfully when it is about to hand it secrets.** Ancestor descriptions can stay abbreviated — they are context. The run-mode child line is an authorization decision and must not omit paths, hosts, or trailing argv.

The tradeoff accepted throughout: an attacker who already executes code as the user can still place a binary at a *user-writable absolute* path and put it early on PATH for a program that trusts PATH. These fixes remove opx's own trust in PATH; they do not (and cannot) make opx safe against an attacker who can rewrite the user's shell profile. That residual risk gets written down in [[sec_hardening_task_8]] rather than left implicit.

## Tasks

1. ~~[[sec_hardening_task_1]] — Sanitize every dialog interpolation, not just `CallerDetail`~~ — **merged, #15** (+ `0bb2506`).
2. [[sec_hardening_task_2]] — Reject C0/C1 control bytes in `uri.IsOPURI` so a forged URI fails before the prompt (F2, F4, F8, defense in depth).
3. ~~[[sec_hardening_task_3]] — Resolve dialog helpers from a trusted absolute-path allowlist~~ — **merged, #14**.
4. [[sec_hardening_task_4]] — Invoke `ps` by absolute path with a minimal environment (F3).
5. [[sec_hardening_task_5]] — Resolve `op` once, reject untrusted locations, fail closed (F5).
6. ~~[[sec_hardening_task_6]] — Render the run-mode child command faithfully~~ — **merged, #16** (+ #19).
7. [[sec_hardening_task_7]] — Base caller identity on the executable path, not the self-asserted `comm` (F6).
8. ~~[[sec_hardening_task_8]] — Record the merged invariants and the residual risk~~ — **merged, #20** (+ #21, which added the `%q` layer to invariant 6 and a test pinning it).
9. [[sec_hardening_task_9]] — Graduate the durable decisions to `dev_docs/security-hardening.md` and delete this plan folder.

Remaining order: **4** (`ps`, small, unblocks 7), then **7** (blocked by 4 — both rewrite `psPPIDComm`; needs open question 3 answered), then **2**, then **5** once its open question is answered. Task 9 closes the plan out.

## Progress

| | Task | Finding(s) | State |
|---|---|---|---|
| ✅ | 1 — sanitize every dialog interpolation | F2, F4, F8 | merged #15, + `0bb2506` |
| ⬜ | 2 — reject control bytes in `uri.IsOPURI` | (defense in depth) | open |
| ✅ | 3 — resolve dialog helpers from absolute paths | **F1 (HIGH)** | merged #14 |
| ⬜ | 4 — `ps` by absolute path, minimal env | F3 | **next** |
| ⬜ | 5 — resolve `op` once, fail closed | F5 | open — needs question 2 |
| ✅ | 6 — faithful run-mode child rendering | F7, F9, F10 | merged #16, + #19 |
| ⬜ | 7 — identity from exe path, not `comm` | F6 | blocked by 4; needs question 3 |
| ✅ | 8 — invariants + residual risk in docs | (none) | merged #20, + #21 |
| ⬜ | 9 — graduate to `dev_docs/`, delete plan | (none) | last |

**7 of 10 findings closed** — F1, F2, F4, F7, F8, F9, F10. Outstanding: **F3** (task 4), **F5** (task 5), **F6** (task 7).

Not covered by any task: the **2 candidate sites the scan's verification panel left unreviewed** when it hit its budget. They are unexamined, not cleared, and closing every task below still leaves them that way.

## Open questions

1. ~~**Is a compiled-in absolute-path allowlist for helper binaries acceptable under the "no allowlists, no config" rule in `AGENTS.md`?**~~ **Answered by #14 merging.** The rule targets indirection between the user-typed URI and the read; a helper-path list touches neither and is invisible to the user. `resolveHelper` has since taken a third caller without objection.
2. **How strict should `op` resolution be (task 5)?** Strict absolute allowlist (`/usr/local/bin/op`, `/opt/homebrew/bin/op`, `/usr/bin/op`) is the strongest, but breaks anyone with `op` installed elsewhere — and breaking a security tool's happy path invites people to stop using it. The looser alternative is: keep `exec.LookPath`, but reject results under the current working tree, relative PATH entries, and `node_modules/.bin`. The plan currently specifies the looser rule with a hard failure on rejection; say the word to tighten it.
3. **Should the dialog header show the full executable path (task 7), or keep the short name and put the path in the detail line?** Full paths in the header are unambiguous but noisy for the common case (`"claude"` becomes `"/usr/local/bin/claude"`). The plan currently keeps the short name in the header and adds the resolved path to the detail line. **Still open — task 7 needs this before implementation.**
4. ~~**Is a wider truncation limit acceptable for the run-mode line (task 6)?**~~ **Answered by #16 merging**: 300 runes for the child line, explicit `+N more arguments` elision, `argv[0]` never truncated.
5. **F3's panel is worth a second look.** Two of that finding's three verifiers were flagged for out-of-scope behaviour during the scan. The finding rests on four lines of `internal/caller/caller.go` and reads as sound, and the macOS narrowing has since made it *more* load-bearing rather than less — `ps` is now the only identity source. Flagged for the record; it does not change the plan.
6. **What is the durable lesson for display escaping?** Two follow-up fixes on this plan (`0bb2506`, #19) each restored a property the original author believed was held — a missed interpolation site and an unescaped backslash. Both are quoting/escaping bugs found *after* review by someone reading the merged code. Task 8 wrote the rule down (#20).

   **Partly answered.** The specific gap I worried about — bidi overrides slipping past `sanitizeDisplay` — does not exist on the supported platform. `%q` wraps the entire AppleScript body and title, so a U+202E in a URI becomes a literal backslash-u escape, AppleScript's parser rejects the script, `osascript` exits non-zero, and `confirmDarwin` maps that to `ErrDenied`. Verified directly: the escape yields `syntax error … (-2741)`, rc 1. So the layering is `sanitizeDisplay` for C0/C1 (rendered visibly) and `%q` for everything else non-printable (fails closed). That is a real design, not an accident, and it argues **against** a single shared escaper: the two layers deliberately fail differently.

   **Now fully answered — #21** states the layering in invariant 6 and adds `TestDialogScript_NonPrintableNeverReachesAppleScriptSource` to pin it, so the `%q` can no longer be tidied away silently.

   The lesson that survives, for tasks 4, 5 and 7: **escaping and quoting are where review has actually failed on this plan** — four separate corrections (`0bb2506`, #19, and two caught in co-review on #20 and #21), every one a property the author believed was already held. Two of those were doc claims that overstated coverage, which is the same error in prose. Assert what the code does, then go check; don't assert a count you haven't grepped.
