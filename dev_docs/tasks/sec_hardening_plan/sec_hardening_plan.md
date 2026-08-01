---
type: epic
title: Remediate the 2026-07-31 security scan findings
status: active
created: 2026-07-31
---

# Remediate the 2026-07-31 security scan findings

## Goal

Close the ten verified findings from `CLAUDE-SECURITY-20260731-081020/CLAUDE-SECURITY-RESULTS.md` (1 HIGH, 8 MEDIUM, 1 LOW). All ten attack the same thing from three directions: **the confirmation dialog is opx's entire trust boundary, and the untrusted caller currently gets to influence what that dialog says and which binaries implement it.**

## Root causes

The ten findings collapse into four groups, which is how the tasks are sliced:

| # | Root cause | Findings | Tasks | Status |
|---|---|---|---|---|
| A | Dialog interpolates caller-controlled text without sanitization — `sanitizeCallerDetail` from `a58bd99` was never applied to `Binding.URI` / `Binding.Name` | F2, F4, F8 | [[sec_hardening_task_1]], [[sec_hardening_task_2]] | both **merged** (#15, #25) |
| B | Security-critical helper binaries (`osascript`, `ps`, `op`) resolved by bare name through caller-controlled PATH | **F1 (HIGH)**, F3, F5 | [[sec_hardening_task_3]], [[sec_hardening_task_4]], [[sec_hardening_task_5]] | all **merged** (#14, #22, #26) |
| C | Run-mode dialog understates the child that receives the secrets — `renderArgv` basenames path-like tokens, `RenderCommand` cuts at 120 runes | F7, F9, F10 | [[sec_hardening_task_6]] | **merged** (#16, + #19) |
| D | Caller identity is the self-asserted `comm` string; the verifiable executable path is fetched and then discarded | F6 | [[sec_hardening_task_7]], [[sec_hardening_task_10]] | both **merged** (#23, #24) — F6 closed |

Group B contained the only HIGH: a planted `zenity` that exits `0` **was** the approval, and its presence also suppressed the `/dev/tty` fallback, so no prompt appeared at all.

## Revised 2026-07-31, after #17 narrowed opx to macOS only

The Linux and `/dev/tty` code paths are gone from the tree, which changes three things:

- **F1's Linux half is moot** as well as fixed. What survives from task 3 and still matters is `resolveHelper` and the `osascript` hardening — the same class on the one supported platform.
- **Task 7 shrinks** (size 5 → 3): the `/proc/<pid>/exe` half went with the Linux support. The macOS side was always the important one.
- **Tasks 4 and 7 get sharper, not easier.** `ps` is now the *only* source of caller identity, with no `/proc` walk to fall back on or cross-check against.

Task 8 was **unblocked and promoted** on those grounds — three security properties were merged and undocumented — and has since landed (#20, #21).

## Scope / non-goals

In scope: `internal/prompt`, `internal/caller`, `internal/oprunner`, `internal/uri`, and the two `prompt.Request` construction sites in `main.go` (`confirmAndRead` at `main.go:278`, `runSubcommand` at `main.go:418`).

Explicitly **not** in scope, per `AGENTS.md`:

- No new flags, config files, allowlists of *URIs*, nicknames, or env-var knobs. (Allowlisting *helper binary paths* is different — it is a fixed, compiled-in set with no user-facing surface, and that distinction is the one judgment call worth confirming; see Open questions.)
- No third-party dependencies, no cgo (`make cross` must stay CGO-free).
- No change to the exit-code set (`exitSuccess`, `exitOpFail`, `exitUsage`, `exitDenied`).
- No change to where secrets go: stdout in single/`--env` mode, child environment in run mode.
- The two candidate sites the scan's panel left unreviewed are not covered here; they are unknown, not cleared.

## Approach

Three principles, applied in this order:

1. **Nothing the caller controls reaches the terminal unescaped.** One sanitizer (`sanitizeDisplay`) on every dialog-body interpolation, plus rejection of control bytes at the `uri.IsOPURI` validator so a control-laden URI is a usage error before any prompt is drawn. Belt and braces, because this is the boundary.
2. **Security-critical subprocesses are resolved from a compiled-in absolute-path allowlist, not PATH.** Each helper gets a `resolve` step that checks the candidate is a regular file at a known system location and is not group/world-writable. If no trusted candidate exists, degrade to the safe option (TTY prompt for the dialog; "unknown" for identity) or fail closed (`op`).
3. **The dialog describes the child faithfully when it is about to hand it secrets.** Ancestor descriptions can stay abbreviated — they are context. The run-mode child line is an authorization decision and must not omit paths, hosts, or trailing argv.

The tradeoff accepted throughout: an attacker who already executes code as the user can still place a binary at a *user-writable absolute* path and put it early on PATH for a program that trusts PATH. These fixes remove opx's own trust in PATH; they do not (and cannot) make opx safe against an attacker who can rewrite the user's shell profile. That residual risk gets written down in [[sec_hardening_task_8]] rather than left implicit.

## Tasks

1. ~~[[sec_hardening_task_1]] — Sanitize every dialog interpolation, not just `CallerDetail`~~ — **merged, #15** (+ `0bb2506`).
2. ~~[[sec_hardening_task_2]] — Reject C0/C1 control bytes in `uri.IsOPURI` so a forged URI fails before the prompt~~ — **merged, #25**, with the optional length cap included and the rune-vs-byte subtlety recorded in the task file.
3. ~~[[sec_hardening_task_3]] — Resolve dialog helpers from a trusted absolute-path allowlist~~ — **merged, #14**.
4. ~~[[sec_hardening_task_4]] — Invoke `ps` by absolute path with a minimal environment~~ — **merged, #22**.
5. ~~[[sec_hardening_task_5]] — Resolve `op` once, reject untrusted locations, fail closed~~ — **merged, #26**. Landed as a wide compiled-in candidate list rather than the `LookPath`-plus-denylist this plan originally specified; see open question 2.
6. ~~[[sec_hardening_task_6]] — Render the run-mode child command faithfully~~ — **merged, #16** (+ #19).
7. ~~[[sec_hardening_task_7]] — Base caller identity on the executable path, not the self-asserted `comm`~~ — **merged, #23**, but its premise was wrong and the task file carries the correction: macOS `ps -o comm=` is forgeable, so the path comes from `lsof`.
8. ~~[[sec_hardening_task_8]] — Record the merged invariants and the residual risk~~ — **merged, #20** (+ #21, which added the `%q` layer to invariant 6 and a test pinning it).
9. [[sec_hardening_task_9]] — Graduate the durable decisions to `dev_docs/security-hardening.md` and delete this plan folder.
10. ~~[[sec_hardening_task_10]] — Disclose the skipped ancestor chain instead of trusting the skip heuristic~~ — **merged, #24**. F6 fully closed. The obvious fix (matching `uninteresting` against the kernel basename) was rejected as unsound before implementation; step 4's open decision was settled as **suppress SIP-sealed entries, do not substitute argv**, for the reasons in the task file's Outcome section.

11. ~~[[sec_hardening_task_11]] — A non-printable rune in a legitimate item name (emoji ZWJ, option-space NBSP) makes `opx` unusable and reports it as a denial~~ — **PR #27, open**. Pre-existing, found while building task 2; not one of the ten scan findings. Took the third of the three candidate directions, but replaced its hand-maintained list with `unicode.Bidi_Control` ∪ `Zl` ∪ `Zp` — the objection to that option was that a list rots, and the list sketched in the task file had already dropped three of the twelve. Invariant 6's boundary moves; `AGENTS.md` says so in the same change.

Remaining order: **9 only** — task 11 is in review as #27. Task 9 closes the plan out — and must carry task 7's correction, task 10's outcome, task 2's rune-vs-byte reasoning **and task 11's correction to it** (`op` rejects non-ASCII in secret references, so a unicode item name is reachable only by ID, and no dialog change makes it readable) into `dev_docs/security-hardening.md`, since that file will outlive this folder.

## Progress

| | Task | Finding(s) | State |
|---|---|---|---|
| ✅ | 1 — sanitize every dialog interpolation | F2, F4, F8 | merged #15, + `0bb2506` |
| ✅ | 2 — reject control bytes in `uri.IsOPURI` | (defense in depth) | merged #25 (`eff8e0e`) |
| ✅ | 3 — resolve dialog helpers from absolute paths | **F1 (HIGH)** | merged #14 |
| ✅ | 4 — `ps` by absolute path, minimal env | F3 | merged #22 |
| ✅ | 5 — resolve `op` once, fail closed | F5 | merged #26 (`92177b9`) |
| ✅ | 6 — faithful run-mode child rendering | F7, F9, F10 | merged #16, + #19 |
| ✅ | 7 — identity from exe path, not `comm` | F6 | merged #23 — impersonation closed |
| ✅ | 8 — invariants + residual risk in docs | (none) | merged #20, + #21 |
| ⬜ | 9 — graduate to `dev_docs/`, delete plan | (none) | **next / last** |
| 🔄 | 11 — non-printable rune reads as a denial | (none — found in task 2) | PR #27, open |
| ✅ | 10 — disclose the skipped ancestor chain | F6 (laundering half) | merged #24 (`20195dc`) |

**All 10 findings closed** — F1, F2, F3, F4, F5, F6, F7, F8, F9, F10.

What "closed" does and does not mean, in the two places it is weaker than it sounds. **F6:** subject *selection* is still best-effort and cannot be made otherwise — a real shell is a legitimate ancestor and a plausible attacker at once — so what #24 closed is the *silent* half: paths are kernel-true and nothing between the subject and opx is absorbed unseen. **F5:** `op` now comes from a fixed location, which stops the caller's PATH choosing the binary; it does not make the binary at that location genuine, since a Homebrew prefix is user-owned.

Task 11 is not a scan finding — it is a pre-existing usability defect the task 2 work uncovered, tracked here because it lives in the same two invariants and would otherwise be forgotten. Note what "F6 closed" does and does not mean: subject *selection* is still best-effort and cannot be made otherwise (a real shell is a legitimate ancestor and a plausible attacker at once), so what #24 closed is the *silent* half — paths are kernel-true and nothing between the subject and opx is absorbed unseen.

Not covered by any task: the **2 candidate sites the scan's verification panel left unreviewed** when it hit its budget. They are unexamined, not cleared, and closing every task below still leaves them that way.

## Open questions

1. ~~**Is a compiled-in absolute-path allowlist for helper binaries acceptable under the "no allowlists, no config" rule in `AGENTS.md`?**~~ **Answered by #14 merging.** The rule targets indirection between the user-typed URI and the read; a helper-path list touches neither and is invisible to the user. `resolveHelper` has since taken a third caller without objection.
2. ~~**How strict should `op` resolution be (task 5)?**~~ **Answered: strict compiled-in list, made wide.** Not because strict beats loose in the abstract, but because the framing was wrong. `/opt/homebrew/bin` is group-writable and its `op` is a user-owned symlink, so an allowlist does **not** establish that the binary is genuine — `resolveHelper`'s SIP-sealed-volume argument does not transfer. What it establishes is that **the caller's PATH does not choose**, which is the whole of F5.

   Once that is the property, three things follow. The looser rule is a denylist over a search order the attacker writes — it stops `node_modules/.bin` and the cwd tree, while `$TMPDIR/op`, a pnpm store path or a mise shim dir all pass as clean 755 files at absolute PATH entries. The *width* of a compiled-in list costs nothing, because every location on it is one the user chose rather than one a caller injected — so it covers both Homebrew prefixes, `/usr/bin`, and MacPorts, and the "breaks a legitimate install" objection mostly evaporates. And the list must contain **no environment-derived path**: `~/.local/bin` was considered and rejected, because there is no `~` at runtime — it comes from `$HOME`, set by the same caller that sets `PATH`, so a HOME-derived candidate is a PATH allowlist under another variable's name.

   Two implementation notes that came out of building it. The candidate must be matched **before** symlink resolution (`/opt/homebrew/bin/op` is a symlink into a versioned Caskroom path, so the `EvalSymlinks` the task file specified would break the rule on every `op` upgrade), and resolving once pins the **path, not the binary** — a swap at the resolved path between the read and the signout is not closed, and needs the write access that already defeats opx outright.
3. ~~**Should the dialog header show the full executable path (task 7), or keep the short name and put the path in the detail line?**~~ **Answered, and implemented in #23**: short name in the header, path on its own line — in **both** modes, not just the one `Describe()` feeds. The question as written missed that `opx run` sets `CallerDetail` to the *child* command, so a path folded only into `Describe()` would leave run mode — the mode that actually hands over the secrets — with no path at all. That needed a new `prompt.Request.CallerOrigin` field.
4. ~~**Is a wider truncation limit acceptable for the run-mode line (task 6)?**~~ **Answered by #16 merging**: 300 runes for the child line, explicit `+N more arguments` elision, `argv[0]` never truncated.
5. ~~**F3's panel is worth a second look.**~~ **Closed by #22.** Two of that finding's three verifiers had been flagged for out-of-scope behaviour during the scan, so the finding was worth re-reading rather than trusting. It held up: the four lines were exactly as described, and the fix landed with three external reviewers (codex, agy, devin) returning no findings.
6. **What is the durable lesson for display escaping?** Two follow-up fixes on this plan (`0bb2506`, #19) each restored a property the original author believed was held — a missed interpolation site and an unescaped backslash. Both are quoting/escaping bugs found *after* review by someone reading the merged code. Task 8 wrote the rule down (#20).

   **Partly answered.** The specific gap I worried about — bidi overrides slipping past `sanitizeDisplay` — does not exist on the supported platform. `%q` wraps the entire AppleScript body and title, so a U+202E in a URI becomes a literal backslash-u escape, AppleScript's parser rejects the script, `osascript` exits non-zero, and `confirmDarwin` maps that to `ErrDenied`. Verified directly: the escape yields `syntax error … (-2741)`, rc 1. So the layering is `sanitizeDisplay` for C0/C1 (rendered visibly) and `%q` for everything else non-printable (fails closed). That is a real design, not an accident, and it argues **against** a single shared escaper: the two layers deliberately fail differently.

   **Now fully answered — #21** states the layering in invariant 6 and adds `TestDialogScript_NonPrintableNeverReachesAppleScriptSource` to pin it, so the `%q` can no longer be tidied away silently.

   The lesson that survives, for task 5: **escaping and quoting are where review has actually failed on this plan** — four separate corrections (`0bb2506`, #19, and two caught in co-review on #20 and #21), every one a property the author believed was already held. Two of those were doc claims that overstated coverage, which is the same error in prose. Assert what the code does, then go check; don't assert a count you haven't grepped.

7. **A task spec is not evidence.** Task 7 asserted that macOS `ps -o comm=` returns an unforgeable executable path. It does not — `ps` echoes the process's own `argv[0]` — and implementing the task as written would have made the dialog vouch for an attacker-chosen location, which is worse than the gap it was closing. The false claim survived planning, re-baselining, and an unblocking review, because it was written by analogy to Linux's `/proc/<pid>/exe` (where it *is* true) and read plausibly every time.

   For task 5, which rests on comparable claims about where `op` lives and how `exec.LookPath` behaves: **run the thing before building on what the plan says about it.** The forgery took one throwaway Go program to demonstrate. Every claim in a task file that begins "X returns…" or "Y cannot be forged" is a claim someone should have tested, and on this plan, twice now, nobody had.

   **Task 10 adds the sharper corollary: check the claims the file *didn't* make.** Every assertion in task 10's file had been run before it was written, and all of them held. What nearly shipped a bug was an unstated premise inherited from #23 — that `lsof` returning non-zero means the lookup failed. It exits **1, silently, when any one requested pid yields no match**, while still printing the sections it resolved, so a root-owned `login` in range would have blanked the entire chain through the retained `if err != nil { return "" }`. None of the acceptance criteria would have caught it: they all feed the parser directly, never the subprocess. When a change widens what a command is asked for (one pid → many), re-run the command's *failure* modes, not just its success path.

   **Task 4 adds a corollary about the checking itself.** Its "identity is unchanged" claim was checked with `go test`, which replayed a **cached** result from an earlier sandboxed run and reported `Name() == "unknown"` — a fabricated regression that a second, uncached run disproved. The Bash sandbox also cannot exec the setuid `/bin/ps` at all, so *any* sandboxed run of `internal/caller` exercises the degradation path rather than the real one. For task 5, and for anything else touching `internal/caller`: verify behaviour with `go test -count=1`, unsandboxed, or the evidence is about the harness rather than the change.

   **Task 11 generalizes that corollary past `internal/caller`.** Its
   end-to-end check compiled the AppleScript `dialogScript` actually builds
   and reported *every* case broken — plain ASCII included. Under the Bash
   sandbox `osacompile` cannot load StandardAdditions, so a bare `beep 3`
   fails to compile too. What caught it was the control case failing, which
   is the general lesson: **a verification harness needs a case that is
   expected to pass**, or a harness-level failure is indistinguishable from
   the finding you were looking for. Task 4's cached-result phantom and this
   one are the same bug in the evidence, twice.
