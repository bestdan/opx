---
title: Record the new trust invariants and the residual risk in AGENTS.md and README.md
priority: high
size: 2
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - AGENTS.md # "Security invariants — do not regress"
  - README.md # threat model
  - dev_docs/development.md
tags: [docs, security]
---

Part of [[sec_hardening_plan]]. No finding of its own — this is what stops the other seven from being quietly undone.

> **Unblocked and promoted to `high`.** Originally sequenced last, after every
> code task. That was wrong: tasks 1, 3 and 6 have merged, so three security
> properties are live in the tree with nothing in `AGENTS.md` describing them,
> and the window where someone refactors one away is open *now*. Write the
> invariants for what has merged; tasks 4, 5 and 7 add their own lines when
> they land. The `is_blocked_by: sec_hardening_task_7` edge is removed.
>
> Evidence this is not hypothetical: `0bb2506` and #19 were both follow-up
> fixes to merged work on this plan, each restoring a property the original
> author (me) believed was already held.

## Context

`AGENTS.md` has a numbered "Security invariants — do not regress" list, and it is the reason this codebase has held its shape. Every invariant on it corresponds to a property someone would otherwise refactor away as cleanup. The five current entries cover Confirm-before-read, ForgetSession-on-every-path, URI validation, the secret sink, and batch atomicity.

The fixes in tasks 1–7 add three properties that are equally invisible in the code and equally easy to undo:

- **Nothing caller-controlled reaches the dialog unescaped.** Someone adding a field to `Request` and interpolating it into `message()` reintroduces F2/F4/F8 in one line, and no test they think to write would catch it.
- **Security-critical helpers are located by absolute path, never PATH.** `exec.Command("zenity", ...)` looks completely normal. That is the HIGH.
- **The run-mode child line is authorization-relevant and must render faithfully.** The obvious "cleanup" is to notice `renderChildCommand` and `renderAncestorArgv` are nearly identical and merge them back — which is precisely F7/F9.

The README's threat model also needs the honest limit stated, and this is the task where honesty matters most: **opx does not defend against an attacker who already executes code as the user with the ability to write to user-writable absolute paths.** The PATH fixes close a specific, cheap, ambient vector — `node_modules/.bin`, a PATH prefix set for one command. They do not make the tool safe against a fully compromised account, and a README that implies otherwise sells a guarantee the code cannot keep.

## Task

1. Add three invariants to `AGENTS.md`'s numbered list (currently five), in the same voice as the existing entries — each stating the property, where it lives, and what breaking it costs. Write them for what has **merged**, not for the plan's eventual end state:
   - **Every caller-controlled value reaching the dialog passes through `sanitizeDisplay`** — body *and* title. Scope it that way, not as "`message()` is the chokepoint": `message()` was the original sweep's scope, and the title interpolation it missed is what `0bb2506` had to fix.
   - **Dialog helpers are resolved from compiled-in absolute paths via `resolveHelper`, never PATH.** Currently `osascript` and the `defaults` appearance query. PATH belongs to the process being prompted about. `ps` and `op` join this line when tasks 4 and 5 land — do not describe them as already covered.
   - **`RenderCommand`'s output is an authorization statement about the child receiving the secrets, not a summary.** It must not basename, drop, or silently truncate any part of the destination, and it may **not** be merged back into `renderAncestorArgv` however similar the two look.
2. Extend the "Things to avoid" list with the merge trap in (1c), with "do not reintroduce `exec.LookPath` for a security-critical helper", and with a note that display quoting is not shell quoting: `internal/shellquote` is for `eval` consumption and is the wrong tool for the dialog, which is how the hand-rolled quoter in #16 came to exist — and then to ship the backslash bug #19 fixed. Anyone touching `quoteForDisplay` should be pointed at that pair of commits.
   The "Things to avoid" list has grown since this plan was written (the `beep` entry), so read it before editing rather than appending blind.
3. Update `README.md`'s threat model with what is defended and what is not. As of the merged work: a planted `osascript` earlier on PATH is defended; a planted `ps` or `op` is **not yet** (tasks 4 and 5). An attacker who can write to `/usr/local/bin` or modify the user's shell profile is out of reach entirely. State it plainly — a caveat buried in a subordinate clause is not a caveat, and claiming coverage the code does not have is worse than claiming none.
4. If the `opx run` dialog's appearance changed materially in [[sec_hardening_task_6]] or [[sec_hardening_task_7]], update any README screenshot or sample dialog text so the documentation matches what users see.
5. Check `skills/` for anything now inaccurate. Per `AGENTS.md`, material skill edits require a `version` bump in the skill frontmatter **and** in both `.claude-plugin/*.json` manifests — do the bump if you touch them, skip it if you do not.

## Acceptance Criteria

**Code-enforced**

- `make test` and `make lint` still pass (no code change expected, but the manifests are parsed).
- If `.claude-plugin/*.json` changed, both files remain valid JSON and their `version` fields agree with the skill frontmatter.

**User-run**

- Read the three new invariants against the merged code and confirm each one describes what the code actually does now — an invariant that overstates is worse than none, because the next reader trusts it.
- Confirm the README's threat-model section can be read by someone deciding whether to adopt opx and leaves them with an accurate picture of what it does not stop.
