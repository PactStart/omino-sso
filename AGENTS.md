# omino-sso 项目规范

> 本文件是项目的**唯一权威规范**，所有开发会话（人类或 AI）必须先读本文件再动手。
> 与本文件冲突的实现一律视为缺陷；确需变更规范时，先改本文件并写明理由，再改代码。
> 配套文档：`技术选型报告.md`（背景与决策依据，不作为实现约束）。

---

## 1. 项目概述

| 项 | 内容 |
|---|---|
| 项目名 | `omino-sso` |
| 一句话描述 | 面向企业内部的统一身份提供方（IdP），基于 OIDC/OAuth 2.1 为内部系统提供单点登录、单点登出与集中账号管理。 |
| 目标用户 | ① 企业 IT / 运维管理员（管理用户、组织、应用与审计）② 全体员工（登录、改密、绑定 MFA、查看授权记录）③ 接入方研发（按 OIDC 标准接入，无需了解内部实现） |

**核心功能（仅此三项，不多做）**

1. **统一认证**：账号密码 + TOTP MFA，签发标准 OIDC 令牌（authorization code + PKCE，仅 S256）。
2. **单点登录 / 单点登出**：一次登录贯通所有接入系统；登出即时失效服务端会话。
3. **身份与应用管理**：用户、组织架构、OAuth client 的增删改查，以及全量认证审计日志。

---

## 2. 技术栈（锁定版本）

版本一经锁定不得随意升级；升级需单独提交并说明回归验证范围。初始基线如下，实际以 `go.mod` / `package.json` 为准。

### 2.1 后端

| 组件 | 模块 | 锁定版本 | 用途 |
|---|---|---|---|
| 语言 | Go | `1.24` | `go.mod` 的 `go` 指令 |
| HTTP 框架 | `github.com/gin-gonic/gin` | `v1.10.0` | 仅管理 API（`:8081`） |
| DI | `go.uber.org/fx` | `v1.23.0` | 生命周期与装配 |
| ORM | `gorm.io/gorm` | `v1.25.12` | 管理类 CRUD |
| MySQL 驱动 | `gorm.io/driver/mysql` | `v1.5.7` | |
| OIDC Provider | `github.com/zitadel/oidc/v3` | `v3.28.0` | 协议层（OP），**唯一实现来源** |
| JOSE | `github.com/go-jose/go-jose/v4` | `v4.0.4` | JWS / JWKS，禁止使用 v3 |
| 密码哈希 | `golang.org/x/crypto` | `v0.31.0` | `argon2.IDKey` |
| TOTP | `github.com/pquerna/otp` | `v1.4.0` | MFA |
| Redis | `github.com/redis/go-redis/v9` | `v9.7.0` | session / 限流 / 缓存 |
| ID | `github.com/oklog/ulid/v2` | `v2.1.0` | 主键 |
| 校验 | `github.com/go-playground/validator/v10` | `v10.23.0` | 请求校验 |
| 测试 | `github.com/stretchr/testify` | `v1.10.0` | |
| 日志 | `log/slog` | 标准库 | JSON handler |

### 2.2 前端

| 组件 | 模块 | 锁定版本 | 用途 |
|---|---|---|---|
| 框架 | `vue` | `3.5.13` | |
| 构建 | `vite` | `6.3.5` | |
| 语言 | `typescript` | `5.7.2` | `strict: true` |
| 路由 | `vue-router` | `4.5.0` | |
| 状态 | `pinia` | `2.3.0` | |
| 请求 | `axios` | `1.7.9` | `withCredentials: true` |
| 样式（portal） | `tailwindcss` | `4.1.0` | 登录页 / 门户 |
| 组件库（admin） | `element-plus` | `2.9.1` | 表格 / 树 / 表单 |
| 图表 | `echarts` + `vue-echarts` | `5.5.1` / `7.0.3` | 审计统计 |

### 2.3 基础设施

| 组件 | 版本 | 说明 |
|---|---|---|
| MySQL | `8.4` LTS | 字符集 `utf8mb4`，排序规则按 §6 规定 |
| Redis | `7.4` | 单实例 + AOF，不使用 Cluster |
| Docker / Compose | `27.x` / `v2` | 本地与 MVP 部署 |

### 2.4 接口文档生成链（构建期依赖）

**不进入请求路径**，仅在 `make docs` / `make docs-check` 时使用；四个依赖均为 MIT 许可。版本为 `go.mod` 实际锁定值，升级需按本文件顶部约定单独提交并说明回归范围。

| 组件 | 模块 | 锁定版本 | 许可证 | 用途 |
|---|---|---|---|---|
| 注解解析 | `github.com/swaggo/swag` | `v1.16.6` | MIT | 从 handler 注释生成 Swagger 2.0（**该库不产出 OpenAPI 3**，见下） |
| 规范转换 | `github.com/getkin/kin-openapi` | `v0.135.0` | MIT | `openapi2conv.ToV3` 做 2.0→3.0 转换、`openapi3` 做漂移比对 |
| 文档 UI（接入） | `github.com/swaggo/gin-swagger` | `v1.6.1` | MIT | 在 Gin 上挂载文档页 |
| 文档 UI（资源） | `github.com/swaggo/files` | `v1.0.1` | MIT | swagger-ui 静态资源，内嵌进二进制，不依赖 CDN |

**锁定理由（不得随意改动）**

- `kin-openapi` 停在 `v0.135.0`：`v0.136.0` 起的版本要求 `go >= 1.25`，与本项目 `go 1.24.1` 不兼容。升级此处必然连带升级 Go 版本，属独立议题。
- `swaggo/swag` 仅产出 Swagger 2.0（其 OpenAPI 3 支持自 2019 年起一直是未合入的提案），因此**转换步骤不可省略**，禁止为了"少一个依赖"而直接交付 2.0。
- swag CLI 通过 `tools/tools.go`（`//go:build tools`）纳入 `go.mod`，命令行调用一律写 `go run github.com/swaggo/swag/cmd/swag`，**禁止写 `@version`**（会绕过 `go.sum` 锁定并要求联网）。

