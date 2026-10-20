# Spec Kit · API Evolve

> **Managed API contract evolution for Spec-Driven Development.** Detect breaking changes before they ship, enforce semver, orchestrate deprecations, and gate `/speckit.implement` until the contract policy is satisfied. Works across **OpenAPI / Swagger, GraphQL SDL, gRPC `.proto`, and JSON Schema**.

`API Evolve` plugs into every Spec Kit lifecycle hook, so contract drift can never sneak past `/speckit.specify → /speckit.plan → /speckit.tasks → /speckit.implement`. It is the missing governance layer between a freshly written spec and a published API.

---

## What it does

A Spec describes *what* you intend to build. A contract describes *what clients can rely on*. These two drift apart constantly. API Evolve closes that gap:

1. **Detect** — find every contract file in the repo on `after_specify`.
2. **Baseline** — snapshot the current contract on `after_plan`, before anyone edits it.
3. **Diff & classify** — compare proposed contract changes against the baseline, classify each one as `additive` / `non-breaking` / `breaking` / `removal`.
4. **Bump** — recommend the SemVer bump (`major` / `minor` / `patch`) implied by the diff and refuse to silently violate semver.
5. **Deprecate** — for breaking changes, require a deprecation window, deprecation notice, sunset date, and migration guide *before* removal lands.
6. **Gate** — `before_implement` hook refuses to dispatch tasks until policy is satisfied (no undeclared breaking change, version bumped, deprecation policy met).
7. **Release** — after implementation, generate the consumer-facing changelog and a client-impact report (which endpoints/messages each known client uses).

---

## Why it's different

| Concern | Without API Evolve | With API Evolve |
|---|---|---|
| Breaking change shipped silently | Found by a client at runtime | Caught at `before_implement`, blocks the gate |
| Version bump forgotten | Manual reminder, easy to miss | Required before gate passes |
| Deprecation forgotten | "Coming soon" comment on a removed field | Deprecation policy enforced (notice, sunset, migration doc) |
| Client impact unknown | Inbox flooded with broken integrations | `client-impact` report names the affected consumers per endpoint |
| Changelog hand-written | Misses fields, drifts from reality | Generated from the actual diff |

---

## Phase model

API Evolve runs a **6-phase pipeline** on each feature branch:

```
detect → baseline → diff → classify → deprecate → bump → release
        \____ on plan ____/   \________ on tasks ________/   \_ on implement _/
```

State is stored under `specs/<branch>/api-evolve/`:

```
specs/<branch>/api-evolve/
├── detected.json          # contract files & format per file
├── baseline/              # frozen snapshot at after_plan
├── diff.json              # additive/non-breaking/breaking/removal entries
├── classification.md      # human-readable diff with rationale
├── policy.yml             # per-feature policy override (deprecation window, etc.)
├── deprecations.json      # active deprecations + sunset dates
├── bump.json              # required version bump + new version
├── client-impact.md       # which known consumer is affected by which change
└── orchestration.md       # phase status, gate result, last action
```

---

## Commands

### Orchestration

| Command | Purpose |
|---|---|
| `/speckit.api-evolve.orchestrate` | Run the full pipeline end-to-end for the current feature branch. |
| `/speckit.api-evolve.gate` | Manually run the `before_implement` gate; reports pass/fail with the failing rule. |

### Detection & baseline

| Command | Purpose |
|---|---|
| `/speckit.api-evolve.detect` | Scan the repo for contract files (OpenAPI, GraphQL, gRPC, JSON Schema); write `detected.json`. |
| `/speckit.api-evolve.baseline` | Freeze a snapshot of the detected contracts under `api-evolve/baseline/`. |

### Diff & classification

| Command | Purpose |
|---|---|
| `/speckit.api-evolve.diff` | Diff working contracts against baseline; emit structured `diff.json`. |
| `/speckit.api-evolve.classify` | Classify each diff entry as `additive` / `non-breaking` / `breaking` / `removal`; explain why. |
| `/speckit.api-evolve.client-impact` | For every breaking entry, list the known consumers/clients that depend on it. |

### Versioning & deprecation

