---
name: 1password-onboard
description: Generate a new developer onboarding runbook for a project — discovers required vaults from .env.secrets and op:// references, then produces op/opx install, sign-in, vault access request, and local dev verification steps.
argument-hint: "[path] [account]"
allowed-tools:
  - Glob
  - Grep
  - Read
  - AskUserQuestion
disallowed-tools:
  - Edit
  - Write
  - NotebookEdit
version: 1.1.0
---

# 1Password Developer Onboarding

Generate a runbook for a new developer joining a project that uses 1Password for secrets. Read-only — produce a runbook only; do not edit any files.

**Arguments:** `$ARGUMENTS`

**Reference:** [`1password-local-dev-best-practices.md`](../1password-audit/1password-local-dev-best-practices.md) — vault structure, the `opx` read model (section 4), multi-account handling, and the reasoning behind each onboarding step.

---

## Phase 1 — Parse Arguments

Parse `$ARGUMENTS` positionally:

1. **Path** (optional, first arg) — path to a project directory. Defaults to the current working directory. Confirm it is a directory; if not, tell the user and stop.

2. **Account** (optional, second arg) — the 1Password account domain (e.g. `your-team.1password.com`). If not provided, attempt to infer from Phase 2 findings; ask if still unknown.

---

## Phase 2 — Discover Vault Requirements

Scan the project to understand what 1Password vaults and secrets the developer will need. Run these searches in parallel:

**Files to read:**
- `.env.secrets` — primary source of `op://` references
- Any `*.tpl` or `*.tmpl` files containing `op://` references
- `.envrc` — may contain `from_op` or `op read` calls
- `package.json`, `Makefile`, `justfile` — look for `op run` invocations and `--env-file` references
- `.pre-commit-config.yaml` — to know whether Step 6 (detect-secrets) applies
- `CODEOWNERS`, `.github/CODEOWNERS`, `docs/CODEOWNERS` — to find the vault-access contact for Phase 3
- `README.md`, `docs/` — may contain onboarding steps or vault names to cross-reference

**Extract from these files:**
- All unique vault names from `op://` references (format: `op://{vault}/{item}/{field}` — extract the vault segment)
- Account domain from `--account` flags in any `op` command, or from `op://` references if they include the account prefix
- Whether `op inject` is used (presence of `.tpl` / `.tmpl` files)
- Whether `direnv` / `.envrc` is used

If no `op://` references are found anywhere, warn the user that the project may not use 1Password yet and suggest running `/1password-scaffold` first. Then stop.

---

## Phase 3 — Gather Missing Information

If the account domain was not found in Phase 2 and not provided as an argument, use `AskUserQuestion`:

> What's your team's 1Password account domain? (e.g. `your-team.1password.com`)

If it's unclear who to contact for vault access (no CODEOWNERS, no README mention), use `AskUserQuestion`:

> Who should the new developer contact to request vault access? (name, email, or Slack handle — this will appear in the runbook)

---

## Phase 4 — Generate the Runbook

Output a complete markdown runbook for the new developer. No files are written.

Structure:

```markdown
# Developer Onboarding — {project-name}

This runbook gets you set up with secrets access for **{project-name}**. Follow the steps in order.

---

## Prerequisites

- macOS or Linux
- [1Password](https://1password.com/downloads/) desktop app installed and signed in to `{account}`
- Your 1Password account has been provisioned — if not, contact {access-contact or "your team lead"}

---

## Step 1 — Install the 1Password CLI

Check if you already have it:

```bash
op --version
```

If not installed:

```bash
# macOS (Homebrew)
brew install 1password-cli

# Or download directly: https://developer.1password.com/docs/cli/get-started/
```

### Install `opx`

Reads you run by hand go through [`opx`](https://github.com/bestdan/opx), not `op read`. `opx` shows a dialog naming the URI and the process asking for it, forces a fresh biometric prompt, and runs `op signout --all` afterwards so no cached session is left behind for another process to reuse.

```bash
git clone https://github.com/bestdan/opx.git
cd opx && make build
sudo mv opx /usr/local/bin/          # or: mv opx ~/.local/bin/
opx                                  # prints usage, exit code 2 — install is good
```

Requires Go 1.24+ and the `op` CLI above. On macOS, clear the quarantine flag if Gatekeeper blocks the unsigned binary: `xattr -d com.apple.quarantine "$(which opx)"`.

Verify the CLI is linked to your desktop app (required for biometric unlock):

```bash
op account list
```

You should see `{account}` in the output. If not, open 1Password > Settings > Developer > Enable CLI integration.

---

## Step 2 — Sign in

**If you have the 1Password desktop app installed and CLI integration enabled (Step 1):** you don't need to do anything here. The CLI uses the desktop app's session and prompts for Touch ID on each command. Set `OP_ACCOUNT` so commands without an explicit `--account` flag pick the right account:

```bash
# Add to ~/.zshrc or ~/.bashrc
export OP_ACCOUNT={account}
```

`opx` takes no `--account` flag by design, so `OP_ACCOUNT` is what points it at the right account. If you also have a personal 1Password account, setting this is not optional — without it reads silently resolve against whichever account is default.

Verify it works:

```bash
op whoami --account {account}
```

**Fallback — no desktop app (servers, headless dev environments):** sign in manually each shell session.

```bash
eval $(op signin --account {account})
```

This prompts for your master password and lasts until the shell closes. Do **not** put `eval $(op signin ...)` in your shell profile — it's interactive and will break non-interactive shells (CI, scripts).

---

## Step 3 — Request vault access

You need access to the following vaults. Contact {access-contact or "your team lead"} and ask them to run:

{For each vault discovered:}
```bash
# Grant access to vault: {vault-name}
op vault user grant \
  --vault {vault-name} \
  --user <your-email> \
  --permissions view_items,copy_and_share_items \
  --account {account}
