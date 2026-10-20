# Contract: 文档路由（`/docs/*`）

**Feature**: `001-api-docs` | **端口**: `:8081`（管理 API，Gin） | **Date**: 2026-10-20

本契约描述**本特性自身新增的对外接口**。16 个业务端点的契约由生成的 `openapi.json` 承载（见 [annotation-conventions.md](./annotation-conventions.md)），不在此重复。

---

## 1. 路由清单

| 方法 | 路径 | 用途 | 响应 Content-Type |
|---|---|---|---|
| GET | `/docs/index.html` | 文档页 HTML | `text/html; charset=utf-8` |
| GET | `/docs/` | 同 `index.html`（尾斜杠归一） | `text/html; charset=utf-8` |
| GET | `/docs/openapi.json` | OpenAPI 3.0.3 描述（运行时注入环境与版本） | `application/json; charset=utf-8` |
| GET | `/docs/*any` | swagger-ui 静态资源（JS / CSS / 字体），来自内嵌文件系统 | 按扩展名 |

> 路由注册点：`internal/api/router.go`，位于 `v1` 分组**之外**（路径不含 `/api/v1` 前缀）。

## 2. 环境行为矩阵

| 环境（`cfg.AppEnv`） | `/docs/index.html` | `/docs/openapi.json` | `/docs/<静态资源>` |
|---|---|---|---|
| `dev` / `test` / 其它非 `prod` | 200 HTML | 200 JSON | 200 资源字节 |
| `prod` | 404 + 统一错误体 | 404 + 统一错误体 | 404 + 统一错误体 |

**prod 的判定要求（FR-016 / SC-004）**

- 路由**不被注册**，请求落入既有 `r.NoRoute` 分支。
- 响应体必须与该实例上任意不存在路径（如 `/docs-not-exist`）的响应**完全一致**（唯一允许的差异是每次新生成的 `traceId`）：

```json
{"code":"NOT_FOUND","message":"资源不存在","data":null,"traceId":"<ULID>"}
```

  仅 `traceId` 允许不同（每次请求新生成）。
- 不得返回 403、不得返回"文档已禁用"之类的提示。

## 3. 鉴权与审计

| 项 | 约定 |
|---|---|
| 鉴权 | `/docs/*` 不经过 `middleware.Auth`、不要求 `sid` cookie（FR-015） |
| 限流 | **不挂任何限流器**：既有全局 `RateLimit(api)`（`router.go:38` 的 `r.Use`）须按 `/docs/` 前缀跳过。理由：swagger-ui 首屏约 10+ 个静态资源请求，占额度且容易在演示时触发 429；更重要的是限流依赖 Redis，若 fail-closed，则运维故障时恰恰打不开文档页（critique E2/E3） |
| 审计 | 不产生认证审计事件（`audit_events` 无新增记录）——文档页加载属运维行为，非业务操作 |
| 统一响应 | **不适用**。`/docs/*` 返回原始 HTML / JSON / 静态字节，不套 `{code,message,data,traceId}`（与 `/oauth2/*` 同理，属非业务面） |

## 4. `/docs/openapi.json` 的响应结构要求

顶层必须满足 OpenAPI 3.0.3：

```json
{
  "openapi": "3.0.3",
  "info": {
    "title": "omino-sso 管理 API",
    "version": "<构建版本，如 v0.1.0 或 dev>",
    "description": "...环境：dev | 服务：omino-sso...",
    "contact": { "...": "..." }
  },
  "servers": [{ "url": "/" }],
  "tags": [{ "name": "认证" }, { "...": "..." }],
  "paths": { "/api/v1/...": { "get": { "...": "..." } } },
  "components": { "schemas": { "...": "..." }, "securitySchemes": { "...": "..." } }
}
```

| 字段 | 约束 | 依据 |
|---|---|---|
| `openapi` | 固定 `3.0.3` | FR-021 |
| `info.version` | 等于构建注入的版本；`go run` 下回退 `dev` 或 VCS 短哈希 | FR-019、research D8 |
| `info.description` | 必含运行环境标识（`APP_ENV` 实际值），并可含 `servers` 实际 host 提示 | FR-019 |
| `paths` 的 `(method,path)` 组合数 | 恒为 16 | INV-1、SC-001 |
| `components.securitySchemes.cookieAuth` | 必须存在，类型为 `apiKey`，`in: cookie`，`name: sid` | INV-12 |

## 5. 失败与边界

| 场景 | 期望行为 |
|---|---|
| prod 下访问任一 `/docs/*` 路径 | 404，响应体与非存在路径同构 |
| 非 prod 下请求 `/docs/nonexistent.js` | 由静态文件系统返回 404（非统一响应体亦可），不得 500 |
| 内嵌资源缺失（构建问题时） | 500 + 明确日志；不得 panic 导致整个进程退出 |
| 无外网环境访问文档页 | 页面与所有 JS/CSS/字体均从内嵌文件系统加载，浏览器 Network 面板无外部域请求 |
| 重复 `make docs` 无源码变化 | 产物逐字节不变（INV-2），`make docs-check` 返回 0 |

## 6. 测试锚点

| 编号 | 断言 |
|---|---|
| CT-1 | 非 prod：`GET /docs/index.html` → 200、`text/html`、body 含 `swagger` |
| CT-2 | 非 prod：`GET /docs/openapi.json` → 200、`openapi == "3.0.3"`、`len(paths 展开组合) == 16` |
| CT-3 | 非 prod：`GET /docs/openapi.json` 的 `info.description` 含当前 `APP_ENV` 值 |
| CT-4 | prod：`GET /docs/index.html` 与 `GET /docs-absent` 的 body（除 `traceId`）完全一致，均为 `NOT_FOUND` |
| CT-5 | prod：`GET /docs/openapi.json` → 404，且不含任何 openapi 字段 |
| CT-6 | 非 prod：文档页响应头不含 `Set-Cookie` |
| CT-7 | 停掉 Redis 后，非 prod 的 `GET /docs/index.html` 与 `/docs/openapi.json` 仍返回 200（文档页不依赖限流器的存储） |
