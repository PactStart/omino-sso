# API Evolve · Orchestration — `007-deployment`

**本轮产物**：`after_plan` 钩子在 2026-10-23 执行了 `detect` 与 `baseline`，产出：

```
specs/007-deployment/api-evolve/
├── detected.json                              # 1 个契约文件：internal/api/docs/openapi.json（openapi-3，78 端点 / 69 schema）
├── baseline/manifest.json                     # sha256 锚点
└── baseline/internal/api/docs/openapi.json    # 逐字节快照
```

项目根与特性目录均无 `policy.yml`，按缺省策略判定。

---

## 契约面事实

本特性**不改变**任何 `/api/v1/**` 端点、DTO 字段、错误码或审计事件枚举：

| 变更 | 是否影响契约 |
|---|---|
| 新增 `GET /healthz`、`GET /readyz`（`8080` + `8081`） | **否**。探针不套统一响应、不加 swag 注解，`openapi.json` 零变化（plan D1 / contracts/health-probes.md §5） |
| 新增环境变量 `TRUSTED_PROXIES` | 否（运维配置，非接口） |
| `COOKIE_DOMAIN` 语义收紧（必须 `.example.com`） | 否（部署配置；prod 已强制非空） |
| admin 前端路由去 `/admin` 前缀 | 否（浏览器路由，非 HTTP API） |
| 四域名拓扑 / nginx 反代 / CORS 不引入 | 否（网络边界） |
| 新增 `deploy/`、`web/nginx/`、`web/Dockerfile` | 否（部署产物） |

> 因此 `openapi.json` 的 sha256 在实现完成后必须与 `baseline/manifest.json` **完全一致**；若不等，说明探针被误加进 swagger 注解，属实现偏离。

---

## Gate — 2026-10-23（plan 阶段，预检）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `diff.json`；零端点删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 隐含 bump 为 `none`（契约面零变化） |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但 `published_in_version` 为 null | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml`；本特性零表结构变更 |

Exit code: `0`

### 备注

- 唯一需要盯住的漂移点：`internal/api/docs/openapi.json` 必须保持字节不变（探针不进文档）。`make docs-check` 是这条的自动守护。
- 实现完成后应复跑本 Gate，并在 `tasks.md` 的收口任务中登记「sha256 与 baseline 一致」。

---

## Gate — 2026-10-23T17:45:22Z（implement 前置复跑）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 特性目录无 `diff.json`、无 `deprecations.json`；本轮零端点删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 无 `bump.json`；契约面零变化，隐含 bump = `none` |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但 `published_in_version` 为 null | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 项目根与特性目录均无 `policy.yml` |

Exit code: `0`

**契约字节复核**：`internal/api/docs/openapi.json` 的 sha256 = `7a5cb8d1…3ede41`，与 `baseline/manifest.json` 一致（探针未进 swagger 注解）。

## Gate — 2026-10-23T18:07:51Z（Phase 8 Convergence 实现前复跑）

Result: **PASS**；五条规则同上一轮（无 `diff.json`/`bump.json`/`deprecations.json`/`policy.yml`，零端点变化，隐含 bump = `none`）。
契约字节复核：`openapi.json` sha256 `7a5cb8d1…3ede41` = baseline。
