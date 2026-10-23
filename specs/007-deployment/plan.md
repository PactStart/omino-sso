# Implementation Plan: 容器化多域名部署

**Branch**: `007-deployment` | **Date**: 2026-10-23 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/007-deployment/spec.md`

## Summary

把「本机能跑」变成「可交付环境」：以**四域名**形态交付三个容器（`sso` 单镜像双端口、`portal`、`admin`），按环境构建前端镜像，提供 test/prod 编排 + 一体化 compose，并把 admin 前端**根路径化**。

核心形态（spec FR-001～FR-009）：

- 浏览器只访问 `portal`/`admin`/`sso` 三域；`api` 域只在 VPC 内解析，是 `:8081` 的唯一暴露点。
- 前端 XHR 全部同源相对 `/api/v1`，由各自 nginx 反代到 `api` 域（内网 HTTP）。**不引入 CORS**。
- `OIDC_ISSUER` 恒为 `https://sso.<主域>`，未登录的授权请求落 `sso/login`（协议层不动）。
- admin 路由去掉 `/admin` 前缀（`admin.<主域>/users`），vite `base` 保持 `/`。

交付顺序：

1. **后端能力**：`/healthz`/`/readyz`（两个 server，**绕过限流**）+ `TRUSTED_PROXIES`（Gin 与协议层共用一套判定）+ `prod` 的 `COOKIE_DOMAIN` 校验 + 登出/改密两条路径收敛到 `clearSessionCookie`（D10，**验证码重置不清 cookie**）+ 配置三处登记。
2. **前端根路径化**：`web/admin` 路由/菜单/tab/离线页去前缀；跨域回跳 `next` 改带绝对地址并按 `ADMIN_HOME` origin 校验。
3. **镜像与编排**：`web/Dockerfile`（双 target）、`web/nginx/{portal,admin,api}.conf` + `API_UPSTREAM` 模板、`docker-compose.yml` 补前端、`deploy/compose.{test,prod}.yml`、`deploy/env/*.example`。
4. **收口**：AGENTS.md（§2.3/§3/§4.0/§8.6/§9）、`.env.example`、`DEPLOYMENT.md` 与实现对齐，`make check` / `make docs-check` / `make test` / `make lint` / `make build-web` 全绿。

## Technical Context

**Language/Version**: Go 1.26.0（后端）；TypeScript 5.7.2 + Vue 3.5.13（前端）

**Primary Dependencies**: **后端与前端的 npm/go 依赖均零新增**。探针用标准库 `net/http` + 既有 `store.Ping`；可信代理由 `gin.SetTrustedProxies`（内置）与 `net/netip` 实现。镜像侧新增的只是基础镜像标签：`golang:1.26-alpine`（已有）、`node:22-alpine`（前端构建，LTS）、`nginx:1.27-alpine`（前端运行时）

**Storage**: MySQL 8.4 + Redis 7.4；**本特性零表结构变更**（不新增迁移文件），`store.Ping` 已覆盖 MySQL+Redis 连通性

**Testing**: `go test ./...`（`internal/config` 增 `TRUSTED_PROXIES` 解析/校验用例 + `prod` 下 `COOKIE_DOMAIN` 空值/裸域报错用例；`internal/api` 增 `/healthz` `/readyz` 状态码用例 + **探针三条隐性契约**用例；`internal/oidc` 用 `httptest` 验证探针暴露面与客户端 IP 提取的多跳反证）；portal/admin 的 `vitest`（admin 菜单与跳转去前缀、portal `next` 白名单、登出 `?loggedOut=1` 提示）；门禁 `make check`（环境变量三向一致）、`make docs-check`（探针不得进入 openapi，`openapi.json` sha256 必须与 `api-evolve/baseline/manifest.json` 一致）

**探针三条隐性契约（必须有用例固定，否则重构必被改坏）**：① `RATE_LIMIT_API_PER_MIN=1` 时连续请求 `/healthz` 仍 200（探针不受限流）；② 响应体是 `{"status":...}` 而非 `{code,message,data,traceId}`（不套统一响应）；③ `/readyz` 失败时 503 且 `failing` 只含 `mysql`/`redis`、不含错误原文

