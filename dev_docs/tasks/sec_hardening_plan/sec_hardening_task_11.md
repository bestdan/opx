---
title: A non-printable rune in a legitimate item name makes opx unusable and blames the user
priority: low
size: 2
status: new
created: 2026-08-01
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/prompt/prompt.go # sanitizeDisplay, dialogScript, confirmDarwin
  - internal/uri/uri.go # IsOPURI
  - AGENTS.md # invariants 3 and 6
tags: [usability, prompt]
---

Part of [[sec_hardening_plan]]. Found while building [[sec_hardening_task_2]]; **pre-existing**, not introduced by it, and not one of the ten scan findings.

## The defect

A 1Password item whose name contains a rune Go's `%q` escapes — but that is not a C0/C1 control character — cannot be read through `opx` at all, and the failure names the wrong cause.

`dialogScript` embeds the dialog body via `%q`, which renders any `!strconv.IsPrint` rune as a literal `\uXXXX`. AppleScript has no such escape, so the script fails to parse, `osascript` exits non-zero, and `confirmDarwin` maps that to `ErrDenied`. The user sees `denied (access denied by user)` — for a secret they never got the chance to approve, because no dialog was ever drawn.

For a forged URI that is the correct, deliberate behaviour (invariant 6: the layers fail differently on purpose). The problem is that the same path catches **ordinary names**. Measured on macOS 26.4 by building the exact source `dialogScript` builds and having `osascript` parse it — assignment only, no dialog displayed:

```
U+2028 line separator          REJECTED   syntax error … (-2741)
U+2029 paragraph separator     REJECTED   syntax error … (-2741)
U+202E bidi override           REJECTED   syntax error … (-2741)
U+200D ZWJ (emoji sequences)   REJECTED   syntax error … (-2741)
U+00A0 non-breaking space      REJECTED   syntax error … (-2741)
plain emoji (U+1F511)          PARSES
ordinary ASCII                 PARSES
```

The first three are the ones invariant 6 intends to fail closed. The last two are not: `U+200D` is what joins an emoji sequence, so an item named `👨‍💻 Work` is unreadable, and `U+00A0` is what macOS types on **option-space**, so a name with one typed by accident is indistinguishable from a name that is fine. Plain emoji work, which makes the failure look arbitrary.

## Why this is not obviously fixable in either direction

Both candidate fixes change a documented invariant, which is why [[sec_hardening_task_2]] did not fold it in:

1. **Reject `!strconv.IsPrint` in `IsOPURI`.** Turns the misleading denial into a clear usage error, and is a two-line change. But it widens invariant 3 from "control characters" to "anything Go considers non-printable" — a rule whose boundary is a Go stdlib implementation detail rather than anything about terminals — and the name still cannot be read. It relabels the failure without removing it.
2. **Extend `sanitizeDisplay` to render every `!strconv.IsPrint` rune visibly.** The read then *succeeds*: the dialog shows an escaped form, `op` receives the real string. But it collapses invariant 6's deliberate two-layer split, where `sanitizeDisplay` handles C0/C1 visibly and `%q` fails everything else closed. Anything that becomes visible stops failing closed, and the bidi-override case (U+202E) is one where failing closed is the point — a reordered path in the dialog is precisely the lie the layering exists to prevent.

A third option is to split the difference: render the *safe* non-printables visibly (`U+00A0`, `U+200D` — no reordering or line-breaking effect) and keep failing closed on the ones with display-integrity consequences (`U+2028`, `U+2029`, and the bidi controls `U+202A`–`U+202E`, `U+2066`–`U+2069`). That preserves both invariants' intent but replaces one boundary with a hand-maintained list, which is the kind of thing that rots.

**Decide deliberately and write down which and why** — this is the same shape as task 10's step 4, and the wrong reflex is to reach for whichever is fewest lines.

## Task

1. Pick one of the three directions above. State the reasoning in the code, not just the PR.
2. Implement it, keeping invariants 3 and 6 accurate afterwards — if the chosen fix moves the boundary between them, `AGENTS.md` must say so in the same change.
3. Whatever is chosen, the failure must stop reporting `denied (access denied by user)` for a request the user was never shown. If the read cannot proceed, the diagnostic has to name the actual cause.

## Acceptance Criteria

**Code-enforced**

- A test that an item name containing `U+200D` and one containing `U+00A0` produce whatever the chosen behaviour is — a successful dialog body, or a usage error — and **not** `ErrDenied`.
- `TestDialogScript_NonPrintableNeverReachesAppleScriptSource` still passes, or is deliberately amended with the reasoning recorded; it is the guard on invariant 6's `%q` layer.
- A test pinning that the display-integrity runes (`U+2028`, `U+2029`, `U+202E`) still fail closed, whichever direction is taken.
- `make test`, `make lint` pass.

**User-run**

- Create an item whose name contains an option-space, and read a field from it through `opx`.

## Note

Frequency is low but not hypothetical: option-space is a single keystroke slip on macOS, and emoji in item names are ordinary. Priority is low because nothing is *unsafe* here — the failure is closed, just mislabelled and unhelpful.
