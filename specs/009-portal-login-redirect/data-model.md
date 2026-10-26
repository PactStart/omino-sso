# Data Model: 未登录 OIDC 授权统一跳转门户登录页

**Feature**: `009-portal-login-redirect` | **Date**: 2026-10-25

**结论先行**：本特性**不新增表、不新增列、不新增 Redis 键**。变化只有三处：① 待认证授权请求的「登录入口状态」被扩展为三种落点（由 URL 标记决定，非持久化字段）；② 审计事件类型枚举新增一个取值；③ 配置项新增一个。因此 `migrations/` 无改动（宪法 V 不触发）。

---

## 1. 待认证授权请求（Pending Authorization Request）

**现状**：`internal/oidc/storage.go` 的 `pending sync.Map`，键为一次性请求 ID（`id.New()`），值 `pendingRequest{req *authRequest, expiresAt time.Time}`，TTL 10 分钟（`authRequestTTL`）。单副本进程内存（规范 §7.2 非目标 3）。

**本特性新增的语义**：请求本身**不加字段**，但「到达 `/login` 时的入口状态」由三要素决定——`pending` 是否存在、浏览器是否带有效会话、URL 是否带 `portal=1`。

### 状态机

```text
CreateAuthRequest
      │
      ▼
  pending ──(GET /login?id=, 无会话, 无标记, 已配门户)──► 302 门户（请求仍是 pending）
      │                                                        │
      │◄──────────(门户登录成功 → GET /login?id=&portal=1)──────┘
      │
      ├──(GET /login?id=, 有会话)──────────────► CompleteLogin(done) ──► SaveAuthCode ──► DeleteAuthRequest
      │
      ├──(GET /login?id=&portal=1, 仍无会话)───► 渲染内置登录页（loop guard 触发，请求保持 pending）
      │                                                  │
      │◄──────────(表单 POST /login)─────────────────────┘
      │
      ├──(GET /login?id=, 未配置门户)──────────► 渲染内置登录页（现状）
      │
      └──(TTL 过期 / 不存在)──────────────────► 400「登录请求已过期，请重新发起授权」
```

### 字段级约束（不变，列出供对照）

| 项 | 约束 | 说明 |
|---|---|---|
| ID | ULID，一次性 | 出现在 `/login?id=` 与门户 `next` 中；**不是凭据**（无会话时只能看到登录页） |
| ClientID | 关联 `clients.client_id` | 决定登录页展示的应用名（本特性用于 `app` 参数） |
| RedirectURI / Scopes / Nonce / State / CodeChall | 来自授权请求 | 不变 |
| Subject / AuthTime / AMR / done | 由 `CompleteLogin` 回写 | 不变 |
| expiresAt | now + 10min | 不变 |

### 为什么 `portal=1` 不落库

标记只回答「这次回跳是否已经过门户」，是**跳转链上的临时事实**，且必须跟着浏览器走（用户可能在两次跳转之间关掉标签页）。落库/落 Redis 会引入「标记与请求生命周期不一致」的清理负担，而 URL 标记天然随请求过期失效。见 [research.md](./research.md) R2。

---

## 2. 会话（Session）

**存储**：Redis `sess:{sid}`，TTL 与会话一致；MySQL `sessions` 仅审计副本。**无改动**。

**本特性关注**：会话必须能在「门户域 ↔ IdP 域」之间被携带，因此 cookie 属性约束收紧为：

| 属性 | 值 | 约束来源 |
|---|---|---|
| Name | `sid` | 不变 |
| Domain | `COOKIE_DOMAIN`（四域部署为 `.example.com`） | 不变（007） |
| Path | `/` | 不变 |
| HttpOnly | `true` | 宪法 IV |
| Secure | prod 必 `true` | 不变 |
| SameSite | **恒为 `Lax`** | FR-012：`Strict` 启动失败；代码不再分支 |

---

## 3. 审计事件（`audit_events`）

表结构不变（`event_type VARCHAR(64)`），枚举新增一个取值：

| event_type | result | user_id | client_id | detail（JSON） | 触发点 |
|---|---|---|---|---|---|
| `sso_login_success`（新增） | `success` | 会话用户 | 本次授权的应用 | `{"entry":"portal"\|"template","amr":["pwd"\|"otp"]}` | `/login?id=` 认出既有会话并完成授权时 |
| `login_success`（不变） | `success` | 表单登录用户 | 应用 | 现状 | 表单 POST 登录成功时 |

**规则**：

- 两者互不替代、互不重复：一次「门户登录 → 回跳完成授权」只产生 `sso_login_success`（门户侧登录本身已由 `POST /api/v1/auth/login` 记录 `login_success`）。
- `detail` 禁止写入 token / 密码 / 验证码 / 会话值（宪法 IV）。
- 事件经 `service.Auditor` 异步通道落库；队列满丢弃并告警（既有行为，不因本特性改变）。
- 登记要求：`internal/store/model/audit_event.go` ↔ AGENTS §6.7 ↔ `web/admin/src/utils/format.ts` 的 `EVENT_LABEL`（审计页筛选由此生成）。

---

## 4. 配置项（`PORTAL_LOGIN_URL`）

| 属性 | 值 |
|---|---|
| 类型 | 字符串（绝对 URL） |
| 默认 | 空（`dev` 回落内置登录页） |
| 必填性 | `prod` 必填；`test`/`dev` 可空 |
| 格式 | `http(s)://<host>[:port][/path]`；必须含 scheme 与 host；不允许 fragment |
| 协议 | `prod`/`test` 必须 `https`；`dev` 允许 `http` |
| 用途 | 未登录授权的落点（门户登录页） |
| 回滚 | `dev`/`test` 清空即回落内置页；`prod` 因必填只能回退版本（SC-007） |

**校验矩阵**（启动即失败，非零退出）

| 场景 | dev | test | prod |
|---|---|---|---|
| 空值 | 允许（内置页） | 允许（内置页） | **失败**：`PORTAL_LOGIN_URL is required in prod` |
| `https://portal.example.com/login` | 允许 | 允许 | 允许 |
| `http://portal.example.com/login` | 允许 | **失败**：`must use https in test` | **失败** |
| `portal.example.com/login`（无 scheme） | **失败**：`must be an absolute URL` | 失败 | 失败 |
| `https://`（无 host） | 失败 | 失败 | 失败 |
| `https://portal.example.com/login#frag` | 失败 | 失败 | 失败 |

**`COOKIE_SAME_SITE` 校验**：只接受 `Lax`（大小写不敏感）与空；`Strict` / 其它值 → 启动失败并指名变量（FR-012）。

---

## 5. 门户登录页的查询参数（前端契约）

| 参数 | 来源 | 约束 | 缺失/非法时 |
|---|---|---|---|
| `next` | IdP 生成 | 必须通过 origin 白名单（门户自身 / 管理后台 / IdP 登录域） | 丢弃，登录后落门户默认页 |
| `app` | IdP 生成（应用显示名） | 长度 ≤ 64、去控制字符、仅作文本渲染 | 退回「所访问的应用」 |
| `loggedOut` | 既有 | 不变 | 不变 |
| `reason` | 既有 | 不变 | 不变 |

> `app` 只影响文案，**不参与**跳转决策；跳转目标始终由 `next` 单独决定，避免「文案可被篡改就改变落点」。
