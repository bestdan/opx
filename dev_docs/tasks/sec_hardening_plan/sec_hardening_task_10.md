---
title: Disclose the skipped ancestor chain instead of trusting the skip heuristic
priority: medium
size: 3
status: new
created: 2026-08-01
source_branch: bestdan/security-scan-fixes
parent: sec_hardening
is_blocked_by: sec_hardening_task_7
related_files:
  - internal/caller/caller.go # ancestorChain, firstInteresting, isUninteresting, exePath, parseLsofTxt
  - internal/prompt/prompt.go # Request, message
  - main.go # confirmAndRead, runSubcommand
tags: [security, caller]
---

Part of [[sec_hardening_plan]]. Closes the **attribution-laundering half of F6**, which [[sec_hardening_task_7]] left open (#23, invariant 9 in `AGENTS.md`).

## Context

The dialog names one process: `firstInteresting` walks up to 6 ancestors and picks the first whose `ps` comm is not in the `uninteresting` set (shells, terminals, multiplexers). #23 made that process's *path* kernel-true via `lsof`, closing direct impersonation. It did not change **which** process gets named.

So malware still launders attribution by being boring: name yourself `bash`, get skipped, and the dialog attributes your request to the trusted program above you — now with that program's genuine kernel path printed beside it, which makes the misattribution more convincing than before.

## Why the obvious fix does not work

The obvious fix is to match `uninteresting` against `basename(kernelPath)` instead of the `ps` comm. **Do not implement that and call the hole closed.** Three failure modes, the third fatal:

1. **The attacker names the file too.** A dropper writes its binary to a file called `bash`. Kernel-basename matching then catches only malware that forges `argv[0]` but did not bother to name its file consistently.
2. **Unreadable paths break honest attribution.** `login` runs setuid-root and `lsof` returns nothing for it to a same-user caller. Verified on the dev machine: the real chain is `zsh ← claude ← zsh ← login (uid 0)`, and `lsof -p <login> -F fn -a -d txt` returns empty. A strict "unconfirmed ⇒ treat as interesting" rule would make a bare-terminal invocation say `"login" wants to read` with no path line. The forced fallback ("unreadable ⇒ trust comm") reopens the hole for any process that unlinks its own binary after exec.
3. **The interpreter hole, which no basename scheme can close.** `zsh evil.sh` has a kernel text vnode of literally `/bin/zsh` — genuine, SIP-sealed, honest. The walk skips it and laundering succeeds with **no forgery anywhere**. Being a real shell is compatible with being the malware, so "is this ancestor a shell?" is not answerable from executable identity at all.

`isUninteresting`'s own doc comment already says it is "an advisory display heuristic, not an identity check and not a security control". That comment is correct. This task must not try to promote it into a security control; it cannot be one.

## Approach

Stop trying to pick the right subject. Make skipping **visible**.

The invariant that can actually be made true, stated, and tested: **every process between the named subject and opx is either shown at its kernel path or shown as unknown — never silently absorbed.** "The dialog names the right process" cannot be made true without an attestation primitive (code-signing identity via `csops`), which needs cgo and is out of scope.

## Task

1. **Batch the path lookup.** `lsof` accepts a comma-separated pid set, and `-F` output delimits per-process sections with `p<pid>` lines. Verified: `lsof -p 16251,8149 -F pfn -a -d txt` returns both in **24 ms**. Resolve the whole chain in **one** call rather than one call per ancestor. `parseLsofTxt` grows a `p`-line dispatch and returns a `map[int]string`; keep the existing first-txt-record-wins rule per process (a nameless first record still yields "", per #23).
2. **Change the skip gate from membership to disagreement.** Skip an ancestor only when `comm ∈ uninteresting` **and** (its kernel path is unreadable, or `basename(kernelPath)` equals comm case-insensitively). A disagreement — comm says `bash`, kernel says `/tmp/evil` — makes that process the subject. This closes the pure `argv[0]`-forgery case and degrades to today's behaviour for `login`. It does **not** close (1) or (3) above; that is what step 3 is for.
3. **Add a `through` line** listing the skipped ancestors between the subject and opx, at their kernel paths, nearest last:

   ```
   "claude" wants to read:
   from /usr/local/bin/claude
   through /Users/x/.cache/tools/bash › /bin/zsh
   op://Personal/AWS/secret_key
   ```

   New field on `prompt.Request` alongside `CallerOrigin`, worded by `main.go` like the others, sanitized like every other interpolation (invariant 6). It must appear in **both** modes — in `opx run` the detail line describes the child, so the through line is the only disclosure of the requesting ancestry besides the header.
4. **Suppress SIP-sealed entries** (`/bin/`, `/usr/bin/`, `/System/`) from the through line to keep the common case quiet. This is *not* the location allowlist [[sec_hardening_task_7]] rejected: SIP paths are the one set of locations an attacker cannot occupy, so suppressing exactly those is a fact about the platform, not a judgment about the user's layout. Note the tradeoff explicitly in the code: suppression hides `/bin/zsh`, which is where failure mode (3) lives. If (3) is to be covered, show the skipped shell's **argv** basename instead (`through zsh evil.sh`) — self-asserted, so a disclosure and never a control, but at worst uninformative rather than falsely reassuring. **Decide this one deliberately and write down which was chosen and why.**
5. **An unreadable ancestor appears as `unknown`**, never omitted. Omission is what the invariant forbids.
6. Promote `isUninteresting`'s "additions widen the laundering surface" note into `AGENTS.md`, and rewrite invariant 9's "still open" paragraph to state the new invariant: subject *selection* is best-effort; subject and chain *paths* are kernel-true; nothing between subject and opx is silently absorbed.

## Acceptance Criteria

**Code-enforced**

- A test that a chain containing an ancestor whose comm is `bash` but whose kernel path is `/tmp/evil` names **`evil`** as the subject — asserting what it renders as, not merely that `bash` is absent.
- A test that an ancestor with an unreadable path renders as `unknown` in the through line rather than vanishing.
- A test that a stock chain (`zsh ← login`) still names the interesting process and does **not** regress to `"login"`.
- `parseLsofTxt` tests for multi-process `-F` output: correct pid→path mapping, a nameless record, and a pid absent from the output.
- Through-line tests in `internal/prompt` covering both dialog branches, SIP suppression, and control-character sanitizing.
- `make test`, `make lint`, `make cross` pass.

**User-run**

- Run `opx op://V/I/f` from a plain terminal and confirm the dialog reads cleanly with no through line (all skipped ancestors SIP-sealed).
- Copy a shell to `~/.cache/tools/bash`, run `opx` beneath it, and confirm it appears in the through line at its real path.

## Verification note

Every factual claim in this file was checked on the dev machine before it was written: the batched-`lsof` timing, the `login` uid and empty `lsof` output, and the real ancestor chain. [[sec_hardening_task_7]] shipped a wrong premise that survived three reviews because it was plausible and untested — see open question 7 in [[sec_hardening_plan]]. Do not add a claim here without running it.
