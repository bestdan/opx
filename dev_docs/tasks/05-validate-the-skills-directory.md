# Add a check over `skills/`

**Surfaced by:** co-review of PR #11. Every finding in the skills was found by
reading prose. Nothing mechanical checks that directory at all.

## How it is

`AGENTS.md` says `skills/` is "documentation, not code — no Go build or test
touches it", then lists three things that must stay consistent by hand:

- `opx` replaces `op read` only (`op run` / `op inject` stay)
- the skills must not claim `opx` takes `--account`
- `version` must be bumped in the frontmatter *and* both `.claude-plugin/`
  manifests when content changes materially

All three are grep-able. None are grepped. The co-review also turned up two
classes the invariants don't cover: internal contradictions between a skill's
Rules section and the commands the same skill emits (the `--account everywhere`
absolutism), and cross-document contradictions (`onboard` claiming Touch ID per
command while the shared reference describes a cached window).

## How it should be

A `scripts/check-skills.sh` wired into `make lint`, asserting:

- no `op read` in `skills/` outside a "don't do this" context
- no `opx --account` anywhere
- every relative link in a skill resolves to a file that exists
- frontmatter parses, and `name` matches the directory name
- `version` in all four `SKILL.md` files and both manifests are each valid
  semver (not that they match each other — plugin and skill versions bump on
  different triggers)

The contradiction classes can't be caught by grep. What can be done cheaply is
to state the exceptions once — the `--account` carve-outs are now duplicated in
three Rules sections and will drift again — and have the skills reference a
single shared statement in the best-practices doc instead.

## Acceptance

- `make lint` runs the skills check.
- The `--account` rule lives in one place, referenced by all four skills.
