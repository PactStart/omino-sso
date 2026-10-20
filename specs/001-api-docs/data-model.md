# Phase 1 Data Model: API 接口文档与自助调试门户

**Feature**: `001-api-docs` | **Date**: 2026-10-20

本特性**不涉及数据库**：无新表、无字段变更、无迁移文件（宪法原则 V 无触发）。以下模型是**文档域模型**——描述 `internal/api/docs/openapi.json` 必须承载的信息结构及其不变量，用于指导注解编写与校验。

---

## 1. 实体总览

| 实体 | 对应 OpenAPI 构件 | 数量基线 | 来源 |
|---|---|---|---|
| 接口条目 DocOperation | `paths.<path>.<method>` | 16 | `internal/api/router.go` 的 16 处注册 |
| 参数 DocParameter | `parameters[]` / `requestBody` | 逐接口 | handler 内 `ShouldBindJSON` / `Query` / `Param` |
| 数据结构 DocSchema | `components.schemas.*` | 现有 DTO + 3 个信封 | `internal/api/dto`、`internal/api/response` |
| 错误码 DocError | `responses.<code>.content.<mt>.schema` + `x-code` 语义 | 17 个已登记码 | `internal/api/response/errors.go` |
| 鉴权等级 DocAuthLevel | `security` + `x-auth-level` | 4 值枚举 | 中间件挂载位置 |

---

## 2. DocOperation（接口条目）

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `operationId` | string | 是 | 全局唯一，命名 `<分组>.<handler 方法名>`，如 `admin.ListUsers`；漂移比对的主键 |
| `method` | enum | 是 | `GET` / `POST` / `PATCH` / `DELETE`（本项基线不出现 `PUT`） |
| `path` | string | 是 | 必须以 `/api/v1` 开头，且与 `router.go` 注册字面量逐字符一致（含 `:sid`、`:id` 占位符） |
| `summary` | string | 是 | 中文，≤ 40 字，动词开头 |
| `tags` | string[] | 是 | 枚举：`认证` / `我的` / `用户管理` / `组织架构` / `应用管理` / `审计` |
| `authLevel` | enum | 是 | `public` / `session` / `admin`（见 §6） |
| `parameters` | DocParameter[] | 否 | `GET` 的查询/路径参数 |
| `requestBody` | DocSchema 引用 | 否 | 仅接受 JSON body 的端点 |
| `responses` | DocResponse[] | 是 | 至少含成功响应 |

**不变量**

- **INV-1（完整性）**：`paths` 下展开后的 `(method, path)` 组合数恒等于 16；少一个即 SC-001 失败。
- **INV-2（幂等）**：同一份源码连续生成两次，产物**逐字节相同**（要求 schema 与路径排序稳定），否则漂移检测会持续误报。
- **INV-3（与路由同源）**：`path` 集合必须等于 `router.go` 中 `/api/v1` 组下注册的路径集合；实现上由人工保证、由 T0xx 的一致性用例断言（比对 `r.Routes()` 与文档 `paths`）。
- **INV-4（方法白名单）**：不出现 `HEAD` / `OPTIONS`（`gin` 自动生成的与 `NoMethod` 兜底不属于业务契约）。

---

## 3. DocParameter（参数）

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `name` | string | 是 | 与 `json` tag 或 query key 一致；路径参数与 `:name` 一致 |
| `in` | enum | 是 | `path` / `query` / `header` / `cookie` |
| `type` | string | 是 | `string` / `integer` / `boolean` / `array` / `object` |
| `required` | bool | 是 | 与 `binding:"required"` 一致；分页参数为否 |
| `constraints` | object | 否 | `minLength` / `maxLength` / `pattern` / `minimum` / `maximum` / `enum` |
| `default` | any | 否 | 仅分页参数有（`page=1`、`pageSize=20`） |
| `description` | string | 是 | 中文；枚举值须列出 |

**不变量**

- **INV-5（校验一致性）**：`binding` tag 中的 `required` / `min` / `max` / `oneof` 必须在文档中有对应约束表达；无 `binding` 的字段不得标注为必填。
- **INV-6（分页上限）**：`pageSize` 必须同时标注 `default=20`、`maximum=100`（AGENTS.md §4.6 既有约定），缺一即视为契约缺失。

---

