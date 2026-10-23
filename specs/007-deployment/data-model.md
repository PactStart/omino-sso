# Phase 1 Data Model: 容器化多域名部署

**Feature**: `007-deployment` | **Date**: 2026-10-23

## 0. 数据库影响

**零表结构变更**。本特性不新增/修改任何表、列、索引，**不新增迁移文件**；`migrations/` 目录保持 `0001`–`0006`。部署时仍按 [DEPLOYMENT.md](../../DEPLOYMENT.md) §6 手动执行既有迁移（宪法 V：禁止 `AutoMigrate`）。

> 因此本文件描述的是**部署期实体**（域名、部署单元、配置、路由），不是持久化模型。它们的"单一事实来源"分别是：域名/路径 → `contracts/nginx-routing.md`；配置 → `contracts/env-and-build.md`；路由 → `contracts/admin-routes.md`。

---

## 1. 域名（Domain）

| 字段 | 类型 | 说明 | 约束 |
|---|---|---|---|
| `name` | string | `portal` / `admin` / `api` / `sso`（同一主域下的二级域名） | 四者 MUST 同属一个 registrable domain |
| `publicReachable` | bool | 是否对公网解析 | `api` = **false**；其余 = true（FR-001） |
| `serves` | enum | `spa` / `api` / `oidc` | `portal`+`admin` = spa；`api` = api；`sso` = oidc |
| `upstream` | string | 反代目标 | spa → 静态根（nginx 本地）；api → `:8081`；oidc → `:8080` |
| `tls` | enum | `terminated` / `none` | `portal`/`admin`/`sso` = terminated；`api` = none（内网明文，FR-007） |
| `cookieScope` | bool | 是否参与 `sid` 读写 | 四个都参与（`Domain=.example.com`，FR-006） |

**关系**：`portal`/`admin` 的 `/api/` 依赖 `api`；`sso` 与 `api` 指向**同一个容器**的两个端口（assumption A11）。`api` 与 `sso` 不得互换职责：OIDC 端点只允许出现在 `sso`（FR-002）。

**状态**：无状态机；仅"是否已配置 DNS/证书"两个部署前置条件（DEPLOYMENT.md §3）。

---

## 2. 部署单元（Deployment Unit）

| 字段 | 类型 | 说明 |
|---|---|---|
| `service` | string | compose 服务名：`sso` / `portal` / `admin`（一体化额外 `mysql` / `redis`） |
| `image` | string | `registry.example.com/omino-{sso,portal,admin}:<tag>`；**三镜像 tag 必须一致** |
| `ports` | string[] | `sso`: `8080`,`8081`；`portal`/`admin`: `80` |
| `healthcheck` | object | `{cmd, interval, timeout, retries, startPeriod}`；命令打 `127.0.0.1:<port>/healthz`（FR-015） |
| `dependsOn` | string[] | test/prod：无；一体化：`mysql`/`redis` 就绪后再起 `sso` |
| `envFrom` | string | test/prod：`env_file: /etc/omino-sso/<env>.env`；一体化：compose 内联默认值 |

**校验规则**

- 三个服务的镜像 tag 必须相同，否则回滚会出现"半新半旧"（DEPLOYMENT.md §5）。
- `sso` 的 healthcheck 必须打 `8081/healthz`（业务面），`8080/healthz` 只作附加检查——`:8081` 挂掉时后台完全不可用，而 OIDC 可能仍在听。
- `portal`/`admin` 的 healthcheck 打自身 `80/healthz`（nginx 返回 200 即可，不穿透到后端），避免后端抖动导致前端容器被反复重启。

---

## 3. 前端构建参数（Build Arg）

| 字段 | portal | admin | test 例 | prod 例 | 说明 |
|---|:--:|:--:|---|---|---|
| `VITE_API_BASE_URL` | — | — | 不传（默认 `/api/v1`） | 同 | FR-008：恒为同源相对值 |
| `VITE_ADMIN_HOME` | ✔ | — | `https://admin.test.example.com/` | `https://admin.example.com/` | 域名根，不带 `/admin/`（FR-005） |
| `VITE_PORTAL_HOME` | ✔ | ✔ | `https://portal.test.example.com/account/profile` | `https://portal.example.com/account/profile` | admin 用它做"返回门户" |
| `VITE_PORTAL_LOGIN_URL` | ✔ | ✔ | `https://portal.test.example.com/login` | `https://portal.example.com/login` | 未登录跳转目标 |
| `VITE_SITE_NAME` | ✔ | ✔ | `统一身份认证（测试）` | `统一身份认证` | 站点名 |

**校验**：三个 URL 型参数 MUST 为绝对地址（构建期无法校验，靠 `deploy/env/*.example` 模板 + 验收清单）；`VITE_ADMIN_HOME` 的 pathname MUST 为 `/`。

---

## 4. 运行时环境变量（新增/变更）

| 字段 | 新增 | 必填 | 默认 | 校验 | 登记位置 |
|---|:--:|:--:|---|---|---|
| `TRUSTED_PROXIES` | **新增** | 否 | 空（不信任任何代理） | 逗号分隔 CIDR/单 IP，非法即启动失败 | `config.go` / `.env.example` / `AGENTS.md §9`（`make check` 强制） |
| `COOKIE_DOMAIN` | 语义变更 | 生产必填 | 空 | prod 必须非空且为主域（`.example.com`） | 同上（示例从 `sso.example.com` 改为 `.example.com`） |
| `OIDC_ISSUER` | 不变 | 是 | — | prod 必须 https；恒等于 `https://sso.<主域>` | 同上 |
| `APP_ENV` | 不变 | 否 | `dev` | prod 触发严格校验 | 同上 |

