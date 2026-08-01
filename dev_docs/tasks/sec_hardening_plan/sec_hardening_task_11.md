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

## Outcome — merged as #27 (pending)

**Direction taken: the third — split the difference — but not with the
hand-maintained list the option was written around.**

The three candidates differ only in what happens to which runes, so it is
worth stating the two questions separately. *Does the read succeed?* and
*what does the dialog show?* For every non-printable rune except one class,
those have the same answer under options 2 and 3: escape it visibly, let the
read proceed. The class where they differ is the runes whose effect is on the
text *around* them — bidi controls, and the line/paragraph separators — where
an escaped rendering and a raw one disagree about what the rest of the URI
says. That is the whole of the disagreement, and it is where the boundary
belongs.

Why not the other two:

- **Option 1 (reject `!IsPrint` in `IsOPURI`)** relabels the failure without
  removing it — `👨‍💻 Work` is still unreadable — and it is incomplete on its
  own terms: the same `%q` catches the caller name, the origin line and the
  through line, none of which any URI validator sees. Invariant 3 is
  untouched by what shipped.
- **Option 2 (escape everything non-printable)** fixes the usability half and
  gives up the fail-closed on bidi overrides, on the argument that escaping is
  enough. In this repo that argument has a track record: four corrections on
  this plan, all in escaping and quoting, every one a property its author
  believed held.

**The task file's objection to option 3 does not survive contact with the
standard library.** "A hand-maintained list that rots" is right about lists
and wrong about this set: `unicode.Bidi_Control` ∪ `Zl` ∪ `Zp` is Unicode's
own name for the class, maintained by the Go release. The objection is also
self-demonstrating — the list sketched in this file for exactly this purpose
omitted U+061C, U+200E and U+200F, three of the twelve.

**Invariant 6 moves, and AGENTS.md says so in the same change.** The layers
still fail differently on purpose; what changed is where the line sits — `%q`
used to hold the whole non-printable range. Both directions are now written
down, because both are real: narrowing it removes the fail-closed, widening it
makes ordinary items unreadable.

**The misleading denial is fixed at its own layer, not as a side effect.**
`confirmDarwin` catches the fail-closed runes before it spends an `osascript`
and returns a new `ErrUndisplayable` naming the code point; `main` reports it
on stderr regardless of verbosity and exits 1, leaving exit 3 to mean the user
said no. The check is a diagnostic — `%q` is still the guard — and the code
says so, because a reader who mistakes it for the guard will "simplify" the
`%q` away.

### What the acceptance criteria got right, and the one thing they didn't

The third code-enforced criterion — "display-integrity runes still fail
closed, whichever direction is taken" — matches what shipped, and would have
been wrong for option 2, which was still live when it was written. Worth
noticing rather than trusting: a criterion written before the design is
settled can only match some of the designs it is meant to judge.

The user-run criterion (an item named with an option-space, read through
`opx`) needs a real vault and is left to the reviewer.

### What was measured rather than reasoned

- `strconv.IsPrint` is false for U+00A0, U+200D, U+2028, U+2029 and the tag
  characters, and true for plain emoji — the discriminator the task file
  identified, confirmed on this toolchain.
- `unicode.Bidi_Control` on Go's Unicode 15.0.0 tables is exactly the twelve
  explicit formatting characters: U+061C, U+200E, U+200F, U+202A–U+202E,
  U+2066–U+2069.
- End-to-end against a real `osacompile`, on the source `dialogScript`
  actually builds: ZWJ, NBSP, tag characters, C0 escapes and plain text all
  compile; U+202E and U+2028 still do not.
- **An unstated premise that nearly cost an afternoon:** under the Bash
  sandbox `osacompile` cannot load StandardAdditions, so *every* script fails
  to compile — including a bare `beep 3`. The first end-to-end run reported
  the escaping broken for all seven cases, plain ASCII included. The tell was
  that the control case failed too.

### Co-review (#27)

Three external reviewers. One returned no findings. The other two each raised
one substantive claim, and both were empirically false:

- That `strconv.IsPrint(U+00A0)` is true, so the NBSP case could not work.
  It is false; Go's `IsPrint` admits only ASCII space from `Zs`.
- That an undisplayable rune in `Caller` escapes the check, because
  `message`'s header `%q`s the caller. Half-right premise: it does — but
  `dialogTitle` interpolates with `%s`, so the raw rune survives there and is
  caught. Verified by running `confirmDarwin`.

The second claim was wrong and useful anyway. The property holds through a
route nobody would guess from reading `confirmDarwin`, and nothing pinned it —
so a later change adding `%q` to `dialogTitle`, which invariant 6's own
emphasis invites, would have sent that case quietly back to reporting a
denial. `TestConfirmDarwin_UndisplayableRuneCaughtInEveryField` now covers
every caller-controlled field, and says why.
