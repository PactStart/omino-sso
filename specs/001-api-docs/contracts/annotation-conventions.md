# Contract: 注解约定（16 个业务端点）

**Feature**: `001-api-docs` | **Date**: 2026-10-20

本文件规定 handler 源码中**必须出现的注解形状**。它是"文档与路由同源"（INV-3）与"校验一致"（INV-5）的可执行依据：注解写错，`make docs-check` 与单测必须报错。

---

## 1. 全局注解（写在 `cmd/sso/main.go` 顶部，仅注释）

```go
// @title           omino-sso 管理 API
// @version         1.0
// @description     企业统一身份提供方的管理接口。会话通过 HttpOnly cookie `sid` 携带；响应统一为 {code,message,data,traceId}。
// @description     协议端点（/oauth2/*、/.well-known/*）不在本文档范围内，见 /.well-known/openid-configuration
// @BasePath        /api/v1
// @securityDefinitions.apikey cookieAuth
// @in              cookie
// @name            sid
// @description     登录成功后由服务端下发的会话 cookie
```

约束：`@version` 写 `1.0`（接口契约版本），**运行版本**由运行时 info 注入覆盖（见 doc-endpoints.md §4），两者语义不同，不得混用。

## 2. 每个 handler 的注解骨架

```go
// @Summary      登录（账号密码）
// @Description  校验账号密码；若账号启用了 MFA，返回 mfaTicket 而非会话。连续失败 5 次锁定 15 分钟。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body      dto.LoginRequest  true  "登录请求体"
// @Success      200   {object}  response.Body{data=dto.LoginResult}   "success"
// @Failure      400   {object}  response.Body  "VALIDATION_FAILED / AUTH_CAPTCHA_INVALID"
// @Failure      401   {object}  response.Body  "AUTH_INVALID_CREDENTIALS"
// @Failure      423   {object}  response.Body  "AUTH_ACCOUNT_LOCKED"
// @Failure      403   {object}  response.Body  "AUTH_ACCOUNT_DISABLED"
// @Failure      429   {object}  response.Body  "RATE_LIMITED"
// @Failure      500   {object}  response.Body  "INTERNAL_ERROR"
// @Router       /auth/login [post]
func (h *Handler) Login(c *gin.Context) { ... }
```

### 2.1 必填字段规则

| 注解 | 规则 |
|---|---|
| `@Summary` | 中文，动词开头，≤ 40 字 |
| `@Description` | 至少一句；必须写出该端点独有的副作用（写库、吊销会话、单次返回 secret 等） |
| `@Tags` | 取值必须来自固定枚举：`认证` / `我的` / `用户管理` / `组织架构` / `应用管理` / `审计` |
| `@Router` | **路径与方法必须与 `router.go` 字面量一致**；路径参数用 `{id}` / `{sid}`（swag 语法），router 注册用 `:id` / `:sid` |
| `@Success` | 必须给出，且用组合语法指定 `data` 的具体类型（见 §3） |
| `@Failure` | 至少 `500`；需登录端点至少含 `401`；管理端点至少含 `401` + `403`；写操作至少含 `400` |
| `@Security` | `session` / `admin` 端点必须标 `@Security cookieAuth`；`public` 端点不得标注 |
| `@x-auth-level` | `admin` 端点额外标注（swag 的扩展字段写法：`@x-auth-level admin`） |

## 3. 统一信封的组合写法（关键约束）

`response.Body.Data` 是 `any`，若直接写 `{object} response.Body`，生成物中 `data` 会是空对象，接入方无法据以生成代码。因此**每个接口必须内联指定 `data` 的具体类型**：

| 场景 | 写法 |
|---|---|
| 单对象 | `@Success 200 {object} response.Body{data=dto.User}` |
| 数组 | `@Success 200 {object} response.Body{data=[]dto.App}` |
| 分页 | `@Success 200 {object} response.Body{data=response.Page{list=[]dto.User,total=int64,page=int,pageSize=int}}` |
| 无数据（如改密、登出、撤销会话） | `@Success 200 {object} response.Body{data=nil}` |
| 创建成功 | `@Success 201 {object} response.Body{data=dto.User}` |