**会话失效用例（D10）**：改密后同 sid 401、登出后 401、`Set-Cookie` 带 `Domain=.example.com`；**回归保护**：验证码重置（`/me/password/reset`）后当前 sid 仍有效、其他设备 401（防止把 reset 误并进 `clearSessionCookie`）

**Target Platform**: Linux 服务器（Docker Compose 单副本）+ 浏览器（portal / admin）

**Project Type**: web-service（`:8080` 协议 + `:8081` 管理 API）+ 两个 SPA + 部署产物（Dockerfile / nginx / compose）

**Performance Goals**: 探针 P95 < 5ms（`/healthz` 纯内存、`/readyz` 仅 ping，不查业务表）；内网反代新增一跳 < 2ms；静态资源 gzip + 长缓存

**Constraints**: 不引入新依赖、不改 `/api/v1/**` 路径前缀；探针不套统一响应（宪法「技术与接口约束」）、不进 swagger、**MUST 绕过限流**（否则配额调小即重启风暴）；`TRUSTED_PROXIES` 必须同时出现在 `config.go` / `.env.example` / `AGENTS.md §9`（否则 `make check` 红）；`prod` MUST 校验 `COOKIE_DOMAIN` 非空且为主域（宪法「必填项缺失必须非零退出」）；`:8080` 与 `:8081` 各自独立中间件栈（宪法 II）

**Scale/Scope**: 单副本；4 个域名；3 个容器（sso / portal / admin）；2 套环境编排 + 1 套一体化；前端改 5 个文件 + 2 个配置；后端新增 2 个探针 handler、1 个配置字段、1 个可信代理工具；AGENTS 改 5 节

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 说明 |
|------|------|------|
| I. 协议层单一来源 | PASS | 只在 `internal/oidc/server.go` 的 mux 上加两个探针路由，不触碰 `op.Provider`、不手写任何协议状态机；`/login`、`/logout` 行为不变（FR-002） |
| II. 端口与信任边界物理分离 | PASS | `:8080` 仍只挂 mux + Provider；`:8081` 仍只挂 Gin。探针各自实现、各自注册，且**不套统一响应包装**（探针是运维面，不是 `/api/v1`） |
| III. 分层与数据一致性 | PASS | 就绪探针只调 `store.Ping(ctx)`（既有方法，廉价无事务）；不新增 SQL、不改授权码/令牌链路 |
| IV. 凭证不出服务端 | PASS | 新增产物只有静态站点与 nginx 配置；env 模板不含真实密钥；`deploy/env/*.example` 只放占位符 |
| V. 结构变更只走手写 SQL 迁移 | PASS | 零表结构变更，不新增迁移文件，不触发 `AutoMigrate` |
| 技术与接口约束 | PASS（有条件） | ① `/api/v1/**` 前缀与统一响应不变；② 探针路径必须在 `AGENTS.md §4.0` 端口/路径表中登记（check-registry 只比对 `/api/v1`，登记靠 review）；③ `TRUSTED_PROXIES` 三处登记；④ 新增文件不得引入 AGENTS §7.2 非目标（无 K8s/Helm、无多副本）；⑤ 探针不套统一响应且绕过限流；⑥ `prod` 必填项（`COOKIE_DOMAIN`）缺失必须非零退出 |

**结论**：无违规，无需 Complexity Tracking 例外说明（见文末留空表）。

## 关键设计决策与取舍

**D1 探针挂在两个 server 的根路径，不进 `/api/v1`、不进 openapi、不受限流。**
`/healthz`、`/readyz` 不是业务接口，套 `{code,message,data,traceId}` 会让 LB/运维工具多一层解析，也会把运维面混进对外 API 契约。`:8081` 用 `r.GET` 而非 `v1.Group` 注册（`scripts/check-registry.sh` 只比对 `/api/v1*`，因此不会要求写进 §4.1）；同时**不加 swag 注解**，`openapi.json` 零变化，`make docs-check` 保持绿。登记位置改为 `AGENTS.md §4.0` 的端口/路径表（人工核对项）。

