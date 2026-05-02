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
   `op session forget --all` to invalidate any cached token.

The result: every secret read is explicitly authorized by you, and no
residual session is left behind for another process to abuse.

## Installation

Requires Go 1.24+ and the [`op`](https://developer.1password.com/docs/cli/)
CLI installed and on `PATH`.

```sh
make build              # builds ./opx for the current platform
make cross              # cross-compiles for darwin-arm64, darwin-amd64, linux-amd64
make test
make lint
```

Drop the resulting `opx` binary somewhere on your `PATH`.

## Usage

Direct mode — pass an `op://` URI:

```sh
opx op://Personal/GitHub/token
```

Allowlist mode — pass a logical name resolved through your config file:

```sh
opx get github-token
```

The allowlist lives at `~/.config/opx/allowlist.json` (override with
`OPX_CONFIG=/path/to/file`) and looks like:

```json
{
  "github-token": "op://Personal/GitHub/token",
  "aws-key":      "op://Work/AWS/access_key_id"
}
```

`opx` refuses to load the file unless it is owned by the current user and
not world-readable — fix with `chmod 600 ~/.config/opx/allowlist.json`.

### Exit codes

| Code | Meaning                                               |
|------|-------------------------------------------------------|
| 0    | Secret printed to stdout                              |
| 1    | `op` failed, user denied the prompt, or interrupted   |
| 2    | Usage error (no args, malformed URI)                  |
| 3    | Config error (allowlist missing, bad perms, bad JSON) |

## Common gotchas

- **`op` not on `PATH`.** `opx` shells out to `op`; if it isn't installed
  the read fails with an exec error. Install the 1Password CLI separately.
- **No GUI prompt available.** On macOS `opx` requires `osascript`
  (preinstalled). On Linux it tries `zenity` first and falls back to a
  `/dev/tty` y/N prompt — if there is no TTY (e.g. a daemonized cron job)
  the request is denied with `ErrDenied`. Run `opx` interactively.
- **Allowlist permissions.** `Load` rejects files that are world-readable
  (`mode & 0o004 != 0`) or owned by another UID. Both checks fire before
  any JSON parsing, so a "config error" usually means `chmod 600`.
- **Allowlist URI validation.** Every value must be a syntactically valid
  `op://vault/item/field` URI with three non-empty segments. A typo like
  `op://vault/item/` rejects the whole file.
- **`make build` says `version=dev`.** The Makefile injects
  `main.version` from `git describe --tags --always --dirty`, which prints
  `dev` when there are no tags or you build from a tarball. Tag a release
  (`git tag v0.1.0`) before building if you want a real version stamped in.
- **Cross-compiling.** `make cross` sets `CGO_ENABLED=0` so the binaries
  are statically linked and portable. Don't add cgo dependencies unless
  you're prepared to drop the cross target.
- **macOS Gatekeeper.** A locally built `opx` binary is unsigned; the
  first run from Finder will be blocked. Either run it from a terminal or
  remove the quarantine attribute: `xattr -d com.apple.quarantine ./opx`.
- **Caller name on non-Linux/macOS.** The `caller` package reads
  `/proc/<ppid>/comm` on Linux and shells out to `ps` elsewhere. On
  platforms without either it returns `"unknown"` — the dialog still
  works, it just can't name the requesting process.

## Layout

```
main.go                 # CLI entry point, exit codes, signal handling
internal/allowlist/     # Loads & validates ~/.config/opx/allowlist.json
internal/caller/        # Resolves the parent process name (ppid → comm)
internal/oprunner/      # Thin wrapper around `op read` and `op session forget`
internal/prompt/        # Platform-native confirmation dialog (osascript/zenity/tty)
internal/uri/           # `op://` URI syntax validator
```
