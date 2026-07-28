# Offer a mounted 1Password Environment as the preferred local-dev path

**Surfaced by:** `dev_docs/designs/op_mcp_review.md` (review of the
Environments MCP server).
**Status:** open. Blocked on judgment, not on work — Environments are beta.

## How it is

`1password-migrate` (Step 2) and `1password-scaffold` (Step 3) both land the
reader on the same shape: a committed `.env.secrets` holding `op://`
references, plus `op run --account … --env-file=.env.secrets -- <cmd>` wired
into the dev script. That is correct and remains the safe default.

It is now one generation behind. 1Password Environments can mount a local
`.env` directly from the Environment, with no plaintext on disk and no
reference file to keep in sync against the vault. Same guarantee, one fewer
artifact.

## How it should be

Both skills present the mounted Environment as the preferred path, and keep
`op run --env-file` documented immediately below it as the fallback. The
fallback is not optional — Environments require the 1Password desktop app on
Mac / Windows / Linux, are unavailable on iOS and Android, and are beta.

Constraints that must survive the edit:

- Don't recommend `opx` anywhere new. The existing rule holds: `opx` replaces
  raw `op read`, never `op run` or `op inject`.
- Don't claim `opx` accepts `--account`. It follows `OP_ACCOUNT`.
- Environments are a separate namespace from `op://vault/item/field`. A
  project already on vault items is not one command away from this; say so
  rather than implying a drop-in swap.

## Acceptance

- Both skills show the mounted-Environment path first and `op run --env-file`
  as an explicitly supported fallback, with the beta / platform caveat stated
  once per skill.
- `version` bumped in the frontmatter of each edited skill and in both
  `.claude-plugin/*.json` manifests.
- Cross-check against task 07 — the audit skill's grading must accept whatever
  shape these two skills now emit.