---

## 3. 目录结构

```
omino-sso/
├── AGENTS.md                          # 本文件
├── 技术选型报告.md
├── go.mod
├── Makefile
├── docker-compose.yml                 # mysql + redis + sso + portal + admin
├── .env.example                       # 环境变量模板（§9）
├── .tmp/                              # 文档生成中间产物（gitignore，不入库）
├── tools/                             # 构建期工具，禁止被业务包 import
│   ├── tools.go                       # //go:build tools，固定 swag CLI 版本
│   └── openapi3/
│       ├── main.go                    # Swagger 2.0 → OpenAPI 3.0.3 转换 + -check 漂移比对
│       └── main_test.go
├── cmd/
│   └── sso/
│       └── main.go                    # fx.New 入口，仅装配，无业务
├── internal/
│   ├── config/
│   │   ├── config.go                  # Config 结构体 + 加载/校验
│   │   └── config_test.go
│   ├── oidc/
│   │   ├── module.go                  # fx.Module("oidc")
│   │   ├── provider.go                # newProvider: op.NewProvider 包装（变长参数）
│   │   ├── storage.go                 # op.Storage 完整实现（唯一适配点）
│   │   ├── login.go                   # 登录闭环：AuthRequest 回写 sub/amr/auth_time
│   │   ├── claims.go                  # SetUserinfoFromScopes / GetPrivateClaimsFromScopes
│   │   ├── keys.go                    # 签名密钥加载与 kid 轮换
│   │   └── server.go                  # 独立 http.Server(:8080)，挂 op.Provider
│   ├── api/
│   │   ├── module.go                  # fx.Module("api")
│   │   ├── router.go                  # Gin 路由注册 + 中间件顺序
│   │   ├── middleware/
│   │   │   ├── auth.go                # 从 cookie sid 解析会话
│   │   │   ├── ratelimit.go           # Redis 令牌桶
│   │   │   ├── trace.go               # traceId 生成与透传
│   │   │   └── recover.go
│   │   ├── response/
│   │   │   ├── response.go            # 统一响应封装（§5）
│   │   │   └── errors.go              # 错误码枚举与 HTTP 映射
│   │   ├── docs/                      # 接口文档（非 prod 可达；§4.0 豁免统一响应）
│   │   │   ├── docs.go                # //go:embed 产物 + 运行时注入环境/版本 + gin handler
│   │   │   ├── openapi.json           # 生成物，入库（OpenAPI 3.0.3）
│   │   │   └── docs_test.go
│   │   ├── auth/                      # /api/v1/auth/*
│   │   │   ├── login.go               # 账号密码登录、失败计数、锁定
│   │   │   ├── mfa.go                 # TOTP 校验、绑定/确认
│   │   │   └── logout.go
│   │   ├── me/                        # /api/v1/me/*
│   │   │   ├── profile.go
│   │   │   ├── password.go
│   │   │   ├── apps.go
│   │   │   └── sessions.go
│   │   └── admin/                     # /api/v1/admin/*
│   │       ├── users.go
│   │       ├── orgs.go
│   │       ├── clients.go
│   │       └── audit.go
│   ├── service/                       # 业务规则层（不依赖 gin/gorm）
│   │   ├── user.go
│   │   ├── client.go
│   │   ├── session.go
│   │   └── audit.go
│   ├── store/
│   │   ├── module.go                  # fx.Module("store")，Provide 接口绑定
│   │   ├── db.go                      # gorm.Open + 连接池 + 健康检查
│   │   ├── tx.go                      # tx-in-context（§7 约束 3）
│   │   ├── model/
│   │   │   ├── user.go
│   │   │   ├── organization.go
│   │   │   ├── client.go
│   │   │   ├── auth_code.go
│   │   │   ├── refresh_token.go
│   │   │   ├── session.go
│   │   │   ├── consent.go
│   │   │   └── audit_event.go
│   │   ├── user_store.go
│   │   ├── client_store.go
│   │   ├── code_store.go              # 原子 UPDATE 消费授权码
│   │   ├── token_store.go             # 刷新轮换 + family 吊销
│   │   ├── session_store.go           # Redis
│   │   └── audit_store.go
│   └── pkg/
│       ├── crypto/
│       │   ├── password.go            # argon2id 编码/校验（常量时间）
│       │   ├── random.go              # 256bit 随机串
│       │   └── hash.go                # SHA-256 -> []byte(32)
│       ├── id/
│       │   └── ulid.go
│       ├── clock/
│       │   └── clock.go               # 接口化，便于测试
│       └── log/
│           └── log.go
├── migrations/
│   ├── 0001_init.up.sql
│   ├── 0001_init.down.sql
│   └── README.md                      # 手写 SQL，禁止 AutoMigrate
└── web/
    ├── portal/                        # 登录页 + 账号中心（Tailwind）
    │   ├── index.html
    │   ├── vite.config.ts
    │   ├── src/main.ts
    │   ├── src/router.ts
    │   ├── src/api/client.ts
    │   ├── src/views/Login.vue        # 账号密码 + 图形验证码 + MFA 二步
    │   ├── src/views/Account.vue
    │   ├── src/views/Apps.vue
    │   └── src/styles/tokens.css      # 品牌变量（主色 / 圆角 / 字体）
    └── admin/                         # 管理后台（Element Plus）
        ├── index.html
        ├── vite.config.ts
        ├── src/main.ts
        ├── src/router.ts
        ├── src/layout/AppLayout.vue
        ├── src/views/Users.vue        # 左组织树 + 右用户表
        ├── src/views/Clients.vue
        ├── src/views/AuditLogs.vue
        └── src/views/Settings.vue
```

