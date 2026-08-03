# Teach `1password-audit` to recognize mounted Environments

**Surfaced by:** `dev_docs/designs/op_mcp_review.md` (review of the
Environments MCP server).
**Status:** open. Pairs with task 06 — land them together or 06 emits a shape
the audit penalizes.

## How it is

Three checks in `skills/1password-audit/SKILL.md` grade on the presence of the
`.env.secrets` + `op run` pattern specifically, not on the property that
pattern is a proxy for:

- **Check 1** treats a `.env.secrets` whose values are all `op://` refs as
  correctly structured.
- **Check 2** WARNs on any `.env*` file that doesn't end in `.secrets` or
  `.tpl`.
- **Check 3** WARNs when a `dev` / `start` / `serve` script lacks `op run`. The
  `.env.secrets` / `.envrc` gate applies only to the Makefile/justfile branch
  and the shell-script branch; the `package.json` branch is ungated and WARNs
  on any matching script without `op run`.

A project using a mounted Environment has no `.env.secrets` or `.envrc`, so
Check 1 finds nothing to praise. Check 3 then splits by branch: a `package.json`
project on a plain `npm start` takes the ungated branch and gets a spurious
WARN, while a Makefile or shell-script project stays silent for the wrong
reason — its gate never fires — and would miss a genuinely unprotected project
alongside it.

The worse case is the mounted `.env` itself. Whether it lands inside the
project tree isn't established here — but the detection-signal section below
already contemplates gitignoring the mount point, which concedes it can. If it
does: Step 1 collects `.env*` files, **Check 1 reads them** (`SKILL.md:75`),
and a mounted `.env` holds *live secret values*, not `op://` refs. That pulls
real secrets into the auditing agent's context — precisely what the skill's own
"never print secret values" rule exists to prevent — and Check 2 WARNs on the
file for good measure. Check 1 doesn't merely fail to praise such a project; it
FAILs it for containing plaintext secrets, which is the correct verdict for the
wrong reason.

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

- A project on a mounted Environment audits clean: no WARN from Check 2 or
  Check 3, and a positive note from Check 1.
- The audit never reads the contents of a mounted Environment `.env`. Identify
  it and move on — reading it defeats the point of the mount and puts live
  secrets in the agent's context.
- A project with neither `.env.secrets`, `op run`, nor an Environment still
  WARNs — the fix must not create a hole by making "no evidence" look like
  "uses Environments".
- `version` bumped in the skill frontmatter and in both `.claude-plugin/*.json`
  manifests.
