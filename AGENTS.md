# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, Cursor, etc.) working in
this repository. Humans should read `README.md` first.

## What this project is

`opx` is a small Go CLI that wraps the 1Password `op` binary to force a
biometric prompt on every secret read and to invalidate the `op` session
token after every invocation. It targets **macOS only** — the confirm
dialog is AppleScript and the caller lookup shells out to `ps`; other
platforms are not supported or tested. It is a **security tool**: changes that
weaken the trust boundary need to be flagged explicitly, not slipped in
as cleanup.

## Project layout

```
main.go                 # entry point; argument parsing, exit codes, signal & panic handling
main_test.go            # end-to-end tests of run() with fake Runner/Confirmer
run_subcommand_test.go  # tests for `opx run` (fake Runner/Confirmer/Spawner)
internal/caller/        # parent process name (ppid → `ps`)
internal/envfile/       # dotenv-style NAME=VALUE parser used by `opx run --env-file`
internal/oprunner/      # `op read` / `op signout` subprocess wrapper (Runner interface)
internal/prompt/        # native macOS confirm dialog (osascript)
internal/prompt/assets/ # embedded white-on-transparent PNG (Go recolors at runtime); build/ has the Python source — `make icon`
internal/shellquote/    # POSIX single-quote escaper for --env output
internal/spawn/         # exec wrapper used by `opx run` (Spawner interface)
internal/uri/           # `op://vault/item/field` syntax validator
scripts/                # local-dev scaffolding: fixtures, smoke test, install helper
Makefile                # build, test, test-integration, test-all, lint, clean, cross
skills/                 # Claude Code skills shipped with the repo (1password-audit/migrate/onboard/scaffold)
.claude-plugin/         # plugin + marketplace manifests making this repo installable as a Claude Code plugin
```

`skills/` is documentation, not code — no Go build or test touches it. Two
things to keep consistent when editing it: the skills recommend `opx` only
where raw `op read` would have been used (`op run` and `op inject` stay as
they are, since they scope secrets to a subprocess or a gitignored file),
and they must not claim `opx` accepts `--account` — it doesn't, it follows
`OP_ACCOUNT`. Bump `version` in the skill frontmatter and in both
`.claude-plugin/*.json` manifests when the content changes materially.

All packages are under `internal/` and importable only from this module.
Add new packages there unless there is a clear reason to expose them.

## Build, test, lint

```sh
make build             # compile ./opx for the current platform
make test              # unit tests (hermetic; what CI runs)
make test-integration  # local-only: hits real op binary, requires scripts/.env.example
make test-all          # test + test-integration
make lint              # go vet ./...
make cross             # CGO_ENABLED=0 builds for darwin-arm64, darwin-amd64
make clean
```

Run `make test` and `make lint` before reporting work as complete. If you
touch the cross-compile flow, run `make cross` too — it must stay
CGO-free. `make test-integration` is fixture-dependent and triggers
biometric prompts, so it is not required for every change — run it when
touching `internal/oprunner` or anything else at the `op` boundary.

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
  `exitUsage`, `exitDenied`). Reuse them; don't introduce ad-hoc integers.
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
- Don't write tests that shell out to a real `op` or `osascript`; CI
  will not have them.

## Security invariants — do not regress

These properties are the entire point of the project. Touching them needs
deliberate intent.

1. **Every successful read is preceded by a `Confirmer.Confirm` call.**
   See `confirmAndRead` and `runSubcommand` in `main.go`. In batch /
   `--env` / `opx run` modes a single Confirm covers every URI in the
   request; the dialog must show all of them so the user can review the
   full set before approving. (`opx run` skips Confirm only when there
   are zero `op://` references — it then behaves as a plain dotenv
   loader.)
2. **`Runner.ForgetSession` is called on every exit path** — success,
   error, signal, panic. See the `defer` in `main()` and the
   unconditional calls in `readAndForget` and `runSubcommand`. Batch and
   run modes do not change this: one Forget per invocation, regardless
   of N. In `opx run`, ForgetSession runs **before** the child is
   spawned so the child never inherits a usable op session — and a
   ForgetSession *failure* there is fatal: the child is not spawned.
   Run mode fails closed because it hands control to a potentially
   long-lived child; the other modes exit immediately, so they only
   warn.
3. **`op://` URIs are validated before being passed to `op`.** All args
   run through `uri.IsOPURI` before the read. Validation happens before
   `Confirm`, so a malformed URI fails as a usage error without prompting.
   In `opx run`, a value that starts with `op://` but fails validation
   is rejected as a usage error rather than silently passed through as
   a literal string.
4. **Secrets leave the process only via the chosen sink.** In single
   and `--env` modes that sink is `os.Stdout` (shell-quoted via
   `internal/shellquote` in `--env` mode). In `opx run` the sink is the
   child process's environment — secrets must never be written to opx's
   own stdout in run mode. Don't log them, don't include them in error
   messages, don't write them to temp files.
5. **Batch reads are atomic.** If any read in a `--env` or `opx run`
   batch fails, nothing reaches the sink — no stdout output, and in run
   mode the child is not spawned at all. Half-populated environments
   are a footgun, not a feature.
6. **Every caller-controlled value the user sees passes through
   `sanitizeDisplay`** — dialog body *and* title. See `message` and
   `dialogTitle` in `internal/prompt`. The URI is attacker-controlled in
   every input mode (`uri.IsOPURI` checks the `op://` prefix and three
   non-empty segments, so any byte is legal inside a segment), and the
   caller name is a self-asserted process name. Scope this to "every
   place caller-controlled text reaches the user", not to one function:
   the original sweep was scoped to `message()`, and `dialogTitle` — the
   interpolation it missed — needed a follow-up fix. Unescaped, a CR or
   ESC repaints the one dialog that authorizes the read.

   `sanitizeDisplay` is only half the guard, and the halves fail
   differently on purpose. It covers C0/C1 (`< 0x20`, `0x7f`–`0x9f`),
   rendering those visibly so the dialog still shows. Everything else
   non-printable — bidi overrides like U+202E, other format characters —
   is caught by the `%q` that `dialogScript` wraps the body and title in:
   Go renders the rune as a literal `\uXXXX`, AppleScript's parser
   rejects that, `osascript` exits non-zero, and `confirmDarwin` maps
   that to `ErrDenied`. So the request fails closed rather than
   rendering a reordered path. **Do not "simplify" that `%q` into plain
   interpolation** — it is the half that stops a URI from lying about
   which secret is being requested.
   `TestDialogScript_NonPrintableNeverReachesAppleScriptSource` guards
   it; if that test is in your way, you are removing the guard, not the
   noise.
7. **Security-critical helpers are resolved from compiled-in absolute
   paths, never PATH.** See `resolveHelper` in `internal/prompt`, which
   screens for a regular, executable, non-group/world-writable file;
   it currently covers `osascript` and the `defaults` appearance query.
   PATH belongs to the process opx is prompting the user about, so a
   helper found there is the caller choosing what answers its own
   dialog — and a helper that exits 0 is indistinguishable from the user
   clicking Allow. **`ps` (`internal/caller`) and `op`
   (`internal/oprunner`) are still resolved by bare name and are not yet
   covered**; closing that is outstanding work, not a documented
   exemption.
8. **`caller.RenderCommand`'s output is an authorization statement, not
   a summary.** In `opx run` it is the only disclosure of which process
   receives the plaintext secrets, so it keeps full paths and full
   argument values, quotes anything whose boundaries would otherwise be
   ambiguous, and states how many arguments it dropped rather than
   trailing off. Do **not** merge it back into `renderAncestorArgv`
   however similar they look: abbreviation is a readability win when
   describing a process that already ran, and a lie when describing one
   about to be handed secrets.

If a change appears to remove or weaken any of these, call it out
explicitly in the PR description rather than burying it in a refactor.

Invariants 6 and 8 have each already needed a follow-up fix after the
original change merged — a missed interpolation site, and a display
quoter that escaped `"` but not `\`, letting an argument forge its own
closing quote. Both were believed held by the author and caught by
someone reading the merged code. Escaping and quoting are where this
codebase's reviews have actually failed; weight them accordingly.

## Things to avoid

- Adding flags or features beyond what the task asks for. The CLI is
  deliberately small: three input modes (single positional URI;
  repeatable `--env NAME=op://...` pairs; `opx run` with `--env-file`
  / `--env` feeding a child command), four exit codes.
- Adding nicknames, allowlists, config files, or any other indirection
  between the user-typed `op://` URI and the read. `opx` was scoped down
  to a pure security boundary around `op`; convenience layers were
  considered and explicitly rejected because they don't add safety and
  the dialog is the trust boundary. `--env` is *not* an exception: the
  user still types every URI on the command line; it only batches the
  approval, it does not store or alias anything.
- Adding a non-strict / "skip signout" mode. The forced session
  invalidation is the entire reason this tool exists.
- Making the confirm dialog's `beep` configurable. A switch was considered
  and rejected: the beep is tamper evidence — it makes an access attempt
  audible when the dialog opens unseen and would otherwise absorb a silent
  60-second timeout — and any opx-level switch, env var or config file,
  would be readable and writable by the very process opx is gating. macOS's
  System Settings › Sound is the configuration surface. A caller can reach
  that too, but only by muting the machine globally and persistently, which
  is the difference that matters. See `dialogScript` in `internal/prompt`.
- Reintroducing `exec.LookPath`, or a bare command name, for any
  security-critical helper. See invariant 7 — the lookup itself is the
  vulnerability, not just what the helper does afterwards.
- Reaching for `internal/shellquote` to quote something for **display**.
  It exists for `eval` consumption: it always wraps in single quotes
  (noise in a dialog) and passes control bytes through unchanged, since
  nothing expands inside single quotes. Display quoting is a different
  problem — see `quoteForDisplay` in `internal/caller`, and the two
  commits that got it wrong before touching it.
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
