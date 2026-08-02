# Security hardening

Why `opx` is built the way it is, for whoever changes it next.

Three documents divide this up, and the split matters:

- [`../AGENTS.md`](../AGENTS.md) states the **invariants** — the rules a change
  must not break. It is the normative one.
- [`../README.md`](../README.md) states what opx **does and does not defend
  against**, for someone deciding whether to use it.
- This file explains **why those rules exist**, and — more usefully — records
  the reasoning that was wrong on the way to them. Several of these properties
  have a plausible-looking simplification that reopens the hole they close.
  Each one is named below, because the plausible version is what the next
  person will reach for.

How the work was done is in git history and the PRs (#14–#27). Nothing here
repeats that.

## The scan that started it

A whole-repository scan on 2026-07-31 at revision `306fd05`, in `scan` mode at
`medium` effort. Forty-five researchers produced 71 candidates, 47 after
de-duplication; a three-lens panel cast 135 votes and 10 findings survived —
1 HIGH, 8 MEDIUM, 1 LOW. All ten are closed.

Three caveats on that record, all of which outlive the findings:

- **Nothing in the scan executed the code.** Every finding was derived from
  reading it. That cuts both ways: it also means no finding was confirmed by a
  proof of concept, and two of them turned out to rest on premises that only
  running the code could settle (below).
- **Two candidate sites went unreviewed** when the verification panel hit its
  budget. They are neither confirmed nor refuted — simply absent. Closing all
  ten findings did not touch them, and no task since has. They are unexamined,
  not cleared.
- During F3's panel, two of its three verifiers were flagged for wandering
  out of scope. That finding was re-read on its own merits before being fixed
  and held up.

The report itself is at `CLAUDE-SECURITY-20260731-081020/`, which carries its
own `.gitignore` of `*` and has never been committed. It stays that way: it is
a point-in-time artifact of a revision that no longer exists, its ten findings
are now described more accurately by this file and by the invariants, and the
one thing in it that is still live — the two unreviewed sites — is recorded
above where it will actually be read.

## Why the code had these problems

Not carelessness, and the distinction is the useful part. The pre-existing
`sanitizeCallerDetail` shows the original author had already found the class of
bug and fixed the instance in front of them: the caller-detail line was
sanitized, and the URI — equally attacker-controlled, and the thing the user is
actually being asked to approve — was not.

That is the shape of almost everything the scan found. The ten findings
collapse into four causes, and three of them are "the fix was scoped to the
instance rather than to the class":

1. **The dialog interpolated caller-controlled text without sanitizing it** —
   in every site except the one that had been noticed.
2. **Security-critical helpers were located through `PATH`** — `osascript`,
   `ps`, and `op`. This was the only HIGH: a planted helper that exits 0 *is*
   the approval.
3. **The run-mode dialog understated the child** about to receive the
   secrets — basenamed paths, truncated argv.
4. **Caller identity came from a self-asserted string**, while the verifiable
   one was fetched and discarded.

The lesson worth carrying is procedural: when you fix one of these, sweep for
the class and write down the scope you swept. Invariant 6 exists in its current
form because the original sweep was scoped to `message()`, and the one
interpolation it missed — `dialogTitle` — needed a follow-up fix after review
had passed the first one.

## The decisions that outlive the plan

### 1. Helpers come from compiled-in absolute paths — and what that does not buy

`PATH` belongs to the process opx is prompting the user *about*. A helper found
there is the caller choosing what answers its own dialog, and a helper that
exits 0 is indistinguishable from the user clicking Allow. So three screeners —
`resolveHelper` (prompt), `resolveTool` (caller), `resolveOp` (oprunner) —
accept only a regular, executable, non-group/world-writable file at a
compiled-in absolute path.

They are deliberately duplicated rather than shared. `caller` cannot import
`prompt`; `main` composes both; and the three candidate lists have no reason to
move together.

**The part that is easy to get wrong is what this establishes.** For
`osascript`, `ps` and `lsof` it is close to a guarantee: their real candidates
live under `/bin` and `/usr/bin` on the SIP-sealed system volume, which no
account can write to. For `op` it is not, and `resolveOp` must never be
documented as if it inherited that argument. `op` normally lives in a Homebrew
prefix owned by the invoking user (`/opt/homebrew/bin` is `drwxrwxr-x`, and the
`op` in it is a user-owned symlink), so anything running as the user can
replace it.

What `resolveOp` buys is narrower and is the whole of the finding: **the
caller's PATH no longer chooses.** Injecting a PATH entry — or dropping
`node_modules/.bin/op`, which npm puts first on PATH for every script it runs —
is ephemeral, targeted, and invisible. Overwriting the machine's real `op` is
persistent, global, and already total victory independent of opx.

Two implementation details that look like tidying and are not:

- The candidate list is matched **before** symlink resolution.
  `/opt/homebrew/bin/op` is a symlink into a versioned Caskroom path;
  canonicalising it would break the rule on every `op` upgrade.
- The list contains **no environment-derived path**. `~/.local/bin/op` is a
  real install location and was deliberately left out: there is no `~` at
  runtime, it comes from `$HOME`, and `$HOME` is set by the same caller that
  sets `PATH`. A HOME-derived candidate is a PATH allowlist wearing another
  variable's name.

`op` is resolved **once** and the path reused for both `ReadSecret` and
`ForgetSession`. Two lookups would re-run the scan, so a file appearing at an
earlier candidate between the calls could serve the signout while the real `op`
served the read — one biometric prompt paid, session never invalidated. What
this pins is the *path*, not the binary; a swap at the resolved path between
the two calls is knowingly left open, since it needs write access that already
defeats opx outright.

When nothing resolves: the dialog denies, `caller` degrades to `"unknown"` or
omits the path line, and `op` fails closed — including in `opx run`, where the
child is not spawned.

### 2. The caller path comes from the kernel, never from `ps`

**Rule: never display a path taken from `ps -o comm=`.**

This is the single most important correction in the whole effort, and it was
shipped as a *premise* in a task file that survived planning, re-baselining and
an unblocking review before anyone ran it. The premise was: "macOS
`ps -o comm=` returns the full executable path." It does not. Despite printing
what looks like a full path, `ps` derives `comm` from the process's **own
arguments**. Demonstrated with a binary in a temp directory exec'ing itself
with a forged `argv[0]`:

```
ps  -o comm=          -> "/usr/local/bin/claude"      <- the forgery
lsof -F fn -a -d txt  -> "/private/var/.../exe/forge" <- the truth
```

Implementing that task as written would have made the dialog vouch for an
attacker-chosen location — worse than the gap it was closing, because the
forgery would have arrived with an affirmatively clean-looking path beside it.

So `caller.exePaths` reads the text vnode via `lsof -F pfn -a -d txt`, for the
whole ancestor chain in one call. (`proc_pidpath()` would be the natural
choice; it needs cgo, which `make cross` forbids.) `ps` is still used for the
ppid walk and for the basename fed to `isUninteresting` — both tolerate a
self-asserted source. A displayed path does not.

Two things that are load-bearing and look like cleanup:

- **The batched `lsof` call ignores its exit status on purpose.** Verified on
  macOS 26.4: lsof exits 1, silently, when *any* requested pid yields no
  match, while still printing complete sections for the ones it resolved. A
  root-owned `login` anywhere in the chain is enough, so treating non-zero as
  failure discards every path in the common case. Partial output is normal
  here, not degraded.
- **opx does not classify the path** as expected or suspicious. A location
  allowlist was specified, built far enough to test, and dropped: it fires on
  real callers — Claude Code installs under `~/.local/share`, npm under
  `~/.npm-global`, cargo under `~/.cargo/bin` — and a marker that cries wolf on
  the ordinary case trains the user to click through the one dialog that
  matters.

### 3. What "the impersonation finding is closed" means, and what it does not

Two different properties hide under that sentence, and only one of them is
guaranteed.

**Which** process the dialog names is best-effort and **cannot be made
otherwise.** A real shell is both a legitimate ancestor and a plausible
attacker: `zsh evil.sh` has a kernel text vnode of literally `/bin/zsh` —
genuine, SIP-sealed, honest — and is the malware. No rule over executable
identity catches that, because being a real shell is compatible with being the
thing you are trying to describe.

**Where** the named process lives, and what was walked past to reach it, *is*
guaranteed: the subject's path and every skipped ancestor's path are
kernel-true, and nothing between the subject and opx is silently absorbed.
Three parts hold that up and none is optional:

- `skippable` gates on **disagreement, not list membership**. An ancestor is
  walked past only when `isUninteresting(comm)` *and* the kernel either agrees
  (`basename(exe)` equals `comm`) or says nothing. Reverting it to a bare list
  lookup reopens the pure `argv[0]`-forgery case. The asymmetry is deliberate:
  an unreadable path skips, because `login` runs as uid 0 and returns no txt
  vnode, and "unconfirmed ⇒ interesting" would make every bare-terminal read
  say `"login" wants to read`.
- `Identity.Through` **discloses what was walked past**, at kernel paths,
  nearest-to-opx last, with an unreadable ancestor rendered as `unknown` rather
  than omitted. Omission is the entire mechanism of laundering. It is wired
  into both input modes; in `opx run` the detail line is spent on the child, so
  this line and the header are the whole account of who asked.
- Entries under SIP-sealed prefixes are **suppressed** from that line, so the
  ordinary case stays quiet and the line still means something when it appears.
  This is not the location allowlist rejected above: SIP paths are the ones an
  attacker cannot occupy, so suppressing exactly those is a fact about the
  platform rather than a judgment about a path.

Substituting a skipped shell's argv onto that line was considered and rejected:
argv is self-asserted, so it would make some entries believable and others not,
destroying the property that makes the line worth reading at all.

Additions to `uninteresting` widen what can be laundered through. Treat them as
a security change, not a display-list tidy.

### 4. The dialog's two escaping layers, and where the boundary sits

`sanitizeDisplay` renders visibly — so the dialog still shows — every rune
whose effect is confined to itself: C0/C1 as `\xNN`, every other
`!strconv.IsPrint` rune as `\uXXXX`. The runes it deliberately leaves raw are
the ones whose effect is on the text *around* them, where an escaped rendering
and a raw one disagree about what the rest of the string says:
`unicode.Bidi_Control` plus `Zl`/`Zp`. Those hit the `%q` verbs on the
dialog-construction path, AppleScript's parser rejects the escape, and the
request fails closed.

The two layers fail differently **on purpose**, and the boundary moves in both
directions:

- Narrowing it — escaping bidi controls visibly too — removes the fail-closed
  and leaves nothing but an escaper holding the one dialog that authorizes the
  read.
- Widening it — leaving more runes raw — takes down whole requests over a rune
  in *any* dialog string, including a caller's directory name.
- Enumerating the set instead of asking `unicode` for it rots. The list
  hand-drafted for exactly this purpose omitted U+061C, U+200E and U+200F —
  three of the twelve.

A failure of the second layer is **not** a denial. `confirmDarwin` detects
those runes before spending an `osascript` and returns `ErrUndisplayable`
naming the code point; `main` reports it on stderr regardless of verbosity and
exits 1, so exit 3 keeps meaning "the user said no." Reporting a denial for a
dialog nobody was ever shown blames the user for a decision they did not make.

### 5. Ancestor rendering and child rendering are different problems

`RenderCommand` describes the process about to *receive* the secrets. It is an
authorization statement, not a summary: full paths, full argument values,
quoting anything whose boundaries would be ambiguous, and an explicit
`+N more arguments` count rather than trailing off.

`renderAncestorArgv` describes a process that already ran, where abbreviation
is a readability win.

**Do not reunify them however similar they look.** `/tmp/evil/curl` shortened
to `curl`, or `https://evil.example/collect` shortened to `collect`, is exactly
the part of the decision the user needed.

Also: do not reach for `internal/shellquote` to quote anything for display. It
exists for `eval` consumption — it always wraps in single quotes and passes
control bytes through unchanged, because nothing expands inside single quotes.
Display quoting is a different problem; see `quoteForDisplay` in
`internal/caller`, and the two commits that got it wrong.

### 6. Validation rejects what opx must not display — not what `op` will not accept

`uri.IsOPURI` rejects control characters, invalid UTF-8, and anything over
1024 bytes. Each has a reason that is not "tidy input":

- **Control characters** would reach the dialog and be shown as `\x1b` escapes
  — safe, but leaving the user to interpret gibberish in the one prompt that
  authorizes the read. Rejecting them at the door means the prompt is never
  drawn.
- **Invalid UTF-8** is rejected because ranging over a string decodes a bad
  byte as `U+FFFD`, so a raw `0x9b` passes a rune-only test. It cannot reach
  AppleScript, but the user would approve a dialog reading `�` while `op`
  receives the original byte. **The approved text and the used text must be the
  same string.**
- **The length cap** is the only one that restricts otherwise-legitimate input,
  and it is deliberate: a URI the dialog cannot show in full is one the user
  cannot review, and truncation that hides the tail is how a URI lies about
  which secret it names.

The control-character test is over **runes, not bytes**. Bytes in `0x80`–`0x9f`
are ordinary UTF-8 continuation bytes — verified: `‘quoted’`, `日本` and
`—dash` all contain them — so a byte scan would reject real vault names.

**And that argument is about display, not about resolution.** 1Password item
names legitimately carry unicode; `op://` references do not. Verified
2026-08-01 against the real binary, `op`'s own secret-reference parser accepts
a restricted ASCII set and rejects everything else before any vault lookup —
`é`, `日`, `—`, `‘`, a plain emoji, a non-breaking space, an ASCII apostrophe,
a tab. An item with a unicode name is reachable only by its ASCII item ID.
`IsOPURI` still must not encode that rule: it is `op`'s parser, `op`'s error
names the offending character precisely, and a copy here would reject names a
future `op` accepts.

## Residual risk

An attacker who already executes code as the user can place a binary at a
user-writable absolute path — `/usr/local/bin`, a Homebrew prefix — or edit the
shell profile. These changes remove opx's own trust in `PATH`; they do not and
cannot make opx safe against a fully compromised account. The vector they close
is the cheap, ambient, invisible one: a PATH entry, an npm `node_modules/.bin`
shim, a shell function. That is a real distinction, not a consolation — the
closed vector is ephemeral and targeted, and the remaining one is persistent
and global.

The dialog is also only as good as the reading of it. opx makes the request
legible. It cannot make an approval considered.

## Still not covered

- **The two unreviewed candidate sites** from the scan's panel. Unexamined, not
  cleared.
- **A failed `op read` exits 1 with no reason** unless `--verbose` — the user
  approves a biometric prompt and gets a bare exit code. Tracked in
  [`tasks/08-say-why-a-read-failed.md`](tasks/08-say-why-a-read-failed.md).
- **Subject selection**, per §3, which is best-effort by nature rather than by
  omission.

## How this went wrong, four times

Kept because the failure modes recurred, and because three of the four were
caught by someone re-reading merged code rather than by any review of the diff.

1. **Prose claiming more than the code holds** — eight corrections, every one a
   property its author believed was held. Widening a paragraph to cover a third
   case made a pre-existing sentence ("every invocation runs with a minimal
   environment") newly false about two of them. When you edit an invariant,
   re-read the whole invariant.
2. **Escaping, quoting, and separators** — four corrections. A display quoter
   that escaped `"` but not `\`, letting an argument forge its own closing
   quote. A through line joined with `" › "` while entries were raw paths, so a
   directory named `.../Downloads › /bin` rendered as two entries, the second
   looking SIP-sealed. Every path genuine; the entry count forged.
3. **Unverified premises — including the ones nobody stated.** The `ps` premise
   above is the famous one. The subtler case: a change that widened `lsof` from
   one pid to many inherited "non-zero exit means failure" from the
   single-pid version, and no stated acceptance criterion could have caught it
   because they all fed the parser directly. When a change widens what a
   command is asked for, re-run its *failure* modes. And the last defect of the
   effort was found not by any reviewer but by running the one acceptance
   criterion a human had to perform by hand.
4. **Acceptance criteria written for a design that was not chosen.** One task's
   criterion expected a refusal that only the *rejected* looser design would
   produce; a correct implementation would have read as a failing test. Check
   the criteria still match the design before trusting a red result.

Two testing traps cost real time and will recur, because both fail in the
direction of a plausible-looking finding:

- `internal/caller` cannot be exercised under a restrictive sandbox — it cannot
  exec the setuid `/bin/ps`, so every sandboxed run takes the degradation path
  and reports `unknown`. Verify with `go test -count=1`, unsandboxed; a cached
  result from a sandboxed run once faked a regression.
- `osascript`/`osacompile` cannot load StandardAdditions under the same
  sandbox, so *every* script fails to compile — including a bare `beep 3`. A
  verification harness needs a case that is expected to pass, or a broken
  harness is indistinguishable from the bug you went looking for.
