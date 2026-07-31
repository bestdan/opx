---
name: 1password-audit
description: Audit a project's 1Password secret management hygiene — checks for hardcoded secrets, gitignore coverage, op run in dev scripts, detect-secrets setup, --account safety, and whether interactive reads go through opx. Run in a project directory.
argument-hint: "[path]"
allowed-tools:
  - Glob
  - Grep
  - Read
  - Bash(git ls-files:*)
  - Bash(git check-ignore:*)
  - Bash(git rev-parse:*)
  - Bash(command -v:*)
disallowed-tools:
  - Edit
  - Write
  - NotebookEdit
version: 1.1.2
---

# 1Password Secrets Audit

Audit a project directory for 1Password secret management hygiene. Read-only — produce a report only; do not edit any files.

**Arguments:** `$ARGUMENTS`

**Reference:** [`1password-local-dev-best-practices.md`](./1password-local-dev-best-practices.md) — the rationale behind these checks (vault structure, service account scoping, AI-agent and supply-chain hardening, common foot-guns). Read it when a finding needs justification beyond PASS/FAIL, or when the user asks *why* a check matters.

## Step 0 — Determine Project Root

If a path was given in `$ARGUMENTS`, use it. Otherwise use the current working directory.

Confirm it is a directory. If not, tell the user and stop.

Run `git rev-parse --show-toplevel` from the directory to find the git root (use this for `.gitignore` checks). If not in a git repo, note this in the report — some checks won't apply.

---

## Step 1 — Locate Secret-Related Files

Search for the following files, starting from the project root:

**Config/secret files to read:**

- `.env`, `.env.*` (any variant: `.env.local`, `.env.development`, `.env.secrets`, etc.)
- `.envrc`
- `.pre-commit-config.yaml`
- `.secrets.baseline`
- Any `*.tpl` or `*.tmpl` files that contain `op://` references (these are template files for `op inject`)

**Dev script files to read:**

- `package.json`
- `Makefile`
- `justfile`, `Justfile`
- Any `.bash`, `.sh` files under a `scripts/`, `bin/`, or `script/` directory

**Docs to read (for Check 8):**

- `README.md` and any onboarding docs under `docs/` — these often carry the `op read` snippets developers copy

Run these searches in parallel.

Note: Do NOT read files that are likely `op inject` outputs (resolved secrets). If a `database.yml.tpl` exists alongside a `database.yml`, read only the `.tpl`.

---

## Step 2 — Run Each Check

