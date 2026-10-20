---
hook: after_specify
runs: speckit.api-evolve.detect
---

# after_specify

Runs immediately after `/speckit.specify` finishes writing `spec.md`.

## What it does

1. Invokes `/speckit.api-evolve.detect`.
2. Appends an `## API Surface` section to `spec.md` summarizing detected contracts and current versions.

## Failure mode

Soft — exits 0 even if no contracts are found. The user gets a one-line note in the spec confirming nothing was detected.

## When to disable

Set `hooks.after_specify.enabled: false` in `policy.yml` if the project intentionally has no API contracts (CLI-only tools, libraries).
