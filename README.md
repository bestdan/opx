# opx

A defensive wrapper around the 1Password CLI (`op`) that forces a fresh
biometric prompt on every secret read and tears down the `op` session token
immediately after.

## Why

The `op` CLI caches a session token after a successful biometric unlock, so a
later `op read` can succeed silently without prompting again. That is
convenient for scripted automation but gives any process running as you
(shell history scrapers, malicious npm postinstalls, a stale background job)
a window in which it can read arbitrary secrets without your knowledge.

`opx` closes that window:

1. Show a platform-native confirmation dialog disclosing **which URI** is
   being requested and **which process** is asking.
2. Run `op read <uri>`, which triggers a fresh biometric prompt.
3. On exit — success, failure, panic, or `SIGINT`/`SIGTERM` — run
   `op signout --all` to invalidate any cached token.

The result: every secret read is explicitly authorized by you, and no
residual session is left behind for another process to abuse.

## Installation

Prerequisites:

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

4. **(macOS only) Clear the quarantine attribute** so Gatekeeper doesn't
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

### Exit codes

| Code | Meaning                                             |
|------|-----------------------------------------------------|
| 0    | Secret printed to stdout                            |
| 1    | `op` failed, user denied the prompt, or interrupted |
| 2    | Usage error (no args, malformed URI)                |

All non-usage failures collapse to exit 1 by design: callers should treat
them identically (no secret on stdout) and read `stderr` for the reason.

## Common gotchas

- **`op` not on `PATH`.** `opx` shells out to `op`; if it isn't installed
  the read fails with an exec error. Install the 1Password CLI separately.
- **No GUI prompt available.** On macOS `opx` requires `osascript`
  (preinstalled). On Linux it tries `zenity` first and falls back to a
  `/dev/tty` y/N prompt — if there is no TTY (e.g. a daemonized cron job)
  the request is denied. Run `opx` interactively.
- **macOS Gatekeeper.** A locally built `opx` binary is unsigned; the
  first run from Finder will be blocked. Either run it from a terminal or
  remove the quarantine attribute: `xattr -d com.apple.quarantine ./opx`.

## Contributing

See [`dev_docs/development.md`](dev_docs/development.md) for build,
test, layout, and coding conventions.