## 4. DocSchema（数据结构）

复用而非重复：以下 5 类结构以 `components.schemas` 单一定义被多处引用。

| Schema 名 | 来源 | 关键字段 | 复用范围 |
|---|---|---|---|
| `ResponseBody` | `response.Body`（已存在） | `code`(string)、`message`(string)、`data`(any)、`traceId`(string) | 全部成功响应与业务错误响应 |
| `Page` | `response.Page`（已存在） | `list`(array)、`total`(int64)、`page`(int)、`pageSize`(int) | 3 个列表端点（users / clients / audit-logs） |
| `LoginRequest` / `MFAVerifyRequest` / `PasswordRequest` / `CreateUserRequest` / `UpdateUserRequest` / `CreateClientRequest` / `RotateSecretRequest` | 已存在（分散在 `auth/*.go`、`me/profile.go`、`admin/*.go`） | 见各自定义 | 对应写操作端点 |
| `UserBrief` / `UserDetail` / `UserAdmin` / `Client` | `internal/api/dto/dto.go`（已存在） | 见 dto 定义 | #1′#2 / #4 / #9~#11 / #13 |
| `OrgNode` | `internal/api/admin/users.go:188`（已存在） | `id,name,parentId,path,userCount,children` | #12 |
| `LoginMfaChallenge` | **待新建** `dto` | `mfaRequired`(bool,true)、`mfaTicket`(string)、`expiresIn`(int,300) | #1 |
| `LoginResult` | **待新建** `dto` | `mfaRequired`(bool,false)、`mfaTicket`(string,可空)、`expiresIn`(int,0)、`redirectTo`(string)、`user`(UserBrief) | #1′ |
| `MfaResult` | **待新建** `dto` | `redirectTo`(string)、`user`(UserBrief) | #2 |
| `AppItem` | **待新建** `dto` | `clientId,name,description,logoUrl,homeUrl,lastAccessAt`(可空时间) | #5 |
| `SessionItem` | **待新建** `dto` | `sid,ip,userAgent,createdAt,lastSeenAt,current` | #7 |
| `CreatedClient` | **待新建** `dto` | `clientId`(string)、`clientSecret`(string,可空) | #14 |
| `RotatedSecret` | **待新建** `dto` | `clientId`(string)、`clientSecret`(string)、`expiresAt`(null) | #15 |
| `AuditItem` | **待新建** `dto` | `id,eventType,userId,username,clientId,ip,userAgent,result,detail,createdAt` | #16 |

**为什么要有"待新建"这一列**：`gin.H` 是 `map[string]any`，swag 无法从中推导任何字段，直接写 `@Success {object} response.Body` 会得到空的 `data`。因此 8 处匿名响应必须先成为具名结构（键名与 null 语义保持不变；键序由 map 的字典序变为字段声明序，对 JSON 消费方无影响），注解才有可引用的类型。这是"类型具名化"，不是"行为改造"；等价性判据为反序列化后的 `DeepEqual` + 键集合相同。

**不变量**

- **INV-7（单一定义）**：同名结构在 `components.schemas` 中只出现一次；出现两份即视为 FR-007 违反（edge case"同一数据结构被多个接口复用"）。
- **INV-8（信封泛化写法）**：由于 `response.Body.Data` 是 `any`，各接口以组合语法 `ResponseBody{data=<ConcreteType>}` 表达内层类型；**禁止**为每个接口各造一个 `XxxResponse` 包装结构（会产生 16 份重复信封）。
- **INV-9（示例脱敏）**：所有 `example` 值必须为虚构数据；`token` / `secret` / `password` 类字段示例固定使用形如 `"<redacted>"` 或 `"eyJhbGciOi...（示例）"` 的占位串（FR-022、宪法原则 IV）。

---

## 5. DocError（错误码条目）

每个端点声明其**实际可能返回**的错误子集，来源是 `response/errors.go` 的 17 个变量。

