# opx

A defensive wrapper around the 1Password CLI (`op`) that forces a fresh
biometric prompt on every secret read and tears down the `op` session token
immediately after.

**macOS only.** `opx` is built and supported on macOS; the confirmation
dialog is a native AppleScript dialog and the tool is not tested anywhere
else.

## Why

The `op` CLI caches a session token after a successful biometric unlock, so a
later `op read` can succeed silently without prompting again. That is
convenient for scripted automation but gives any process running as you
(shell history scrapers, malicious npm postinstalls, a stale background job)
a window in which it can read arbitrary secrets without your knowledge.

`opx` closes that window:

1. Show a native macOS confirmation dialog disclosing **which URI** is
   being requested and **which process** is asking.
2. Run `op read <uri>`, which triggers a fresh biometric prompt.
3. On exit — success, failure, panic, or `SIGINT`/`SIGTERM` — run
   `op signout --all` to invalidate any cached token.

The result: every secret read is explicitly authorized by you, and no
residual session is left behind for another process to abuse.

### What this does and does not defend against

The dialog is the whole trust boundary, so it matters what can influence it.
The calling process is the adversary here — it chooses the URI, the child
command, and the environment `opx` inherits.

**Defended:**

- Text the caller controls — the URI, the bound variable name, the process
  name — cannot inject terminal or AppleScript control sequences to repaint
  or forge the dialog.
- In `opx run`, the dialog names the child that will receive the secrets
  exactly as you wrote it, never shortened to a bare program name, with its
  arguments quoted and unshortened up to a bounded width. Anything past that
  width is disclosed as a count of omitted arguments rather than silently
  dropped, so the destination cannot be hidden by padding the command line.
- Every helper `opx` runs is located at a fixed absolute path rather than
  through `PATH`: the dialog helper (`osascript`), the two tools that
  identify the calling process (`ps`, `lsof`), and `op` itself. A binary
  planted earlier on `PATH` cannot stand in for any of them — it cannot
  approve the request in place of the dialog, name a program you trust in
  place of the real caller, or pretend to end the session while leaving it
  live. (The child command in `opx run` is the one you typed, and is
  resolved the way your shell would resolve it.)

**Not defended, today:**

- `opx` trusts your `op` install. Fixing the location `op` is loaded from
  does not make the binary there genuine: `op` normally lives in a Homebrew
  prefix your own account owns, so anything already running as you can
  replace it. That is a bigger compromise than `opx` is scoped to survive,
  and it defeats every `op read` you run, not just the ones through `opx`.
- An attacker who can already write to a directory like `/usr/local/bin`, or
  edit your shell profile, is outside what `opx` can reach. It hardens a
  specific, cheap, ambient vector; it is not a defense against a fully
  compromised account.
- The confirmation is only as good as your reading of it. `opx` makes the
  request legible — it cannot make an approval considered.

## Installation

Prerequisites:

- **macOS.** `opx` is macOS-only.
- **Go 1.24 or newer.** Check with `go version`. If missing, install from
  [go.dev/dl](https://go.dev/dl/) or via Homebrew (`brew install go`).
- **The 1Password CLI (`op`).** Check with `op --version`. If missing,
  install from [1Password's docs](https://developer.1password.com/docs/cli/get-started/)
  or via Homebrew (`brew install --cask 1password-cli`). Make sure
  biometric unlock is enabled in the 1Password desktop app under
  **Settings → Developer → Integrate with 1Password CLI**.

Step by step:

1. **Clone the repo and enter it.**

   ```sh
   git clone https://github.com/bestdan/opx.git
   cd opx
   ```

2. **Build the binary.** This produces an `opx` executable in the current
   directory.

   ```sh
   make build
   ```

3. **Move the binary onto your `PATH`.** `/usr/local/bin` is a standard
   location that is already on `PATH` for most shells:

   ```sh
   sudo mv opx /usr/local/bin/
   ```

   If you'd rather not use `sudo`, install to `~/.local/bin` and add that
   directory to your `PATH` in `~/.zshrc` or `~/.bashrc`:

   ```sh
   mkdir -p ~/.local/bin
   mv opx ~/.local/bin/
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
   source ~/.zshrc
   ```

4. **Clear the quarantine attribute** so Gatekeeper doesn't
   block the unsigned binary on first run:

   ```sh
   xattr -d com.apple.quarantine "$(which opx)" 2>/dev/null || true
   ```

5. **Verify the install.** From any directory:

   ```sh
   opx
   ```

   You should see a usage message ending in exit code 2. That means
   `opx` is on your `PATH` and ready to use — see the next section.

## Usage

Pass an `op://` URI:

```sh
opx op://Personal/GitHub/token
```

The secret is written to stdout; everything else (dialog text, errors,
warnings) goes to stderr, so you can pipe the secret directly:

```sh
export GITHUB_TOKEN="$(opx op://Personal/GitHub/token)"
```

### Loading multiple secrets at once

Calling `opx` once per secret means one biometric prompt per secret. To
load several secrets under a single approval, use `--env NAME=op://...`
pairs and `eval` the output:

```sh
eval "$(opx \
    --env GITHUB_TOKEN=op://Personal/GitHub/token \
    --env AWS_KEY=op://Personal/AWS/access_key \
    --env AWS_SECRET=op://Personal/AWS/secret_key)"
```

The dialog lists every URI and the variable each one will be bound to,
so you can review the whole batch before approving. Output is atomic:
if any read fails, nothing is written to stdout and the exit code is 1
— so you never end up with a partially populated environment. The
`op` session is forgotten once at the end, exactly as in single-URI
mode.

This trades a finer trust boundary (one approval per URI) for fewer
interruptions (one approval per batch). The user still types every URI
and every variable name themselves, so a malicious caller can't sneak
extra reads in.

### `opx run` — wrap a command with secrets from an env file

If you already keep secrets in a dotenv-style file, `opx run` reads it,
resolves any `op://` references with a single biometric approval, and
exec's your command with those values in its environment — the same
shape as `op run`, but with opx's per-call dialog and forced session
teardown:

```sh
# .env.secrets
SUPABASE_URL=op://finplan/Supabase/website
SUPABASE_SERVICE_ROLE_KEY=op://finplan/Supabase/service_role_key
LOG_LEVEL=info        # non-op:// values pass through verbatim
```

```sh
opx run --env-file=.env.secrets -- uv run pytest
```

Flags:

- `--env-file=PATH` — repeatable. Lines are `NAME=VALUE`; blank lines
  and `#` comments are ignored; matching surrounding quotes are
  stripped. A leading `export ` is tolerated.
- `--env NAME=VALUE` — repeatable. Same shape as the top-level batch
  mode and overrides any same-named entry from a file.
- `--` — optional separator; flag parsing also stops at the first
  non-flag token, matching `op run`'s UX.

Behavior:

- One confirmation dialog covers every `op://` URI in the request.
- The `op` session is forgotten **before** the child is spawned, so the
  child never inherits a usable session. If that signout fails, the
  child is not spawned at all.
- Secrets are stripped of the trailing newline `op read` emits, so a
  token or connection string arrives in the child's environment exactly
  as stored.
- `Ctrl-C` reaches the child directly; opx does not kill it, so a child
  that traps `SIGINT` can finish its own cleanup.
- Reads are atomic: if any URI fails to resolve, the child is not
  spawned and nothing is written.
- Secrets reach the child only via its environment — they are never
  printed to opx's stdout in run mode.
- The child's exit code is propagated.

### Exit codes

| Code | Meaning                                                        |
|------|----------------------------------------------------------------|
| 0    | Secret printed to stdout                                       |
| 1    | `op` failed, the read was interrupted, the dialog could not be shown, or not macOS |
| 2    | Usage error (no args, malformed URI)                           |
| 3    | Denied — dialog dismissed, timed out, or no GUI                |

Exit 3 is the user-intent path; every other failure collapses to exit 1.
In all non-zero cases nothing reaches stdout — read `stderr` for the reason.

## Common gotchas

- **`op` not on `PATH`.** `opx` shells out to `op`; if it isn't installed
  the read fails with an exec error. Install the 1Password CLI separately.
- **No GUI prompt available.** `opx` requires `osascript` (preinstalled on
  macOS) to show the dialog. If it can't run — a daemonized job with no
  window server, for instance — the request is denied by design. Run `opx`
  interactively.
- **A few invisible characters in an item name are refused, not shown.**
  Directional formatting characters (bidi overrides and marks) and the
  Unicode line/paragraph separators can re-order or break the rest of the
  text around them, which would let a URI misrepresent which secret it
  names. `opx` refuses to draw a dialog containing one: it exits 1 and
  says which code point on stderr, rather than showing you something it
  can't vouch for. Other invisible characters — the zero-width joiner in
  an emoji sequence, the non-breaking space macOS types on option-space —
  are shown as an escape like `\u200d` and read normally.
- **The prompt beeps three times, and there is no setting to stop it.** The
  dialog plays your system alert sound so an access attempt is audible when it
  opens on another Space or behind a fullscreen window; the triple is what
  makes it recognizably `opx` rather than any other alert. Volume and sound
  choice come from System Settings › Sound — deliberately, because silencing
  it there is global, persistent, and something you would notice, where a
  per-invocation `OPX_SOUND=0` would not be. The beep is tamper evidence, not
  a security control in itself: a determined caller can still mute the
  machine, and hearing nothing is never proof nothing was read.
- **macOS Gatekeeper.** A locally built `opx` binary is unsigned; the
  first run from Finder will be blocked. Either run it from a terminal or
  remove the quarantine attribute: `xattr -d com.apple.quarantine ./opx`.

## Claude Code skills

This repo doubles as a Claude Code plugin. Four skills under [`skills/`](skills/)
cover the workflows around the binary — auditing a project's secret
hygiene, scaffolding a vault, migrating a plaintext `.env`, and onboarding
a developer. They recommend `opx` for interactive reads and `op run` for
launching apps, and share a background doc,
[`1password-local-dev-best-practices.md`](skills/1password-audit/1password-local-dev-best-practices.md).

| Skill | What it does |
|---|---|
| `/1password-audit [path]` | Reports on hardcoded secrets, gitignore coverage, `op run` in dev scripts, `--account` safety, `detect-secrets`, and raw `op read` that should be `opx` |
| `/1password-scaffold <name> [service\|team\|ai-agent] [account]` | Generates vault creation, service account scoping, `.env.secrets`, and `.gitignore` setup |
| `/1password-migrate [path] [account]` | Maps an existing `.env` to `op://` references and produces the migration runbook |
| `/1password-onboard [path] [account]` | Discovers required vaults and produces an install / sign-in / access-request runbook |

All four are read-only: they print a runbook, they do not write files.

Install:

```sh
claude plugin marketplace add bestdan/opx
claude plugin install opx@opx
```

## Contributing

See [`dev_docs/development.md`](dev_docs/development.md) for build,
test, layout, and coding conventions.
