---
name: 1password-migrate
description: Migrate an existing .env file to 1Password — reads hardcoded secrets, maps them to op:// references, and produces a runbook with op item create commands, a new .env.secrets file, and updated dev script invocations.
argument-hint: "[path] [account]"
allowed-tools:
  - Glob
  - Grep
  - Read
  - AskUserQuestion
  - Bash(git ls-files:*)
disallowed-tools:
  - Edit
  - Write
  - NotebookEdit
version: 1.1.0
---

# 1Password Migration Runbook

Migrate an existing project from hardcoded `.env` files to `op://` references. Read-only — produce a runbook only; do not edit any files.

**Arguments:** `$ARGUMENTS`

**Reference:** [`1password-local-dev-best-practices.md`](../1password-audit/1password-local-dev-best-practices.md) — vault structure, service account scoping, the `opx` read model (section 4), and the reasoning behind these conventions.

---

## Phase 1 — Parse Arguments

Parse `$ARGUMENTS` positionally:

1. **Path** (optional, first arg) — path to a `.env` file or a project directory. If a directory, scan it for `.env` files (see Phase 2). Defaults to the current working directory.

2. **Account** (optional, second arg) — the 1Password account domain (e.g. `your-team.1password.com`). If not provided, defer to Phase 3.

---

## Phase 2 — Locate .env Files

If the path is a file: use it directly.

If the path is a directory: find all `.env*` files using Glob (`**/.env`, `**/.env.*`), excluding:
- `.env.example`, `.env.sample`, `.env.template` — these are safe-to-commit templates, skip them
- Files where every non-blank, non-comment line is already an `op://` reference — already migrated, skip and note this

For each file found, read it and extract all key=value pairs where the value is not empty, not a comment, and not already an `op://` reference.

Then **filter out obvious non-secrets** before migration. Skip a key=value pair if any of these match:

- Key is in the non-secret list: `PORT`, `HOST`, `HOSTNAME`, `NODE_ENV`, `RAILS_ENV`, `RACK_ENV`, `PYTHON_ENV`, `APP_ENV`, `ENV`, `LOG_LEVEL`, `LOG_FORMAT`, `DEBUG`, `VERBOSE`, `TZ`, `LANG`, `LC_ALL`, `PATH`
- Value is purely numeric (e.g. `3000`, `8080`)
- Value is a boolean literal (`true`, `false`, `1`, `0`, `yes`, `no`, `on`, `off`)
- Value is one of: `development`, `production`, `test`, `staging`, `local`
- Value looks like a hostname or URL with no credential component (e.g. `localhost`, `0.0.0.0`, `http://localhost:3000`, `redis://localhost:6379` — note the absence of `user:password@`)

Include the filtered-out keys in the runbook as a "Not migrated (config, not secrets)" section so the user can confirm. If the user disagrees with any classification, they can move them manually — but the default is: don't put non-secrets in 1Password.

The remaining key=value pairs are the secrets to migrate.

If no files with hardcoded values are found, tell the user and stop. If all files are already using `op://` references, congratulate the user and stop.

---

## Phase 3 — Gather Missing Information

If account was not provided in arguments, use `AskUserQuestion`:

> What's your 1Password account domain? (e.g. `your-team.1password.com`)

Use `AskUserQuestion` to ask about the target vault:

> What vault should these secrets go into? If you're not sure, `/1password-scaffold` can generate a vault naming convention for your project. Or type a vault name directly (e.g. `service-payments-dev`).

---

## Phase 4 — Map Secrets to 1Password Items

For each key=value pair collected in Phase 2, derive a 1Password item name and field name using these rules:

**Well-known patterns:**
- `DATABASE_URL`, `DB_URL`, `POSTGRES_URL`, `POSTGRESQL_URL` → item: `postgres`, field: `url`
- `REDIS_URL` → item: `redis`, field: `url`
- `STRIPE_SECRET_KEY`, `STRIPE_API_KEY` → item: `stripe`, field: `secret-key`
- `STRIPE_PUBLISHABLE_KEY` → item: `stripe`, field: `publishable-key`
- `AWS_ACCESS_KEY_ID` → item: `aws`, field: `access-key-id`
- `AWS_SECRET_ACCESS_KEY` → item: `aws`, field: `secret-access-key`
- `AWS_REGION`, `AWS_DEFAULT_REGION` → item: `aws`, field: `region`
- `SENDGRID_API_KEY` → item: `sendgrid`, field: `api-key`
- `TWILIO_ACCOUNT_SID` → item: `twilio`, field: `account-sid`
- `TWILIO_AUTH_TOKEN` → item: `twilio`, field: `auth-token`
- `GITHUB_TOKEN`, `GH_TOKEN` → item: `github`, field: `token`
- `DD_API_KEY`, `DATADOG_API_KEY` → item: `datadog`, field: `api-key`
- `SEGMENT_WRITE_KEY` → item: `segment`, field: `write-key`
- `SESSION_SECRET`, `SECRET_KEY_BASE`, `APP_SECRET` → item: `app-config`, field: `session-secret`
- `SLACK_BOT_TOKEN`, `SLACK_TOKEN` → item: `slack`, field: `bot-token`
- `ANTHROPIC_API_KEY` → item: `anthropic`, field: `api-key`
- `OPENAI_API_KEY` → item: `openai`, field: `api-key`

**Fallback pattern (anything not matched above):**
Split the key on `_`. The last segment becomes the field name (lowercased, underscores → hyphens). Everything before it becomes the item name (lowercased, underscores → hyphens).
- `ACME_WEBHOOK_SECRET` → item: `acme-webhook`, field: `secret`
- `PAYMENTS_API_KEY` → item: `payments`, field: `api-key`
- `FOO` (no underscore) → item: `foo`, field: `value`

Group keys that share the same derived item name — they will be created as a single `op item create` call with multiple fields.

---

## Phase 5 — Generate the Runbook

Output a complete markdown runbook. No files are written; the user copies and runs commands.

Structure:

```markdown
# 1Password Migration — {project-name or .env filename}

## What's being migrated

| Variable | File | → 1Password Reference |
|---|---|---|
| `VAR_NAME` | `.env` | `op://{vault}/{item}/{field}` |
...

> **Note:** Secret values are not shown. This table maps variable names to their future `op://` references only.

---

## Step 1 — Create items in 1Password

You can do this in the 1Password app (recommended) or via CLI. One command per item:

{For each distinct item, emit one command with the derived fields pre-declared as empty `concealed` (password-style) fields so the references in Step 2 resolve to the right field names:}
```bash
# {item-name} — holds: {comma-separated list of field names for this item}
op item create \
  --category "API Credential" \
  --title "{item-name}" \
  --vault {vault} \
  --account {account} \
{For each derived field name on this item:}
  '{field-name}[concealed]=' \
```

Fields are created empty. Open each item in the 1Password app and paste the actual secret values into the matching fields. (Do not set values from the command line — they end up in shell history.)

---

## Step 2 — Create the secrets reference file(s)

These files contain **only** `op://` references and are **safe to commit to git**.

{If exactly one input env file was migrated:}
Create `.env.secrets` in your project root:

```env
{For each env var, in the same order as the original file:}
{VAR_NAME}=op://{vault}/{item}/{field}
```

{If multiple env-specific input files were migrated (e.g. `.env.development` and `.env.production`):}
Emit one secrets file per input, preserving the environment suffix so the split is preserved at runtime. For each input file `{original-file}` (e.g. `.env.development`), produce `{original-file}.secrets` (e.g. `.env.development.secrets`):

```env
# {original-file}.secrets
{For each env var found in {original-file}, in the same order:}
{VAR_NAME}=op://{vault}/{item}/{field}
```

Update Step 4 to reference the matching file per environment (e.g. `op run --env-file=.env.development.secrets -- ...` for the dev start command).

---

## Step 3 — Update .gitignore

Add these lines to `.gitignore` if not already present:

```gitignore
.env
.env.local
.env.*.local
{list any other .env* files found during scan that aren't already gitignored}
```

**Important:** `.env.secrets` should NOT be gitignored — it contains only `op://` references, not real values.

---

## Step 4 — Replace your dev start command

Instead of sourcing `.env` directly, use `op run`:

```bash
# Generic pattern — replace <your-start-command> with your actual command
op run --account {account} --env-file=.env.secrets -- <your-start-command>
```

{If Makefile, justfile, or package.json was found in the project:}
Update your dev scripts to wrap commands with `op run`. For example:

**package.json:**
```json
"scripts": {
  "dev": "op run --account {account} --env-file=.env.secrets -- <current-dev-command>"
}
```

**Makefile / justfile:**
```makefile
dev:
	op run --account {account} --env-file=.env.secrets -- <current-dev-command>
```

For one-off reads at a terminal, use [`opx`](https://github.com/bestdan/opx) instead of `op read` — it names the URI and the calling process in a dialog, forces a fresh biometric prompt, and tears down the `op` session afterwards:

```bash
opx 'op://{vault}/{item}/{field}'
```

`opx` has no `--account` flag; export `OP_ACCOUNT={account}` in your shell profile so it resolves against the right account.

---

## Step 5 — Verify the migration

```bash
# Confirm 1Password resolves every reference without errors. Exit 0 = all resolved.
# Do not print values — only check the exit code.
op run --account {account} --env-file=.env.secrets -- true && echo OK
```

If `op` exits non-zero, run with `OP_DEBUG=1` to see which reference failed:

```bash
OP_DEBUG=1 op run --account {account} --env-file=.env.secrets -- true
```

Common causes: the item doesn't exist in the vault, or the field name doesn't match exactly.

If you have `opx` installed, spot-check one reference end to end — approve the dialog when it appears:

```bash
# Length only — never print the value.
opx 'op://{vault}/{item}/{field}' | wc -c
```

---

## Step 6 — Remove or archive the original .env file(s)

Once you've verified secrets load correctly, remove each plaintext file scanned in Phase 2:

```bash
{For each .env* file that contained hardcoded values:}
rm {original-file}
```

If you want to keep a non-secret backup of a file, remove the secret values manually and leave only the variable names with a comment.

Do not commit any of these files after migration. If any were already committed, rotate the exposed credentials — removing a file from git does not remove it from history.

---

## What to do if a secret was already committed to git

If any of the migrated secrets appear in `git log`, treat them as compromised:

1. Rotate the credential immediately (in the service that issued it — Stripe, AWS, etc.)
2. Update the new value in 1Password
3. The `op://` references in `.env.secrets` don't need to change — they point to the item, not the value

---

## Next steps

1. Run Steps 1–2 to create items and the `.env.secrets` file
2. Verify with Step 5 before removing the original `.env`
3. Run `/1password-audit` to confirm the project's hygiene checks pass after migration
```

---

## Rules

- **Read-only.** Do not edit, write, or modify any files. All output is a runbook.
- **Never echo secret values.** The migration table maps variable names to `op://` references only — never include the actual values found in `.env`.
- **Preserve order.** The `.env.secrets` template should list variables in the same order as the original `.env` file to make diffing easy.
- **Warn on committed secrets.** If any `.env` file found during scan is tracked in git (`git ls-files` would show it), add a prominent warning at the top of the runbook that the secrets may already be in git history and need rotation.
- **--account everywhere.** Every `op` command in the output must include `--account {account}`.
- **`opx` for reads, `op run` for launches.** Recommend `opx` only where the runbook would otherwise use `op read`. Never swap `op run` for `opx --env` — `op run` scopes secrets to the subprocess, `opx --env` puts them in the shell.
- **Group items.** Multiple variables that map to the same item (e.g. `STRIPE_SECRET_KEY` and `STRIPE_PUBLISHABLE_KEY` both → item `stripe`) must appear in a single `op item create` call, not two separate calls.