**校验规则**：`TRUSTED_PROXIES` 的每一项 MUST 能被 `netip.ParsePrefix` 或 `netip.ParseAddr` 解析；解析失败必须在启动时报出**具体非法项**，禁止静默忽略（R11）。`prod` 下 `COOKIE_DOMAIN` MUST 非空且以 `.` 开头（不含 `://`、不含尾斜杠），否则启动失败——host-only 是新拓扑下最常见且最隐蔽的配错（见 [critiques/critique-20261023-0859.md](./critiques/critique-20261023-0859.md) E4）。

### 4.1 会话失效路径（按语义分三类，不得合并处理）

| 路径 | 触发 | 服务端 | 浏览器 |
|---|---|---|---|
| 主动登出 | `/api/v1/auth/logout`、`sso/logout` | 删 session + revoke | `Set-Cookie: sid=; Domain=.example.com; Max-Age=-1`（**必须带 Domain**） |
| 改密 | `/api/v1/me/password` | 已 `DeleteSessionsByUser`（全部会话） | 同上 |
| **验证码重置** | `/api/v1/me/password/reset` | 仅吊销**其他**设备（保留当前） | **不清** 当前 cookie |
| 会话过期 | 任意 401 | session 自然失效 | **不清** cookie（避免误杀其他域登录态），由前端跳登录页 |

> **登出与改密**必须共用 `clearSessionCookie(c, cfg)`，禁止各写一份 `http.Cookie{}`——现状 `me/profile.go:178` 就因为没有复用而不带 `Domain`。**验证码重置与会话过期不清当前 cookie**，不得并入（`ResetPasswordWithCode` 的契约是"保留当前会话"，见 `service/user.go:573-575`）。

---

## 5. Admin 前端路由（结构变更）

**Before → After**（`web/admin/src/router.ts`）

| name | before | after |
|---|---|---|
| — | `/` → redirect `/admin/users` | `/` → redirect `/users` |
| — | `/admin` → redirect `/admin/users` | （删除） |
| `users` | `/admin/users` | `/users` |
| `clients` | `/admin/clients` | `/clients` |
| `client-detail` | `/admin/clients/:clientId` | `/clients/:clientId` |
| `client-overview` | `…/:clientId/overview` | `…/:clientId/overview` |
| `client-members` | `…/:clientId/members` | 同（前缀已变） |
| `client-roles` / `client-permissions` / `client-grants` | 同上 | 同上 |
| `client-tab-fallback` | `…/:clientId/:tab` | 同上 |
| `audit` | `/admin/audit` | `/audit` |
| `files` | `/admin/files` | `/files` |
| `send-records` | `/admin/send-records` | `/send-records` |
| `offline` / `403` / `not-found` | 不变 | 不变 |

**联动字段**：`AdminLayout` 的 5 个 `el-menu-item index`、`activeMenu` 的 `startsWith` 判断、`ClientDetail` 的 tab `index` 与 fallback、`Offline.vue` 的重试回跳，全部同步去前缀。**不动** `web/admin/src/api/index.ts` 里的 `/admin/users` 等字符串——它们相对 `baseURL=/api/v1`，是 API 路径，与前端路由无关。

**状态**：无。

---

## 6. 跨域回跳（Return-To）

| 字段 | 说明 | 约束 |
|---|---|---|
| `next` | 登录后回跳地址 | 相对（`/` 开头，不得以 `/login` 或 `/offline` 开头）**或**绝对（origin ∈ 白名单） |
| `originWhitelist` | 允许的绝对回跳 origin | `[window.location.origin, new URL(VITE_ADMIN_HOME).origin]` |
| `fallback` | 无 `next` 时的落地 | `admin` 角色 → `https://admin.<主域>/`；否则 `/account/profile` |

**校验**：绝对 `next` MUST 命中白名单且协议为 http/https；不命中直接丢弃（不报错、不回跳），防止开放重定向（FR-009）。

---

## 7. 探针端点（Probe）

| 字段 | `/healthz` | `/readyz` |
|---|---|---|
| 端口 | `8080` + `8081` | `8080` + `8081` |
| 依赖检查 | 无 | MySQL + Redis（`store.Ping`，1s 超时） |
| 成功 | `200 {"status":"ok"}` | `200 {"status":"ready"}` |
| 失败 | 不会失败（进程活着就 200） | `503 {"status":"unready","failing":["mysql"｜"redis"]}` |
| 公网暴露 | 仅 `sso` 域 | **不暴露**（公网 404） |
| 响应包装 | 不套统一响应 | 不套统一响应 |

---

## 8. 编排形态（Profile）

| 形态 | 组件 | DB / Redis | 用途 | 入口 |
|---|---|---|---|---|
| `dev`（命令启动） | `go run` + `npm run dev` | 本机/容器 | 日常开发 | `:8080`/`:8081`/`:5173`/`:5174` |
| `local`（一体化 compose） | mysql + redis + sso + portal + admin | **容器内** | 演示/联调/新人上手 | `:8088`/`:8090`/`:8080` |
| `test` / `prod` | sso + portal + admin | **外部云实例** | 交付环境 | 四个域名 |

**校验**：一体化 compose MUST NOT 被用于 test/prod（`deploy/compose.*.yml` 不引用本地 mysql/redis service）。