禁止：为单个接口另造 `XxxResponse` 包装结构（INV-8）。

## 4. 16 个端点的注解锚点表

> **前置事实（已核验源码）**：`dto` 包现有具名类型只有 `UserBrief`、`UserDetail`、`UserAdmin`、`Client`；`OrgNode` 定义在 `admin` 包内。其余 8 处响应使用 `gin.H` 匿名映射，**swag 无法从 `gin.H` 推导 schema**，必须先具名化才能写 `@Success`。具名化只改"构造方式"：**JSON 键名与 null 语义不变；键序会变，且不影响任何消费方**（`gin.H` 走 map 序列化，键按字典序；结构体按字段声明序）。

| # | 方法 | 路径 | handler | 文件 | `data` 类型 | 类型现状 | 鉴权 |
|---|---|---|---|---|---|---|---|
| 1 | POST | `/api/v1/auth/login` | `auth.Login` | `auth/login.go:79` | `dto.LoginMfaChallenge` | **待新建**（替换 `gin.H{mfaRequired:true,mfaTicket,expiresIn:300}`） | public |
| 1' | 同上（登录完成分支） | — | `auth.completeLogin` | `auth/login.go:112` | `dto.LoginResult` | **待新建**（5 键：`mfaRequired:false`、`mfaTicket:null`、`expiresIn:0`、`redirectTo`、`user:UserBrief`） | public |
| 2 | POST | `/api/v1/auth/mfa/verify` | `auth.VerifyMFA` | `auth/mfa.go:63` | `dto.MfaResult` | **待新建**（`redirectTo`、`user:UserBrief`） | public |
| 3 | POST | `/api/v1/auth/logout` | `auth.Logout` | `auth/login.go:139` | `nil` | 已就绪（`response.Success(c, nil)`） | session |
| 4 | GET | `/api/v1/me` | `me.Profile` | `me/profile.go:43` | `dto.UserDetail` | 已存在（`dto.Detail`） | session |
| 5 | GET | `/api/v1/me/apps` | `me.Apps` | `me/profile.go:58` | `[]dto.AppItem` | **待新建**（`clientId,name,description,logoUrl,homeUrl,lastAccessAt`） | session |
| 6 | POST | `/api/v1/me/password` | `me.ChangePassword` | `me/profile.go` | `nil` | 已就绪 | session |
| 7 | GET | `/api/v1/me/sessions` | `me.Sessions` | `me/profile.go:130` | `[]dto.SessionItem` | **待新建**（`sid,ip,userAgent,createdAt,lastSeenAt,current`） | session |
| 8 | DELETE | `/api/v1/me/sessions/{sid}` | `me.RevokeSession` | `me/profile.go` | `nil` | 已就绪 | session |
| 9 | GET | `/api/v1/admin/users` | `admin.ListUsers` | `admin/users.go:71` | `response.Page{list=[]dto.UserAdmin}` | 已存在（`dto.Admin`） | admin |
| 10 | POST | `/api/v1/admin/users` | `admin.CreateUser` | `admin/users.go:184` | `dto.UserAdmin` | 已存在 | admin |
| 11 | PATCH | `/api/v1/admin/users/{id}` | `admin.UpdateUser` | `admin/users.go` | `dto.UserAdmin` | 已存在 | admin |
| 12 | GET | `/api/v1/admin/orgs/tree` | `admin.OrgTree` | `admin/users.go:211` | `[]admin.OrgNode` | 已存在（`admin/users.go:188`） | admin |
| 13 | GET | `/api/v1/admin/clients` | `admin.ListClients` | `admin/clients.go` | `response.Page{list=[]dto.Client}` | 已存在 | admin |
| 14 | POST | `/api/v1/admin/clients` | `admin.CreateClient` | `admin/clients.go:75` | `dto.CreatedClient` | **待新建**（`clientId`、`clientSecret *string`；仅此一次返回，`nil` 表示公开客户端） | admin |
| 15 | POST | `/api/v1/admin/clients/{id}/secret` | `admin.RotateSecret` | `admin/clients.go:105` | `dto.RotatedSecret` | **待新建**（`clientId`、`clientSecret string`、`expiresAt:null`；键集与 #14 **不同**，不可复用同一类型） | admin |
| 16 | GET | `/api/v1/admin/audit-logs` | `admin.ListAuditLogs` | `admin/audit.go:54` | `response.Page{list=[]dto.AuditItem}` | **待新建**（`id,eventType,userId,username,clientId,ip,userAgent,result,detail,createdAt`） | admin |

