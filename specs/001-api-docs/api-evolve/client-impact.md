# Client Impact — 001-api-docs

对比结果为空（`diff.json` 的 `entries` 为 `[]`），**无 breaking / removal 条目，故无受影响消费方**。

## 已知消费方（据 AGENTS.md §1 / PRD.md）

| 消费方 | 依赖面 | 本特性影响 |
|---|---|---|
| `web/portal`（账号中心） | `/api/v1/auth/*`、`/api/v1/me/*` | 无（仅新增文档入口） |
| `web/admin`（管理后台） | `/api/v1/admin/*` | 无 |
| 第三方接入方 | `/oauth2/*`、`/.well-known/*` | 无（协议层未改动） |

## 备注

`policy.yml` 的 `clients:` 段未建立（本特性无 breaking 变更，无需 client-impact 映射）。若后续特性出现 breaking，需要先补 `clients:` 段再跑 `/speckit.api-evolve.client-impact`。
