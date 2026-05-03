# Proposal: session mode

Status: draft, not yet implemented.

## Summary

Add a `opx session` subcommand that activates a named, persistent
profile of `op://` URIs into a child shell as environment variables,
gated by one biometric approval covering the whole batch. Profiles
live in an encrypted file on disk; the encryption key is itself a
1Password item, so reading or editing the profile store also costs
one biometric. Single-shot `opx <uri>` and `opx --env …` are
unchanged when no session is active; this is purely additive.

The motivating problem is friction. `opx` was deliberately scoped so
that every secret read costs a biometric prompt, which is the entire
security property. In practice that means a developer iterating on a
script in a single terminal can hit the same prompt 30 times in 10
minutes for the same `op://Personal/AWS/access_key`. Click-fatigue
sets in, and at that point the prompt is no longer providing real
authorization — it has become a reflex.

`opx --env NAME=op://...` already batches multiple URIs into one
prompt for a single invocation, but it does not persist the URI list
across invocations. The user retypes the same `--env` flags every
shell. Session mode is the natural extension: persist the *set*, not
the secrets, behind a biometric-gated store.

## Goals

- Reduce prompt count from "per read" to "per shell" for workflows
  that always need the same URIs.
- Preserve the existing security property at the activation boundary:
  one explicit, scope-disclosing biometric approval that lists every
  URI before any secret leaves the process.
- Keep secrets off disk. Profiles persist; secret values do not.
- Make reading or editing the profile store itself require biometric
  approval, so an attacker with file access cannot silently widen
  what a future activation will fetch.
- Keep single-shot and `--env` modes byte-for-byte unchanged when no
  session is active.

## Non-goals

- **Caller-keyed allowlists.** `internal/caller.Name()` reads
  `/proc/<ppid>/comm` (or `ps -o comm=` on macOS), which is a
  human-readable label spoofable via symlink, `prctl(PR_SET_NAME)`,
  or `argv[0]` rewrites. Any "allow `python3` to read URI X" rule is
  trivially defeated by a malicious binary exec'd from a path with
  the right basename. Hash-keyed variants either rot on every tool
  upgrade (path-keyed → TOCTOU) or trust whole interpreters and so
  trust any script the interpreter runs. Caller identity is a
  warning label in the prompt; it cannot bear authentication weight.
- **Adding a URI to a *running* session without re-activating.**
  Doing this safely requires a long-lived parent process serving a
  Unix socket, which adds a daemon, peer-uid checks, socket
  permissions, and lifecycle code (zero-on-signal, zero-on-panic,
  socket cleanup). Out of scope for v1. The workaround is plain
  `opx <uri>` for one-offs, or `opx session add <profile> …` plus
  a re-activation for permanent additions.
- **Time-bounded skip-prompt without scoping.** That is the `op`
  session cache that `opx` exists to invalidate.
