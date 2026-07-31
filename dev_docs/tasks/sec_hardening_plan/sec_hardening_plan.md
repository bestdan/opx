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

| # | Root cause | Findings | Tasks |
|---|---|---|---|
| A | Dialog body interpolates caller-controlled text without sanitization — `sanitizeCallerDetail` from `a58bd99` was never applied to `Binding.URI` / `Binding.Name` | F2, F4, F8 | [[sec_hardening_task_1]], [[sec_hardening_task_2]] |
| B | Security-critical helper binaries (`zenity`, `osascript`, `ps`, `op`) resolved by bare name through caller-controlled PATH | **F1 (HIGH)**, F3, F5 | [[sec_hardening_task_3]], [[sec_hardening_task_4]], [[sec_hardening_task_5]] |
| C | Run-mode dialog understates the child that receives the secrets — `renderArgv` basenames path-like tokens, `RenderCommand` cuts at 120 runes | F7, F9, F10 | [[sec_hardening_task_6]] |
| D | Caller identity is the self-asserted `comm` string; the verifiable executable path is fetched and then discarded | F6 | [[sec_hardening_task_7]] |

Group B contains the only HIGH: on Linux a planted `zenity` that exits `0` **is** the approval, and its presence also suppresses the `/dev/tty` fallback, so no prompt appears at all.

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

1. [[sec_hardening_task_1]] — Sanitize every dialog-body interpolation, not just `CallerDetail` (F2, F4, F8).
2. [[sec_hardening_task_2]] — Reject C0/C1 control bytes in `uri.IsOPURI` so a forged URI fails before the prompt (F2, F4, F8, defense in depth).
3. [[sec_hardening_task_3]] — Resolve `zenity` and `osascript` from a trusted absolute-path allowlist; fall back to TTY (**F1, HIGH**).
4. [[sec_hardening_task_4]] — Invoke `ps` by absolute path with a minimal environment (F3).
5. [[sec_hardening_task_5]] — Resolve `op` once, reject untrusted locations, fail closed (F5).
6. [[sec_hardening_task_6]] — Render the run-mode child command faithfully: full paths, full values, explicit elision (F7, F9, F10).
7. [[sec_hardening_task_7]] — Base caller identity on the executable path, not the self-asserted `comm` (F6).
8. [[sec_hardening_task_8]] — Record the new invariants and the residual risk in `AGENTS.md` and `README.md`.
9. [[sec_hardening_task_9]] — Graduate the durable decisions to `dev_docs/security-hardening.md` and delete this plan folder.

Tasks 1–7 are independently shippable PRs and touch disjoint files, with two exceptions noted in their `is_blocked_by` fields. Task 8 waits on the code; task 9 closes the plan out.

## Open questions

1. **Is a compiled-in absolute-path allowlist for helper binaries acceptable under the "no allowlists, no config" rule in `AGENTS.md`?** That rule targets indirection between the user-typed URI and the read. A helper-path allowlist is invisible to the user and does not touch the URI, so I read it as permitted — but it is the one place this plan brushes against a stated invariant, and it is load-bearing for the HIGH finding.
2. **How strict should `op` resolution be (task 5)?** Strict absolute allowlist (`/usr/local/bin/op`, `/opt/homebrew/bin/op`, `/usr/bin/op`) is the strongest, but breaks anyone with `op` installed elsewhere — and breaking a security tool's happy path invites people to stop using it. The looser alternative is: keep `exec.LookPath`, but reject results under the current working tree, relative PATH entries, and `node_modules/.bin`. The plan currently specifies the looser rule with a hard failure on rejection; say the word to tighten it.
3. **Should the dialog header show the full executable path (task 7), or keep the short name and put the path in the detail line?** Full paths in the header are unambiguous but noisy for the common case (`"claude"` becomes `"/usr/local/bin/claude"`). The plan currently keeps the short name in the header and adds the resolved path to the detail line.
4. **Is a wider truncation limit acceptable for the run-mode line (task 6)?** The current cut is 120 runes. A long `sh -c '...'` payload will not fit at any sane width, so the plan makes the elision explicit (`… +3 more arguments`) rather than raising the limit much. macOS `display dialog` will scroll a long body; the TTY path wraps.
5. **F3's panel is worth a second look.** Two of that finding's three verifiers were flagged for out-of-scope behaviour during the scan. The finding itself rests on four lines of `internal/caller/caller.go:327-344` and reads as sound, but the task is cheap enough that this shouldn't change the plan — flagging it for the record.