**具名化的铁律**

- **禁止合并键集不同的分支**：`#1` 与 `#1'`、`#14` 与 `#15` 的键集互不相同，合并会改变响应字节（例如 `#1'` 会多出 `redirectTo`/`user`，`#15` 会多出 `expiresAt`），违反 spec Assumptions 的"不改变任何业务端点的字段"。
- **禁止加 `omitempty`**：`mfaTicket:null`、`expiresAt:null` 是有意的空值语义，必须继续输出。
- **禁止改 JSON tag 名**：具名结构字段与现有 `gin.H` 键逐字对应，大小写敏感。
- 具名化的验收方式是**语义等价比对**（不是逐字节比对）：用同一组值分别构造 `gin.H{...}` 与 `dto.X{...}`，各自 `json.Marshal` 后 `json.Unmarshal` 为 `map[string]any`，断言 `reflect.DeepEqual` 为真且**键集合完全相同**（含 `mfaTicket`、`expiresAt` 这类 `null` 值键）。该断言不依赖 DB/Redis，可直接作为单测（AC-7）。

> 若实际类型名与上表冲突，以源码为准并回写本表；不要为了迎合文档而重命名既有 DTO（宪法"不顺手重构"）。

## 5. 示例数据规则（FR-022）

| 规则 | 要求 |
|---|---|
| 来源 | 优先在 `dto` 结构体字段上用 `example:"..."` tag；数组/分页级示例用注解行内 `Example` |
| 脱敏 | `token`、`clientSecret`、`password`、`refreshToken` 类字段示例固定为占位串，不得出现真实形态密钥 |
| 个人信息 | 用户/手机/邮箱一律用虚构值（如 `zhangsan`、`13800000000`、`zhangsan@example.com`） |
| 标识符 | ULID 用合法形态虚构值（26 位 Crockford Base32，如 `01J9ZJ8Q2M4N6P8R0T2V4X6Z8A`） |
| 时间 | RFC3339 UTC，如 `2026-10-20T10:12:33Z` |
| 一致性 | 同一实体在不同端点的示例值必须相互自洽（同一 `id` 对应同一 `username`） |

## 6. 错误码映射

`@Failure` 的业务码必须取自 `internal/api/response/errors.go`；映射表见 `data-model.md` §5。两个通用项按 FR-009 逐接口声明：`500 INTERNAL_ERROR`、`503 DEPENDENCY_UNAVAILABLE`；`429 RATE_LIMITED` 仅登录与 MFA 端点声明（其余端点虽受全局限流，但按"文档只描述端点自身语义"原则不重复）。

## 7. 验收断言

| 编号 | 断言 |
|---|---|
| AC-1 | `openapi.json` 的 `paths` 展开组合数 == 16，且与 `router.go` 的 `r.Routes()` 中 `/api/v1` 前缀集合完全一致 |
| AC-2 | 每个 operation 都有 `summary`、`tags`、`security`（public 除外）、至少 1 个 2xx 与 `500` |
| AC-3 | `components.schemas` 中不存在 `XxxResponse` 形态的逐接口信封（INV-8） |
| AC-4 | `components.schemas` 每个名字只出现一次（INV-7） |
| AC-5 | 文档中业务码集合 == `errors.go` 中被使用码集合（INV-11） |
| AC-6 | 序列化后不出现 `"clientSecret": "` 后跟非占位值的形态（示例脱敏，INV-9） |
| AC-7 | 8 处具名化各有等价性单测：`gin.H` 与 `dto.X` 同值构造 → 反序列化为 map 后 `DeepEqual` 为真、键集合相同（§4「具名化的铁律」） |