**限流短路（critique E3）**：`router.go` 的 `r.Use(middleware.RateLimit(...))` 是**全局**中间件，`middleware/ratelimit.go` 目前只对 `/docs/` 短路。必须把 `/healthz`、`/readyz` 加进同一处短路——探活是运维动作，不能被业务配额化；否则 `RATE_LIMIT_API_PER_MIN` 调小（或探活频率提高）时探针 429 → 容器 unhealthy → 重启风暴。响应同时带 `Cache-Control: no-store`，避免中间层缓存 200 掩盖故障。

**D2 可信代理做一套判定，两条链路共用。**
`:8081` 用 `gin.Engine.SetTrustedProxies(...)`（Gin 内置的 `ClientIP()` 才有意义）；`:8080` 的 `login.go:clientIP()` 当前**无条件信任 `X-Forwarded-For`**，等于任何人可伪造审计 IP（现状缺陷，本特性顺手修到同一口径）：仅当 `RemoteAddr` 命中可信网段时，从右向左取第一个非可信地址；否则用 `RemoteAddr`。实现放 `internal/pkg/clientip`，Gin 与 `net/http` 两侧都调它。`TRUSTED_PROXIES` 为空时：Gin 传 `nil`（不信任任何代理），协议层退化为只看 `RemoteAddr`。

**多跳链路（critique E7）**：生产是 `浏览器 → 入口 LB → portal/admin nginx → api 入口 → sso:8081`，XFF 至少两跳。`TRUSTED_PROXIES` 必须列全**所有**反代网段（入口 LB + 前端容器网段 + api 入口网段）；漏一段时算法会取到"第一个不可信地址"= 前端容器 IP，审计 IP 变成 `172.x` 且**看起来像是配好了**。quickstart 必须有这条反证。

**D3 admin 去前缀不留兼容重定向。**
spec FR-004 明确 MUST NOT 再出现 `/admin/*`；后台尚未上线，书签/外链没有存量。保留 `/:pathMatch(.*)*` 兜底（404 页）即可，避免"两套路由并存"带来的长期歧义。`portal` 的 `ADMIN_HOME` 缺省值同步改为域名根（dev 为 `http://localhost:5174/`）。

**D4 跨域回跳 `next` 携带绝对地址，按 `ADMIN_HOME` origin 校验。**
现状 admin 跳门户登录时传的是相对路径（`/users`），门户登录成功后 `router.replace('/users')` 会在**门户**域名下找路由 → 落 404，跨域回跳实际是坏的（spec FR-009 要求回到 admin 原路径）。改为：admin 传 `next=<origin><path>`；门户侧白名单 = `[window.location.origin, new URL(ADMIN_HOME).origin]`，命中则 `window.location.assign(next)`（整页跳转），否则丢弃 `next`。相对路径沿用现有"必须 `/` 开头"的规则。登录页副标题同步支持绝对地址的展示（只显示 pathname）。

**D5 nginx 反代目标用模板 + `envsubst` 注入，浏览器侧永远相对。**
`API_UPSTREAM`（如 `http://sso:8081` 或 `http://api.example.com`）通过官方 nginx 镜像的 `/etc/nginx/templates/*.template` 机制在容器启动时渲染；镜像内不出现环境域名，同一镜像可在 test/prod 复用。前端 `VITE_API_BASE_URL` 固定 `/api/v1`，与 dev 的 vite 代理语义一致（FR-008）。

**必须用官方模板机制，禁止裸 `envsubst`（critique E6）**：裸 `envsubst < tpl > conf` 会把 `$host`、`$uri`、`$remote_addr` 一并替换成空串，产生"看起来生效、实际头全错"的静默故障；官方 `20-envsubst-on-templates.sh` 只替换已定义的环境变量。

**启动期解析（critique E5）**：`proxy_pass http://api.<主域>;` 是静态主机名，nginx 在**启动时**解析，DNS 或后端未就绪会直接退出 → restart 循环。一体化用 `depends_on: {sso: {condition: service_healthy}}`，test/prod 用 `restart: unless-stopped` 并在排障表登记该现象；本期不引入 `resolver` + 变量式 `proxy_pass`（避免 DNS 抖动期间请求失败）。

