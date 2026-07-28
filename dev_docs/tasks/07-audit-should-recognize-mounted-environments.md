# Teach `1password-audit` to recognize mounted Environments

**Surfaced by:** `dev_docs/designs/op_mcp_review.md` (review of the
Environments MCP server).
**Status:** open. Pairs with task 06 — land them together or 06 emits a shape
the audit penalizes.

## How it is

Two checks in `skills/1password-audit/SKILL.md` grade on the presence of the
`.env.secrets` + `op run` pattern specifically, not on the property that
pattern is a proxy for:

- **Check 1** treats a `.env.secrets` whose values are all `op://` refs as
  correctly structured.
- **Check 3** WARNs when a `dev` / `start` / `serve` script lacks `op run`,
  gated on the project having `.env.secrets` or `.envrc`.

A project using a mounted Environment has neither artifact. Its dev script is
a plain `npm start` with no `op run`, and there is no reference file to find.
Check 1 finds nothing to praise; Check 3 either WARNs on a project that is
fine, or stays silent for the wrong reason (its `.env.secrets` / `.envrc` gate
never fires) and would miss a genuinely unprotected project alongside it.

## How it should be

Both checks recognize a mounted Environment as an equally good — not merely
tolerated — outcome, and grade the underlying property: *are secret values
kept out of the repo and out of plaintext on disk?*

The detection signal needs deciding before the edit. Candidates: 1Password
Environments config committed to the project, an entry in `.gitignore` for the
mount point, or an explicit note in the project's README. Pick one that
doesn't produce false positives on a stray `.env` — that would invert the
check.

## Acceptance

- A project on a mounted Environment audits clean: no WARN from Check 3, and a
  positive note from Check 1.
- A project with neither `.env.secrets`, `op run`, nor an Environment still
  WARNs — the fix must not create a hole by making "no evidence" look like
  "uses Environments".
- `version` bumped in the skill frontmatter and in both `.claude-plugin/*.json`
  manifests.
