# Contract: 登录入口 `/login`（IdP 协议端口 `:8080`）

**Feature**: `009-portal-login-redirect`

本文件是 `GET /login?id=<reqID>` 与 `POST /login` 的**对外行为契约**。它不属 `/api/v1`（不套统一响应、不进 `openapi.json`），实现位于 `internal/oidc/login.go`。

## 1. 请求形态

| 方法 | 路径 | 参数 | 说明 |
|---|---|---|---|
| GET | `/login?id=<reqID>` | `id`（必需）、`portal`（可选，值恒为 `1`） | 浏览器入口；由 `op.Client.LoginURL` 生成 |
| POST | `/login` | 表单 `id`、`username`/`password`，或 `id`+`ticket`+`code`（MFA 第二步） | 内置登录页提交 |

`portal=1` 是**回跳标记**：只在「IdP 把浏览器送去门户」时由 IdP 自己追加，用于 loop guard。它不是鉴权参数，去掉不会提权、加上不会降级已登录用户。

## 2. GET 行为矩阵（判定顺序自上而下，先命中先生效）

| # | 条件 | 状态码 | 响应 | 副作用 |
|---|---|---|---|---|
| C1a | `id` 不存在或已过期（>10min）+ **不带** `portal=1` | `400` | 文本「登录请求已过期，请重新发起授权」（既有行为） | 清理该请求 |
| C1b | `id` 不存在或已过期 + **带** `portal=1` | `200` | 过期提示页：说明登录状态已失效，并提供可继续的入口（返回接入系统重新发起 / 重新登录），**不是裸文本** | 无（请求已不存在） |
| C2 | 带有效会话（无论是否带 `portal=1`） | `302` | `Location: <op.AuthCallbackURL>`（相对路径 `/oauth2/authorize/callback?id=…`） | `CompleteLogin`（回写 sub/amr/auth_time）+ 审计 `sso_login_success` |
| C3 | 无会话 + `PORTAL_LOGIN_URL` 为空 | `200` | 内置登录页 HTML（与改造前逐字一致） | 无 |
| C4 | 无会话 + 已配置 + **带** `portal=1` | `200` | 内置登录页 HTML **+ 降级说明**（含「未能在跳转中延续，请在此直接登录」语义） | `slog.Warn` 一条降级日志（`reason=portal_roundtrip_without_session`，含 client_id / request_id，**不含凭据**）；**不再跳门户** |
| C5 | 无会话 + 已配置 + **不带** `portal=1` | `302` | `Location: <PORTAL_LOGIN_URL>?next=<issuer>/login?id=<reqID>&portal=1&app=<clientName>` | 无（请求保持 pending） |

**判定顺序不可调换**：C2 必须在 C4 之前——已登录用户即便 URL 带 `portal=1` 也必须直接完成授权（FR-009）。

**C1b 过期提示页的约束**：不得出现凭据；不得自动重定向到门户（会再次落入过期分支形成链）；必须提供可点击的"返回应用重新发起"或"重新登录"入口；页面文案不得暴露内部实现（`pending`、`TTL`、`sameSite` 等词）。

**标记被剥离时的行为**：用户手动删除 `portal=1` 只会让下一次 `/login` 再走一次 C5（多一次门户往返），不会形成死循环——每次 C5 都由 IdP 重新生成完整带标记地址；若参数被浏览器扩展持续剥离，仍不构成服务端环（无状态、无计数）。

## 3. 门户跳转 URL 的构造规则（C5）

```text
base   = cfg.PortalLoginURL（绝对地址，可能已带查询串，如 /login?from=sso）
next   = issuer + "/login?id=" + QueryEscape(reqID) + "&portal=1"
app    = clientName（clients.client_name；取不到时用 clientID）
result = base 追加/覆盖查询参数 next=<next>（单次 URL-encode）、app=<app>
```

约束：

- `next` 的**编码只做一次**（`url.Values.Encode`），否则门户解码后拿到的 `id` 会带多余转义；
- `app` 缺失时不下发该参数（门户回落默认文案），不发送空串；
- base 里已有的查询参数（若有）必须保留，同名参数以本次值为准；
- 不得把 `reqID`、`app` 之外的任何字段（scope / state / 用户信息）放进 URL。

## 4. 内置登录页契约（C3/C4/C5 降级后）

保持不变的能力（FR-008）：账号密码、MFA 第二步、锁定/停用/密码错误文案、成功后 `302` 到授权回调 + `Set-Cookie: sid`。

新增唯一差异（C4）：页面顶部提示区多一行降级说明。该说明**不得**以技术术语暴露内部状态（不出现 `SameSite`、`cookie`、`Redis` 等词）。

## 5. 跨域与安全约束

| 约束 | 要求 |
|---|---|
| 会话 cookie | `HttpOnly` + `Secure`（prod）+ `SameSite=Lax`；Domain 取 `COOKIE_DOMAIN` |
| 开放重定向 | `next` 只由 IdP 生成；`/login` **不得**接受外部传入的 `next`/`redirect` 参数改变落点 |
| 凭据泄漏 | 响应体与日志不得出现 sid、token、密码、验证码 |
| CSRF | 表单 POST 维持现状（同站 `Lax` cookie + 一次性 reqID），不新增 token |
| 缓存 | 所有 `302` 响应不得被缓存（由 `http.Redirect` 默认语义保证；如需显式设置 `Cache-Control: no-store` 不得影响协议层其它端点） |

## 6. 明确不做

- 不新增 `/login` 的其它方法（PUT/DELETE）与 JSON 响应；
- 不把 `/login` 迁到 Gin（`:8081`）或加统一响应包装（宪法 II）；
- 不在 `/login` 上做限流（现状由 nginx / 协议层既有策略承担，本特性不改变）。
