# Contract: 示例应用对外接口

**适用对象**：`examples/go`、`examples/java`、`examples/python` 三个工程。三份实现必须逐项一致（SC-006），差异只允许出现在实现内部。

**基线**：与 `docs/integration-guide.md` §9.0 的四端点表对齐，仅在此基础上**新增**一页演示首页 `/`。协议事实以该文档为准，本文件只固化「三份实现必须一致的对外形状」。

---

## 路由总表

| 方法 | 路径 | 返回 | 说明 |
|---|---|---|---|
| GET | `/` | `text/html` | 演示首页：未登录显示「登录」入口；已登录显示用户卡片与「登出」入口 |
| GET | `/login` | `302` | 生成 `state`/`nonce`/PKCE，跳 IdP 授权端点 |
| GET | `/callback` | `302` 或 `4xx` | 校验 `state` → 换令牌 → 建本地会话 → 302 到 `/` |
| GET | `/api/me` | `200` JSON / `401` | 当前用户；未登录 401 |
| GET | `/logout` | `302` | 清本地会话，跳 IdP `/logout?next=/logged-out` |

除以上五条外不得暴露其他业务路由（US3 场景 1）。静态样式内联在页面里，不引入静态资源目录。

---

## 会话 cookie 命名（critique E1，硬约束）

三个示例与统一身份都跑在 `localhost`，而 **HTTP cookie 不按端口隔离**——`localhost:8000` 写下的 cookie 与 `localhost:8080` 的 cookie 属于同一个域。因此：

| 角色 | cookie 名 | 属性 | 实现落点（**默认名不是契约值，必须显式改**） |
|---|---|---|---|
| 统一身份（不可改） | `sid` | 由 IdP 写入 | — |
| Go 示例 | `sid_go` | `HttpOnly`、`SameSite=Lax`、`Path=/`、host-only（不带 `Domain`） | `http.SetCookie(&http.Cookie{Name: "sid_go", ...})` |
| Java 示例 | `sid_java` | 同上 | `server.servlet.session.cookie.name: sid_java`（默认是 `JSESSIONID`） |
| Python 示例 | `sid_py` | 同上 | `SessionMiddleware(app, session_cookie="sid_py")`（默认是 `session`） |

- **禁止**示例使用 `sid`：重名会与 IdP 会话互相覆盖，导致「登录示例后门户掉线」或 SSO 断言失真；
- 示例任何路径都不得写 `sid`，也不得清除它；
- 本地 http 下 `Secure` 关闭；生产必须开启（见文末「仅演示」）。
- `pending` 状态（`state`/`nonce`/`codeVerifier`）存服务端，与登录态分开，避免复用一个 cookie。
- 正例断言见 [e2e-examples-cli.md](./e2e-examples-cli.md) 的 **A16**（三个 `Set-Cookie` 名恰为 `sid_go`/`sid_java`/`sid_py` 且互不相同）。

## GET /login

生成并**存进服务端会话**：`state`（≥16 字节随机）、`nonce`（≥16 字节随机）、`codeVerifier`（43–128 字符，`[A-Za-z0-9-._~]`）。

302 目标（参数一个都不能少）：

```text
{issuer}/oauth2/authorize
  ?response_type=code
  &client_id={SSO_CLIENT_ID}
  &redirect_uri={SSO_REDIRECT_URI}          # 与注册值逐字符一致，需 URL 编码
  &scope=openid profile email offline_access
  &state={state}
  &nonce={nonce}
  &code_challenge={BASE64URL(SHA256(verifier))}
  &code_challenge_method=S256
```

约束：
- 只允许 `S256`；不得发送 `plain`；
- `scope` 必须包含 `offline_access`（否则拿不到 refresh_token）；
- 重复点击 `/login` 时覆盖 `pending`，不并行保留多份 `state`。

## GET /callback

| 入参 | 处理 |
|---|---|
| `code` + `state` | `state` 与服务端 `pending.state` **逐字节比对**；成功则用 `code_verifier` 换令牌 |
| `error` / `error_description` | 原样透出（IdP 的 RFC 6749 错误），清 `pending`，返回 `400` |
| 缺少 `pending` | 返回 `400 {"error":"no_pending_authorization"}`，不建会话、不换令牌 |

换令牌请求（服务端发起，浏览器不可见）：

```text
POST {issuer}/oauth2/token
Authorization: Basic base64(client_id:client_secret)
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&code=...&redirect_uri={SSO_REDIRECT_URI}&code_verifier=...
```

成功后的动作：
1. 用库验签 `id_token`（签名、`iss`、`aud`、`exp`、`nonce` 与本地一致）；
2. 把 `sub`/`username`/`name`/`email`/`tenant`/`dept_id`/`roles`/`amr` 与三个令牌写进**服务端**会话；删除 `state`/`nonce`/`codeVerifier`；
3. 302 到 `/`。

失败一律走错误契约（见文末），不得 302 到 `/` 造成「假登录」。

## GET /api/me

`200` 响应（字段名固定，缺值用空串或空数组，**不得省略键、不得为 null**）：

