# API Evolve · Orchestration — `009-portal-login-redirect`

**本轮产物**：`after_plan` 钩子在 2026-10-25 执行了 `detect` 与 `baseline`：

```text
specs/009-portal-login-redirect/api-evolve/
├── detected.json                              # 1 个契约文件：internal/api/docs/openapi.json（openapi-3，78 端点 / 69 schema）
├── baseline/manifest.json                     # sha256 锚点
└── baseline/internal/api/docs/openapi.json    # 逐字节快照
```

项目根与特性目录均无 `policy.yml`，按缺省策略判定。

---

## 契约面事实

本特性**不改变**任何 `/api/v1/**` 端点、DTO 字段、错误码或审计事件枚举的**结构**：

| 变更 | 是否影响契约 |
|---|---|
| 未登录授权落点改为门户登录页（`sso :8080` 的 `/login`） | **否**。协议层端点不在 `openapi.json` 内，由 `/.well-known/openid-configuration` 描述 |
| 新增审计事件 `sso_login_success` | 否（`event_type` 为 VARCHAR 枚举，无 schema 变更；已登记 AGENTS §6.7） |
| 新增环境变量 `PORTAL_LOGIN_URL`、`VITE_SSO_BASE_URL` | 否（运维/构建配置） |
| `COOKIE_SAME_SITE` 收敛为仅 `Lax` | 否（部署配置） |
| portal 登录页新增 `app` 查询参数与白名单 | 否（浏览器路由，非 HTTP API） |
| 新增 `internal/oidc/login_flow.go` | 否（内部纯函数） |

> 因此 `openapi.json` 的 sha256 在实现完成后必须与 `baseline/manifest.json` **完全一致**。

---

## Gate — 2026-10-25（implement 前置）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 特性目录无 `changes.json` / `diff.json` / `deprecations.json`；零端点删除或收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 隐含 bump 为 `none`（契约面零变化） |
| 3 | Sunset honored — 已过 `sunset_at` 的 deprecation 仍在契约中 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present | 通过（不适用） | 无 breaking/removal，无需迁移文档 |

契约字节复核：`openapi.json` sha256 `7a5cb8d19b43…3ede41` = baseline。

---

## Gate — 2026-10-26（converge 后 implement 前置，Phase 8）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `changes.json` / `diff.json` / `deprecations.json`；Phase 8 只补验证证据与脚本注释，零端点变化 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 隐含 bump 为 `none` |
| 3 | Sunset honored — 已过 `sunset_at` 的 deprecation 仍在契约中 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present | 通过（不适用） | 无 breaking/removal |

契约字节复核：`openapi.json` sha256 `7a5cb8d19b43797b8cd5683d2587ca6fa67741b8b6474470268fc513423ede41` = baseline。
