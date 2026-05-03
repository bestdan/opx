# Development

Notes for hacking on `opx` itself. End users should read [`../README.md`](../README.md)
first; this document assumes you've already cloned the repo and have the
prerequisites installed.

For coding conventions, testing conventions, security invariants, and the
list of things to avoid, see [`../AGENTS.md`](../AGENTS.md) — it is the
single source of truth and applies to humans and AI agents alike.

## Prerequisites

- Go 1.24+ (`go version`)
- The `op` CLI installed and on `PATH` (only required at runtime, not to build)
- `make`

## Make targets

```sh
make build              # compile ./opx for the current platform
make test               # go test ./...
make lint               # go vet ./...
make cross              # CGO_ENABLED=0 builds for darwin-arm64, darwin-amd64, linux-amd64
make clean
```

Run `make test` and `make lint` before opening a PR. If you touch the
cross-compile flow, run `make cross` too — it must stay CGO-free.

## Repository layout

```
main.go                 # CLI entry point, exit codes, signal & panic handling
main_test.go            # end-to-end tests of run() with fake Runner/Confirmer
internal/caller/        # parent process name (ppid → /proc/.../comm or `ps`)
internal/oprunner/      # `op read` / `op session forget` subprocess wrapper (Runner interface)
internal/prompt/        # platform-native confirm dialog (osascript / zenity / /dev/tty)
internal/shellquote/    # POSIX single-quote escaper for --env output
internal/uri/           # `op://vault/item/field` syntax validator
Makefile                # build, test, lint, clean, cross
```

All packages are under `internal/` and importable only from this module.
Add new packages there unless there is a clear reason to expose them.

## Build gotchas

- **`make build` says `version=dev`.** The Makefile injects
  `main.version` from `git describe --tags --always --dirty`, which prints
  `dev` when there are no tags or you build from a tarball. Tag a release
  (`git tag v0.1.0`) before building if you want a real version stamped in.
- **Cross-compiling.** `make cross` sets `CGO_ENABLED=0` so the binaries
  are statically linked and portable. Don't add cgo dependencies unless
  you're prepared to drop the cross target.
- **Caller name on non-Linux/macOS.** The `caller` package reads
  `/proc/<ppid>/comm` on Linux and shells out to `ps` elsewhere. On
  platforms without either it returns `"unknown"` — the dialog still
  works, it just can't name the requesting process.

## Branching

Develop on a feature branch, commit with descriptive messages, and open
a draft PR when pushing.
