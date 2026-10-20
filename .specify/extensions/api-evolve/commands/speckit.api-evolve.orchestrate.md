---
name: speckit.api-evolve.orchestrate
description: Run the full API Evolve pipeline end-to-end on the current feature branch.
category: process
---

# /speckit.api-evolve.orchestrate

Run every phase of API Evolve in order for the current feature branch. Use this once on an existing branch to backfill state, or any time you want to re-run the pipeline manually.

## Inputs

- Current feature branch (auto-detected via `git branch --show-current`).
- Project root (auto-detected via `.specify/` location).
- Optional `--from-phase <name>` to resume from a specific phase.

## Steps

1. **Resolve state** — read `specs/<branch>/api-evolve/orchestration.md` if it exists. Determine the last completed phase.
2. **Run `detect`** — write `detected.json` listing every contract file and its format.
3. **Run `baseline`** — if no baseline exists, freeze a snapshot under `api-evolve/baseline/`. Skip if baseline already exists (use `--rebaseline` to force).
4. **Run `diff`** — compare working contracts against baseline. Write `diff.json`.
5. **Run `classify`** — annotate every diff entry as `additive` / `non-breaking` / `breaking` / `removal`. Write `classification.md`.
6. **Run `bump`** — recommend the implied SemVer bump. Write `bump.json`.
7. **Run `client-impact`** — list known consumers affected per change. Write `client-impact.md`.
8. **Run `gate`** — evaluate the policy. Report PASS or the specific failing rule.
9. **Update `orchestration.md`** — record phase completion timestamps and the gate result.

## Exit conditions

- `0` — all phases ran, gate passed (or only `additive` changes detected).
- `1` — gate failed; see `orchestration.md` for the failing rule.
- `2` — runtime error (missing files, malformed contracts).

## Notes

- Idempotent — re-running with no changes is a no-op apart from refreshed timestamps.
- Honors `api-evolve/policy.yml` overrides at the project and feature level.
- Does **not** modify the working contract files; everything is read + write into `api-evolve/`.

## See also

- `/speckit.api-evolve.gate` — run only the gate.
- `/speckit.api-evolve.deprecate` — declare a deprecation (required for breaking changes).
