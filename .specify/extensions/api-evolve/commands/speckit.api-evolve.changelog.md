---
name: speckit.api-evolve.changelog
description: Generate the consumer-facing changelog entry from the final classified diff.
category: docs
---

# /speckit.api-evolve.changelog

Render a Keep-a-Changelog-style entry from `classification.md` and `bump.json`.

## Output

Appends to (or creates) `CHANGELOG.md` at the project root:

```markdown
## [2.0.0] - 2026-05-07

### Added
- `User.display_name` (optional string).

### Removed
- `User.legacy_id` — deprecated since 2026-02-07, sunset 2026-05-07.
  Migration: see `docs/migrations/legacy_id.md`.

### Migration notes
- Existing clients using `legacy_id` must switch to `id` before upgrading.
```

## Inputs

- `classification.md` — entries grouped by class.
- `bump.json` — version, date.
- `deprecations.json` — sunset and migration links for `removal` entries.

## Flags

- `--dry-run` — print the entry to stdout without modifying `CHANGELOG.md`.
- `--section <H2>` — write under a specific heading (default: contract title).

## Exit conditions

- `0` — changelog written or printed.
- `2` — runtime error.