Run all checks. For each one, record: **PASS**, **FAIL**, **WARN**, or **N/A** (not applicable — the file/pattern doesn't exist in this project).

### Check 1: No hardcoded secrets in .env files

For each `.env*` file found:

Read the file. For each line that is not a comment and not blank:

- If the value is an `op://` reference → safe, skip
- If the value is empty → safe, skip
- If the value looks like a real secret → **FAIL**

A value "looks like a real secret" if it matches any of:

- A known secret prefix: `sk_live_`, `sk_test_`, `rk_live_`, `ghp_`, `gho_`, `ghs_`, `glpat-`, `AKIA`, `xoxb-`, `xoxp-`, `xoxa-`, `SG.`, `EAA` (Facebook token pattern)
- An alphanumeric string of 32+ characters with no spaces (entropy heuristic — likely a key or token)
- A connection string containing a password component: `://[^:]+:[^@]+@`

Do **not** FAIL on values that only look secret-shaped but are public by design: Stripe publishable keys (`pk_live_`, `pk_test_`), Twilio Account SIDs (`AC…`), AWS access key *IDs* on their own, and OAuth client IDs. These are meant to be shipped to clients. If one is present alongside its real counterpart (`STRIPE_SECRET_KEY`, `TWILIO_AUTH_TOKEN`, `AWS_SECRET_ACCESS_KEY`), report only the counterpart.

If a file is named `.env.secrets` and ALL values are `op://` refs, note it as correctly structured.

For **FAIL** findings, include: filename, line number, variable name, and the matched pattern (not the value itself).

### Check 2: .gitignore covers op inject output files

For each `*.tpl` or `*.tmpl` file found, derive its output filename: strip the `.tpl`/`.tmpl` suffix (e.g. `database.yml.tpl` → `database.yml`).

Run: `git check-ignore -q <output-file>` for each derived filename.

- Exit 0 → file is gitignored → **PASS** for that file
- Exit 1 → file is NOT gitignored → **FAIL** — the resolved output file could be committed accidentally

Also check for these commonly-missed entries:

- `.env.local` — should be gitignored
- `*.secrets.resolved` — should be gitignored (if the project uses this convention)
- Any `.env*` file that does NOT end in `.secrets` or `.tpl` and is not `.env.example` — flag as **WARN** (these are candidates for accidental commit)

### Check 3: Dev scripts use op run

For each of `package.json`, `Makefile`, `justfile`:

**package.json** — read the `scripts` object. For each script whose name is `start`, `dev`, `serve`, `run`, or `watch`: check if the command includes `op run`. If any matching script does NOT include `op run`, record it as a **WARN** with the script name and current command.

**Makefile / justfile** — read the file. For each target whose name contains `dev`, `start`, `run`, or `serve`: check if the recipe includes `op run`. If any matching target does NOT include `op run` but the project has `.env.secrets` or `.envrc` (indicating secrets are in use), record as **WARN** with the target name.

**Shell scripts** — for each `*.sh` / `*.bash` file collected in Step 1 from `scripts/`, `bin/`, or `script/` whose filename or path matches `dev`, `start`, `run`, `serve`, or `boot`: check if it invokes `op run`. If it doesn't and the project has `.env.secrets` or `.envrc`, record as **WARN** with the script path.

If none of these files exist, mark **N/A**.

### Check 4: op run and from_op always specify --account

Scan all dev scripts, `Makefile`, `justfile`, and `.envrc`. Before checking, **logically join shell line continuations**: any line ending in `\` should be concatenated with the next line so a multi-line `op run \ --env-file=... \ -- npm start` is treated as one command.

After joining, flag:

- `op run` without `--account` in the same logical command
- `from_op` without `--account` in the same logical command
- `op read` without `--account` in the same logical command (also a Check 8 finding — the fix there is `opx`, which supersedes this one)

For each match: **FAIL** with filename and starting line number of the logical command. Multi-account environments will silently resolve against the wrong vault without `--account`.

Exception: if only one account is configured globally (no way to verify statically), note this as a **WARN** rather than **FAIL** and explain why `--account` still matters for shared configs.

### Check 5: detect-secrets pre-commit hook is configured

Check 1: Does `.pre-commit-config.yaml` exist?

- No → **FAIL**

Check 2: If it exists, does it contain a hook with `id: detect-secrets`?

- Yes → continue to check 3
- No → **FAIL**

Check 3: Does `.secrets.baseline` exist?

- Yes → **PASS**
- No → **FAIL** — the hook is configured but the baseline hasn't been generated, so the hook will fail on first run

If `.pre-commit-config.yaml` uses a pinned `rev` for the detect-secrets repo, note the version for awareness (no pass/fail judgement on version currency).

### Check 6: .envrc structure

If `.envrc` does not exist → **N/A**.

If it exists, check:

- Does it use `from_op`, `op read`, or `opx`? If not, it may not be loading secrets via 1Password at all — **WARN**
- Does every `from_op` call include `--account`? (Covered by Check 4, but note here too if applicable)
- Does it use plain `export SECRET=<value>` for anything that looks like a secret value? → **FAIL** (this writes to shell history)

### Check 7: No op inject output files tracked in git

Run `git ls-files` to get all tracked files. For each tracked file, check if a corresponding `.tpl`/`.tmpl` source exists:

- If `database.yml` is tracked AND `database.yml.tpl` exists → **FAIL** — the resolved file may have been committed
- More generally: if any tracked `.env*` file (not `.env.example`, not `.env.secrets`) exists → **WARN** with filename

### Check 8: Interactive secret reads go through opx

`opx` wraps `op read` so every read shows a dialog naming the URI and the calling process, normally raises a fresh biometric prompt (it leaves no session cached between calls), and runs `op signout --all` afterwards. Raw `op read` reuses whatever session is already cached, so any other process running as the developer can read secrets silently in that window. See section 4 of the best-practices reference.

Scan dev scripts, `Makefile`, `justfile`, `.envrc`, `README.md`, and any `docs/` onboarding files (joining line continuations as in Check 4):

- Any invocation of `op read` → **WARN** with file and line, recommending `opx '<uri>'` in its place. Exception: if the command is inside a CI config, a Dockerfile, or a block clearly labelled as CI/headless, mark it **N/A** for that occurrence — `opx` requires a GUI dialog or TTY and is denied without one.
- Any `from_op` call in `.envrc` → **WARN** (informational): `from_op` resolves via `op inject` on `cd`, so reads are not individually approved and a session is left cached. Note the trade-off; do not treat direnv usage as a failure.
- If `opx` invocations are present anywhere in the project, check whether `OP_ACCOUNT` is exported in `.envrc` or documented in the README. `opx` has no `--account` flag, so a multi-account developer without `OP_ACCOUNT` resolves against the wrong account. Missing → **WARN**.
- Run `command -v opx` to check whether the binary is installed locally. Not installed → note it in the report as an environment finding, not a project failure.

Mark **N/A** if the project contains no `op read`, no `from_op`, and no `opx` usage.

---

## Step 3 — Build the Report

Produce a structured report:

```markdown
# 1Password Secrets Audit — <project-name>

Audited: <project root path>
Date: <today>

---

## Summary

| Check                                          | Status            |
| ---------------------------------------------- | ----------------- |
| 1. No hardcoded secrets in .env files          | PASS / FAIL / N/A |
| 2. .gitignore covers op inject outputs         | PASS / FAIL / N/A |
| 3. Dev scripts use op run                      | PASS / WARN / N/A |
| 4. --account specified in all op/from_op calls | PASS / FAIL / N/A |
| 5. detect-secrets pre-commit hook configured   | PASS / FAIL / N/A |
| 6. .envrc structure                            | PASS / WARN / N/A |
| 7. No op inject outputs tracked in git         | PASS / FAIL / N/A |
| 8. Interactive reads go through opx            | PASS / WARN / N/A |

---

## Findings

### FAIL — [Check name]

[For each FAIL: file path, line number, specific issue, and recommended fix]

### WARN — [Check name]

[For each WARN: file path, line number, what to consider]

---

## What to Fix First

[Prioritized list of FAIL items only, in order of risk:

1. Hardcoded secrets in committed files (immediate rotation risk)
2. op inject outputs tracked in git (resolved secrets may be in history)
3. Missing --account (silent wrong-vault resolution)
4. No detect-secrets hook (future commits unguarded)
5. Raw `op read` where `opx` would apply (reads reuse a cached session, unprompted)
6. Other]

---

## Manual Checks (cannot be verified statically)

- Biometric unlock (Touch ID) is enabled in 1Password app
- `opx` is installed and on `PATH`, and `OP_ACCOUNT` is exported in the developer's shell profile
- SSH keys are stored in 1Password, not as plaintext files under ~/.ssh
- AI coding tool sessions do not have access to service account tokens with broad vault permissions
- Vault structure uses per-project or per-team vaults (not the default Shared vault)
- Credentials are rotated when team members leave or on a quarterly cadence
```

If all checks pass, say so clearly and skip the Findings section. Keep the Manual Checks section regardless.

---

## Rules

- **Read-only.** Do not edit, write, or modify any files. All recommendations stay in the report.
- **Never print secret values.** When referencing a finding, identify it by variable name and matched pattern — never echo the actual value.
- **Be specific.** Reference exact file paths and line numbers for every finding.
- **Distinguish severity.** FAIL = active risk or broken workflow. WARN = common footgun worth addressing. N/A = genuinely not applicable (the construct doesn't exist in this project).
- **Don't manufacture findings.** If a check passes cleanly, say so.
- **`opx` for reads, `op run` for launches.** When recommending `opx`, only recommend it in place of `op read`. Do not suggest replacing `op run` or `op inject` with `opx --env` — `op run` scopes secrets to a subprocess, `opx --env` puts them in the shell, which is weaker.
