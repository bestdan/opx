---
name: 1password-scaffold
description: Generate a complete 1Password vault setup for a new service, team, or AI agent — vault creation commands, service account scoping, .env.secrets template, op run invocations, and .gitignore entries.
argument-hint: "<name> [service|team|ai-agent] [account]"
allowed-tools:
  - AskUserQuestion
disallowed-tools:
  - Edit
  - Write
  - NotebookEdit
version: 1.1.1
---

# 1Password Vault Scaffold

Generate the complete 1Password setup for a new service, team, or AI agent — ready to copy-paste and run. No files are written; output is a runbook the user can execute.

**Arguments:** `$ARGUMENTS`

**Reference:** [`1password-local-dev-best-practices.md`](../1password-audit/1password-local-dev-best-practices.md) — vault structure, service account scoping, the `opx` read model (section 4), and the reasoning behind these conventions.

---

## Phase 1 — Parse Arguments

Parse `$ARGUMENTS` positionally:

1. **Name** (required, first arg) — the service, team, or project name. Normalize it: lowercase, replace spaces and underscores with hyphens. Examples: `payments-service` → `payments`, `Platform Team` → `platform`, `ai-coding-agent` → `ai-coding-agent`.

2. **Type** (optional, second arg) — one of:
   - `service` — a specific backend service or application (default if not specified and name doesn't imply otherwise)
   - `team` — shared dev credentials for an eng team
   - `ai-agent` — credentials scoped specifically for an AI coding agent session

   If not provided, infer from the name — matching whole hyphen- or underscore-separated tokens, never substrings: a token `team` → `team`; a token `ai`, `agent`, `agents`, `claude`, `cursor`, or `copilot` → `ai-agent`; everything else → `service`. (Substring matching would classify `mail`, `retail`, and `maintenance` as AI agents.)

3. **Account** (optional, third arg) — the 1Password account domain (e.g. `your-team.1password.com`). If not provided, use `AskUserQuestion` to ask:

   > What's your 1Password account domain? (e.g. `your-team.1password.com`) — this will be used in all generated commands.

---

## Phase 2 — Ask What Secrets This Vault Will Hold

Use `AskUserQuestion` to ask:

> What secrets will **{vault-name}** hold? List the env var names or describe what the service needs (e.g. `DATABASE_URL`, `STRIPE_SECRET_KEY`, `REDIS_URL`, or "Postgres + Stripe + S3"). Type `skip` to get a minimal template.

Parse the response:
- If `skip` → generate a minimal template with two placeholder entries
- If env var names (all caps, underscores) → use them directly as-is
- If descriptive text → infer likely env var names from common patterns:
  - "postgres" / "database" → `DATABASE_URL`
  - "redis" → `REDIS_URL`
  - "stripe" → `STRIPE_SECRET_KEY`, `STRIPE_PUBLISHABLE_KEY`
  - "s3" / "aws" → `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`
  - "sendgrid" / "email" → `SENDGRID_API_KEY`
  - "twilio" → `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`
  - "github" → `GITHUB_TOKEN`
  - "datadog" → `DD_API_KEY`
  - "segment" → `SEGMENT_WRITE_KEY`
  - "jwt" / "session" → `SESSION_SECRET`
  - "slack" → `SLACK_BOT_TOKEN`
  - unknown → use the description as a comment placeholder

For each secret, derive a 1Password item name and field name:
- `DATABASE_URL` → item: `postgres`, field: `url`
- `REDIS_URL` → item: `redis`, field: `url`
- `STRIPE_SECRET_KEY` → item: `stripe`, field: `secret-key`
- `STRIPE_PUBLISHABLE_KEY` → item: `stripe`, field: `publishable-key`
- `AWS_ACCESS_KEY_ID` → item: `aws`, field: `access-key-id`
- `AWS_SECRET_ACCESS_KEY` → item: `aws`, field: `secret-access-key`
- `AWS_REGION` → item: `aws`, field: `region`
- `SENDGRID_API_KEY` → item: `sendgrid`, field: `api-key`
- `SESSION_SECRET` → item: `app-config`, field: `session-secret`
- For anything unrecognized: item: `{lowercased-var-name}`, field: `value`

---

## Phase 3 — Derive Vault Names

Apply naming conventions based on type:

**service:**
- Dev vault: `service-{name}-dev`
- Prod vault: `service-{name}-prod`

**team:**
- Dev vault: `team-{name}-dev`
- (Prod secrets for teams usually live in service vaults; note this in the output)

**ai-agent:**
- Use `AskUserQuestion` to ask:

  > Should this AI agent use the shared `ai-agents` vault or a dedicated `ai-{name}-dev` vault? Use shared for general coding agents; dedicated for agents with project-specific credentials.

  Default to shared if the user doesn't have a strong preference. The chosen value becomes both `{dev-vault-name}` and `{agent-vault-name}` in the runbook (they are the same vault for ai-agent type).

---

## Phase 4 — Generate the Runbook

Output a complete markdown runbook. Structure it as numbered steps with copy-pasteable shell commands. Each command block should be self-contained — no assumed state from previous steps.

Use this structure:

---

```markdown
# 1Password Vault Setup — {display-name}

## Overview

| | |
|---|---|
| **Type** | {service / team / ai-agent} |
| **Account** | {account} |
| **Dev vault** | `{dev-vault-name}` |
| **Prod vault** | `{prod-vault-name}` (if applicable) |
| **Secrets** | {comma-separated list of env var names} |

---

## Step 1 — Create the vaults

```bash
op vault create {dev-vault-name} --account {account}
{if prod vault:}
op vault create {prod-vault-name} --account {account}
```

Grant access to teammates who need dev credentials:

```bash
# Replace <email> with each teammate's 1Password account email
op vault user grant --vault {dev-vault-name} --user <email> --permissions view_items,copy_and_share_items --account {account}
```

---

## Step 2 — Add secrets to the dev vault

Create each item in 1Password. You can do this in the app (recommended) or via CLI:

{For each distinct item name derived in Step 2, emit one command with the derived fields pre-declared as empty `concealed` (password-style) fields so the references in Step 3 resolve to the right field names:}
```bash
# {item-name} — holds: {comma-separated field names for this item}
op item create \
  --category "API Credential" \
  --title "{item-name}" \
  --vault {dev-vault-name} \
  --account {account} \
{For each derived field name on this item:}
  '{field-name}[concealed]=' \
```

The trailing `\` is a line continuation *between* arguments — omit it on the last field line so the emitted command is complete when pasted.

Fields are created empty. Open each item in the 1Password app and paste the actual secret values into the matching fields. (Do not set values from the command line — they end up in shell history.)

---

## Step 3 — Create .env.secrets

Create this file in your project root. It contains only `op://` references — **safe to commit to git**.

```env
{For each env var:}
{ENV_VAR_NAME}=op://{dev-vault-name}/{item-name}/{field-name}
```

---

## Step 4 — Run your app with secrets injected

Replace your dev start command:

```bash
# Instead of: npm start
op run --account {account} --env-file=.env.secrets -- npm start

# Instead of: make dev
op run --account {account} --env-file=.env.secrets -- make dev

# Instead of: rails server
op run --account {account} --env-file=.env.secrets -- bundle exec rails server
```

Update `package.json` or your Makefile to wrap the command — don't rely on developers remembering to add `op run` manually.

For one-off reads at a terminal — pasting a token into a curl call, checking that an item resolved — use [`opx`](https://github.com/bestdan/opx) rather than `op read`:

```bash
opx 'op://{dev-vault-name}/{item-name}/{field-name}'
```

`opx` names the URI and the calling process in a confirmation dialog on every invocation, normally raises a fresh biometric prompt (no session is cached, because it signs out after each call), and runs `op signout --all` on exit, so no cached session is left for another local process to reuse. It has no `--account` flag; export `OP_ACCOUNT={account}` in your shell profile so it targets the right account.

---

{If type is service:}
## Step 5 — Create a service account for CI/CD

Service accounts authenticate non-interactively (no biometric). Create one scoped read-only to the prod vault:

```bash
op service-account create "{name}-ci" \
  --vault {prod-vault-name}:read_items \
  --account {account}
```

The command outputs a token (`ops_...`). Store it in your CI environment as `OP_SERVICE_ACCOUNT_TOKEN`. Do not store it anywhere else.

In CI, inject secrets the same way:

```bash
op run --env-file=.env.secrets -- <your-ci-command>
```

(No `--account` needed when using a service account token — the token is already scoped.)

{If type is ai-agent, add:}
## Step 5 (AI Agent) — Create a service account for agent sessions

```bash
op service-account create "{name}-agent" \
  --vault {agent-vault-name}:read_items \
  --account {account}
```

Run agent sessions with:

```bash
OP_SERVICE_ACCOUNT_TOKEN=<token> op run --env-file=.agent.secrets -- claude
```

The agent gets only what the service-account token is scoped to — nothing in your personal vaults, nothing in other projects. Note that a service-account token bypasses the desktop app's biometric gate; the boundary here is **token scope**, not Touch ID.

If instead you want the agent to read secrets as *you*, with a prompt on every read, give it no token and have it call `opx 'op://...'`. Each read then raises a dialog naming the URI and the calling process, and the session is invalidated afterwards — a prompt-injected agent cannot quietly reuse an unlock you granted a minute earlier. The trade-off is that this only works in an interactive session; unattended agent runs need the service-account token above. Treat the token like any other long-lived credential: keep it out of shell history, rotate it regularly, and revoke it if a session is compromised.

**Also consider:** storing your `ANTHROPIC_API_KEY` itself in 1Password using the [1Password shell plugin for Claude Code](https://developer.1password.com/docs/cli/shell-plugins/claude-code/). This replaces a plaintext export in your dotfiles with a biometric-gated injection — the same security model as `op run`, applied to the agent's own credential.

---

## Step 6 — Update .gitignore

Add to your project's `.gitignore`:

```gitignore
.env.local
*.secrets.resolved
```

If you use `op inject` for config file templates (e.g. `database.yml.tpl` → `database.yml`), also add:

```gitignore
database.yml        # or whatever op inject targets — the resolved output, not the .tpl
```

---

## Step 7 — Set up detect-secrets (if not already done)

```bash
pip install pre-commit detect-secrets
detect-secrets scan > .secrets.baseline
```

Add to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/Yelp/detect-secrets
    rev: v1.5.0
    hooks:
      - id: detect-secrets
        args: ['--baseline', '.secrets.baseline']
```

```bash
pre-commit install
git add .secrets.baseline
```

---

## Vault access summary

| Vault | Who has access | Permissions |
|---|---|---|
| `{dev-vault-name}` | All team members (grant individually) | view + copy |
| `{prod-vault-name}` | CI service account (`{name}-ci`) | view only |
| `{prod-vault-name}` | On-call / platform team | view only (grant as needed) |

Developers should not need direct access to the prod vault for day-to-day work.

---

## What to do when a team member leaves

```bash
# Revoke their access to each vault
op vault user revoke --vault {dev-vault-name} --user <email> --account {account}

# Rotate any credentials they had access to
# (do this in 1Password app — update item values, don't create new items)
```

Rotating the 1Password item value automatically updates all `op://` references — no `.env` file changes needed.
```

---

## Phase 5 — Output

Print the full runbook. Do not write any files.

After the runbook, add a short "Next steps" summary:

```markdown
---

## Next steps

1. Run Steps 1–2 to create vaults and stub items, then fill in secret values in the 1Password app
2. Commit `.env.secrets` and updated `.gitignore` to your repo
3. Update your dev start command (Step 4) — open a PR if it touches a shared Makefile or `package.json`
4. Add the CI service account token to your CI environment (Step 5)
5. Install [`opx`](https://github.com/bestdan/opx) and export `OP_ACCOUNT={account}` so ad-hoc reads are gated per-read
6. Run `/1password-audit` in this project after setup to verify everything is wired up correctly
```

---

## Rules

- **No files written.** Output is a runbook only. The user decides what to execute.
- **--account everywhere.** Every `op` command in the output must include `--account {account}`, with two exceptions: `opx` has no such flag (it follows the `OP_ACCOUNT` export), and commands running under a service-account token (`OP_SERVICE_ACCOUNT_TOKEN=... op run ...`) don't need it — the token is already scoped.
- **Read-only service accounts.** Always scope CI and agent service accounts to `read_items` only, unless the user explicitly says write access is needed. (`read_items` is the service-account vocabulary; `view_items` belongs to `op vault user grant`.)
- **Never generate fake secret values.** Placeholder `op://` references use the derived item/field names — never invent example values that look like real credentials.
- **`opx` for reads, `op run` for launches.** Any ad-hoc read in the runbook uses `opx`, never `op read`. Do not replace `op run` or `op inject` with `opx --env` — those scope secrets to a subprocess or a gitignored file; `opx --env` puts them in the interactive shell, which is weaker.
- **Vault names follow the convention.** `service-{name}-{env}`, `team-{name}-dev`, `ai-agents`. Do not deviate unless the user asks.