**分层规则**：`api → service → store`，禁止跨层调用（api 不得直接用 store）；`oidc` 只能通过 `service`/`store` 访问数据，不得直接写 SQL。

---

## 4. API 接口定义

### 4.0 端口与路径划分

| 服务 | 地址 | 路径前缀 | 响应格式 |
|---|---|---|---|
| IdP（`net/http`） | `:8080` | `/oauth2/*`、`/.well-known/*`、`/login`、`/logout` | **RFC 原生格式**，禁止套统一响应 |
| 管理 API（Gin） | `:8081` | `/api/v1/*` | 统一响应（§5） |
| 接口文档（Gin，仅非 prod） | `:8081` | `/docs/*` | **原始 HTML / JSON / 静态字节**，禁止套统一响应 |

> `/oauth2/*` 与 `/.well-known/*` 的报文由 `zitadel/oidc/v3` 生成，**不得包装、不得改写字段名**。

**`/docs/*` 的三条硬性约定**

1. **豁免统一响应**：文档页与 `openapi.json` 必须返回浏览器与 OpenAPI 工具链可直接消费的原始内容；套 `{code,message,data,traceId}` 会使 swagger-ui 与任何规范消费方无法解析。属与 `/oauth2/*` 同类的"非业务面"豁免。
2. **仅非 prod 注册**：`APP_ENV=prod` 时该路由**不得注册**，请求须落入 `NoRoute` 分支，响应与任意不存在路径完全一致（不得返回 403 或"已禁用"提示）。环境判定只在 `internal/api/router.go` 一处。
3. **不挂限流**：`/docs/*` 必须跳过全局限流中间件，使文档页不依赖 Redis 可用性（运维故障时正是最需要查文档的时刻）。

### 4.1 接口总览

| 方法 | 路径 | 说明 | 鉴权 |
|---|---|---|---|
| GET | `/oauth2/authorize` | 授权端点 | session |
| POST | `/oauth2/token` | 令牌端点（code / refresh_token） | client |
| GET | `/oauth2/userinfo` | 用户信息 | Bearer |
| GET | `/oauth2/jwks` | 公钥集 | 无 |
| POST | `/oauth2/revoke` | 撤销令牌 | client |
| POST | `/oauth2/introspect` | 令牌自省 | client |
| GET | `/oauth2/end_session` | 登出 | session |
| GET | `/.well-known/openid-configuration` | 元数据 | 无 |
| POST | `/api/v1/auth/login` | 账号密码登录 | 无 |
| POST | `/api/v1/auth/mfa/verify` | MFA 二次校验 | mfaTicket |
| POST | `/api/v1/auth/logout` | 登出 | session |
| GET | `/api/v1/me` | 当前用户 | session |
| GET | `/api/v1/me/apps` | 我可访问的应用 | session |
| POST | `/api/v1/me/password` | 修改密码 | session |
| GET | `/api/v1/me/sessions` | 登录设备列表 | session |
| DELETE | `/api/v1/me/sessions/:sid` | 撤销会话 | session |
| GET | `/api/v1/admin/users` | 用户列表 | admin |
| POST | `/api/v1/admin/users` | 创建用户 | admin |
| PATCH | `/api/v1/admin/users/:id` | 更新用户 | admin |
| GET | `/api/v1/admin/orgs/tree` | 组织树 | admin |
| GET | `/api/v1/admin/clients` | 应用列表 | admin |
| POST | `/api/v1/admin/clients` | 注册应用 | admin |
| POST | `/api/v1/admin/clients/:id/secret` | 轮换密钥 | admin |
| GET | `/api/v1/admin/audit-logs` | 审计日志 | admin |

---

### 4.2 `POST /oauth2/token`（核心接口 1）

`Content-Type: application/x-www-form-urlencoded`，**不接受 JSON**。
客户端认证：`Authorization: Basic base64(client_id:client_secret)`，或表单内 `client_id` + `client_secret`。

**请求字段（grant_type=authorization_code）**

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `grant_type` | string | 是 | 固定 `authorization_code` |
| `code` | string | 是 | 授权码明文，服务端按 SHA-256 比对 |
| `redirect_uri` | string | 是 | 必须与授权请求中的值**完全一致** |
| `client_id` | string | 是 | 大小写敏感精确匹配 |
| `code_verifier` | string | 是 | 43–128 字符，`[A-Za-z0-9-._~]`，S256 校验 |
| `client_secret` | string | 否 | 与 Basic 二选一 |

**请求字段（grant_type=refresh_token）**

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `grant_type` | string | 是 | 固定 `refresh_token` |
| `refresh_token` | string | 是 | 旧 token；使用后立即失效并签发新 token |
| `client_id` | string | 是 | |
| `scope` | string | 否 | 只能是原 scope 的子集 |

**响应 200**

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IjIwMjYtMDEifQ...",
  "token_type": "Bearer",
  "expires_in": 900,
  "refresh_token": "rt_9f2b8c1e4d7a...",
  "scope": "openid profile email",
  "id_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IjIwMjYtMDEifQ..."
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `access_token` | string | JWT，RS256，含 `iss/aud/exp/iat/sub/scope/tenant` |
| `token_type` | string | 固定 `Bearer` |
| `expires_in` | number | 秒，默认 900 |
| `refresh_token` | string | 不透明串，仅 `authorization_code` 与首次 `refresh_token` 返回 |
| `scope` | string | 空格分隔，实际授予的 scope |
| `id_token` | string | 仅当 scope 含 `openid` 时返回；含 `nonce`、`auth_time` |

**错误响应（RFC 6749 §5.2，无统一包装）**

```json
{
  "error": "invalid_grant",
  "error_description": "authorization code is expired or already used"
}
```

