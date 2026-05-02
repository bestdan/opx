# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, Cursor, etc.) working in
this repository. Humans should read `README.md` first.

## What this project is

`opx` is a small Go CLI that wraps the 1Password `op` binary to force a
biometric prompt on every secret read and to invalidate the `op` session
token after every invocation. It is a **security tool**: changes that
weaken the trust boundary need to be flagged explicitly, not slipped in
as cleanup.

## Project layout

```
main.go                 # entry point; argument parsing, exit codes, signal & panic handling
main_test.go            # end-to-end tests of run() with fake Runner/Confirmer
internal/allowlist/     # loads ~/.config/opx/allowlist.json with strict perm/ownership checks
internal/caller/        # parent process name (ppid → /proc/.../comm or `ps`)
internal/oprunner/      # `op read` / `op session forget` subprocess wrapper (Runner interface)
internal/prompt/        # platform-native confirm dialog (osascript / zenity / /dev/tty)
internal/uri/           # `op://vault/item/field` syntax validator
Makefile                # build, test, lint, clean, cross
```

All packages are under `internal/` and importable only from this module.
Add new packages there unless there is a clear reason to expose them.

## Build, test, lint

```sh
make build        # compile ./opx for the current platform
make test         # go test ./...
make lint         # go vet ./...
make cross        # CGO_ENABLED=0 builds for darwin-arm64, darwin-amd64, linux-amd64
make clean
```

Run `make test` and `make lint` before reporting work as complete. If you
touch the cross-compile flow, run `make cross` too — it must stay
CGO-free.

Go 1.24+ is required (see `go.mod`).

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
  `exitUsage`, `exitConfig`). Reuse them; don't introduce ad-hoc integers.
- **No `fmt.Println` to stdout** outside of writing the secret bytes —
  `os.Stdout` is the secret channel. Diagnostics go to `os.Stderr`.

## Testing conventions

- Tests live in `_test` packages (e.g. `package allowlist_test`) and import
  the package under test. Keep that boundary — it forces tests to use the
  exported API.
- The seams that let tests work are the `oprunner.Runner` and
  `prompt.Confirmer` interfaces. New behavior in `main.go` should plumb
  through those interfaces rather than calling the real `op` or `osascript`
  directly. See `main_test.go` for the `fakeRunner` / `fakeConfirmer`
  pattern.
- Permission/ownership tests in `internal/allowlist` skip on Windows via
  `runtime.GOOS == "windows"` — preserve that guard if you add similar
  Unix-only assertions.
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
3. **Allowlist files must be owned by the current user and not
   world-readable.** Both checks happen before JSON is parsed
   (`internal/allowlist/allowlist.go`).
4. **`op://` URIs are validated before being passed to `op`.** Both
   direct-mode args and allowlist values run through `uri.IsOPURI`.
5. **Secrets only ever go to `os.Stdout`.** Don't log them, don't include
   them in error messages, don't write them to temp files.

If a change appears to remove or weaken any of these, call it out
explicitly in the PR description rather than burying it in a refactor.

## Things to avoid

- Adding flags or features beyond what the task asks for. The CLI is
  deliberately tiny: two modes, four exit codes.
- Adding logging frameworks, config loaders, or CLI parsing libraries.
- Introducing cgo (breaks `make cross`).
- Caching the `op` session — that is exactly what this tool exists to
  prevent.
- Editing `.gitignore` to allowlist build artifacts; the repo intentionally
  ignores all `opx*` binaries and `*.test`.

## Branching

Develop on a feature branch, commit with descriptive messages, and open
a draft PR when pushing. The active development branch for the current
task is set in the session prompt.
