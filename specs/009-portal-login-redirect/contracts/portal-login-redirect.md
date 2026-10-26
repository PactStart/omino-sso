# Contract: 门户登录页的 `next` / `app`

**Feature**: `009-portal-login-redirect`

门户登录页（`web/portal`，路由 `/login`）在本特性里成为「未登录 SSO 的第一落点」。本文件定义它接收什么、信任什么、以及登录成功后怎么回去。

## 1. 入参

| 参数 | 类型 | 必需 | 语义 | 非法时 |
|---|---|---|---|---|
| `next` | 绝对 URL 或站内相对路径 | 否 | 登录成功后整页跳转的目标 | 丢弃（`resolveNext` 返回 `null`），落门户默认页 |
| `app` | 字符串（≤64 字符） | 否 | 来源应用显示名，仅用于副标题文案 | 忽略，回落固定文案 |
| `loggedOut` | `1` | 否 | 登出提示（既有） | 不变 |
| `reason` | `password-changed` 等 | 否 | 提示原因（既有） | 不变 |

## 2. 白名单（`utils/redirect.ts#allowedOrigins`）

允许回跳的 origin，**仅此三类**：

1. 门户自身 origin（`window.location.origin`）；
2. 管理后台 origin（`VITE_ADMIN_HOME` 的 origin，解析失败则不加入）；
3. **新增**：统一身份登录域（`VITE_SSO_BASE_URL` 的 origin，解析失败则不加入）。

判定保持既有严格度：`//evil.com`（协议相对）、`javascript:`/`data:`、白名单外域名一律丢弃；`/login*`、`/offline*` 相对路径丢弃。

> 白名单必须包含 IdP 域，否则「用户登录成功却回到门户首页」——这条是 spec Edge Cases 里点名必须在交付前被断言拦住的缺陷。

**Referrer 约束（FR-022）**：该页面的 URL 含 `app`（内部系统显示名），必须设置 `Referrer-Policy: same-origin`（由前端 nginx / vite 配置下发）。这样从门户跳到 IdP 时不携带 `Referer` 给任何第三方，同时同源请求（门户自己的 XHR）仍可正常工作。该地址不得写入日志或埋点。

## 3. 文案规则（副标题）

| `next` 形态 | 副标题 |
|---|---|
| 无 `next` | 「登录后返回统一门户」（既有） |
| 相对路径 | 「登录后返回 `<path>`」（既有） |
| 绝对地址，origin = 管理后台 | 「登录后返回 `<path>`」（既有） |
| 绝对地址，origin = IdP 登录域 + 有合法 `app` | 「登录后返回 `<app>`」 |
| 绝对地址，origin = IdP 登录域 + 无/非法 `app` | 「登录后返回 所访问的应用」——**不得**出现 `/login` 之类技术路径 |

`app` 的处理：`trim` 后长度 > 64 截断；含控制字符（`\u0000-\u001F`、`\u007F`）则整体丢弃；仅作文本渲染（Vue 插值已转义），不拼进任何 URL 或 HTML 属性。

## 4. 登录成功后的跳转

沿用 `Login.vue#goAfterAuth`：相对路径走 `router.replace`，绝对地址走 `window.location.assign`。对 IdP 目标必须是**整页跳转**（跨域），不得用 SPA 路由。

跳转序列（整链）：

```text
门户 /login?next=https://sso.example.com/login?id=X&portal=1&app=CRM
  └─ POST /api/v1/auth/login（api 域，Set-Cookie sid Domain=.example.com）
     └─ 302/前端跳转 → https://sso.example.com/login?id=X&portal=1
        └─ IdP 认出 sid → 302 /oauth2/authorize/callback?id=X
           └─ 302 https://sp.example.com/cb?code=…&state=…
```

## 5. 构建期配置（**必须构建期强制**）

| 变量 | dev 缺省 | test/prod | 用途 | 缺失时的行为 |
|---|---|---|---|---|
| `VITE_SSO_BASE_URL` | `http://localhost:8080` | 必须注入 `https://sso.<主域>` | 白名单 + 副标题判定 | 静默退化为「只信任自身 + admin」→ 回跳被丢弃（用户登录成功却回不到应用） |

**因此必须构建期失败，而不是运行期降级**：`web/Dockerfile` 的 portal target 增加与 `VITE_ADMIN_HOME` 同款的强制校验（`test -n "$VITE_SSO_BASE_URL" || { echo 缺失; exit 1; }`）；三份模板（`.env.example` / `.env.test.example` / `.env.production.example`）、`docker-compose.yml` 的 portal `build.args`、`deploy` 构建命令同步。

串环境防线：`scripts/check-build-env.sh` 增加 IdP 域断言（镜像内 `sso.<本环境>` ≥1 次、`sso.<外来环境>` 0 次）——把 test 镜像里带 prod IdP 域这种"比跳错后台更严重"的错配拦在发版前。

## 6. 发布顺序与兼容矩阵

| 步骤 | 动作 | 反序的后果 |
|---|---|---|
| 1 | 先发 **portal**（含 `VITE_SSO_BASE_URL`） | — |
| 2 | 再给后端配 `PORTAL_LOGIN_URL` | 后端先开 → 旧门户丢弃回跳 → **全员登录成功却进不去任何系统**（滚动窗口内表现为"偶发"） |
| 3 | 滚动后端镜像 | prod 缺变量会 CrashLoop，故配置必须早于镜像 |

| 门户版本 | 后端配置 | 结果 |
|---|---|---|
| 旧（白名单无 IdP 域） | 未开跳转 | 可用（改造前行为） |
| 旧 | 已开跳转 | **禁止上线** |
| 新 | 未开跳转 | 可用（内置页；白名单多一项无副作用） |
| 新 | 已开跳转 | 目标状态 |

## 7. 明确不做

- 不在门户侧实现「检测 IdP 会话后自动跳过登录」（会增加一次跨域探测，且与本特性的 loop guard 判定重复）；
- 不支持把 `next` 指向任意 SP 域（门户只服务自身 / 后台 / IdP 三处回跳）；
- 不改变 `loggedOut` / `reason` 等既有参数行为。
