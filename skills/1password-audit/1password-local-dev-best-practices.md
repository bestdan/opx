# 1Password Local Dev: Secret Management Best Practices

Recommendations for engineers using 1Password. Assumes the 1Password CLI (`op`) and [`opx`](https://github.com/bestdan/opx) are installed for all developers; this guide covers how to use them well, harden against AI agent and supply chain attacks, and avoid common foot-guns.

---

## 1. Vault and Service Account Structure

**Use per-project or per-team vaults. Never put dev secrets in the default Shared vault.**

1Password vaults are the unit of access control. Structure them so permissions can be granted narrowly:

- `team-platform-dev` — shared dev credentials for the platform team
- `service-payments-prod` — prod secrets for the payments service (CI/CD service account only)
- `project-a` — a vault exclusively for project A keys

**Service accounts** are for non-human access (AI agents, automation). They authenticate via a token (`OP_SERVICE_ACCOUNT_TOKEN=ops_xxx`) and have **immutable, scoped vault permissions** — they cannot access Personal/Employee vaults, cannot escalate privileges, and cannot create other service accounts. Grant read-only (`read_items`) unless writing is required. [1Password Service Accounts docs](https://developer.1password.com/docs/service-accounts/) · [Service account security model](https://developer.1password.com/docs/service-accounts/security/)

**Supply chain protection:** A service account creator can only grant access to vaults _they themselves can access_, preventing privilege escalation via a compromised dependency. [1Password service account security](https://developer.1password.com/docs/service-accounts/security/)

---

## 2. Secret References: Never Hardcode, Never Plaintext `.env`

The core pattern: commit files containing **secret references**, not values. A secret reference looks like:

```
op://vault-name/item-name/field-name
op://prod-vault/postgres/connection-url
op://dev-vault/stripe/secret-key
```

Create a `.env.secrets` file with references (safe to commit to Git):

```env
DATABASE_URL=op://dev-vault/postgres/url
STRIPE_SECRET_KEY=op://dev-vault/stripe/secret-key
SESSION_SECRET=op://dev-vault/app-config/session-secret
```

Then launch your app:

```bash
op run --account your-team.1password.com --env-file=.env.secrets -- npm start
op run --account your-team.1password.com --env-file=.env.secrets -- python manage.py runserver
```

`op run` resolves references and injects them as env vars **only for the subprocess duration**. They are never written to disk, never in shell history, and not visible to the parent shell. Stdout/stderr are automatically masked by default — the modern CLI uses a PTY so color output and `isatty()` checks generally still work, but if a specific program misbehaves under masking, `--no-masking` disables it (and removes the protection). [op run reference](https://developer.1password.com/docs/cli/reference/commands/run/) · [Secret references guide](https://developer.1password.com/docs/cli/secret-references/)

For config files (not environment variables), use `op inject`:

```bash
op inject --account your-team.1password.com --in-file=database.yml.tpl --out-file=database.yml
```

**Important:** The output file contains resolved secrets and must be gitignored. Use a naming convention (`.tpl` or `.tmpl` suffix for templates) to make the distinction obvious.

[Injecting secrets into config files](https://developer.1password.com/docs/cli/secrets-config-files/)

---

## 3. Biometric Authentication (Touch ID / Face ID)

Enable biometric unlock in 1Password (Settings → Security → Touch ID / Apple Watch). Once configured, **every new terminal session requires Touch ID approval** before the CLI can access secrets.

This is the most important defense against AI agent and supply chain attacks. A compromised background process — whether a malicious npm package or an AI coding agent that has been prompt-injected — **cannot access secrets without user presence**. The biometric gate is a hard boundary.

Security properties:

- Approval establishes a ~10-minute inactivity session, refreshing on each use. This timeout is fixed — it cannot be shortened via config (relevant for shared workstations).
- macOS reports only "fingerprint recognized" — keys don't pass through the OS
- Still encrypted with account password + Secret Key even with Touch ID enabled

[Touch ID setup guide](https://support.1password.com/touch-id-mac/) · [Touch ID security model](https://support.1password.com/touch-id-apple-watch-security-mac/) · [1Password app integration security](https://developer.1password.com/docs/cli/app-integration-security/)

---

## 4. Interactive Reads: `opx` Instead of `op read`

Biometric unlock (Section 3) has a gap: after one successful approval, `op` caches a session token for the rest of the inactivity window. Inside that window a *different* process running as you — a shell-history scraper, a malicious postinstall script, a prompt-injected coding agent — can call `op read` on any URI you have access to, with no prompt and no trace.

[`opx`](https://github.com/bestdan/opx) closes that window. It is a small wrapper around `op read` that, on every invocation:

1. Shows a platform-native dialog naming **which `op://` URI** is being read and **which process** is asking. This is unconditional — it is the trust boundary.
2. Runs `op read`. Because step 3 tore down the session after the previous call, there is normally no session to reuse and this raises a fresh biometric prompt. (If some *other* `op` command — a `from_op` on `cd`, an `op run` — established a session in the meantime, that read reuses it; the dialog still fires, so the approval gate holds either way.)
3. Runs `op signout --all` on every exit path — success, failure, denial, `SIGINT`, panic — so no cached session survives the call.

Use it anywhere you would have typed `op read`:

```bash
# Instead of: op read 'op://dev-vault/github/token'
export GITHUB_TOKEN="$(opx 'op://dev-vault/github/token')"
```

The secret goes to stdout; the dialog text, warnings, and errors go to stderr, so the value pipes cleanly. Exit codes: `0` = secret printed, `1` = `op` failed or the read was interrupted, `2` = usage error, `3` = you denied the dialog, it timed out, or there was no GUI/TTY to show it in.

To load several secrets under a single approval, use repeatable `--env NAME=op://...` pairs and `eval` the output. The dialog lists every URI and the variable it binds, and the batch is atomic — if any read fails, nothing is written to stdout:

```bash
eval "$(opx \
    --env DATABASE_URL=op://dev-vault/postgres/url \
    --env STRIPE_SECRET_KEY=op://dev-vault/stripe/secret-key)"
```

**Where `opx` does and does not apply:**

- **Replaces `op read`** for one-off interactive reads. There is no reason to prefer raw `op read` at a terminal.
- **`--env` mode is not a replacement for `op run`.** Variables `eval`'d into your shell live as long as the shell and are visible to every child process; `op run` scopes them to one subprocess. Prefer `op run` for launching an app (Section 2), and reach for `opx --env` when you genuinely need the values in the interactive shell itself.
- **`op run` still caches a session.** If you want the same teardown after a run, follow it with `op signout --all`.
- **macOS only, and not for CI or headless environments.** `opx` needs macOS and a GUI dialog (`osascript`); without one the request is denied by design. Automation should use a scoped service account token with `op run` (Section 1).

**Multi-account caveat:** `opx` deliberately takes no `--account` flag — it passes the URI straight to `op read`, which resolves against the default account. In a multi-account setup (Section 8), export `OP_ACCOUNT` in your shell profile so `opx` and any bare `op` command target the right account:

```bash
export OP_ACCOUNT=your-team.1password.com
```

Install: clone the repo, run `make build`, and move the resulting binary onto your `PATH` — see the [opx README](https://github.com/bestdan/opx#installation).

---

## 5. Shell and Dotfiles Integration: direnv + 1Password

Use `direnv` to auto-load secrets when you `cd` into a project directory, and unload them when you leave. This avoids ever running `export SECRET=...` (which lands in shell history).

Install direnv and the direnv-1password plugin:

```bash
brew install direnv
mkdir -p ~/.config/direnv/lib
curl -sfLo ~/.config/direnv/lib/1password.sh \
    https://raw.githubusercontent.com/tmatilai/direnv-1password/main/1password.sh
```

`direnv` auto-loads every `~/.config/direnv/lib/*.sh`, so no `source` line is needed in `.envrc`.

`direnv-1password` is a community-maintained plugin (not official 1Password). Check the repo's commit history before adopting it as a team standard.

In your project `.envrc` (committed to Git):

```bash
# Specify --account to avoid accidentally pulling from a personal vault
from_op --account your-team.1password.com DATABASE_URL=op://dev-vault/postgres/url
from_op --account your-team.1password.com REDIS_URL=op://dev-vault/redis/connection
```

Secrets load on `cd`, unload on `cd ..`. No shell history exposure.

Note the trade-off against Section 4: `from_op` pipes its references to `op inject` under the hood, so a `cd` into the directory resolves secrets silently, using whatever cached session exists — and leaving one cached for anything else running as you. That is the point — it is automatic — but it means the biometric gate does not fire per read and a session may be left cached. If you want every read explicitly approved and the session torn down afterwards, drop `direnv` for that project and load the values on demand instead:

```bash
eval "$(opx \
    --env DATABASE_URL=op://dev-vault/postgres/url \
    --env REDIS_URL=op://dev-vault/redis/connection)"
```

[direnv-1password plugin](https://github.com/tmatilai/direnv-1password) · [1Password secrets in env vars](https://developer.1password.com/docs/cli/secrets-environment-variables/) · [direnv + 1Password setup guide](https://dev.to/agonza05/load-secrets-automatically-with-1password-and-direnv-pn0)

**Recommended project layout:**

```
my-service/
├── .envrc              # direnv config using from_op (committed)
├── .env.secrets        # op:// references for op run (committed)
├── .env.local          # local overrides, no real values (gitignored)
├── database.yml.tpl    # config template with op:// references (committed)
└── database.yml        # op inject output — contains real secrets (gitignored)
```

Ensure your `.gitignore` explicitly covers injected output files and local overrides:

```gitignore
.env.local
database.yml        # or whatever op inject targets
*.secrets.resolved  # if you use a resolved-file convention
```

---

## 6. Pre-Commit Secret Scanning

The `op://` reference pattern only protects you if you actually use it. The most common failure mode is accidentally committing a real secret — a resolved `.env`, a test fixture with hardcoded credentials, a config file from `op inject` that wasn't gitignored.

Install `detect-secrets` as a pre-commit hook to catch this before it reaches git history:

```bash
pip install detect-secrets pre-commit
detect-secrets scan > .secrets.baseline   # baseline known false positives
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
```

Commit `.secrets.baseline` (it contains hashed references, not values). When `detect-secrets` flags a false positive, update the baseline with `detect-secrets scan --update .secrets.baseline`. [detect-secrets](https://github.com/Yelp/detect-secrets) · [pre-commit hooks](https://pre-commit.com/)

---

## 7. SSH Keys via 1Password SSH Agent

Store SSH keys in 1Password and use the built-in SSH agent. Private keys **never leave the app** — 1Password responds to SSH requests on behalf of stored keys.

Setup:

```bash
# Add to ~/.ssh/config
Host *
    IdentityAgent ~/.1password/agent.sock
```

For keys in non-default vaults, configure `~/.config/1password/ssh/agent.toml`:

```toml
[[vaults]]
name = "Engineering"
```

Each `git push` or SSH operation prompts for Touch ID approval per key. Private keys are never synced to disk. Use **Ed25519** keys (fastest and most secure). [1Password SSH agent](https://developer.1password.com/docs/ssh/agent/) · [SSH agent config](https://developer.1password.com/docs/ssh/agent/config/) · [SSH key management guide](https://sinovi.uk/articles/managing-ssh-keys-with-1password-ssh-agent)

---

## 8. Multiple 1Password Accounts

Many developers have a personal 1Password account alongside the enterprise one. The `op` CLI supports multiple accounts simultaneously, which creates a risk: team-shared configs (`.envrc`, scripts) that don't specify an account may silently resolve against a personal vault, causing confusing failures or — worse — pulling the wrong credential.

Always specify `--account` in any shared config. In `.envrc`, pass it to `from_op` as shown in Section 5. For `op run`, set `OP_ACCOUNT` or pass `--account` explicitly:

```bash
op run --account your-team.1password.com --env-file=.env.secrets -- npm start
```

`opx` has no `--account` flag by design (Section 4), so it follows `OP_ACCOUNT`. Export it in your shell profile — that also makes bare `op` commands and `from_op` default to the right account:

```bash
export OP_ACCOUNT=your-team.1password.com
```

To list configured accounts and verify which is active:

```bash
op account list
op whoami
```

[Manage multiple accounts](https://developer.1password.com/docs/cli/sign-in-manually/#sign-in-to-multiple-accounts)

---

## 9. Hardening Against AI Agent and Supply Chain Attacks

Recent incidents illustrate the risk: the OpenClaw malware campaign distributed macOS infostealing payloads via AI agent skill registries, targeting SSH keys, cloud credentials, and browser sessions. [1Password: From magic to malware](https://1password.com/blog/from-magic-to-malware-how-openclaw-agent-skills-become-an-attack-surface)

The 1Password model limits blast radius at multiple layers:

| Layer                      | Threat                                           | Defense                                                    |
| -------------------------- | ------------------------------------------------ | ---------------------------------------------------------- |
| Biometric gate             | Background process silently reading secrets      | Touch ID required; no user presence = no access            |
| Session timeout            | Long-lived token stolen during active session    | ~10-minute inactivity window before re-auth                |
| Per-read approval (`opx`)  | Another local process reusing a cached session   | Dialog names the URI and caller; `op signout --all` after  |
| Scoped service accounts    | Compromised token pivoting to other vaults       | Token only reaches its assigned vault; can't escalate      |
| Secret references in files | Compromised dependency reading `.env`            | `op://` refs are useless without CLI + auth                |
| Shell history protection   | `export SECRET=...` captured in history          | `direnv` + `from_op` never writes secrets to shell         |
| Subprocess scoping         | Secrets persisting in history, dotfiles, or the shell env | `op run` scopes values to one subprocess — never on disk, never inherited by the shell. A same-uid process can still inspect another process's environment; the boundary is persistence, not process isolation. |
| Pre-commit scanning        | Developer accidentally commits a resolved secret | `detect-secrets` blocks the commit before it reaches git   |

**For AI coding agents specifically** (Claude, Cursor, Copilot):

- Never give an AI agent access to a service account token with broad vault permissions
- Create a dedicated `ai-agents` vault with only the minimum credentials the agent needs
- Run agent sessions with `op run --env-file=.agent.secrets -- <agent>` so it gets scoped secrets for that session only
- Biometric gate on the developer's machine prevents the agent from silently fetching additional secrets mid-session
- Have the agent read individual secrets through `opx`, never raw `op read` — an agent that has been prompt-injected then cannot reuse a session you unlocked a minute ago, and every read it attempts surfaces a dialog naming the URI and the calling process. (A service account token bypasses this entirely: there the boundary is token scope, not user presence.)
- Store your own AI tool API key (e.g. `ANTHROPIC_API_KEY`) in 1Password using the tool's shell plugin rather than a dotfile export — for Claude Code, see [1Password shell plugin for Claude Code](https://developer.1password.com/docs/cli/shell-plugins/claude-code/); this applies the same biometric-gate model to the agent's own credential

1Password Unified Access (enterprise feature) provides discovery of unmanaged credentials on dev machines, centralized control across human and AI credentials, and a unified audit log. [1Password Unified Access announcement](https://1password.com/blog/introducing-1password-unified-access) · [Agentic AI security](https://1password.com/solutions/agentic-ai)

---

## 10. Checklist for Onboarding / Team Adoption

- [ ] Enable Touch ID / biometric unlock in 1Password app (Settings → Security)
- [ ] Install `op` CLI and connect it to the desktop app for biometric integration
- [ ] Install `opx` and use it in place of `op read` for interactive reads; export `OP_ACCOUNT` so it targets the right account
- [ ] Run `op whoami` and `op account list` to confirm the correct enterprise account is active
- [ ] Install `direnv` and the `direnv-1password` plugin; verify it's using `--account` in `.envrc`
- [ ] Replace any plaintext `.env` files with `.env.secrets` using `op://` references
- [ ] Update `npm start` / `make dev` / etc. to run under `op run --account <your-team>.1password.com --env-file=.env.secrets --`
- [ ] Gitignore all `op inject` output files; use `.tpl` suffix convention for templates
- [ ] Install `detect-secrets` pre-commit hook; commit `.secrets.baseline` to the repo
- [ ] Migrate SSH keys into 1Password and configure the SSH agent
- [ ] For each project: create a dedicated vault; grant teammates access to vault, not root
- [ ] AI coding tools: confirm they don't have access to service account tokens or shell env
- [ ] Enable 1Password Watchtower to monitor stored credentials for breach exposure
- [ ] Rotate vault credentials when team members leave or on a quarterly cadence
- [ ] Audit 1Password item access logs quarterly for unusual patterns

---

## Key CLI Reference

```bash
# Verify active account
op whoami
op account list

# Resolve a single secret (dialog + fresh biometric prompt, session torn down after)
opx 'op://vault/item/field'

# Load several secrets into the current shell under one approval
eval "$(opx --env DATABASE_URL=op://vault/postgres/url --env GITHUB_TOKEN=op://vault/github/token)"

# Invalidate any cached op session by hand
op signout --all

# Run a command with secrets injected
op run --account your-team.1password.com --env-file=.env.secrets -- npm start

# Inject into a config file template (output file must be gitignored)
op inject --account your-team.1password.com --in-file=config.tpl --out-file=config.json

# Update detect-secrets baseline after reviewing a new false positive
detect-secrets scan --update .secrets.baseline

# Keep CLI up to date
op update
```

---

_Sources: [1Password Developer Docs](https://developer.1password.com/docs/cli/) · [1Password Support](https://support.1password.com/) · [1Password Blog: AI agent security](https://1password.com/blog/introducing-1password-unified-access) · [1Password Blog: OpenClaw malware](https://1password.com/blog/from-magic-to-malware-how-openclaw-agent-skills-become-an-attack-surface)_
