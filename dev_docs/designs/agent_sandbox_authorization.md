# Agent sandboxes and the raw-read path

**Status:** open question. No code written; nothing here is committed to.
**Question asked:** can an agent sandbox be opened for `opx` without also
being opened for raw `op`?
**Short answer:** not with the knobs that exist today. The sandbox's unit of
authorization is a **socket path**, and `op` and `opx` are indistinguishable
at that boundary. There is a posture that works today (§3), tooling worth
building now (§4), and a structural option that stays open (§5).

## 1. The failure that raised it

Observed 2026-07-30, in an agent session that had a coding agent's usual
shell access.

The agent needed an API key for a maintenance task. Its `op` call failed with
`1Password CLI couldn't connect to the 1Password desktop app`. It read that
as a broken CLI install, declared the task blocked, and **suggested the user
paste the API key into the chat by hand.**

Nothing was broken. Agent sandboxes commonly block unix sockets, and `op`
reaches the desktop app over one, so every sandboxed `op` call fails
identically regardless of session state. The convenient fix — allowlist the
socket directory — was tried, worked, and was then abandoned, because it also
makes raw `op read` succeed silently for any sandboxed process for the
lifetime of the session.

Two observations from that session outlive it, and they are the reason this
document is not only about sockets:

- **The agent reached for raw `op read`, never `opx`** — despite `opx` being
  the documented path for interactive reads in that environment's agent
  instructions. Prose guidance did not survive contact with an agent that had
  a working `op` on `PATH`.
- **When blocked, its fallback was to ask the human to paste the secret into
  the transcript.** So the failure mode of an over-tight sandbox is not "no
  secret leaks" — it is "the secret leaks through a worse channel, in
  cleartext, into a logged conversation." Any hardening story needs an
  ergonomic sanctioned path, or it will route around itself.

This section is motivation, not evidence. The argument in §2 stands on its
own and can be reproduced by anyone in about ten minutes.

## 2. The structural problem

The sandbox's unit of authorization is a **filesystem path to a socket**.
Both `op` and `opx` must reach the same socket — `opx` shells out to
`op read`. So:

| Sandbox posture            | Raw `op read` from an agent | `opx` from an agent |
| -------------------------- | --------------------------- | ------------------- |
| Socket **not** allowlisted | blocked                     | blocked             |
| Socket allowlisted         | **works, silently**         | works               |

There is no configuration that produces "blocked / works". The allowlist
cannot express "only this binary," because it is not a binary-level control.
Nor can `opx` fix it from where it sits today: as a wrapper invoked *by* the
agent, it is one `PATH` lookup away from being bypassed by the tool it wraps.

Note this is strictly worse than the pre-`opx` desktop-app story, where at
least the biometric prompt gated the first read. Once the session is warm,
an allowlisted socket makes the vault ambiently readable by anything the
agent runs, bounded only by the session's inactivity timeout.

Three mechanics are load-bearing to the argument, all tested rather than
assumed (2026-07-30, macOS):

- The socket filenames under the 1Password group-container directory are
  **regenerated per session**, so only a directory/subpath grant works. A
  grant on one socket file would expire on its own; a directory grant does
  not.
- Putting `1password.com` in an allowed-domains list does **nothing** for
  this. That covers the HTTPS API; desktop-app auth never goes over HTTP.
  This is an easy trap and worth an explicit check (§4).
- Sandbox config changes of this kind take effect immediately, with no
  session restart — so the grant is as easy to add mid-session as it is to
  forget about afterwards.

## 3. The posture today: do not allowlist the socket

This is `opx`'s recommendation, and it needs no infrastructure.

**Leave the 1Password socket blocked in agent sandboxes.** The resulting
failure is not a defect to be worked around — it *is* the control. It forces
every secret read to become a visible, individually approved escalation
out of the sandbox, which is exactly the property an allowlist destroys.
Reads still happen; they just cannot happen ambiently.