| 业务码 | HTTP | 典型归属端点 |
|---|---|---|
| `VALIDATION_FAILED` | 400 | 全部写操作 |
| `UNAUTHENTICATED` | 401 | 全部需登录端点 |
| `AUTH_INVALID_CREDENTIALS` / `AUTH_CAPTCHA_INVALID` / `AUTH_MFA_INVALID` / `AUTH_MFA_TICKET_EXPIRED` | 401/400 | 登录、MFA 校验 |
| `AUTH_ACCOUNT_LOCKED` | 423 | 登录 |
| `AUTH_ACCOUNT_DISABLED` | 403 | 登录 |
| `FORBIDDEN` | 403 | 全部管理端点 |
| `NOT_FOUND` | 404 | 含路径参数的端点、未匹配路由兜底 |
| `USERNAME_ALREADY_EXISTS` / `EMAIL_ALREADY_EXISTS` / `CLIENT_ALREADY_EXISTS` | 409 | 创建用户、创建应用 |
| `WEAK_PASSWORD` | 400 | 创建用户、改密 |
| `RATE_LIMITED` | 429 | 登录、MFA（有独立限流器）、其余端点（全局限流） |
| `INTERNAL_ERROR` | 500 | 全部端点（兜底） |
| `DEPENDENCY_UNAVAILABLE` | 503 | 全部端点（依赖不可用兜底） |

**不变量**

- **INV-10（全覆盖兜底）**：每个端点必须至少声明 `INTERNAL_ERROR`；`RATE_LIMITED` 与 `DEPENDENCY_UNAVAILABLE` 作为通用项在文档级统一声明一次，逐接口引用，避免 16 处重复（对应 FR-009）。
- **INV-11（无臆造码）**：文档中出现的业务码必须存在于 `response/errors.go`；反向也要求该文件中每个被端点使用的码都在文档中出现。两侧一致性由单测比对字符串集合保证。

---

## 6. DocAuthLevel（鉴权等级）

| 取值 | 判定依据（中间件挂载） | 端点 |
|---|---|---|
| `public` | 无 `middleware.Auth` | `/auth/login`、`/auth/mfa/verify` |
| `session` | `authed` 组（`middleware.Auth`） | `/auth/logout`、`/me`、`/me/apps`、`/me/password`、`/me/sessions`（GET/DELETE） |
| `admin` | `adminGroup`（`Auth` + `Admin`） | `/admin/*` 共 8 个 |

**不变量**

- **INV-12（映射完备）**：`authLevel` 到 `security` 的映射必须使 `session` / `admin` 均要求 `cookieAuth`；`admin` 额外以 `x-auth-level: admin` 标注角色门槛（OpenAPI 3.0 无法表达"角色"语义，故用扩展字段承载）。
- **INV-13（调试前置）**：文档页本身为 `public`，但其承载的端点仍各自受约束——文档不得提供任何绕过鉴权的路径说明。

---

## 7. 状态与生命周期

本特性**无状态机**。仅有的"生命周期"是构建期产物的流转：

```text
handler 源码注释
   │ swag init（只读源码，不连库）
   ▼
.tmp/swagger/swagger.json   (Swagger 2.0，gitignore)
   │ tools/openapi3 转换
   ▼
internal/api/docs/openapi.json   (OpenAPI 3.0.3，入库)
   │ //go:embed
   ▼
运行时 /docs/openapi.json（info 块按环境注入版本与环境标识）
   │ swagger-ui 渲染
   ▼
/docs/index.html
```

**合法迁移**：源码变更 → 重新生成 → 产物变更入库。
**非法迁移**：手工编辑 `openapi.json`（下次生成即被覆盖，且漂移检测会以"源码未变但产物变了"的形式暴露；FR-010 明确禁止）。
**环境分支**：`APP_ENV == "prod"` 时上述最后两步（注册路由）不发生，产物本身仍编译进二进制但不对外可达。

---

## 8. 规模与容量

| 指标 | 基线值 | 来源 |
|---|---|---|
| 文档化端点 | 16 | `router.go` |
| handler 文件 | 8 | `internal/api/{auth,me,admin}/*.go` |
| 复用 schema | ≥ 13 | `dto.go` + `response` 两型 |
| 已登记错误码 | 17 | `errors.go` |
| 产物体积估计 | 30–80 KB | 16 端点 × (参数 + 示例 + 错误) |
| 生成耗时目标 | ≤ 30s | SC-005 |
| **二进制体积增量** | 预计 +3~5 MB（待实测） | `swaggo/files` 内嵌 swagger-ui 资源；由 `quickstart.md` 场景 3 实测后回写本行（critique E6） |