| HTTP | `error` | 触发条件 |
|---|---|---|
| 400 | `invalid_request` | 缺参、参数重复、Content-Type 非 form |
| 400 | `invalid_grant` | code 过期/已用/redirect_uri 不匹配/code_verifier 校验失败/refresh 重放 |
| 400 | `unsupported_grant_type` | grant_type 非白名单 |
| 400 | `invalid_scope` | scope 越权 |
| 401 | `invalid_client` | 客户端认证失败（`WWW-Authenticate: Basic`） |

---

### 4.3 `POST /api/v1/auth/login`（核心接口 2）

**请求体**

```json
{
  "username": "zhangsan",
  "password": "S3cure!pass",
  "captchaId": "01J9ZK2Q3W5X7Y9A1B3C5D7E9F",
  "captchaCode": "8f3k",
  "clientId": "portal",
  "rememberMe": false
}
```

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `username` | string | 是 | 1–64，trim 后非空，大小写敏感 |
| `password` | string | 是 | 8–128 |
| `captchaId` | string | 否 | 连续失败 ≥ 3 次后必填 |
| `captchaCode` | string | 否 | 4 位，不区分大小写 |
| `clientId` | string | 是 | 默认 `portal` |
| `rememberMe` | bool | 否 | true 时会话 7 天，否则 8 小时 |

**响应 200（需二次 MFA）**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "mfaRequired": true,
    "mfaTicket": "mft_5c1e9a7b3d2f",
    "expiresIn": 300
  },
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

**响应 200（登录完成）**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "mfaRequired": false,
    "mfaTicket": null,
    "expiresIn": 0,
    "redirectTo": "/account",
    "user": {
      "id": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
      "username": "zhangsan",
      "displayName": "张三"
    }
  },
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

> 成功后由服务端下发 `Set-Cookie: sid=<opaque>; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=...`。**响应体任何时候都不包含 access_token / refresh_token。**

**错误码**

| HTTP | code | 场景 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 字段缺失或格式错误 |
| 401 | `AUTH_INVALID_CREDENTIALS` | 用户名或密码错误（不区分是哪个错） |
| 400 | `AUTH_CAPTCHA_INVALID` | 验证码错误/过期 |
| 423 | `AUTH_ACCOUNT_LOCKED` | 连续失败 5 次锁定 15 分钟 |
| 403 | `AUTH_ACCOUNT_DISABLED` | 账号被禁用 |
| 429 | `RATE_LIMITED` | 超出限流 |

---

### 4.4 `POST /api/v1/admin/users`（核心接口 3）

**请求体**（`Authorization` 由 session cookie 承载，需 admin 角色）

```json
{
  "username": "lisi",
  "displayName": "李四",
  "email": "lisi@example.com",
  "phone": "13800000000",
  "password": "Init!pass2026",
  "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
  "tenantId": "default",
  "roleIds": ["user"],
  "mfaRequired": false,
  "sendNotify": false
}
```

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `username` | string | 是 | 3–64，`^[a-zA-Z][a-zA-Z0-9._-]{2,63}$`，全局唯一（大小写敏感） |
| `displayName` | string | 是 | 1–64 |
| `email` | string | 否 | 合法邮箱，全局唯一 |
| `phone` | string | 否 | `^1[3-9]\d{9}$` |
| `password` | string | 是 | 8–128，至少含字母与数字 |
| `deptId` | string | 是 | 必须存在 |
| `tenantId` | string | 否 | 默认 `default` |
| `roleIds` | string[] | 否 | 必须是已有角色，默认 `["user"]` |
| `mfaRequired` | bool | 否 | 是否强制 MFA，默认 false |
| `sendNotify` | bool | 否 | 本阶段恒为 false（不发通知，见 §7 非目标） |

