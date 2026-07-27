# Rework Check 1's secret detection

**Surfaced by:** co-review of PR #11 — `pk_live_` / `pk_test_` (Stripe
*publishable* keys) and `AC[a-z0-9]{32}` (Twilio Account SID) were in the FAIL
prefix list. Both are public by design. Removed as a point fix; the shape of the
check is the real issue.

## How it is

Check 1 in `skills/1password-audit/SKILL.md` is a hand-maintained list of ~13
literal prefixes plus a 32-char entropy heuristic. Every FAIL it produces is
ranked #1 in the "What to Fix First" section, so a false positive sends the user
to rotate a credential that was never secret. The list has no notion of
public-by-design values, no way to grow without editing the skill, and no
coverage for the long tail (GCP service-account JSON, Azure connection strings,
private-key PEM blocks, JWTs).

The point fix added a "do not FAIL on these" paragraph. That works but it's the
same hand-maintained shape, now in two directions.

## How it should be

Two options, worth deciding between rather than drifting:

1. **Delegate.** The project already recommends `detect-secrets` (Check 5). Have
   Check 1 shell out to it when available (`detect-secrets scan --string`) and
   fall back to the prefix list when not. Upstream maintains the rules; the
   skill maintains the workflow. Costs a tool dependency in `allowed-tools`.
2. **Restructure.** Keep the list but split it into three tiers — *secret*,
   *public-by-design* (never FAIL, optionally note), *ambiguous* (WARN, needs a
   human) — and move it to a reference table next to the best-practices doc so
   it's editable without touching the skill's control flow.

Option 1 is less to maintain and more accurate. Option 2 keeps the skill
self-contained, which matters because the skill is read-only and may run in
projects without `detect-secrets` installed.

## Acceptance

- A decision recorded, and Check 1 restructured to match.
- Fixture cases covering: a real `sk_live_`, a `pk_live_` alongside its secret
  counterpart, a bare `AWS_ACCESS_KEY_ID`, and a JWT.
