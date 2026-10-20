# API Classification — 001-api-docs

对比锚点：`baseline/internal/api/docs/openapi.json`（sha256 `e29a35aa…`，2026-10-20 冻结）
工作契约：`internal/api/docs/openapi.json`（sha256 与基线一致）

| ID | File | Change | Class | Rationale |
|----|------|--------|-------|-----------|
| — | — | 无差异 | — | 工作契约与基线逐字节一致，diff 为空 |

## Summary

- additive: 0
- non-breaking: 0
- breaking: 0
- removal: 0
- implied bump: none

## 说明

本特性是**契约的首次建立**，不是对既有契约的修改：

- 16 个端点的请求/响应契约在实施前后**未发生变化**（T009 的具名化只改 Go 侧类型，JSON 键集与语义经 `dto_test.go` 逐项断言等价）；
- `/oauth2/*`、`/.well-known/*` 由 `zitadel/oidc/v3` 生成，未改动；
- 新增的 `/docs/*` 属非业务面（不套统一响应、非 prod 不注册），按 AGENTS.md §4.0 不计入管理 API 契约。

因此 baseline 直接取自交付后的当前状态，后续特性以此为 diff 锚点。