Three rules follow, and each of them failed at least once in the episode
above:

1. **Decode the error correctly.** `1Password CLI couldn't connect to the
   1Password desktop app` means the call ran sandboxed. It does not mean the
   CLI is broken, and it does not tell you anything about session state.
   Re-run it unsandboxed.
2. **Do not probe for session state.** No probe is reliable: `op whoami`
   reports `account is not signed in` against a live session that serves
   reads fine, and `op account list` prints the configured account whether or
   not a session exists. Attempt the read you actually need and decode *its*
   error. (An earlier draft of this document recommended `op account list`
   as the probe. That was wrong, and is corrected here.)
3. **Never fall back to pasting a secret into a transcript.** A blocked read
   is a prompt to escalate, not a prompt to downgrade the channel.

If a team allowlists the socket anyway, the mitigation is on the 1Password
side rather than in `opx`: bound what a live session can reach — a dedicated
vault or account for agent work, or a scoped service account — so the
ambient read that the allowlist enables is ambient over very little. This is
1Password configuration, not an `opx` design, and it does not answer the
question in §2; it only shrinks the blast radius of answering it wrongly.

## 4. What ships now: make the sanctioned path the easy one

§3 makes the "blocked / blocked" row of the table correct. It does not make
it *livable* — and §1 shows what an unlivable posture produces. The failure
there was behavioral, not structural: an agent bypassed the sanctioned path,
and then proposed the worst available fallback. That part is addressable
today, in `skills/`, with no change to the binary.

- **`1password-audit`: add a sandbox-configuration check.** Scan the
  project's agent sandbox config for a unix-socket grant covering the
  1Password group-container path, and fail it with the reason — *this
  re-opens raw `op read` for every sandboxed process for the session
  lifetime*. Add the allowed-domains trap from §2 as a second check: naming
  `1password.com` there does nothing for desktop-app auth, and someone who
  added it believes they configured something. A mechanical check is the
  strongest form of this idea, because it is not prose an agent can skim.
- **`1password-onboard`: add a troubleshooting section.** Decode the
  desktop-app error as sandbox-versus-session, prescribe the escalation, and
  state the paste-into-chat ban explicitly. This is the material an agent
  needs at the exact moment the episode in §1 went wrong.

Two limits, stated honestly:

- **This is still prose, and prose already failed once.** The difference is
  the delivery point — a skill fires at the decision moment, and an audit
  check is mechanical — so it plausibly does better. It is not a guarantee.
- **Tooling makes the posture livable; it does not close the gap in §2.**
  Only something with the shape of §5 produces the "blocked / works" row.
  These are answers to different problems and should not be traded off
  against each other.

An `opx doctor`-style subcommand is explicitly **not** the answer. `opx`
cannot diagnose the sandbox from inside it — its own `op` subprocess fails
identically — and it would violate the deliberately small CLI. There is also
a sharper reason, verified in the code rather than assumed: `confirmAndRead`
calls `Confirm` and only reaches `readAndForget` if it succeeds
(`main.go:343`–`355`), which is invariant 1. So a sandboxed `opx` draws the
dialog, collects the user's approval, and *then* fails the read. The
approval is spent on a request that could never have succeeded — which is an
argument for keeping the sandbox boundary legible (§3, §4), not for teaching
`opx` to explain itself after the fact.

## 5. The open option: `opx` as a broker

The only shape that yields the row §2 cannot: **`opx` owns the only process
that can talk to 1Password, and agents talk to `opx`.**

- The broker runs **outside** the agent sandbox and is the sole holder of the
  `op` relationship. It keeps the existing guarantees: disclose URI and
  requesting process, force a fresh biometric prompt, `op signout --all`
  after each read.
- It listens on its **own** unix socket, at a path `opx` controls.
- The sandbox allowlists *that* socket and **not** 1Password's. The 1Password
  socket stays blocked, so raw `op read` from inside the sandbox fails
  exactly as it does under §3.
