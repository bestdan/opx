# Pin the direnv-1password plugin the docs tell people to install

**Surfaced by:** co-review of PR #11 — the install step cloned the repo into
`~/.config/direnv/lib/direnv-1password` and then `source`d that *directory*,
which cannot work. Fixed to fetch `1password.sh` directly.

## How it is

The best-practices doc now does:

```bash
curl -sfLo ~/.config/direnv/lib/1password.sh \
    https://raw.githubusercontent.com/tmatilai/direnv-1password/main/1password.sh
```

That works, but it pulls whatever is on `main` at the moment you run it, over
`curl | file`, with no integrity check — into a shell library that auto-loads on
every `cd` and handles secrets. Section 7 of the same document is about supply
chain hardening. This is the one command in the doc that contradicts it.

The doc does say "community-maintained plugin (not official 1Password), check
the commit history before adopting it" — which is honest, and insufficient.

## How it should be

Use direnv's own `source_url` with a sha256 pin, which is the mechanism upstream
documents for exactly this:

```bash
source_url "https://raw.githubusercontent.com/tmatilai/direnv-1password/v1.1.0/1password.sh" \
    "sha256-<hash>"
```

That pins a tag, verifies the content, and lives in the committed `.envrc` where
a reviewer sees it change. Needs the real hash for whichever tag is current, and
a note on how to re-pin on upgrade.

## Acceptance

- Section 5 of `1password-local-dev-best-practices.md` uses a pinned
  `source_url` with a real sha256 for a real tag.
- Check 6 of the audit skill WARNs on an unpinned `source_url` or a bare
  `curl`-into-`direnv/lib` install.