| Command | Purpose |
|---|---|
| `/speckit.api-evolve.bump` | Recommend the SemVer bump implied by the classified diff. Writes `bump.json`. |
| `/speckit.api-evolve.deprecate` | Open a deprecation entry: notice text, sunset date, migration steps. Required for breaking changes. |
| `/speckit.api-evolve.deprecation-status` | Show every active deprecation, its sunset date, and the endpoints still using it. |

### Release

| Command | Purpose |
|---|---|
| `/speckit.api-evolve.changelog` | Generate the consumer-facing changelog entry from the final classified diff. |
| `/speckit.api-evolve.release` | Bump the version, update contract `info.version` (or equivalent), tag the release artifact. |

---

## Lifecycle hooks

| Hook | What runs | Side effects |
|---|---|---|
| `after_specify` | `detect` | Annotates `spec.md` with contract surface + current version. |
| `after_plan` | `baseline` | Writes immutable snapshot under `api-evolve/baseline/`. |
| `after_tasks` | Injects `[API-*]` tasks for diff/classify/bump/deprecate based on planned changes. | Adds dispatch trailers prefixed `speckit.api-evolve.*`. |
| `before_implement` | `gate` | **Blocks** dispatch on policy failure. |
| `after_implement` | `diff`, `changelog`, `client-impact` | Writes final changelog and the client-impact report. |

The gate refuses to pass when **any** of these is true:

1. There is a `breaking` or `removal` entry without a matching deprecation in `deprecations.json`.
2. The implied SemVer bump is `major` but `bump.json` has not been confirmed.
3. A deprecation has passed its sunset date but the deprecated entry is still present in the working contract.
4. A `removal` is not preceded by a deprecation that has already been published in a prior release.

Each rule can be overridden per-feature in `api-evolve/policy.yml` if a maintainer signs off.

---

## Supported contract formats

- **OpenAPI 3.x / Swagger 2.0** — `*.yaml`, `*.yml`, `*.json`
- **GraphQL SDL** — `*.graphql`, `*.gql`
- **gRPC / Protobuf** — `*.proto`
- **JSON Schema** — `*.schema.json`, `*.schema.yaml`

The detector is content-sniffing, so unconventional file names still work as long as the file is a valid document of one of the formats above.

---

## Installation

```bash
specify extension add api-evolve --from https://github.com/Quratulain-bilal/spec-kit-api-evolve/archive/refs/tags/v1.0.0.zip
```

After install, the five lifecycle hooks become active automatically. Run `/speckit.api-evolve.orchestrate` once on an existing feature branch to backfill state.

---

## Configuration

Create `api-evolve/policy.yml` in the project root or per-feature:

```yaml
# Deprecation policy
deprecation:
  required_notice_days: 90       # min days between deprecation and sunset
  required_migration_doc: true   # migration steps must be present

# Versioning policy
versioning:
  scheme: semver                 # semver | calver
  enforce_bump: true             # gate fails when bump is missing
  allow_unstable_breaking: true  # contracts with version 0.x.y allow breaking on minor

# Gate overrides
gate:
  allow_undeclared_additive: true
  allow_undeclared_breaking: false
  fail_on_sunset_violation: true

# Known consumers (drives client-impact reports)
clients:
  - name: web-app
    endpoints: ["/api/v1/users", "/api/v1/auth/*"]
  - name: mobile-android
    endpoints: ["/api/v1/feed", "/api/v1/auth/*"]
```

---

## Example

```bash
# 1. Write a spec that touches /api/v1/users
/speckit.specify Remove the deprecated `legacy_id` field from the user object and add `display_name`.

# 2. Plan the change
/speckit.plan Update OpenAPI spec, schema validators, and persistence layer.

# 3. API Evolve auto-snapshots the contract baseline (after_plan hook)

# 4. Tasks emitted include API-* entries:
#   - [API-1] speckit.api-evolve.diff
#   - [API-2] speckit.api-evolve.classify
#   - [API-3] speckit.api-evolve.deprecate legacy_id
#   - [API-4] speckit.api-evolve.bump

# 5. before_implement runs the gate.
#    legacy_id removal without a published deprecation → BLOCKED.

# 6. After publishing the deprecation in a prior release, the gate passes.
/speckit.implement
```

---

## License

MIT — see [LICENSE](./LICENSE).
