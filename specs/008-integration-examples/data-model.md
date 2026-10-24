# Phase 1 Data Model: 三语言接入示例

**Feature**: 008-integration-examples | **Date**: 2026-10-24（critique 修订：会话 cookie 命名、降级字段）

本特性**不新增数据库表**，也不修改 IdP 任何既有表。这里的「数据模型」指**示例进程内的运行时对象**与**验证脚本的本地产物**，以及它们与 IdP 既有实体（客户端注册）的关系。

---

## 1. 示例应用配置（ExampleAppConfig）

三语言同名同义（SC-006），全部来自环境变量；缺任一项必须在启动阶段失败并打印缺失项名（FR-010）。

| 变量 | 必填 | 语义 | 校验规则 |
|---|---|---|---|
| `SSO_ISSUER` | 是 | IdP 基地址，如 `http://localhost:8080` | 非空、不以 `/` 结尾、能取到 `/.well-known/openid-configuration`（失败即启动退出） |
| `SSO_CLIENT_ID` | 是 | 注册得到的客户端标识 | 非空 |
| `SSO_CLIENT_SECRET` | 是 | 注册得到的客户端密钥 | 非空；**只从环境读**，不落盘、不打日志、不进页面 |
| `SSO_REDIRECT_URI` | 是 | 回调地址 | 非空；必须与注册值**逐字符一致**（不做尾斜杠归一化） |
| `SESSION_SECRET` | 是 | 会话签名密钥 | 长度 ≥ 32 字符 |
| `APP_PORT` | 否 | 监听端口 | 默认 `8000`/`8001`/`8002`；被占用时**报错退出**，不自动换端口（FR-017） |
| `SSO_USERINFO_URL_OVERRIDE` | 否 | **仅调试用**：覆盖 `userinfo` 地址 | 默认空（用发现文档的值）；置为不可达地址用于验证降级（FR-025），启动日志需打印「debug override 已启用」，生产不得使用 |

**关系**：`SSO_REDIRECT_URI` 的路径与四端点契约绑定；端口变化必须同步重新注册（`redirect_uris`）。

---

## 2. 客户端注册（Client Registration，IdP 侧既有实体，只经管理 API 读写）

示例不直接访问数据库，全部通过 `POST/GET/PATCH /api/v1/admin/clients`（admin 会话 cookie）操作。

| 字段 | 来源 | 说明 / 约束 |
|---|---|---|
| `clientId` | 服务端生成 | ULID 后 16 位；**请求体不接受自定义**，故脚本必须「按名精确匹配 → 复用」 |
| `clientName` | 脚本指定 | 固定名（`示例-Go` / `示例-Java` / `示例-Python`）作为幂等查找键；查回列表后必须**逐字比对**，不能用模糊命中 |
| `clientSecret` | 创建或轮换时返回 | **只返回一次**；脚本写入 `.tmp/e2e-examples/clients.env`（`umask 077` + `chmod 600`），缺失时才调用轮换接口重取 |
| `redirectUris` | 脚本写入 | 每条示例一条：`http://localhost:<port>/callback`；PATCH 可改（1–10 条），无 https 强制 |
| `grantTypes` | 脚本指定 | `["authorization_code","refresh_token"]`（刷新需要它） |
| `scopes` | 脚本指定 | `["openid","profile","email","offline_access"]`（`offline_access` 才发 refresh_token） |
| `isPublic` | 脚本指定 | `false`（机密客户端，走 HTTP Basic） |

**不变量**：
- INV-1：一个示例端口 ↔ 一条 callback ↔ 一个 clientId，三者一一对应；
- INV-2：同一 `clientName` 最多一条被脚本使用的记录（重复运行只复用，不再新建）；
- INV-3：脚本**不修改**既有演示客户端（`portal`）的 `redirectUris`，也不修改演示账号；
- INV-4：`clients.env` 权限必须是 `0600`，内容只含客户端凭据（不含任何用户令牌）。

---

## 3. 本地会话（LocalSession，示例进程内）

浏览器只持有会话 id；令牌留在服务端（宪法 IV / research D3）。

