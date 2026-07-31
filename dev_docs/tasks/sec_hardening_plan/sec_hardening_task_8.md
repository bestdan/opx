---
title: Record the new trust invariants and the residual risk in AGENTS.md and README.md
priority: medium
size: 2
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
is_blocked_by: sec_hardening_task_7
related_files:
  - AGENTS.md # "Security invariants — do not regress"
  - README.md # threat model
  - dev_docs/development.md
tags: [docs, security]
---

Part of [[sec_hardening_plan]]. No finding of its own — this is what stops the other seven from being quietly undone.

## Context

`AGENTS.md` has a numbered "Security invariants — do not regress" list, and it is the reason this codebase has held its shape. Every invariant on it corresponds to a property someone would otherwise refactor away as cleanup. The five current entries cover Confirm-before-read, ForgetSession-on-every-path, URI validation, the secret sink, and batch atomicity.

The fixes in tasks 1–7 add three properties that are equally invisible in the code and equally easy to undo:

- **Nothing caller-controlled reaches the dialog unescaped.** Someone adding a field to `Request` and interpolating it into `message()` reintroduces F2/F4/F8 in one line, and no test they think to write would catch it.
- **Security-critical helpers are located by absolute path, never PATH.** `exec.Command("zenity", ...)` looks completely normal. That is the HIGH.
- **The run-mode child line is authorization-relevant and must render faithfully.** The obvious "cleanup" is to notice `renderChildCommand` and `renderAncestorArgv` are nearly identical and merge them back — which is precisely F7/F9.

The README's threat model also needs the honest limit stated, and this is the task where honesty matters most: **opx does not defend against an attacker who already executes code as the user with the ability to write to user-writable absolute paths.** The PATH fixes close a specific, cheap, ambient vector — `node_modules/.bin`, a PATH prefix set for one command. They do not make the tool safe against a fully compromised account, and a README that implies otherwise sells a guarantee the code cannot keep.

## Task

1. Add three invariants to `AGENTS.md`'s numbered list, in the same voice as the existing five — each stating the property, where it lives, and what breaking it costs:
   - Dialog-body interpolation passes through `sanitizeDisplay`; `message()` in `internal/prompt` is the chokepoint.
   - `zenity`, `osascript`, `ps`, and `op` are resolved from compiled-in absolute paths; PATH belongs to the process being prompted about, so it is never consulted for a security-critical helper.
   - `RenderCommand`'s output is an authorization statement about the child that receives the secrets, not a summary; it must not basename, drop, or silently truncate any part of the destination. Note explicitly that it may **not** be merged back into the ancestor renderer.
2. Extend the "Things to avoid" list with the merge trap in (1c) and with "do not reintroduce `exec.LookPath` for a security-critical helper".
3. Update `README.md`'s threat model with what is defended and what is not. Concretely: a planted `zenity`/`ps`/`op` earlier on PATH is defended; an attacker who can write to `/usr/local/bin` or modify the user's shell profile is not. State it plainly — a caveat buried in a subordinate clause is not a caveat.
4. If the `opx run` dialog's appearance changed materially in [[sec_hardening_task_6]] or [[sec_hardening_task_7]], update any README screenshot or sample dialog text so the documentation matches what users see.
5. Check `skills/` for anything now inaccurate. Per `AGENTS.md`, material skill edits require a `version` bump in the skill frontmatter **and** in both `.claude-plugin/*.json` manifests — do the bump if you touch them, skip it if you do not.

## Acceptance Criteria

**Code-enforced**

- `make test` and `make lint` still pass (no code change expected, but the manifests are parsed).
- If `.claude-plugin/*.json` changed, both files remain valid JSON and their `version` fields agree with the skill frontmatter.

**User-run**

- Read the three new invariants against the merged code and confirm each one describes what the code actually does now — an invariant that overstates is worse than none, because the next reader trusts it.
- Confirm the README's threat-model section can be read by someone deciding whether to adopt opx and leaves them with an accurate picture of what it does not stop.
