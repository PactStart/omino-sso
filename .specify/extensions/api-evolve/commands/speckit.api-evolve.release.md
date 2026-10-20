---
name: speckit.api-evolve.release
description: Apply the bump, mark deprecations as published, and tag the release artifact.
category: process
---

# /speckit.api-evolve.release

Finalize the contract evolution for this feature.

## Steps

1. Read `bump.json`. Refuse to proceed if `confirmed: false`.
2. For each contract file, update its embedded version field:
   - OpenAPI / Swagger: `info.version`
   - GraphQL SDL: header comment `# version: x.y.z` (created if missing)
   - gRPC: `package` comment line
3. For every entry in `deprecations.json` with `published_in_version: null`, set it to the new version.
4. Write/append `CHANGELOG.md` via `/speckit.api-evolve.changelog`.
5. Optionally tag `v<new_version>` if `--tag` is passed (delegates to `git tag`).

## Flags

- `--tag` — also create a git tag.
- `--dry-run` — print the diff of what would change without writing.

## Exit conditions

- `0` — release applied.
- `1` — bump not confirmed, or gate has not yet passed.
- `2` — runtime error.

## Notes

- Designed to run inside the `after_implement` hook *after* the gate has passed and code has merged.
- Idempotent — re-running on an already-released version is a no-op.