```json
{
  "sub": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
  "username": "zhangsan",
  "name": "张三",
  "email": "zhangsan@example.com",
  "tenant": "default",
  "deptId": "01J...",
  "roles": ["user"],
  "amr": ["pwd"],
  "appRoles": [],
  "permissions": [],
  "degraded": [],
  "accessExpiresIn": 842,
  "lastRefreshedAt": "2026-10-24T12:31:07Z"
}
```

- `appRoles` / `permissions` **必须**来自一次 `userinfo` 调用（它们不进 `id_token`，见指南 §6.1）；即使为空也要返回 `[]`；
- `degraded`：正常为 `[]`；`userinfo` 超时/5xx 时为 `["userinfo"]`，此时 `appRoles`/`permissions` 返回 `[]`，身份字段仍取自本地令牌，**整个响应仍为 200**（SC-009，不得把 IdP 抖动放大成 5xx）；
- `accessExpiresIn`：访问令牌剩余有效秒数（整数，可为负）；`lastRefreshedAt`：上次成功刷新时间（RFC3339，从未刷新为 `null`）。两者**只用于展示刷新是否发生**（FR-024），不得含令牌本身；
- 未登录或会话失效：`401 {"error":"unauthenticated"}`；
- access_token 已过期时先按 research D7 单飞刷新，成功则继续返回 200；刷新被拒则清本地失效令牌并返回 401（不 302）。

## GET /logout

1. 删除服务端会话与 `sid_<lang>` cookie；
2. `302` 到 `{issuer}/logout?next=/logged-out`（指南 §8：`end_session` 不清 `sid`，故不使用）。

## GET /

- 未登录：显示「登录」按钮（指向 `/login`）与一句说明；
- 已登录：显示 `sub`/`username`/`name`/`email`/`tenant`/`deptId`/`roles`/`amr`/`appRoles`/`permissions`；`degraded` 非空时显示「应用内角色暂不可用」提示；
- 已登录：显示一行**会话状态**（FR-024）——「访问令牌剩余 `X` 秒 / 上次刷新：`时间` 或「尚未刷新」」。这是人工验收时确认「刷新真的发生过」的唯一肉眼依据，**但不得显示令牌值**；
- **不得**把任何令牌写进 HTML、URL、`localStorage` 或 `sessionStorage`；
- 页脚必须有「仅本地演示」与生产必改三处的提示（FR-022）；
- 页面本身不发起 XHR（服务端渲染直出），因此不需要前端构建。

## 错误契约

所有错误响应为 JSON，且**不含**令牌、密钥、堆栈：

```json
{ "error": "invalid_grant", "stage": "token_exchange", "detail": "IdP 返回 invalid_grant" }
```

| 状态码 | 触发场景 | 必须包含 |
|---|---|---|
| `400` | `state` 不匹配 / 缺 `code` / 无 `pending` / IdP 返回 `error` | `error`、`stage`、IdP 的原始错误码（若有） |
| `401` | `/api/me` 未登录或会话失效（含刷新被拒） | `error="unauthenticated"` |
| `502` | IdP **发现/换令牌**阶段不可达 | `error`、`stage`、可读 `detail` |
| `500` | 会话存储异常 | `error`、`stage`（不含内部细节） |

> 注意：`userinfo` 不可达**不属于**错误——按上文降级为 200 + `degraded`。

## 启动期约定

| 情形 | 期望 |
|---|---|
| 缺 `SSO_ISSUER`/`SSO_CLIENT_ID`/`SSO_CLIENT_SECRET`/`SSO_REDIRECT_URI`/`SESSION_SECRET` | 进程以非零码退出，打印缺失项名称 |
| `SSO_ISSUER` 的发现文档取不到 | 启动失败，打印「连不上 issuer」与地址 |
| `APP_PORT` 被占用 | 启动失败并打印端口，**不自动换端口** |
| 启动成功 | 打印监听地址、`SSO_ISSUER`、`SSO_REDIRECT_URI`（**不打印密钥**） |

## 调试与故障注入（FR-025）

| 变量 | 作用 |
|---|---|
| `SSO_USERINFO_URL_OVERRIDE` | 覆盖 `userinfo` 地址，默认空（用发现文档的值）。仅用于验证降级：置为 `http://127.0.0.1:1/userinfo` 时，`/api/me` 仍须 `200` 且 `degraded=["userinfo"]` |

- 启用时启动日志必须打印 `[start] debug=userinfo_override url=...`，避免被静默带进生产；
- 该变量**只能影响 userinfo**：不得用来绕过 `id_token` 验签、`iss`/`aud`/`nonce` 校验或令牌交换，也不得成为「关掉安全校验」的开关。

## 日志约定（critique E8）

固定三类行，便于 e2e 失败时取尾部定位，且**不含令牌**：

```text
[start]   listen=:8000 issuer=http://localhost:8080 redirect=http://localhost:8000/callback
[login]   sub=01J... amr=pwd elapsed=118ms
[refresh] ok rotated=1 elapsed=42ms        # 或 refresh fail reason=invalid_grant → reauth
```

## 仅演示声明（FR-022）

示例页脚与 `examples/README.md` 必须写明：

1. 登出为 `GET`（便于地址栏验证），生产须改为 `POST` + CSRF 防护；
2. 本地为 http，生产必须 HTTPS 且 cookie 加 `Secure`；
3. 会话在进程内存，生产须换外部会话存储（重启即登出在演示中可接受，生产不可）。
