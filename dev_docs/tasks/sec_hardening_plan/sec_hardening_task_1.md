---
title: Sanitize every dialog-body interpolation, not just CallerDetail
priority: high
size: 2
status: new
created: 2026-07-31
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
related_files:
  - internal/prompt/prompt.go:91 # message()
  - internal/prompt/prompt.go:115 # the --env bullet, F4/F8's sink
  - internal/prompt/prompt.go:156 # sanitizeCallerDetail
  - internal/prompt/prompt.go:264 # confirmTTY writes to /dev/tty
  - internal/prompt/prompt_test.go
  - internal/prompt/message_internal_test.go
tags: [security, prompt]
---

Part of [[sec_hardening_plan]]. Closes **F2, F4, F8**.

## Context

`sanitizeCallerDetail` (`prompt.go:156`) renders C0/C1 control characters as visible `\xNN` escapes. Commit `a58bd99` added it for exactly the right reason — process-controlled argv must not repaint the confirmation UI — but it is only ever reached through `callerDetailLine`, i.e. only for `Request.CallerDetail`.

Everything else `message()` interpolates goes out raw:

- `req.Caller` at `prompt.go:94` and `:101` (via `%q`, which escapes control bytes as a side effect of Go quoting — so this one is incidentally safe, but only by accident of the verb)
- `req.Bindings[i].URI` at `prompt.go:96`, `:98`, `:115`, `:117` — **raw `%s`**
- `req.Bindings[i].Name` at `prompt.go:115` — **raw `%s`**

`confirmTTY` then writes the whole body to `/dev/tty` with `fmt.Fprintln` (`prompt.go:264`), before any read and regardless of the eventual answer. An ESC or CR inside a URI therefore repaints the one dialog that authorizes the read.

The URI is fully attacker-controlled in every input mode: `uri.IsOPURI` (`internal/uri/uri.go:8`) only checks the `op://` prefix and three non-empty `/`-separated segments, so `op://V/I/f<ESC>[2J<ESC>[H...` passes. It reaches `Binding.URI` from argv, from `--env NAME=op://...`, and from `--env-file` values (`internal/envfile/envfile.go:83`).

`Binding.Name` is already constrained to `^[A-Za-z_][A-Za-z0-9_]*$` by `envfile.nameRE` and `main.go`'s `envNameRE`, so sanitizing it is defense in depth against a future parser change, not a live hole. Sanitize it anyway — the point is that *no* dialog interpolation is exempt.

On darwin the exposure is narrower but real: `%q` at `prompt.go:191` turns an embedded newline into an AppleScript newline, so extra lines can be injected into the dialog body even there.

## Task

1. Rename `sanitizeCallerDetail` to `sanitizeDisplay` and re-document it as "the escaper every dialog-body interpolation passes through", not as a `CallerDetail` special case. Update its one existing call site in `callerDetailLine`.
2. In `message()`, route `bind.URI` and `bind.Name` through `sanitizeDisplay` at all four interpolation sites (`:96`, `:98`, `:115`, `:117`). Apply it to `req.Caller` too, so the safety does not depend on `%q` staying the verb.
3. Add a short comment at the top of `message()` stating the invariant: everything interpolated into the returned body has passed through `sanitizeDisplay`, because the body reaches `/dev/tty` unescaped.
4. Do not change `pangoEscape` — it solves a different problem (Pango markup) and composes fine on top.

Keep the change inside `internal/prompt`. No signature changes to `Confirmer`, `Request`, or `Binding`.

## Acceptance Criteria

**Code-enforced**

- `internal/prompt/message_internal_test.go` gains a table test asserting that a `Binding` whose `URI` contains ESC (`\x1b`), CR (`\r`), and a DEL-range byte renders with those bytes escaped as `\x1b` / `\x0d` / `\x7f` in `message()`'s output, and that the raw bytes appear nowhere in it — assert on the absence of the raw byte, not just the presence of the escape.
- The same assertion for a multi-binding (`--env`) request, since `:115` is the sink F4 and F8 both name.
- A test asserting `Binding.Name` is likewise escaped.
- An existing-behaviour test that a benign URI (`op://Private/AWS/key`) is unchanged byte-for-byte — the sanitizer must not mangle normal output.
- `make test` and `make lint` pass.

**User-run**

- On a Linux host (or a container) with no `zenity` on PATH, run `./opx 'op://V/I/f'$'\x1b''[2Jforged'` and confirm the TTY prompt shows the escape visibly rather than clearing the screen.