**响应 201**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "id": "01J9ZK5B9C1D3E5F7G9H1J3K5M",
    "username": "lisi",
    "displayName": "李四",
    "email": "lisi@example.com",
    "phone": "13800000000",
    "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
    "tenantId": "default",
    "status": "active",
    "mfaEnabled": false,
    "mfaRequired": false,
    "roles": ["user"],
    "createdAt": "2026-10-20T10:12:33Z",
    "updatedAt": "2026-10-20T10:12:33Z"
  },
  "traceId": "01J9ZK5BA7B9C1D3E5F7G9H1J3"
}
```

**错误码**：`VALIDATION_FAILED`(400)、`USERNAME_ALREADY_EXISTS`(409)、`EMAIL_ALREADY_EXISTS`(409)、`DEPT_NOT_FOUND`(400)、`FORBIDDEN`(403)。

---

### 4.5 `GET /api/v1/me`（核心接口 4）

**请求**：无 body；cookie `sid` 必需。

**响应 200**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "id": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
    "username": "zhangsan",
    "displayName": "张三",
    "email": "zhangsan@example.com",
    "phone": "13800000001",
    "avatarUrl": "https://cdn.example.com/avatar/zs.png",
    "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
    "deptPath": ["总公司", "技术中心", "平台组"],
    "tenantId": "default",
    "roles": ["user", "admin"],
    "mfaEnabled": true,
    "mfaRequired": false,
    "lastLoginAt": "2026-10-20T09:00:00Z",
    "createdAt": "2026-01-05T02:11:00Z"
  },
  "traceId": "01J9ZK6C1D3E5F7G9H1J3K5M7P"
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `deptPath` | string[] | 从根到当前部门的名称路径 |
| `mfaEnabled` | bool | 用户是否已绑定 TOTP |
| `mfaRequired` | bool | 是否被管理员强制 MFA |
| `lastLoginAt` | string(RFC3339) | 可能为 `null` |

---

### 4.6 其余接口约定（字段从略，实现时必须与此一致）

| 路径 | 关键请求字段 | 关键响应字段 |
|---|---|---|
| `POST /api/v1/auth/mfa/verify` | `mfaTicket`, `code`(6 位) | `redirectTo`, `user{id,username,displayName}` |
| `POST /api/v1/me/password` | `oldPassword`, `newPassword` | `data: null` |
| `GET /api/v1/me/apps` | — | `[{clientId,name,description,logoUrl,homeUrl,lastAccessAt}]` |
| `GET /api/v1/me/sessions` | — | `[{sid,ip,userAgent,createdAt,lastSeenAt,current}]` |
| `GET /api/v1/admin/users` | `keyword`,`deptId`,`status`,`page`(默认1),`pageSize`(默认20,最大100) | `{list:[...],total,page,pageSize}` |
| `GET /api/v1/admin/orgs/tree` | — | `[{id,name,parentId,path,userCount,children[]}]` |
| `GET /api/v1/admin/clients` | `keyword`,`page`,`pageSize` | `{list:[{clientId,clientName,isPublic,redirectUris,grantTypes,scopes,createdAt}],total,page,pageSize}` |
| `POST /api/v1/admin/clients` | `clientName`,`redirectUris[]`,`grantTypes[]`,`scopes[]`,`isPublic` | `{clientId,clientSecret}`（secret **仅此一次返回**，不落日志） |
| `POST /api/v1/admin/clients/:id/secret` | `gracePeriodSeconds`(默认0) | `{clientId,clientSecret,expiresAt}` |
| `GET /api/v1/admin/audit-logs` | `userId`,`clientId`,`eventType`,`from`,`to`,`result`,`page`,`pageSize` | `{list:[{id,eventType,userId,username,clientId,ip,userAgent,result,detail,createdAt}],total}` |

**分页参数统一**：`page` 从 1 开始，`pageSize` 最大 100，超出则 `VALIDATION_FAILED`。

---

## 5. 统一响应格式

适用于 `/api/v1/**`。`/oauth2/**` 与 `/.well-known/**` **不适用**（RFC 原生格式）。

### 5.1 成功模板

```json
{
  "code": "OK",
  "message": "success",
  "data": {},
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `code` | string | 是 | 成功恒为 `"OK"` |
| `message` | string | 是 | 成功恒为 `"success"` |
| `data` | object \| array \| null | 是 | 载荷；无返回内容时为 `null`，**不得省略该字段** |
| `traceId` | string | 是 | ULID，同时写入响应头 `X-Trace-Id` 与日志 |

- HTTP 状态码：创建返回 `201`，其余成功 `200`，删除成功 `200`（返回 `data: null`）。
- 分页载荷统一放在 `data` 内：`{ "list": [], "total": 0, "page": 1, "pageSize": 20 }`。

### 5.2 错误模板

```json
{
  "code": "AUTH_INVALID_CREDENTIALS",
  "message": "用户名或密码错误",
  "data": null,
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `code` | string | 大写下划线错误码，见下表 |
| `message` | string | 面向人的中文提示，**不得泄露内部细节/堆栈/SQL** |
| `data` | null | 错误时恒为 `null` |
| `traceId` | string | 与日志、审计可关联 |

### 5.3 错误码表

| HTTP | code | 含义 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 参数校验失败 |
| 401 | `UNAUTHENTICATED` | 未登录或会话失效 |
| 401 | `AUTH_INVALID_CREDENTIALS` | 账号密码错误 |
| 400 | `AUTH_CAPTCHA_INVALID` | 验证码错误 |
| 400 | `AUTH_MFA_INVALID` | MFA 校验码错误 |
| 400 | `AUTH_MFA_TICKET_EXPIRED` | MFA 票据过期 |
| 423 | `AUTH_ACCOUNT_LOCKED` | 账号锁定 |
| 403 | `AUTH_ACCOUNT_DISABLED` | 账号禁用 |
| 403 | `FORBIDDEN` | 无权限（非 admin 访问管理接口） |
| 404 | `NOT_FOUND` | 资源不存在 |
| 409 | `USERNAME_ALREADY_EXISTS` | 用户名冲突 |
| 409 | `EMAIL_ALREADY_EXISTS` | 邮箱冲突 |
| 409 | `CLIENT_ALREADY_EXISTS` | client_id 冲突 |
| 400 | `WEAK_PASSWORD` | 密码不满足策略 |
| 429 | `RATE_LIMITED` | 触发限流 |
| 500 | `INTERNAL_ERROR` | 未预期错误（日志记录完整堆栈，响应不含） |
| 503 | `DEPENDENCY_UNAVAILABLE` | MySQL/Redis 不可用 |

新增错误码必须先登记到本表；禁止在 handler 内直接拼字符串作为 `code`。

---

## 6. 数据模型

数据库 `omino_sso`，字符集 `utf8mb4`。
**时间统一用 `BIGINT` 存 Unix 毫秒**（列名以 `_at` 结尾），禁用 `TIMESTAMP` 与 `DATETIME`。

### 6.1 `users`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | ULID |
| `username` | VARCHAR(64) | NOT NULL, UNIQUE, `COLLATE utf8mb4_0900_as_cs` | 大小写敏感 |
| `display_name` | VARCHAR(64) | NOT NULL | |
| `email` | VARCHAR(128) | NULL, UNIQUE | |
| `phone` | VARCHAR(20) | NULL | |
| `avatar_url` | VARCHAR(512) | NULL | |
| `password_hash` | VARCHAR(255) | NOT NULL | argon2id 编码串 |
| `dept_id` | CHAR(26) | NOT NULL, INDEX | FK → `organizations.id`（逻辑外键） |
| `tenant_id` | VARCHAR(32) | NOT NULL DEFAULT `'default'`, INDEX | |
| `roles` | JSON | NOT NULL | 字符串数组，如 `["user","admin"]` |
| `status` | TINYINT | NOT NULL DEFAULT 1 | 1=active 0=disabled 2=locked |
| `mfa_enabled` | TINYINT(1) | NOT NULL DEFAULT 0 | |
| `mfa_required` | TINYINT(1) | NOT NULL DEFAULT 0 | |
| `mfa_secret` | VARBINARY(255) | NULL | AES-GCM 加密后存储 |
| `failed_attempts` | INT | NOT NULL DEFAULT 0 | |
| `locked_until` | BIGINT | NULL | |
| `last_login_at` | BIGINT | NULL | |
| `password_updated_at` | BIGINT | NOT NULL | 改密后吊销全部会话 |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

> 本表**不使用软删除**。删除用户 = 置 `status=0`，数据保留以维护审计完整性。

### 6.2 `organizations`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | |
| `parent_id` | CHAR(26) | NULL, INDEX | 根为 NULL |
| `name` | VARCHAR(64) | NOT NULL | |
| `path` | VARCHAR(512) | NOT NULL, INDEX | 如 `/root/tech/platform/`，用于子树查询 |
| `sort` | INT | NOT NULL DEFAULT 0 | |
| `tenant_id` | VARCHAR(32) | NOT NULL DEFAULT `'default'` | |
| `created_at` | BIGINT | NOT NULL | |

### 6.3 `clients`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `client_id` | VARCHAR(64) | PK, `COLLATE utf8mb4_0900_as_cs` | 大小写敏感 |
| `client_name` | VARCHAR(128) | NOT NULL | |
| `description` | VARCHAR(255) | NULL | 应用描述，供"我的应用"展示 |
| `logo_url` | VARCHAR(512) | NULL | 应用图标地址，供"我的应用"展示 |
| `client_secret_hash` | BINARY(32) | NULL | SHA-256；`is_public=1` 时为空 |
| `is_public` | TINYINT(1) | NOT NULL DEFAULT 0 | 1=仅 PKCE |
| `redirect_uris` | JSON | NOT NULL | 字符串数组，**精确匹配** |
| `post_logout_uris` | JSON | NOT NULL | 白名单 |
| `grant_types` | JSON | NOT NULL | `["authorization_code","refresh_token"]` |
| `scopes` | JSON | NOT NULL | `["openid","profile","email","offline_access"]`；**签发 refresh_token 需客户端申请 `offline_access`** |
| `access_token_ttl` | INT | NOT NULL DEFAULT 900 | 秒 |
| `refresh_token_ttl` | INT | NOT NULL DEFAULT 2592000 | 秒，30 天 |
| `tenant_id` | VARCHAR(32) | NOT NULL DEFAULT `'default'` | |
| `status` | TINYINT | NOT NULL DEFAULT 1 | |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

### 6.4 `auth_codes`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `code_hash` | BINARY(32) | PK | SHA-256(授权码明文) |
| `client_id` | VARCHAR(64) | NOT NULL, INDEX | |
| `user_id` | CHAR(26) | NOT NULL | |
| `redirect_uri` | VARCHAR(512) | NOT NULL | 兑换时精确比对 |
| `scope` | VARCHAR(512) | NOT NULL | 空格分隔 |
| `nonce` | VARCHAR(128) | NULL | 原样回传至 id_token |
| `code_challenge` | VARCHAR(128) | NOT NULL | |
| `code_challenge_method` | VARCHAR(8) | NOT NULL, CHECK = `'S256'` | 只允许 S256 |
| `auth_time` | BIGINT | NOT NULL | |
| `amr` | JSON | NOT NULL | `["pwd","otp"]` |
| `expires_at` | BIGINT | NOT NULL, INDEX | 签发 + 60s |
| `used` | TINYINT(1) | NOT NULL DEFAULT 0 | 原子 UPDATE 更新 |
| `created_at` | BIGINT | NOT NULL | |

> 消费必须使用 `UPDATE auth_codes SET used=1 WHERE code_hash=? AND used=0 AND expires_at>?`，以 `RowsAffected==1` 判定成功。**禁止先 SELECT 再 UPDATE。**
> 表保留 24 小时后由定时任务清理；**禁止软删除**。

### 6.5 `refresh_tokens`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | |
| `token_hash` | BINARY(32) | NOT NULL, UNIQUE | SHA-256(明文) |
| `family_id` | CHAR(26) | NOT NULL, INDEX | 同一授权链共享；重放时按族吊销 |
| `user_id` | CHAR(26) | NOT NULL, INDEX | |
| `client_id` | VARCHAR(64) | NOT NULL | |
| `scope` | VARCHAR(512) | NOT NULL | |
| `amr` | JSON | NOT NULL | |
| `auth_time` | BIGINT | NOT NULL | |
| `expires_at` | BIGINT | NOT NULL, INDEX | |
| `revoked` | TINYINT(1) | NOT NULL DEFAULT 0 | 更新时必须用 `map` 或 `Select` |
| `revoked_at` | BIGINT | NULL | |
| `rotated_from` | CHAR(26) | NULL | 上一代 token id |
| `created_at` | BIGINT | NOT NULL | |

### 6.6 `sessions`

主存 Redis（`sso:sess:{sid}`，TTL 与会话一致），MySQL 仅保留审计副本。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `sid` | VARCHAR(64) | PK | 256bit 随机串的十六进制 |
| `user_id` | CHAR(26) | NOT NULL, INDEX | |
| `tenant_id` | VARCHAR(32) | NOT NULL | |
| `amr` | JSON | NOT NULL | 本会话使用的认证方式 |
| `auth_time` | BIGINT | NOT NULL | |
| `ip` | VARCHAR(45) | NOT NULL | |
| `user_agent` | VARCHAR(512) | NULL | |
| `created_at` | BIGINT | NOT NULL | |
| `last_seen_at` | BIGINT | NOT NULL | |
| `expires_at` | BIGINT | NOT NULL | |
| `revoked` | TINYINT(1) | NOT NULL DEFAULT 0 | |

### 6.7 `audit_events`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | |
| `event_type` | VARCHAR(64) | NOT NULL, INDEX | 见下枚举 |
| `user_id` | CHAR(26) | NULL, INDEX | 匿名事件为空 |
| `username` | VARCHAR(64) | NULL | 冗余存储，便于用户删除后追溯 |
| `client_id` | VARCHAR(64) | NULL, INDEX | |
| `ip` | VARCHAR(45) | NOT NULL | |
| `user_agent` | VARCHAR(512) | NULL | |
| `result` | VARCHAR(16) | NOT NULL | `success` / `failure` |
| `detail` | JSON | NULL | 失败原因、撤销范围等，**禁止写入密码/token 明文** |
| `trace_id` | VARCHAR(32) | NULL | |
| `created_at` | BIGINT | NOT NULL, INDEX | |

`event_type` 枚举（新增需登记）：`login_success`、`login_failure`、`mfa_success`、`mfa_failure`、`logout`、`password_changed`、`token_issued`、`token_revoked`、`code_reused`、`refresh_reused`、`user_created`、`user_updated`、`client_created`、`client_secret_rotated`。

### 6.8 `consents`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `user_id` | CHAR(26) | PK(1/2) | |
| `client_id` | VARCHAR(64) | PK(2/2) | |
| `scopes` | JSON | NOT NULL | 已同意 scope |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

> 所有建表语句写在 `migrations/0001_init.up.sql`，**禁止使用 `gorm.AutoMigrate`**。

---

## 7. 架构约束

### 7.1 必须遵守的 5 条

**约束 1：协议层只允许来自 `zitadel/oidc/v3`。**
禁止手写 OIDC 状态机、授权码签发流程、JWT 签发与验签（统一用 `go-jose/v4`），禁止引入第二个 OIDC 实现库。对 `op.Storage` 的适配只能做"数据存取 + 业务校验"，不得在其中实现协议状态流转。

**约束 2：IdP 与管理 API 物理分离为两个 `http.Server`。**
`:8080` 只挂 `op.Provider`（`net/http`），`:8081` 只挂 Gin。`/oauth2/*` 上禁止挂 CORS、业务鉴权、统一响应包装、全局限流；两个端口各自独立的中间件栈与超时配置。

**约束 3：数据访问必须事务安全，关键路径禁止"读-改-写"。**
`/oauth2/token` 链路通过 `internal/store/tx.go` 的 tx-in-context 传递事务；授权码消费、refresh 轮换、令牌吊销一律使用单条原子 `UPDATE` 并以 `RowsAffected` 判定结果；所有 Storage 方法必须传入 `ctx`（`db.WithContext(ctx)`）。

**约束 4：凭证永不进入浏览器可读区域。**
前端不得使用 `localStorage`/`sessionStorage`/内存变量持有 access_token、refresh_token 或任何 client_secret；浏览器只持有 `HttpOnly + Secure + SameSite=Lax` 的 `sid` cookie。响应体与日志中禁止出现 token 明文、密码、`client_secret`（新增 client 时的 secret 仅单次返回，且需在日志层做屏蔽）。

**约束 5：结构变更只走手写 SQL 迁移。**
`migrations/` 下按 `NNNN_name.up.sql` / `.down.sql` 成对提交；禁止 `AutoMigrate`、禁止在代码中 `CREATE TABLE`；token / code / session 类表禁止软删除；新增大小写敏感列必须显式声明 `COLLATE utf8mb4_0900_as_cs` 或使用 `BINARY(32)` 哈希。

### 7.2 非目标（本阶段明确不做）

> 以下条目**在没有明确指令前一律不实现**，包括不预留接口、不写 TODO、不建空表、不加配置项。

**非目标 1：不做 SAML 2.0 与 CAS 协议。**
不实现 SAML IdP/SP、不集成 `crewjam/saml`、不实现 CAS ticket 校验。需要接入 SAML/CAS 老系统时，通过外部协议网关或 `oauth2-proxy` 转换，本服务只讲 OIDC。

**非目标 2：不做账号自动开通与外部目录写入。**
不做 SCIM 2.0、不向 LDAP/AD 反向写回、不做 HR 系统同步、不发邮件/短信通知（`sendNotify` 恒为 false）、不做开放注册与邀请链接。账号只能由管理员创建。

**非目标 3：不做高可用与多副本部署。**
不写 K8s manifest / Helm / Operator、不做多实例共享缓存、不实现 Redis 版 `AuthRequestStorage` 之外的集群方案、不做读写分离与分库分表。MVP 明确单副本运行，`docker-compose` 为唯一部署形态。

**非目标 4：不做密码找回、短信 MFA 与第三方社交登录之外的认证方式。**
不实现"忘记密码"邮路/短信链路、不做短信验证码、不做邮件 OTP、不做 WebAuthn/Passkey、不做扫码登录的轮询接口、不做人脸/指纹。MFA 仅 TOTP，找回密码仅由管理员重置。企业微信/钉钉/飞书属于**后续阶段**，本阶段不实现。

**非目标 5：不做多租户 SaaS 化、计费与主题引擎。**
不做租户自助注册、不做套餐/计费/配额、不做租户级自定义域名与独立登录页、不做可视化主题编辑器、不做多语言 i18n（UI 恒为简体中文）、不做开放 API 的开发者门户与 SDK 发布。

**附加禁止项（AI 协作特别约定）**

- 不新增本文件未列出的第三方依赖；确需新增时先更新 §2 并说明理由。
- 不重构既有目录结构与分层（§3）。
- 不"顺手优化"与本任务无关的代码，不批量格式化。
- 不生成 `README`、演示脚本、示例数据种子文件，除非任务明确要求。
- 遇到规范未覆盖的情况：选择改动最小、最保守的实现，并在回复中显式说明该决策点。

---

## 8. 启动命令

### 8.1 本地依赖

```bash
# 启动 MySQL 8.4 + Redis 7.4
docker compose up -d mysql redis

# 初始化数据库（幂等，重复执行安全）
mysql -h 127.0.0.1 -P 3306 -u root -p omino_sso < migrations/0001_init.up.sql
```

### 8.2 后端

```bash
cp .env.example .env          # 首次执行，按需修改
go mod download
go run ./cmd/sso              # 同时启动 :8080(IdP) 与 :8081(API)
```

等价于 `make dev`。单端口调试可分别设置 `HTTP_IDP_ADDR` / `HTTP_API_ADDR`。

### 8.3 前端

```bash
cd web/portal && npm ci && npm run dev     # http://localhost:5173
cd web/admin  && npm ci && npm run dev     # http://localhost:5174
```

### 8.4 构建与验证

```bash
make build                    # 产出 bin/sso
make test                     # 单元测试
make test-integration         # 依赖容器内 MySQL/Redis
make lint                     # golangci-lint + eslint
make build-web                # 两个前端产物 -> web/dist
```

### 8.5 合规验收（门禁，不可跳过）

```bash
docker run --rm -p 8080:8080 \
  -e CONFORMANCE_SERVER=http://host.docker.internal:8080 \
  openidentity/conformance-suite
# 手工在 UI 选择 basic OP + config OP profile，全部用例通过方可发布
```

### 8.6 部署（MVP）

```bash
docker compose up -d --build   # mysql + redis + sso + nginx(静态托管两套前端)
```

---

## 9. 环境变量清单

所有变量由 `internal/config` 统一加载并在启动时校验；缺失必填项**直接退出**（非零码），不得静默使用默认值上线。

| 变量 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `APP_ENV` | 是 | `dev` | `dev` / `test` / `prod`；`prod` 下强制校验 cookie 与 issuer 的 https |
| `LOG_LEVEL` | 否 | `info` | `debug` / `info` / `warn` / `error` |
| `HTTP_IDP_ADDR` | 否 | `:8080` | 协议层监听地址 |
| `HTTP_API_ADDR` | 否 | `:8081` | 管理 API 监听地址 |
| `HTTP_READ_TIMEOUT` | 否 | `15s` | |
| `HTTP_WRITE_TIMEOUT` | 否 | `15s` | |
| `OIDC_ISSUER` | 是 | — | 例 `https://sso.example.com`，必须与外部访问地址完全一致 |
| `OIDC_SIGNING_KEY_PATH` | 是 | — | RS256 私钥 PEM 路径（`0600`，不入仓库） |
| `OIDC_SIGNING_KEY_ID` | 是 | — | `kid`，例 `2026-10` |
| `OIDC_CRYPTO_KEY` | 是 | — | 32 字节随机串（base64），用于库内部加密 |
| `OIDC_ACCESS_TOKEN_TTL` | 否 | `900` | 秒 |
| `OIDC_REFRESH_TOKEN_TTL` | 否 | `2592000` | 秒 |
| `OIDC_AUTH_CODE_TTL` | 否 | `60` | 秒，上限 60 |
| `SESSION_TTL` | 否 | `28800` | 秒，8 小时 |
| `SESSION_TTL_REMEMBER` | 否 | `604800` | 秒，7 天 |
| `COOKIE_DOMAIN` | 是 | — | 例 `sso.example.com` |
| `COOKIE_SECURE` | 否 | `true` | 本地 http 调试可设 false |
| `COOKIE_SAME_SITE` | 否 | `Lax` | `Lax` / `Strict` |
| `DB_HOST` | 是 | `127.0.0.1` | |
| `DB_PORT` | 否 | `3306` | |
| `DB_USER` | 是 | — | |
| `DB_PASSWORD` | 是 | — | |
| `DB_NAME` | 否 | `omino_sso` | |
| `DB_MAX_OPEN_CONNS` | 否 | `25` | |
| `DB_MAX_IDLE_CONNS` | 否 | `10` | |
| `DB_CONN_MAX_LIFETIME` | 否 | `30m` | |
| `REDIS_ADDR` | 是 | `127.0.0.1:6379` | |
| `REDIS_PASSWORD` | 否 | 空 | |
| `REDIS_DB` | 否 | `0` | |
| `REDIS_KEY_PREFIX` | 否 | `sso:` | |
| `ARGON2_MEMORY_KIB` | 否 | `65536` | 64MB |
| `ARGON2_ITERATIONS` | 否 | `3` | |
| `ARGON2_PARALLELISM` | 否 | `2` | |
| `LOGIN_MAX_FAILURES` | 否 | `5` | 达到后锁定 |
| `LOGIN_LOCK_DURATION` | 否 | `900` | 秒 |
| `RATE_LIMIT_LOGIN_PER_MIN` | 否 | `10` | 按 `username+IP` |
| `RATE_LIMIT_API_PER_MIN` | 否 | `300` | 按 `sid` 或 IP |
| `AUDIT_ASYNC_BUFFER` | 否 | `1024` | 审计异步队列长度 |
| `ADMIN_BOOTSTRAP_USERNAME` | 是（首次） | — | 初始化管理员，仅在 `users` 为空时生效 |
| `ADMIN_BOOTSTRAP_PASSWORD` | 是（首次） | — | 初始化后必须立即修改，并在生产环境删除该变量 |

`.env.example` 必须与本表保持同步；新增变量时同步更新两处。

---

## 附：一致性检查清单（提交前自检）

- [ ] 改动的接口字段与 §4 完全一致（含 `null` 与可选性）
- [ ] `/api/v1/*` 的响应符合 §5；`/oauth2/*` 未被包装
- [ ] 新增表/字段已写入 `migrations/`，无 `AutoMigrate`
- [ ] 时间字段为 `BIGINT` 毫秒；敏感列大小写敏感或 `BINARY(32)`
- [ ] 关键路径使用原子 `UPDATE` + `RowsAffected`，未出现"先查后改"
- [ ] 未引入 §7.2 非目标中的任何能力
- [ ] 未新增未登记的错误码与 `event_type`
- [ ] 日志与响应中无 token / 密码 / client_secret
