# Changelog

## [1.0.0] - 2026-05-07

### Added
- Initial release of API Evolve — managed API contract evolution for Spec Kit.
- 12 commands covering detect, baseline, diff, classify, bump, deprecate, gate, changelog, client-impact, release.
- 5 lifecycle hooks: `after_specify`, `after_plan`, `after_tasks`, `before_implement`, `after_implement`.
- Multi-format support: OpenAPI 3.x, Swagger 2.0, GraphQL SDL, gRPC `.proto`, JSON Schema.
- Per-feature policy override via `api-evolve/policy.yml`.
- Gate enforcement: blocks `/speckit.implement` on undeclared breaking changes, missing version bumps, and unmet deprecation policy.