```

**Vaults you need:**

{For each vault:}
- `{vault-name}` — referenced in `{source file(s) where this vault was found}`

Once granted, verify access:

```bash
op vault list --account {account}
```

`{vault-name}` should appear in the output.

---

## Step 4 — Verify secret resolution

Confirm all `op://` references resolve without errors. Use the form that matches what this project actually uses:

{If .env.secrets exists:}
```bash
# Exit 0 = every reference resolved. Do not print values.
op run --account {account} --env-file=.env.secrets -- true && echo OK
```

Then spot-check a single reference through `opx` to confirm your vault access and biometric setup work end to end. Approve the dialog when it appears:

```bash
# Pick any reference from .env.secrets. Length only — never print the value.
opx '{first-op-uri-found}' | wc -c
```

{If .env.secrets does NOT exist but the project uses .envrc or .tpl files only:}
> **Note:** This project does not have a `.env.secrets` file. Skip the `op run` verify step and use the `.envrc` / `op inject` checks below instead. If the reviewer believes `.env.secrets` should exist for this project, treat that as a finding and run `/1password-scaffold` or `/1password-migrate` first.

{If op inject / .tpl files are present:}
For template files, verify injection works:

```bash
{For each .tpl file found:}
op inject --account {account} -i {tpl-file} -o /dev/stdout > /dev/null && echo "{tpl-file}: OK"
```

{If .envrc is present:}
If you use `direnv`, allow the `.envrc` after verifying its contents:

```bash
cat .envrc    # review before allowing
direnv allow
```

---

## Step 5 — Run the project

{If op run is used in dev scripts:}
Dev scripts are already configured to use `op run`. Start the project normally:

```bash
{show the existing dev start command found in Makefile/package.json/justfile}
```

{If op run is NOT found in dev scripts:}
Run your start command wrapped with `op run`:

```bash
op run --account {account} --env-file=.env.secrets -- <your-start-command>
```

---

{If detect-secrets pre-commit hook is present in the project:}
## Step 6 — Set up pre-commit hooks

The project uses `detect-secrets` to prevent accidental secret commits. Set it up:

```bash
pip install pre-commit detect-secrets   # or: brew install pre-commit
pre-commit install
```

Verify:

```bash
pre-commit run --all-files
```

---

## Troubleshooting

**`op: command not found`**
→ Install the CLI (Step 1) and ensure `/opt/homebrew/bin` or `/usr/local/bin` is in your `$PATH`.

**`[ERROR] 401 Unauthorized`**
→ Your session has expired. Re-run: `eval $(op signin --account {account})`

**`[ERROR] vault not found` or blank variable values**
→ You don't have access to the vault yet. Re-check Step 3 and confirm the grant was applied.

**`op run` exits with a non-zero code but no error message**
→ One or more `op://` references failed to resolve. Run with `OP_DEBUG=1` for details:
```bash
OP_DEBUG=1 op run --account {account} --env-file=.env.secrets -- echo ok
```

**`opx` exits 1 with "denied"**
→ You dismissed the confirmation dialog, or there was no GUI/TTY to show it in. Run `opx` from an interactive terminal on your own machine; it is not usable over a headless SSH session or from a daemon.

**`opx` reads from the wrong account**
→ `OP_ACCOUNT` is unset or points at your personal account. Check with `echo $OP_ACCOUNT` and `op account list` (Step 2).

**Touch ID prompt never appears**
→ Ensure the 1Password app is open and CLI integration is enabled: 1Password > Settings > Developer > "Connect with 1Password CLI".

---

## Reference

| Vault | Purpose |
|---|---|
{For each vault:}
| `{vault-name}` | {source file — or "dev secrets" if inferred from naming convention} |

| File | Purpose |
|---|---|
| `.env.secrets` | `op://` references — safe to commit, used with `op run` |
| `opx` | Wrapper for one-off reads — per-read dialog, fresh biometric prompt, session torn down after |
{If .tpl files exist:}
| `{tpl-file}` | Template for `op inject` — resolved output is gitignored |
{If .envrc exists:}
| `.envrc` | direnv config — loads secrets via `from_op` or `op read` |
```

---

## Rules

- **Read-only.** Do not edit, write, or modify any files. All output is a runbook.
- **--account everywhere.** Every `op` command in the output must include `--account {account}`. `opx` is the exception — it has no such flag, so it relies on the `OP_ACCOUNT` export from Step 2.
- **`opx` for reads, `op run` for launches.** Use `opx` wherever the runbook would otherwise tell the developer to run `op read`. Never swap `op run` or `op inject` for `opx --env` — those scope secrets to a subprocess or a gitignored file, which `opx --env` does not.
- **Only reference what exists.** Only include steps for tools and files that were actually found in the project (e.g. skip the direnv step if `.envrc` doesn't exist, skip the detect-secrets step if `.pre-commit-config.yaml` doesn't exist).
- **Never print secret values.** If any `.env` file with real values is encountered during scanning, do not include those values anywhere in the runbook.
- **Vault access contact.** If no contact was found and the user didn't provide one, leave a `{contact}` placeholder rather than guessing.
- **Project name.** Derive it from the directory name, `package.json` name field, or the git remote URL — in that order of preference.
