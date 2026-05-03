# scripts/

Local-development scaffolding: fixtures, smoke tests, install helper.
Adapt to your setup.

## Files

### `.env.example`

Fixture file mapping shell variable names to `op://` URIs, one per line:

```
CREDS=op://vault/item/field
KEYS=op://vault/item/field
```

These are **pointers, not secrets** — reading any URI still requires
biometric approval. Replace the example URIs with entries from your own
vault before running anything.

Used by:
- `env_file_test.sh` (manual smoke test)
- `internal/oprunner/integration_test.go` (looks up `CREDS` by name)

### `env_file_test.sh`

End-to-end smoke test of `--env` batch mode: reads each line of
`.env.example`, invokes `opx --env NAME=op://...` for every entry under one
biometric prompt, `eval`s the resulting `export` lines into the current
shell, and prints byte counts as a sanity check.

Run from the repo root after `make build`:

```sh
cd scripts && bash env_file_test.sh
```

You should see one biometric prompt covering every URI, then a summary
line with the lengths of each variable.

### `local_rebuild.sh`

Build, test, lint, and install `opx` into `/usr/local/bin/`. Requires
`sudo` for the move. Bypasses `make test-integration` — run that
separately if you want to hit a real vault.

## Notes

- `.env.example` is committed with example URIs that won't resolve outside
  the original author's vault — that is intentional. Each contributor
  edits it to point at their own fixtures.
- Nothing in `scripts/` is wired into `make test` or CI; everything here
  is local-only.
