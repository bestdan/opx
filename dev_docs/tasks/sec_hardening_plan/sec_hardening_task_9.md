---
title: Graduate the durable decisions to dev_docs and delete the plan folder
priority: low
size: 1
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
is_blocked_by: sec_hardening_task_8
related_files:
  - dev_docs/tasks/sec_hardening_plan/
  - dev_docs/security-hardening.md # to be created
tags: [chore, docs]
---

Part of [[sec_hardening_plan]]. The plan is not done until its scaffolding is gone.

## Context

`dev_docs/tasks/` holds live scaffolding only. Once tasks 1–8 have merged, this plan folder is residue — nine files describing work that is finished, which future readers will mistake for a backlog. But some of what is in it is genuinely durable and must not be deleted with it.

## Task

1. Write `dev_docs/security-hardening.md` capturing only what a future developer needs and cannot reconstruct from the code:
   - The four root causes and why the code originally had them — this was not carelessness; `sanitizeCallerDetail` shows the author found the class and fixed the instance in front of them, and understanding that is what stops the next instance.
   - The helper-resolution design: the allowlists, why the mode check exists, and why `op` fails closed while the dialog helper degrades to TTY.
   - The ancestor-vs-child rendering split and the explicit warning against merging them.
   - The residual risk from [[sec_hardening_task_8]], stated once, canonically.
   - The decisions taken on this plan's open questions and the reasoning, so they are not relitigated.
2. Do **not** copy the task files' step-by-step instructions across. Those describe how the work was done, which is now in git history where it belongs.
3. Delete `dev_docs/tasks/sec_hardening_plan/` entirely.
4. Decide what happens to `CLAUDE-SECURITY-20260731-081020/`. It sits behind its own `.gitignore` and has never been committed. Options: leave it local (default), or commit the report as a dated record. If it is kept for reference, note in `dev_docs/security-hardening.md` which findings it contains and that two candidate sites went unreviewed at the panel's budget — that gap should not be forgotten just because the report reads clean.

## Acceptance Criteria

**Code-enforced**

- `dev_docs/tasks/sec_hardening_plan/` no longer exists.
- `dev_docs/security-hardening.md` exists and every internal link in it resolves.
- `make test` and `make lint` pass.

**User-run**

- Read `dev_docs/security-hardening.md` cold and confirm it answers "why is `zenity` resolved this way?" without needing the deleted plan folder or the PR descriptions.