- **A "remember for N minutes" checkbox in the prompt.** It either
  collapses to caller-keyed (unsafe) or to time-bounded skip-prompt
  (a regression to `op`'s own behavior). Session mode subsumes the
  legitimate use case.

## Design

### Profile and config files

Two files under `~/.config/opx/`, both `0600`:

```
~/.config/opx/config.json       # plaintext, non-secret
~/.config/opx/profiles.enc      # AES-256-GCM encrypted
```

`config.json` is small, JSON-formatted, and human-readable:

```json
{
  "version": 1,
  "profileKey": "op://Personal/opx/profiles-key"
}
```

It stores schema version and the `op://` URI of the profile-store
encryption key. Nothing else lives here.

`profiles.enc` is the encrypted blob. After decryption it is JSON of
the form:

```json
{
  "profiles": {
    "work": {
      "uris": [
        {"name": "AWS_ACCESS_KEY_ID",     "uri": "op://Personal/AWS/access_key"},
        {"name": "AWS_SECRET_ACCESS_KEY", "uri": "op://Personal/AWS/secret_key"},
        {"name": "GH_TOKEN",              "uri": "op://Personal/GitHub/token"}
      ]
    },
    "personal": {
      "uris": [
        {"name": "OPENAI_API_KEY", "uri": "op://Personal/OpenAI/api_key"}
      ]
    }
  }
}
```

Each profile is a list of `(env_var_name, op_uri)` pairs — the same
shape as `--env` arguments today.

### Activation

```
opx session work -- bash
```

Flow:

1. Read `config.json` and `profiles.enc`.
2. Build a single `Confirmer.Request` whose `Bindings` cover the
   profile-store encryption key plus every URI in the requested
   profile. Show ONE dialog, list every URI plainly, get one
   approval.
3. On approve: fetch the encryption key via `op read`, decrypt
   `profiles.enc`, then fetch every profile URI via `op read`. All
   of these reads happen inside the consolidated approval window.
4. Run `op signout --all`. (This matches the current single-shot
   and `--env` behavior; the session activation is a one-shot
   read batch from `op`'s point of view.)
5. Set the named environment variables in a child environment and
   `exec` the requested command (`bash` in the example), inheriting
   the user's other env unchanged.
6. The child shell, and any descendants, see the env vars. When the
   shell exits, the env vars die with the process tree. There is no
   socket, no daemon, nothing to tear down.

The activation's biometric prompt lists every URI, so the user is
reviewing the full grant set every time. This is the same property
`--env` provides today.

### Bootstrap

`opx session init` runs once per machine:

1. Prompt the user for the `op://` path where the profile-store key
   will live (default suggestion: `op://Personal/opx/profiles-key`).
2. Generate a 32-byte random key with `crypto/rand`.
3. Write the key to that path via `op item create` (or, if the
   `Runner` extension is deemed too invasive, prompt the user to
   create the item by hand and paste the path back). One biometric.
4. Write `~/.config/opx/config.json` with the path.
5. Initialize `~/.config/opx/profiles.enc` as an encrypted empty
   profile set.

Subsequent runs read `config.json`, fetch the key, and operate on
`profiles.enc`.

### Tamper resistance

The user's stated requirement was that URIs not be readable or
editable without going through the opx approval process. The design
addresses each direction:

- **Reading** the profile store (e.g. `opx session list`,
  `opx session show work`) requires fetching the encryption key,
  which requires a biometric.
- **Writing** the profile store (e.g. `opx session edit work`,
  `opx session add work …`, `opx session rm work …`) requires
  fetching the key, which requires a biometric. After the edit, opx
  re-encrypts and writes atomically.
- **Direct file tampering** (an attacker overwriting `profiles.enc`)
  is detected at decryption time by AES-GCM's authentication tag.
  An opx that fails to authenticate refuses to use the file and
  exits with a clear error. No prompt is shown.
- **Swapping the key path** in `config.json` is a denial-of-service
  but not a privilege escalation: pointing at an attacker-controlled
  1Password item produces a different decryption key, AES-GCM auth
  fails, opx refuses. The attacker cannot inject grants.

`config.json` is intentionally plaintext. It contains only the
*location* of the encryption key, not the key itself. That location
is no more sensitive than anything else in the user's shell history.

### Why env vars instead of a Unix socket

An earlier design sketch had `opx session` stay alive as a parent
process serving a Unix socket. Children would invoke `opx <uri>`,
which would detect `OPX_SESSION_SOCK` and fetch over the socket.
This was discarded in favor of the env-var model:

- The threat model is essentially identical. In both designs, any
  process descending from the activated shell can read every
  granted secret. The user accepted that scope by typing
  `opx session work -- bash`.
- The env-var model has no daemon, no socket, no peer-uid checks,
  no zero-on-signal lifecycle, no socket-cleanup logic.
- It reuses the existing `--env` rendering path almost verbatim.
- It loses the ability to "add a URI to the running session" — see
  Non-goals. That is a deliberate v1 trade-off.

If a real need for in-session adds appears later, the socket model
can be layered on as v2 without changing the storage format.

## Security analysis

### Threat model

The protections session mode preserves:

1. A user who has not approved an activation cannot get profile
   secrets into their shell. Activation is biometric-gated.
2. An attacker with file access to `~/.config/opx/` cannot read
   the URI list (file is encrypted, key in 1Password) and cannot
   silently inject grants (writes also require the key).
3. After activation, `op signout --all` runs, so no `op` session
   token persists. The cached secrets live only as env vars in the
   user's chosen process tree.
4. Single-shot `opx <uri>` and `opx --env …` outside an active
   session are unchanged.

The protections session mode does *not* provide:

- Inside an active shell, any descendant process can read the
  granted env vars. This is the explicit cost the user is paying for
  the friction reduction; it is the same property as
  `eval "$(opx --env …)"` today.
- An attacker who tricks the user into running
  `opx session add work op://attacker-controlled` does inject a
  grant. The next activation will list the malicious URI in the
  prompt; the user must read the dialog. This is the same property
  `--env` has today.
- If the 1Password account is offline / locked-and-not-unlockable,
  session mode is unavailable. Single-shot mode is unaffected.

### Invariants (AGENTS.md)

The current AGENTS.md states:

> 1. Every successful read is preceded by a `Confirmer.Confirm` call.

This needs reformulation, because the goal is one Confirm covering
many reads at activation. Proposed wording:

> 1. Every secret value leaving the process is preceded by an
>    explicit, scope-disclosing approval. In single-shot mode the
>    approval is per read. In `--env` and session-activation modes
>    the approval is per invocation and the dialog discloses every
>    URI in the batch before any read happens.
> 2. `Runner.ForgetSession` is called on every exit path of every
>    invocation that ran any `op read`. (Unchanged.)
> 3. `op://` URIs are validated before being passed to `op`.
>    (Unchanged.)
> 4. Secrets only ever leave the process via `os.Stdout` (single-shot
>    and `--env`) or via `unix.Exec` of the activated child's
>    environment (session mode). They are never written to disk and
>    never logged.
> 5. Batch reads are atomic. (Unchanged.)

The "Things to avoid" entry on allowlists should be replaced with a
description of session mode's scope and an explicit note that
caller-name-keyed allowlists are still rejected for the spoofability
reasons documented above.

### Trade-offs accepted

1. **No add-to-running-session.** See Non-goals. Workaround is
   `opx <uri>` for one-offs.
2. **Profile store availability depends on `op` working.** If `op`
   cannot reach 1Password, the encryption key cannot be fetched and
   session mode is unavailable for that invocation. Single-shot mode
   is unaffected.
3. **The activation prompt is the integrity check.** AES-GCM
   detects file tampering, but a legitimately-added malicious entry
   (via a tricked `opx session add`) only surfaces at the next
   activation prompt. Documented in the user-facing docs.

## CLI surface

```
opx session init
opx session list
opx session show <profile>
opx session edit <profile>
opx session add <profile> <NAME=op://...> [<NAME=op://...> ...]
opx session rm <profile> [<NAME>]
opx session <profile> -- <command> [<args>...]
```

Each subcommand that touches profile contents goes through the
existing `Confirmer` and `Runner` interfaces. The security seam is
unchanged.

The `--` separator before the command in `opx session <profile> --
<command>` is required; it disambiguates profile names from flags
the user might want to pass to the child.

Exit codes follow the existing convention: 0 on success, 1 on `op`
or filesystem failure, 2 on usage error, 3 on user denial at the
prompt.

## Implementation plan

New packages:

- **`internal/profile/`** — JSON schema, AES-256-GCM
  encrypt/decrypt, schema validation, atomic write. Pure logic; the
  encryption key is provided by the caller, not fetched here. Tests
  cover round-trip, auth-tag failure on tampered ciphertext, schema
  validation, and `0600` mode enforcement.
- **`internal/config/`** — load/save `~/.config/opx/config.json`,
  resolve XDG paths, handle bootstrap. Tests cover round-trip and
  permission checks.

Changes to existing packages:

- **`main.go`** — new subcommand dispatch in the arg parser.
  Activation reuses `confirmAndRead` so the existing
  one-prompt-covers-all-bindings behavior is inherited automatically.
  The exec-with-env step is a new helper but is shorter than the
  current `--env` rendering path.
- **`internal/oprunner/`** — optionally add a `WriteItem` method on
  the `Runner` interface for `opx session init`. If we want to keep
  the `Runner` minimal, bootstrap can ask the user to create the
  1Password item by hand and just configure the path.
- **`internal/prompt/`** — no changes. The existing `Request` struct
  already supports listing N URIs in batch mode, which is exactly
  what activation needs.
- **`AGENTS.md`** — invariant rewording (above) and replacement of
  the "no allowlists" note with a section describing session mode.

The `internal/caller/` package is unchanged; caller name continues
to appear in the prompt as a label but is not used as an
authentication source.

## Verification

- `make test` covers `internal/profile/`, `internal/config/`, and
  the new `main.go` session-mode paths via `fakeRunner` /
  `fakeConfirmer`.
- `make test-integration` drives a real `opx session work -- bash`,
  asserts one prompt at activation, env vars resolve correctly in
  the child, no `op` session lingers, no temp file remains on shell
  exit.
- Adversarial manual checks: byte-flip `profiles.enc` → integrity
  error before any prompt; swap `config.json` `profileKey` →
  decryption fails; from a sibling shell with no inherited env, the
  granted secrets are not present.

## Open questions

- **Should bootstrap write the 1Password item itself, or ask the
  user to create it manually?** Writing it requires extending
  `Runner` with `op item create`; the manual path is a one-line
  doc instruction. Slight preference for the manual path to keep
  the `Runner` interface minimal, but this is a v1 ergonomics
  question.
- **Where does `~/.config/opx/` live on macOS?** XDG conventions
  put it at `$XDG_CONFIG_HOME/opx` (default
  `~/.config/opx`); macOS-native conventions would put it at
  `~/Library/Application Support/opx`. Suggest XDG default with
  `XDG_CONFIG_HOME` honored, since macOS users running CLIs
  generally expect XDG.
- **Should `opx session edit` decrypt to a temp file for `$EDITOR`,
  or read+write through stdin/stdout?** The temp-file path needs
  careful handling (memfd on Linux, secure cleanup on macOS) to
  avoid leaving plaintext on disk. The stdin/stdout path is safer
  but breaks normal editor UX. Suggest temp-file with `memfd_create`
  on Linux and a `0600` file under `$TMPDIR` on macOS, deleted
  immediately after the editor exits.