- `opx <uri>` inside the sandbox becomes a thin client handing the URI to the
  broker and receiving either the secret or a denial.

**Its best argument is not the one originally given.** A broker gets
**out-of-process caller identity**. Today `internal/caller` runs inside a
process the caller spawned, in an environment the caller controls — invariant
7 exists precisely to fight that. A broker can resolve the peer pid from the
socket itself and run the existing `ps`/`lsof` machinery from a process the
caller did *not* spawn. That is a real strengthening of invariant 9's story,
and it is the most security-positive part of the proposal. (The specific
mechanism — `getsockopt` `LOCAL_PEERPID`/`LOCAL_PEERCRED` on macOS — is
believed workable but has not been verified here.)

**Its honest cost-benefit against §3 is thin.** Under §3, raw `op` is already
blocked and every read is already a visible, approved action. The broker
removes one escalation step and one opportunity to misdiagnose. Given §1,
that is worth something real — but it is **ergonomics, not a new security
property**. The gate before and after is the biometric prompt `opx` already
forces per read.

**If it is ever built, launchd socket activation is the only acceptable
shape.** launchd owns the socket and spawns `opx` per connection: no
persistent process, and no state between reads beyond the socket path. A
long-lived daemon is in direct tension with a project whose identity is a
static binary with no runtime dependencies and no residual state; socket
activation reduces the residue to a registered service and a socket path.

Costs the original proposal did not name: a client/broker protocol is a
version-skew and update surface that a single static binary does not have,
and the broker socket is itself a new ambient endpoint reachable by exactly
the process class `opx` distrusts.

## 6. Open questions

1. **Is the residual state acceptable?** Socket activation reduces this from
   "a daemon" to "a registered launchd service plus a socket path," which is
   a much smaller ask than the original framing. It is still more than zero,
   and `AGENTS.md` currently rules out config files, caches, and indirection
   between the typed URI and the read.
2. **What does the broker authenticate?** Any process that can reach the
   broker socket can ask for any URI. The biometric prompt is still the real
   gate, so the broker's value is that it *cannot be bypassed*, not that it
   authenticates callers — with the caveat that peer-credential lookup would
   make the *disclosure* in the dialog materially more trustworthy than
   today's in-process ancestry walk. Sharpen this before building anything.
3. **Per-read prompting versus agent ergonomics — likely unanswerable.** A
   biometric prompt per read is the whole point, but an agent doing a 30-step
   task with 5 secret reads generates 5 prompts, and the human starts
   approving reflexively. Every "scoped grant" answer to this (this URI, this
   task, N minutes) is the `op` session cache reborn under `opx`'s own name,
   which `AGENTS.md` names as the thing this tool exists to prevent. If
   per-read prompting is non-negotiable, this question has no broker-shaped
   answer, and the broker should not be sold as solving it.
4. **Does the Environments MCP server already cover this?** Its
   per-interaction desktop authorization is the same property.
   [`op_mcp_review.md`](./op_mcp_review.md) concludes it does not close the
   raw-read path; §1 here is the concrete, observed instance of that gap.

## 7. What would settle or reopen this

- **A sandbox that authorizes per-binary rather than per-socket-path.** The
  question in §2 dissolves entirely; no broker is needed.
- **Environments reaching GA with a human read-with-approval path.** This is
  `op_mcp_review.md`'s own revisit trigger, and it would overlap `opx`'s
  single-URI mode directly.
- **Evidence that §3 fails in practice.** The paste-into-chat pattern
  recurring despite §4's tooling, or escalation approvals visibly becoming
  reflexive, would show the posture is not livable and raise the value of §5.
- **A workload that needs sandboxed `opx` often enough for the friction to be
  measurable.** The cost-benefit in §5 is thin *at current usage*. It is a
  function of read frequency, and that can change.
