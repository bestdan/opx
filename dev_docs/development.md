# Development

Notes for hacking on `opx` itself. End users should read [`../README.md`](../README.md)
first; this document assumes you've already cloned the repo and have the
prerequisites installed.

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
internal/uri/           # `op://vault/item/field` syntax validator
Makefile                # build, test, lint, clean, cross
```

All packages are under `internal/` and importable only from this module.
Add new packages there unless there is a clear reason to expose them.

## Coding conventions

- **Standard library first.** No third-party dependencies are currently
  imported and that is intentional. Don't add one without a strong
  justification — `cobra`, `viper`, `logrus` etc. are all out.
- **Package comments.** Every package starts with a `// Package foo …`
  doc comment. Keep that style.
- **Exported identifiers documented.** Each exported function/type has a
  short comment explaining intent (not just restating the signature).
- **Errors are wrapped with `%w`** so callers can use `errors.Is` /
  `errors.As`. `prompt.ErrDenied` is the canonical sentinel for user
  denial — return it (or wrap it) rather than inventing parallel errors.
- **Exit codes are centralized** in `main.go` (`exitSuccess`, `exitOpFail`,
  `exitUsage`). Reuse them; don't introduce ad-hoc integers.
- **No `fmt.Println` to stdout** outside of writing the secret bytes —
  `os.Stdout` is the secret channel. Diagnostics go to `os.Stderr`.

## Testing conventions

- Tests live in `_test` packages (e.g. `package uri_test`) and import the
  package under test. Keep that boundary — it forces tests to use the
  exported API.
- The seams that let tests work are the `oprunner.Runner` and
  `prompt.Confirmer` interfaces. New behavior in `main.go` should plumb
  through those interfaces rather than calling the real `op` or `osascript`
  directly. See `main_test.go` for the `fakeRunner` / `fakeConfirmer`
  pattern.
- Don't write tests that shell out to a real `op`, `osascript`, or
  `zenity`; CI will not have them.

## Security invariants — do not regress

These properties are the entire point of the project. Touching them needs
deliberate intent.

1. **Every successful read is preceded by a `Confirmer.Confirm` call.**
   See `confirmAndRead` in `main.go`.
2. **`Runner.ForgetSession` is called on every exit path** — success,
   error, signal, panic. See the `defer` in `main()` and the unconditional
   call in `readAndForget` in `main.go`.
3. **`op://` URIs are validated before being passed to `op`.** All args
   run through `uri.IsOPURI` before the read.
4. **Secrets only ever go to `os.Stdout`.** Don't log them, don't include
   them in error messages, don't write them to temp files.

If a change appears to remove or weaken any of these, call it out
explicitly in the PR description rather than burying it in a refactor.

## Things to avoid

- Adding flags or features beyond what the task asks for. The CLI is
  deliberately tiny: one mode, three exit codes.
- Adding nicknames, allowlists, config files, or any other indirection
  between the user-typed `op://` URI and the read. `opx` was scoped down
  to a pure security boundary around `op`; convenience layers were
  considered and explicitly rejected because they don't add safety and
  the dialog is the trust boundary.
- Adding logging frameworks, config loaders, or CLI parsing libraries.
- Introducing cgo (breaks `make cross`).
- Caching the `op` session — that is exactly what this tool exists to
  prevent.
- Editing `.gitignore` to allowlist build artifacts; the repo intentionally
  ignores all `opx*` binaries and `*.test`.

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
