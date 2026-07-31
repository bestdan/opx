---
title: Reject control bytes in uri.IsOPURI so a forged URI fails before the prompt
priority: medium
size: 2
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/uri/uri.go:8 # IsOPURI
  - internal/uri/uri_test.go
  - main.go # validation happens before Confirm; exit code must stay exitUsage
  - main_test.go
tags: [security, validation]
---

Part of [[sec_hardening_plan]]. Defense in depth for **F2, F4, F8** — independent of [[sec_hardening_task_1]] and touching different files, so the two can land in either order.

## Context

`uri.IsOPURI` accepts anything that starts with `op://` and splits into three non-empty segments. That is a *syntax* check with no character-class check at all, so every byte value except `/` is legal inside a segment — including ESC, CR, and BEL.

`AGENTS.md` invariant 3 says op:// URIs are validated before being passed to `op`, and that validation runs **before** `Confirm`, so a malformed URI fails as a usage error without prompting. That ordering is exactly what makes this the right second line of defense: a URI carrying terminal escapes should never reach the dialog at all, whether or not the dialog escapes it.

[[sec_hardening_task_1]] makes the display safe. This task makes the input invalid. Both are worth having: task 1 protects any *future* interpolation site someone adds, task 2 means a control-laden URI is rejected with a clear usage error instead of being silently rendered as `\x1b` gibberish the user has to interpret.

Real 1Password vault, item, and field names can contain spaces, unicode, and punctuation, so the check must be narrow: reject control characters, not "anything unusual". Rejecting C0 (`< 0x20`), DEL, and C1 (`0x7f`–`0x9f`) matches the set `sanitizeDisplay` escapes, which keeps the two rules aligned.

## Task

1. In `IsOPURI`, after the prefix and segment checks, reject `s` if any rune is `< 0x20` or in `0x7f`–`0x9f`. Keep it a pure predicate — no error type, no new exported surface; the callers already map `false` to `exitUsage`.
2. Update the doc comment to state both conditions (three non-empty segments **and** no control characters) and say why the second exists: the URI is rendered into the confirmation dialog, which is the trust boundary.
3. Consider a length cap in the same pass. A 4 KB URI cannot be meaningfully reviewed in a dialog, and the truncation that would hide its tail is the same weakness [[sec_hardening_task_6]] fixes elsewhere. Suggested: reject above 1024 bytes. Flag this in the PR description as a behaviour change if you include it — it is a hard limit on legitimate input, unlike the control-byte rule.
4. Check the `opx run` path specifically: `AGENTS.md` invariant 3 says a value starting with `op://` that fails validation is a usage error rather than being passed through as a literal. Confirm a control-laden env-file value now takes that path, and that the child is not spawned.

## Acceptance Criteria

**Code-enforced**

- `internal/uri/uri_test.go` gains cases: ESC in the vault segment, CR in the field segment, DEL and a C1 byte anywhere, each asserting `false`; plus positive cases proving spaces, unicode (`op://Personal/Café/pässwörd`), and punctuation in segments still return `true`.
- If the length cap lands: a case at the limit returning `true` and one over it returning `false`.
- `main_test.go` (or `run_subcommand_test.go`) asserts that a control-laden URI exits `exitUsage` and that `fakeConfirmer.Confirm` was **never called** — the point of validating first is that no prompt is drawn.
- `run_subcommand_test.go` asserts the same URI in an `--env-file` value exits `exitUsage` and the fake `Spawner` was not invoked.
- `make test` and `make lint` pass.

**User-run**

- None; fully covered by tests.
