# Development

Notes for hacking on `opx` itself. End users should read [`../README.md`](../README.md)
first; this document assumes you've already cloned the repo and have the
prerequisites installed.

For coding conventions, testing conventions, security invariants, and the
list of things to avoid, see [`../AGENTS.md`](../AGENTS.md) — it is the
single source of truth and applies to humans and AI agents alike.

## Prerequisites

- macOS — `opx` is a macOS-only tool (see [`../README.md`](../README.md)).
  The unit tests are hermetic and will pass elsewhere, but the runtime
  (AppleScript dialog, `ps`-based caller lookup) targets macOS only.
- Go 1.24+ (`go version`)
- The `op` CLI installed and on `PATH` (only required at runtime, not to build)
- `make`

## Make targets

```sh
make build             # compile ./opx for the current platform
make test              # unit tests (hermetic; what CI runs)
make test-integration  # local-only: hits real op binary, requires scripts/.env.example
make test-all          # test + test-integration
make lint              # go vet ./...
make cross             # CGO_ENABLED=0 builds for darwin-arm64, darwin-amd64
make clean
```

Run `make test` and `make lint` before opening a PR. If you touch the
cross-compile flow, run `make cross` too — it must stay CGO-free.
`make test-integration` is local-only (fixtures + biometric prompts);
run it when touching `internal/oprunner` or the `op` binary boundary.

## Repository layout

```
main.go                 # CLI entry point, exit codes, signal & panic handling
main_test.go            # end-to-end tests of run() with fake Runner/Confirmer
internal/caller/        # parent process name (ppid → `ps`)
internal/oprunner/      # `op read` / `op signout` subprocess wrapper (Runner interface)
internal/prompt/        # native macOS confirm dialog (osascript)
internal/shellquote/    # POSIX single-quote escaper for --env output
internal/uri/           # `op://vault/item/field` syntax validator
scripts/                # local-dev scaffolding: fixtures, smoke test, install helper
Makefile                # build, test, test-integration, test-all, lint, clean, cross
```

All packages are under `internal/` and importable only from this module.
Add new packages there unless there is a clear reason to expose them.

`internal/caller` and `internal/prompt` carry more security weight than their
size suggests. Before changing either, read
[`security-hardening.md`](security-hardening.md) — it records why the caller
path comes from `lsof` rather than `ps`, and why the dialog's two escaping
layers deliberately fail differently.

## Build gotchas

- **`make build` says `version=dev`.** The Makefile injects
  `main.version` from `git describe --tags --always --dirty`, which prints
  `dev` when there are no tags or you build from a tarball. Tag a release
  (`git tag v0.1.0`) before building if you want a real version stamped in.
- **Cross-compiling.** `make cross` builds `darwin-arm64` and
  `darwin-amd64` with `CGO_ENABLED=0` so the binaries are statically
  linked. Don't add cgo dependencies unless you're prepared to drop the
  cross target.
- **Caller name.** The `caller` package shells out to `ps` to walk the
  ppid chain. If that fails it returns `"unknown"` — the dialog still
  works, it just can't name the requesting process.

## Branching

Develop on a feature branch, commit with descriptive messages, and open
a draft PR when pushing.