**D6 前端镜像一个 Dockerfile 两个 target，构建参数注入 `VITE_*`。**
`web/Dockerfile` 内 `portal` / `admin` 两个 target：`node:22-alpine` 构建 → `nginx:1.27-alpine` 运行，各自 COPY 对应 `nginx/*.conf`。按环境构建（不注入运行时 config），与 spec「不做 config.js 注入」一致。两个 conf 都必须含 `location = /healthz { access_log off; return 200 "ok"; }`（否则容器 healthcheck 恒 404 → 永远 unhealthy，critique E10）。

**D7 一体化 compose 用端口而非域名，但构建参数必须按 dev 注入。**
本地不引入 hosts/DNS：`portal:8088`、`admin:8090`、`api(=sso):8081`、`sso:8080`，四个端口共用 `localhost` 主机名（cookie 不区分端口），`COOKIE_DOMAIN` 留空即可共享会话；前端直接反代 `http://sso:8081`。

**一体化 compose 的 `build.args`（critique E2，漏了就直接坏 US3）**：

| 服务 | build.args |
|---|---|
| `portal` | `VITE_ADMIN_HOME=http://localhost:8090/`、`VITE_PORTAL_HOME=http://localhost:8088/account/profile`、`VITE_PORTAL_LOGIN_URL=http://localhost:8088/login`、`VITE_SITE_NAME=统一身份认证` |
| `admin` | `VITE_PORTAL_HOME=http://localhost:8088/account/profile`、`VITE_PORTAL_LOGIN_URL=http://localhost:8088/login`、`VITE_SITE_NAME=统一身份认证` |

不传则产物里是 dev 缺省值（`:5174` / `:5173`），点「去后台」与「会话失效回跳」都会跳到空端口。

**D8 探针暴露面在 nginx 层收敛。**
`sso` 域只放行 `/healthz`（`location = /healthz`），`/readyz` 不写 location → 命中 SPA 兜底或 404，符合 FR-015「公网 `/readyz` 404」。`api` 域整体公网不可达，因此其探针自然只在容器网络内。

**两个端口都提供 `/readyz`（复核 N7 决策①）**：`:8080` 与 `:8081` 各实现一份 `/healthz` + `/readyz`。容器 healthcheck 只打 `:8081/healthz`，公网只放行 `/healthz`；保留 `:8080/readyz` 是为了"任一端口的依赖状态都能就地查"，且与 FR-015「两个 HTTP 服务都提供」保持一致。实现上共用同一个 `store.Ping` 包装，避免两份逻辑漂移。

**D9 不新增 CORS 中间件。**
`api` 域只被 nginx 调用，浏览器不直连；若将来需要跨源，另立需求（spec Non-Goals）。这条要在 review 时明确，避免"顺手加个 CORS 更安全"的反向改动。

**D10 会话失效两条路径统一到 `clearSessionCookie`（critique E1 / 复核 N1、N2）。**
新拓扑把 `COOKIE_DOMAIN` 强制成 `.example.com` 后，任何"不带 `Domain` 的清 cookie"都会失效——它只会创建一个 host-only 空 cookie，`.example.com` 上的真 cookie 仍在。

**问题范围必须收窄**：`UserService.ChangePassword`（`internal/service/user.go:371`）**已经**"随后吊销全部会话"，服务端侧没问题；残留缺陷只有 `internal/api/me/profile.go:178` 的 `http.Cookie{Name:"sid", Value:"", Path:"/", MaxAge:-1}` **缺 `Domain` 与 `SameSite`**。因此修复只需把清 cookie 收敛，**不得在 handler 里再加一次 revoke，也不要改 service**。

落点（**仅两条路径**）：