| 字段 | 位置 | 说明 |
|---|---|---|
| `sid_<lang>` | 浏览器 cookie | **必须带语言前缀**：`sid_go` / `sid_java` / `sid_py`。`HttpOnly`、`SameSite=Lax`、`Path=/`、host-only；本地 http 下 `Secure` 关闭。**禁止**使用 `sid`——三个示例与 IdP 同处 `localhost`，而 cookie 不按端口隔离，重名会互相覆盖（critique E1） |
| `sub` / `username` / `name` / `email` / `tenant` / `deptId` / `roles` / `amr` | 服务端会话 | 首次登录时取自 `id_token` |
| `accessToken` / `refreshToken` / `idToken` / `expiresAt` | 服务端会话 | **绝不写进 cookie 或页面** |
| `accessExpiresAt` / `lastRefreshedAt` | 服务端会话 → **允许展示**（FR-024） | 仅用于页面显示「剩余有效期 / 上次刷新时间」，是刷新确实发生的肉眼凭据；不含任何令牌值 |
| `pending`（`state` / `nonce` / `codeVerifier` / `createdAt`） | 服务端会话 | 仅存在于「授权中」阶段，回调成功后立即删除；与登录态会话分开存放，避免覆盖 `sid` |

**生命周期**：进程内存储，重启即全部失效（演示可接受，且天然避免落盘密钥）。并发访问需加锁（刷新单飞见 research D7）。

**降级标记**（FR-007 / SC-009）：`appRoles` / `permissions` 每次经 `userinfo` 现取；取不到时使用空数组并把 `degraded = ["userinfo"]` 一并放进取数结果，供页面提示，不写入会话（下一次请求重试）。

---

## 4. 验证脚本产物（`.tmp/e2e-examples/`，gitignored）

| 文件 | 内容 | 用途 |
|---|---|---|
| `clients.env` | 三个示例的 `clientId` / `clientSecret` / `redirectUri` 与 `SSO_ISSUER` | 供脚本复用；也便于手工照着启动示例；权限 `0600` |
| `<lang>.log` | 各示例进程 stdout/stderr | 断言失败时打印固定三类日志行（启动 / 登录 / 刷新）的尾部 |
| `jar-*.txt`、`token-*.json` | curl cookie jar 与令牌响应 | 断言取数；每次运行前清空 |
| `report.txt` | 逐项断言结果（PASS/FAIL + 实测值 + 各示例首次 200 耗时） | FR-013 的可定位输出 / SC-001 的量化依据 |

**清理策略**：每次运行开始前清空 `jar-*`/`token-*`/`report.txt`，保留 `clients.env`（幂等复用）；退出（含失败与 `Ctrl-C`）时关闭三个示例进程（FR-011）。

---

## 5. 状态迁移（每个示例的会话）

```text
匿名 ──GET /login──▶ 授权中(pending: state/nonce/verifier)
授权中 ──GET /callback(state 匹配 + 换令牌成功)──▶ 已登录(存 token，删 pending)
授权中 ──state 不匹配 / 换令牌失败──▶ 匿名(返回可诊断错误，清 pending)
已登录 ──GET /api/me──▶ 已登录(令牌有效；userinfo 可取或用降级标记) / 刷新中(access 已过期)
刷新中 ──刷新成功(轮换并覆盖)──▶ 已登录
刷新中 ──刷新被拒(过期/吊销)──▶ 匿名(清本地失效令牌，引导重新授权)
已登录 ──GET /logout──▶ 匿名(清本地会话与 sid_<lang>) + 302 IdP /logout?next=/logged-out
```

**非法迁移必须被拒绝**（对应 Edge Cases）：
- 无 `pending` 时收到 `/callback` → 400，不建会话；
- `state` 不匹配 → 400，不换令牌（防 CSRF / 响应错配）；
- 刷新进行中再次要求刷新 → 等待同一结果，不重复使用旧 refresh_token（防整族吊销）；
- **任何路径都不得写 `sid`（IdP 的 cookie 名）**——这是 E1 的回归判据。
