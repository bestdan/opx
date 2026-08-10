# A broker between the caller and `op`

**Decision:** do not put a long-lived process between callers and `op`.
**Status:** rejected.
**Decided:** 2026-08.
**Rule it backs:** `AGENTS.md` → "Things to avoid".

## Context

An agent sandbox that blocks the 1Password socket blocks `opx` with it. The
sandbox's unit of authorization is a socket **path**, and `opx` reaches
1Password by running `op` against that same socket, so there is no setting
that separates them:

| Sandbox posture            | Raw `op read` from an agent | `opx` from an agent |
| -------------------------- | --------------------------- | ------------------- |
| Socket **not** allowlisted | blocked                     | blocked             |
| Socket allowlisted         | **works, silently**         | works               |

The "blocked / works" row is the one a careful user wants and cannot have.
This matters because the failure is not theoretical: observed 2026-07-30, an
agent hit the blocked socket, misdiagnosed it as a broken CLI install,
declared its task blocked, and suggested the human paste an API key into the
chat transcript. So an over-tight boundary does not produce "no secret leaks";
it produces a leak through a worse channel.

A **broker** is the only shape that produces the missing row. A process
outside the sandbox holds the sole `op` relationship and listens on its own
socket; the sandbox allowlists *that* socket and not 1Password's; `opx` inside
the sandbox becomes a thin client. Raw `op read` stays blocked because the
1Password socket stays blocked.

The proposal is coherent. It was still rejected.

## Why it was rejected

**It buys ergonomics, not a security property.** The alternative is not "no
protection" — it is the posture in the README's "Not defended, today": leave
the socket blocked and run `opx` outside the sandbox. Under that posture raw
`op` is *already* blocked and every read is *already* a visible, individually
approved escalation. What the broker removes is one escalation step and one
opportunity to misdiagnose an error. That is worth something real, given the
episode above, but the gate before and after is the same biometric prompt
`opx` already forces on every read.

**Its ergonomic win has no answer that isn't the session cache.** An agent
running a thirty-step task with five secret reads generates five prompts, and
a human approving five prompts starts approving reflexively. Every fix for
that — a grant scoped to this URI, this task, the next N minutes — is the
`op` session cache rebuilt under `opx`'s own name. Forced session
invalidation is the entire reason this tool exists, so the broker cannot
solve the problem it would create without dismantling its own premise.

**It costs what the project is.** A daemon is residual state in a tool whose
identity is a static binary with no runtime dependencies and nothing left
behind. A client/broker split also adds a protocol and a version-skew
surface a single binary does not have, and the broker socket is itself a new
ambient endpoint reachable by exactly the process class `opx` distrusts.

One counter-argument deserves recording, because it is the strongest version
of the idea and it is not what the original proposal led with: **launchd
socket activation** would avoid most of the residual-state objection. launchd
owns the socket and spawns `opx` per connection, so there is no persistent
process and no state between reads beyond a registered service and a socket
path. If this is ever built, that is the only acceptable shape. It does not
rescue the proposal on its own, because the first two objections are
untouched by it.

## What would reopen it

Both of these, not either:

1. **Out-of-process caller identity.** Today `internal/caller` runs inside a
   process the caller spawned, in an environment the caller controls —
   invariant 7 exists to fight exactly that, from a weak position. A broker
   could read the peer pid off its own socket and run the `ps`/`lsof`
   machinery from a process the caller did *not* spawn, which is a materially
   stronger basis for what the dialog attributes the read to than invariant
   9's ancestry walk can be. (The mechanism — `getsockopt` with
   `LOCAL_PEERPID`/`LOCAL_PEERCRED` on macOS — is believed workable and has
   **not** been verified.) This is a genuine security gain and the only one on
   offer here.
2. **Evidence the current posture is failing.** The paste-into-chat pattern
   recurring despite the skills work tracked in the repo's issues, or
   escalation approvals visibly becoming reflexive. Absent that, the broker is
   solving a friction nobody has measured.

Two adjacent changes would dissolve the question instead of reopening it: a
sandbox that authorizes per-binary rather than per-socket-path, or 1Password
Environments reaching GA with a human read-with-approval path (see
[`../designs/op_mcp_review.md`](../designs/op_mcp_review.md), whose own revisit
trigger this is).

## Source

The full 237-line analysis this was distilled from is on the closed
[PR #32](https://github.com/bestdan/opx/pull/32), branch retained. It was not
merged: reasoning behind nothing shipped, marked "open", reads to a later
contributor as sanctioned direction — and a document making undated claims
about a third-party sandbox rots with no test to catch it.