- 在 `internal/api/auth/login.go`（与 `setCookie` 同文件）增加 `clearSessionCookie(c, cfg)`，复用 `CookieDomain`/`Secure`/`SameSite`；
- `Logout` 与 `ChangePassword` 调用它（revoke 仍由各自既有逻辑完成：`Logout` 删 session、`ChangePassword` 走 `DeleteSessionsByUser`）；
- 会话过期的 401 路径不清 cookie（避免误杀其他域的登录态），只由前端跳登录页。

**反例（不得纳入统一处理）**：`/api/v1/me/password/reset`（`ResetPasswordWithCode`）的契约是「**保留当前会话**、仅吊销其他设备会话」（`internal/service/user.go:573-575`、`internal/api/me/password.go:24`）。它**不清**当前 `sid` cookie；若把它并进 `clearSessionCookie`，用户用验证码重置密码后会被踢下线，且 openapi 的 `@Description` 会与真实行为不符——`make docs-check` 抓不到这类描述漂移。

用例：

1. 改密 → 同一 sid 请求 `/api/v1/me` 必须 401；响应 `Set-Cookie` 带 `Domain=.example.com`；
2. 登出 → 同上；
3. **回归保护**：验证码重置（`/me/password/reset`）→ 当前 sid **仍有效**（`/me` 200），其他设备会话 401。

## Project Structure

### Documentation (this feature)

```text
specs/007-deployment/
├── plan.md                        # 本文件
├── research.md                    # Phase 0：D1–D9 的调研与备选方案对比
├── data-model.md                  # Phase 1：部署单元/域名/路由/配置实体（无 DB 变更）
├── quickstart.md                  # Phase 1：四域名与一体化的可执行验收步骤
├── contracts/
│   ├── nginx-routing.md           # 四域 server block 契约（路径→upstream、缓存、SPA fallback、探针）
│   ├── health-probes.md           # /healthz /readyz 的报文与暴露面契约
│   ├── env-and-build.md           # 运行时环境变量与构建参数契约（含 TRUSTED_PROXIES）
│   └── admin-routes.md            # admin 根路径路由表与跨域回跳契约
├── checklists/                    # 由 /speckit.checklist 生成（本命令不产出）
└── tasks.md                       # 由 /speckit.tasks 生成（本命令不产出）
```

### Source Code (repository root)

