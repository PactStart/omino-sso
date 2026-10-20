---
hook: after_implement
runs: speckit.api-evolve.diff, speckit.api-evolve.changelog, speckit.api-evolve.client-impact, speckit.api-evolve.release
---

# after_implement

Runs after `/speckit.implement` completes. Closes the loop on the contract evolution.

## What it does

1. Re-runs `/speckit.api-evolve.diff` against the post-implementation state to confirm the actual change matches the plan.
2. Generates the consumer-facing changelog entry via `/speckit.api-evolve.changelog`.
3. Refreshes `client-impact.md` with the final shape.
4. If `policy.versioning.auto_release: true`, runs `/speckit.api-evolve.release --tag` to apply the version bump and tag the release.

## Failure mode

Soft for steps 1–3 — they are reporting only. Hard for step 4 if the bump cannot be applied.

## When to disable

Set `hooks.after_implement.enabled: false` to keep release/tagging fully manual.
