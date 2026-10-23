# 契约：admin 根路径路由与跨域回跳

**Feature**: `007-deployment` | **覆盖需求**: FR-004 / FR-005 / FR-009 / FR-023

## 1. 路由表（`web/admin/src/router.ts`）

| 路径 | name | 说明 |
|---|---|---|
| `/` | — | `redirect: /users` |
| `/users` | `users` | 用户管理（默认落地） |
| `/clients` | `clients` | 应用管理 |
| `/clients/:clientId` | — | 详情壳（子路由容器） |
| `/clients/:clientId` | `client-detail` | `redirect → client-overview` |
| `/clients/:clientId/overview` | `client-overview` | 应用信息 |
| `/clients/:clientId/members` | `client-members` | 应用成员 |
| `/clients/:clientId/roles` | `client-roles` | 应用角色 |
| `/clients/:clientId/permissions` | `client-permissions` | 应用权限 |
| `/clients/:clientId/grants` | `client-grants` | 授权管理 |
| `/clients/:clientId/:tab` | `client-tab-fallback` | 非法 tab → `client-overview` |
| `/audit` | `audit` | 审计日志 |
| `/files` | `files` | 文件管理 |
| `/send-records` | `send-records` | 发送记录 |
| `/offline` | `offline` | 服务不可用（public） |
| `/403` | `forbidden` | public |
| `/:pathMatch(.*)*` | `not-found` | public |

**硬约束**

- 表中 **不得** 出现 `/admin` 段；删除原 `/admin` redirect 条目，不做兼容重定向（R7）。
- `createWebHistory('/')` 与 vite `base: '/'` 保持不变（根路径托管）。
- 菜单 `index`、`activeMenu` 的 `startsWith`、`ClientDetail` 的 tab `index`、`Offline` 的重试回跳必须与上表完全一致（`/clients`、`/users`）。
- `web/admin/src/api/index.ts` 中的 `/admin/...` 字符串 **不动**：它们相对 `baseURL=/api/v1`，是后端 API 路径。

## 2. 菜单与高亮映射

| 菜单项 | index | `activeMenu` 命中规则 |
|---|---|---|
| 用户管理 | `/users` | `route.path === '/users'` |
| 应用管理 | `/clients` | `route.path.startsWith('/clients')`（子页仍高亮一级菜单） |
| 审计日志 | `/audit` | `===` |
| 文件管理 | `/files` | `===` |
| 发送记录 | `/send-records` | `===` |

## 3. 未登录跳转（admin → portal）

```
https://portal.<主域>/login?next=<encodeURIComponent("https://admin.<主域><fullPath>")>
```

- `fullPath` 含 query/hash；`next` 必须是**绝对地址**（跨域回跳，D4）。
- dev 环境：`http://localhost:5173/login`，`next` 不传（vite 跨端口无意义，沿用现状）。

## 4. `next` 白名单（portal 侧）

| 形态 | 规则 | 命中动作 |
|---|---|---|
| 相对（`/` 开头） | 不得以 `/login` 或 `/offline` 开头 | `router.replace(next)` |
| 绝对（http/https） | `new URL(next).origin` ∈ `[window.location.origin, new URL(VITE_ADMIN_HOME).origin]` | `window.location.assign(next)`（整页跳转） |
| 其他 | 一律丢弃 | 走默认落地（admin 角色 → `ADMIN_HOME`，否则 `/account/profile`） |

**反例（视为缺陷）**：直接 `router.replace(next)` 处理绝对地址（在同源路由里跳外域会抛错）；只校验 `startsWith('/')` 而丢弃绝对地址（跨域回跳失效，违反 FR-009）。

## 5. 登出

- 门户登出后必须落到 `https://portal.<主域>/login`（带"已退出"提示，FR-023）。提示落点固定为 **`?loggedOut=1`**：`Login.vue` 挂载时读取该参数显示"已退出"，并 `router.replace` 清除参数（避免刷新重复提示）。
- **改密同样属于会话失效路径**：`/me/password` 成功后必须 revoke 服务端会话并清除带 `Domain=<主域>` 的 `sid` cookie（不得只清 host-only cookie，否则四个域都还登着）。
- `sso.<主域>/logged-out` 仅作协议层兜底（后端 `/logout` 的默认 `next`），不作为产品落地页。
- `admin`/`sso` 域不主动跳转，靠 cookie 清除 + 下次请求 401 处理。

## 6. 验收断言

| # | 断言 | 期望 |
|---|---|---|
| 1 | `grep -rn "'/admin/" web/admin/src/router.ts web/admin/src/layouts web/admin/src/views` | 仅 `api/index.ts`/`api/types.ts` 注释命中；路由与视图文件 0 命中 |
| 2 | 访问 `https://admin.<主域>/` | 302/SPA 重定向到 `/users` |
| 3 | 直接打开 `https://admin.<主域>/clients/<id>/members` 并刷新 | 200 且停留原页 |
| 4 | 旧地址 `https://admin.<主域>/admin/users` | 404 页（不重定向） |
| 5 | 会话失效后完成门户登录 | 回到 `https://admin.<主域>/clients/<id>/members` |
| 6 | 构造 `?next=https://evil.example.com/x` | 被丢弃，落默认页；不跳外站 |
| 7 | 主动登出 | 落 `https://portal.<主域>/login`；再访问 admin 需重新登录 |
