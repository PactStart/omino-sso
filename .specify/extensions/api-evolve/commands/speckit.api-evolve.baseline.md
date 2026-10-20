---
name: speckit.api-evolve.baseline
description: Freeze a snapshot of the current contract files before any plan-driven changes are implemented.
category: process
---

# /speckit.api-evolve.baseline

Capture an immutable snapshot of every detected contract file under `specs/<branch>/api-evolve/baseline/`. Diff and classify use this as the comparison anchor.

## Inputs

- `detected.json` produced by `/speckit.api-evolve.detect` (this command will run `detect` first if missing).

## Behavior

1. Read `detected.json`.
2. For each contract file, copy bytes verbatim into `baseline/` preserving relative path.
3. Write `baseline/manifest.json` recording each file path, format, and SHA-256 hash.
4. Refuse to overwrite an existing baseline unless `--rebaseline` is passed.

## Output

```
specs/<branch>/api-evolve/baseline/
├── manifest.json
├── openapi/users.yaml
├── graphql/schema.graphql
└── proto/billing.proto
```

`manifest.json`:

```json
{
  "captured_at": "2026-05-07T00:00:00Z",
  "files": [
    { "path": "openapi/users.yaml", "format": "openapi-3", "sha256": "ab12…" }
  ]
}
```

## Exit conditions

- `0` — baseline written.
- `1` — baseline already exists and `--rebaseline` not passed.
- `2` — runtime error.

## Notes

- The baseline is **never** auto-deleted. Use `--rebaseline` only when intentionally re-anchoring (e.g. after a major release ships).
- Hashes let downstream commands skip files that haven't changed.