```text
internal/
├── config/
│   ├── config.go                  # +TrustedProxies 解析；+prod 校验 CookieDomain 非空且以 "." 开头
│   └── config_test.go             # +用例：CIDR/单 IP/非法值/空值；prod 下 CookieDomain 空/host-only 报错
├── pkg/
│   └── clientip/
│       ├── clientip.go            # +FromRequest(r, prefixes) 可信代理解析（新文件）
│       └── clientip_test.go       # +用例：直连/单跳/多跳/伪造 XFF/漏配网段
├── api/
│   ├── router.go                  # +SetTrustedProxies；+r.GET("/healthz"), r.GET("/readyz")
│   ├── health.go                  # +探针 handler（新文件，统一响应不参与，带 Cache-Control: no-store）
│   ├── router_test.go             # +探针状态码；+RATE_LIMIT_API_PER_MIN=1 时探针不被限；+响应非统一信封
│   ├── middleware/
│   │   └── ratelimit.go           # +对 /healthz、/readyz 短路（与 /docs/ 同处）
│   ├── auth/
│   │   └── login.go               # +clearSessionCookie(c,cfg)；Logout 复用
│   └── me/
│       └── profile.go             # 改密后走 clearSessionCookie + sessions.Revoke（修 E1）
└── oidc/
    ├── server.go                  # +mux.HandleFunc("/healthz"), ("/readyz")
    ├── health.go                  # +探针 handler（新文件，复用 store.Ping，no-store）
    ├── login.go                   # clientIP() 委托 internal/pkg/clientip；logout 复用统一清 cookie
    └── health_test.go             # +暴露面（公网 /readyz 404 由 nginx 保证）与 IP 提取用例（新文件）

web/
├── Dockerfile                     # +前端镜像（portal / admin 两个 target）
├── nginx/
│   ├── portal.conf                # +静态托管 + /api/ 反代 + SPA fallback + location = /healthz
│   ├── admin.conf                 # +同上（根路径）+ location = /healthz
│   ├── api.conf                   # +api 域 server（仅内网，不配证书）
│   └── templates/
│       ├── portal.conf.template   # +含 ${API_UPSTREAM} 的反代片段（只用官方 envsubst 机制）
│       └── admin.conf.template
├── admin/src/
│   ├── router.ts                  # 路由去 /admin 前缀（/ → /users）
│   ├── layouts/AdminLayout.vue    # 菜单 index 与高亮判断去前缀
│   ├── views/Offline.vue          # 重试回跳 /users
│   ├── views/client/ClientDetail.vue  # tab index 去前缀
│   ├── config.ts                  # portalLoginUrl 传绝对 next（D4）
│   └── views/*.spec.ts            # +菜单/跳转断言
└── portal/src/
    ├── config.ts                  # ADMIN_HOME dev 缺省改 :5174/
    ├── router.ts                  # next 白名单（本站 origin + ADMIN_HOME origin）
    └── views/Login.vue            # 绝对 next 的展示与跳转；+读 ?loggedOut=1 显示"已退出"
    └── views/Login.spec.ts        # +绝对 next 用例、+loggedOut 提示用例

deploy/
├── compose.test.yml               # +外部云 DB/Redis + 三容器（restart: unless-stopped）
├── compose.prod.yml               # +
└── env/
    ├── test.env.example           # +四域名、issuer、cookie、TRUSTED_PROXIES、通道占位
    └── prod.env.example

scripts/
├── check-registry.sh              # +新增一条：openapi.json 的 sha256 与 api-evolve/baseline/manifest.json 比对（复核 N4）
└── smoke-deploy.sh                # +只读冒烟：探针/深链/同源/api 域不可达/无 OPTIONS（critique E9）

docker-compose.yml                 # +portal/admin 服务，+API_UPSTREAM 注入
.env.example                       # +TRUSTED_PROXIES
AGENTS.md                          # §2.3 基础设施 / §3 目录 / §4.0 路径表 / §8.6 拓扑 / §9 变量
DEPLOYMENT.md                      # 实现完成后回填"已具备/待实现"状态
```

**Structure Decision**：沿用现有单体仓库分层（`cmd/` + `internal/` + `web/` + `migrations/`）；部署产物放在仓库根与新增 `deploy/`、`web/nginx/`，不新建子模块。探针与客户端 IP 放 `internal/pkg/` 与 `internal/api`/`internal/oidc` 各自目录，避免 api 层与 oidc 层互相 import（层级规则）。

## Phase 0 输出

见 [research.md](./research.md)：逐条固化 D1–D9 的决策、理由与被否方案（含"为什么不用 nginx 模板引擎/不加 CORS/不做运行时配置注入"）。

## Phase 1 输出

- [data-model.md](./data-model.md)：部署单元、域名/路由、配置与编排实体；明确"零表结构变更"。
- [contracts/](./contracts/)：四域路由契约、探针契约、环境变量与构建参数契约、admin 路由与回跳契约。
- [quickstart.md](./quickstart.md)：本地一体化 + test/prod 两条验收路径（含反证项：公网 `api` 域连不通、`/readyz` 404、无 OPTIONS 预检）。

## 设计后 Constitution 复查

| 原则 | 复查结论 |
|------|----------|
| I / II | PASS：探针不进 `op.Provider`，两端口中间件栈仍独立；nginx 不参与协议报文 |
| III | PASS：仅新增 `clientip` 工具与探针，无新 SQL、无跨层调用（`api` 直接调 `store.Ping` 属既有装配方式，`store.Ping` 本就是给 `op.Storage.Health` 用的） |
| IV | PASS：`deploy/env/*.example` 全占位符；nginx 不落 token |
| V | PASS：无迁移 |
| 技术与接口约束 | PASS：`TRUSTED_PROXIES` 三处登记、探针登记 §4.0、`/api/v1` 与统一响应不变 |

## Complexity Tracking

> 无 Constitution 违规，本节留空（保留表头以便后续变更时填写）。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
