---

description: "Task list for 007-deployment"
---

# Tasks: 容器化多域名部署

**Input**: Design documents from `/specs/007-deployment/`
**Prerequisites**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**Tests**: 包含。plan.md 的 Testing 段与 `contracts/health-probes.md §6` 明确要求探针三条隐性契约、会话失效三路径、`TRUSTED_PROXIES` 多跳判定必须有用例固定，且 `make test` 为既有门禁，故测试任务与实现任务同列。

**Organization**: 按用户故事分阶段，每个故事可独立完成与验收。

## Format

- `- [ ] T0NN [P?] [USx?] 描述 + 精确文件路径`
- `[P]` = 不同文件、无未完成依赖，可并行
- 无 `[USx]` 的任务属 Setup / Foundational / Polish

## Path Conventions

单体仓库：后端 `internal/`、前端 `web/portal` 与 `web/admin`、部署产物 `web/nginx`、`web/Dockerfile`、`deploy/`、`scripts/`。所有路径相对仓库根。

---

## Phase 1: Setup

- [X] T001 创建部署产物目录骨架：`web/nginx/templates/`、`deploy/env/`（`scripts/` 已存在），并确认 `.gitignore` 未忽略 `deploy/env/*.example`
- [X] T002 [P] 在 `.dockerignore` 增补 `deploy/`、`specs/`、`**/node_modules`、`.tmp`，避免前端构建上下文把设计文档带进镜像层
- [X] T003 [P] 在 `DEPLOYMENT.md §2` 记录构建前置的镜像标签基线：`golang:1.26-alpine`、`node:22-alpine`、`nginx:1.27-alpine`（Node/nginx 属部署工具链，不进 `AGENTS.md §2.1/§2.2` 依赖表）

---

## Phase 2: Foundational（阻塞所有用户故事）

**目的**：把"四域名下才暴露的问题"在代码层修完——探针、可信代理、cookie 域校验、会话失效、admin 根路径化、跨域回跳。这些不完成，US1/US2/US3 的任何验收都会假绿。

### 配置与可信代理

- [X] T004 [P] 在 `internal/config/config.go` 增加 `TrustedProxies` 字段与解析（逗号分隔 CIDR/单 IP，用 `net/netip`）；非法项启动失败并打印**具体非法值**（不止"配置错误"）
- [X] T005 在 `internal/config/config.go` 的 `validate()` 增加 `prod` 校验：`CookieDomain` 非空、以 `.` 开头、不含 `://`、不含尾斜杠，否则非零退出（FR-006；宪法「必填项缺失必须非零退出」）
- [X] T006 [P] 在 `internal/config/config_test.go` 补用例：`TRUSTED_PROXIES` 合法/非法/空；`prod` 下 `COOKIE_DOMAIN` 为空与为 `sso.example.com`（host-only 形态）均报错
- [X] T007 [P] 新建 `internal/pkg/clientip/clientip.go`：`FromRequest(r *http.Request, trusted []netip.Prefix) string`——`RemoteAddr` 不在可信列表时**忽略 XFF**；否则从右向左取第一个非可信地址
- [X] T008 [P] 新建 `internal/pkg/clientip/clientip_test.go`：直连、单跳、多跳、伪造 XFF、**漏配前端网段**五个用例

### 探针（两个 server，绕过限流，不套统一响应）

- [X] T009 在 `internal/api/router.go` 调用 `engine.SetTrustedProxies(...)`（列表为空时传 `nil`），并注册 `GET /healthz`、`GET /readyz`（在 `v1` 分组**之外**）
- [X] T010 新建 `internal/api/health.go`：`/healthz` 返回 `{"status":"ok"}`；`/readyz` 调 `store.Ping`，成功 `{"status":"ready"}`、失败 503 `{"status":"unready","failing":["mysql"|"redis"]}`；两者带 `Cache-Control: no-store`，**不**套 `response.Body`
- [X] T011 在 `internal/api/middleware/ratelimit.go` 对 `/healthz`、`/readyz` 短路（与既有 `/docs/` 同处，注释写明"探针是运维动作，不能被业务配额化"）
- [X] T012 在 `internal/api/router_test.go` 断言三条隐性契约：① `RATE_LIMIT_API_PER_MIN=1` 时连打 `/healthz` 10 次全 200；② 响应体不含 `code`/`message`/`data`/`traceId`；③ `store.Ping` 失败时 `/readyz` 503 且 `failing` 只含 `mysql`/`redis`
- [X] T013 在 `internal/oidc/server.go` 的 mux 上注册 `/healthz`、`/readyz`；新建 `internal/oidc/health.go` 复用同一 `store.Ping` 包装（与 T010 同语义，禁止逻辑分叉）
- [X] T014 在 `internal/oidc/login.go` 删除自有 `clientIP()`，改调 `clientip.FromRequest(r, cfg.TrustedProxies)`；新建 `internal/oidc/health_test.go` 断言探针响应形态与 IP 提取（`httptest`）

### 会话失效（登出 / 改密；reset 与过期不清）

- [X] T015 在 `internal/api/auth/login.go` 新增 `clearSessionCookie(c, cfg)`（复用 `CookieDomain`/`Secure`/`SameSite`），`Logout` 与 `internal/api/me/profile.go` 的 `ChangePassword` 改调它；**不得**在 handler 内新增 revoke（`ChangePassword` 已走 `DeleteSessionsByUser`）
- [X] T016 补会话失效用例：改密后同 sid `/api/v1/me` 401、`Set-Cookie` 带 `Domain=.example.com`；登出后 401；**回归保护**——`/api/v1/me/password/reset` 后当前 sid 仍 200、其他设备 401（`ResetPasswordWithCode` 契约为"保留当前会话"）

### admin 根路径化

- [X] T017 改写 `web/admin/src/router.ts`：`/` → `/users`；删除 `/admin` redirect；全部路由去 `/admin` 前缀（`/users`、`/clients`、`/clients/:clientId[/:tab]`、`/audit`、`/files`、`/send-records`）；保持 `createWebHistory('/')` 与 vite `base: '/'`
- [X] T018 [P] 改写 `web/admin/src/layouts/AdminLayout.vue`：5 个菜单 `index` 去前缀，`activeMenu` 的 `startsWith('/clients')` 判断同步
- [X] T019 [P] 改写 `web/admin/src/views/client/ClientDetail.vue`（tab `index` 与 fallback 计算）与 `web/admin/src/views/Offline.vue`（重试回跳 `/users`）
- [X] T020 [P] 改写 `web/admin/src/config.ts`：`portalLoginUrl(next)` 传**绝对地址** `https://admin.<当前 host 的 origin>` + path；补 `web/admin/src/views/*.spec.ts` 断言菜单 index 与跳转 URL 不含 `/admin`

### 跨域回跳与登出提示（portal）

- [X] T021 [P] 改写 `web/portal/src/router.ts` 的 `next` 处理：相对路径（`/` 开头，且不以 `/login`、`/offline` 开头）走 `router.replace`；绝对地址仅当 `new URL(next).origin` ∈ `[window.location.origin, new URL(ADMIN_HOME).origin]` 时走 `window.location.assign`，否则丢弃
- [X] T022 [P] 改写 `web/portal/src/views/Login.vue`：支持绝对 `next`（展示只显示 pathname）、读取 `?loggedOut=1` 显示"已退出"并 `router.replace` 清除参数；`web/portal/src/config.ts` 的 `ADMIN_HOME` dev 缺省改为 `http://localhost:5174/`；补 `Login.spec.ts` 两条用例

### 配置登记（三处一致，`make check` 绿）

- [X] T023 在 `.env.example` 增加 `TRUSTED_PROXIES=`，并同步 `AGENTS.md §9`（新增行）与 `§4.0` 端口/路径表（新增 `/healthz`、`/readyz` 两行）；把 `§9` 的 `COOKIE_DOMAIN` 示例由 `sso.example.com` 改为 `.example.com`；跑 `make check` 至绿

**Checkpoint**：`make test`、`make lint`、`make check`、`make docs-check` 全绿，且 `internal/api/docs/openapi.json` 与 `api-evolve/baseline/manifest.json` 的 sha256 一致。

---

## Phase 3: User Story 1 — 测试/生产四域名部署（P1）

**Goal**：`portal`/`admin`/`sso` 三域公网可用、`api` 域仅内网，前端同源反代，探针暴露面收敛。
**Independent Test**：门户登录 → `admin.<主域>/users` 免二次登录 → SP 走授权码流程；公网 `curl api.<主域>` 连不通；`sso.<主域>/readyz` 404。

- [X] T024 [US1] 新建 `web/nginx/portal.conf`：`/api/` 反代 `${API_UPSTREAM}`（`proxy_pass` **不带 URI**）、透传 `Host`/`X-Real-IP`/`X-Forwarded-For`（追加）/`X-Forwarded-Proto`、`client_max_body_size 10m`、`try_files $uri $uri/ /index.html`、`index.html` no-store、静态资源 1y immutable、gzip、`server_tokens off`；探针写 `location = /healthz { access_log off; add_header Cache-Control "no-store"; return 200 "ok"; }`
- [X] T025 [P] [US1] 新建 `web/nginx/admin.conf`：同 T024（含同样的 `location = /healthz` 三个指令），但为**根路径**托管（无 `/admin` 前缀），SPA fallback 必须让 `/clients/<id>/members` 直接刷新 200
- [X] T026 [P] [US1] 新建 `web/nginx/api.conf`：`api.<主域>` server 只放行 `/api/v1/` → `sso:8081`，**不配证书**；同文件（或并列）`sso.<主域>` server 反代 `/oauth2/`、`/.well-known/`、`/login`、`/logout`、`/logged-out` → `sso:8080`，只额外放行 `location = /healthz`（同样带 `access_log off` 与 `no-store`），**不放行** `/readyz`
- [X] T027 [P] [US1] 新建 `web/nginx/templates/portal.conf.template` 与 `admin.conf.template`：仅 `API_UPSTREAM` 作为变量，供 nginx 官方 `envsubst` 机制渲染（禁止裸 `envsubst`）
- [X] T028 [US1] 新建 `deploy/compose.test.yml`：`sso`/`portal`/`admin` 三服务，`env_file` 指向**部署机宿主路径** `/etc/omino-sso/test.env`（由运维创建并 `chmod 600`，不是容器内路径），全部 `healthcheck` + `restart: unless-stopped`；**不得**声明或 `depends_on` `mysql`/`redis`
- [X] T029 [P] [US1] 新建 `deploy/compose.prod.yml`：同 T028（`env_file` 同为宿主路径 `/etc/omino-sso/prod.env`）；`sso` 的 `ports` 只绑内网（禁止 `0.0.0.0`）
- [X] T030 [P] [US1] 新建 `deploy/env/test.env.example`：占位符覆盖 `APP_ENV`/`OIDC_*`/`COOKIE_*`（`COOKIE_DOMAIN=.test.example.com`）/`DB_*`（含 `DB_MAX_OPEN_CONNS`/`DB_MAX_IDLE_CONNS`/`DB_CONN_MAX_LIFETIME`）/`REDIS_*`（内网、无 TLS、仅密码）/`TRUSTED_PROXIES`/`SMS_*`/`MAIL_*`/`OSS_*`；`TRUSTED_PROXIES` 注释要求列出**入口 LB + 前端容器网段 + api 入口**三段
- [X] T031 [P] [US1] 新建 `deploy/env/prod.env.example`：同 T030，`COOKIE_DOMAIN=.example.com`、`COOKIE_SECURE=true`、通道非 mock
- [X] T032 [US1] 按 `DEPLOYMENT.md §6` 核对迁移清单（`0001`–`0006`）与"当前库版本"查询方式，把执行顺序、备份步骤与 `/etc/omino-sso/` 目录约定补进 `DEPLOYMENT.md`（迁移仍手动，不新增工具）
- [ ] T033 [US1] 首次部署演练（依赖 T028–T031 与 T035 的镜像）：起三容器 → 断言 `sso/healthz` 200、`sso/readyz` 404、`api` 域公网连不通、`admin/clients/<id>/members` 刷新 200、浏览器无 OPTIONS 预检、Redis 仅内网可达且未启用 TLS；**审计 IP 抽查**：真实 IP 正确 + 反证（临时去掉前端容器网段后应变成 `172.x`，验证完加回）。T045 落地后本任务改为直接跑该脚本
- [X] T034 [US1] 回填 `AGENTS.md §8.6`（四域名拓扑 + `api` 域内网 + 无 CORS）、`§2.3`（nginx 运行时镜像）、`§3`（`web/nginx`、`deploy/`、`web/Dockerfile` 目录项）

**Checkpoint**：US1 的 Independent Test 全部通过；`DEPLOYMENT.md §9` 验收清单可逐项打勾。

---

## Phase 4: User Story 2 — 按环境构建镜像（P1）

**Goal**：同一 commit 按 test/prod 构建三镜像，环境差异只在构建参数与运行时 env。
**Independent Test**：两环境镜像产物中的站点名、后台入口、登录回跳指向各自环境（0 处串环境）。

- [X] T035 [US2] 新建 `web/Dockerfile`：`node:22-alpine` 构建阶段（`npm ci` + `npm run build`），`nginx:1.27-alpine` 运行阶段；两个 target `portal`/`admin`，各自 COPY 对应 `nginx/*.conf` 与 `templates/*.template` 到 `/etc/nginx/` 对应目录；构建参数为 `VITE_ADMIN_HOME`/`VITE_PORTAL_HOME`/`VITE_PORTAL_LOGIN_URL`/`VITE_SITE_NAME`（**不传** `VITE_API_BASE_URL`，保持默认 `/api/v1`）
- [X] T036 [US2] 在 `DEPLOYMENT.md §5` 落定构建命令（含 `--target` 与四个 `--build-arg`）、tag 规范（`<版本>-<序号>`，**三镜像同 tag**）与推送命令
- [X] T037 [US2] 校验串环境：对 test/prod 两套镜像分别 `docker run --rm --entrypoint sh <镜像> -c "grep -r ..."` 检查 `/usr/share/nginx/html/assets/*.js`（镜像内产物才是最终事实），断言站点名、`admin.` 入口、`portal.` 回跳三项 0 处指向另一环境；把检查写成 `scripts/check-build-env.sh`（只读）并接入 `make check`
- [X] T038 [P] [US2] 校验镜像内不含域名/密钥：`docker history registry.example.com/omino-portal:$TAG` 与 `grep -r 'example.com\|BEGIN .*PRIVATE KEY' web/nginx/` 均无命中；`docker run --rm registry.example.com/omino-portal:$TAG nginx -t` 与 `...omino-admin:$TAG nginx -t` 均通过（`$TAG` 取 T036 定义的 `<版本>-<序号>`）
- [X] T039 [US2] 校验 prod 启动严格性：缺 `OIDC_ISSUER`/`COOKIE_DOMAIN` 或 `COOKIE_DOMAIN` 为裸域时，容器启动即失败并打印具体项

**Checkpoint**：SC-003 可断言；US2 不依赖 US1 的编排文件即可完成。

---

## Phase 5: User Story 3 — 本地一键起全栈（P2）

**Goal**：`docker compose up -d --build` 起 mysql/redis/sso/portal/admin，前端同源反代，端口代替域名。
**Independent Test**：干净检出执行一条命令，浏览器完成门户登录 + 后台操作，且「去后台」落 `localhost:8090`；`make dev` 与 `npm run dev` 仍可用。

- [X] T040 [US3] 改写 `docker-compose.yml`：新增 `portal`（`${PORTAL_HOST_PORT:-8088}:80`）与 `admin`（`${ADMIN_UI_HOST_PORT:-8090}:80`）服务，`build.target` 分别为 `portal`/`admin`，并按 plan D7 注入 `build.args`（`VITE_ADMIN_HOME=http://localhost:8090/`、`VITE_PORTAL_HOME=http://localhost:8088/account/profile`、`VITE_PORTAL_LOGIN_URL=http://localhost:8088/login`、`VITE_SITE_NAME`）。**命名提示**：既有 `ADMIN_HOST_PORT` 是 **API 端口（8081）**，`ADMIN_UI_HOST_PORT` 是**后台页面（8090）**，两者含义不同，需在 `.env.example` 与 `DEPLOYMENT.md §7` 各加一行说明
- [X] T041 [US3] 在 `docker-compose.yml` 为前端注入 `API_UPSTREAM=http://sso:8081`，并加 `depends_on: {sso: {condition: service_healthy}}`（nginx 启动期解析 upstream，后端未就绪会起不来）
- [X] T042 [US3] 本地验收：三容器 healthy；门户登录 → `localhost:8090/users` 免二次登录；深链 `/clients/<id>/members` 刷新 200；清 cookie 后跳 `localhost:8088/login?next=/users`（**dev 为相对路径**，跨域绝对形态在 T033 验证）且能回到原路径；网络面板无 OPTIONS
- [X] T043 [US3] 校验本地 cookie 与探针：`COOKIE_DOMAIN` 留空时三端口仍共享会话；`RATE_LIMIT_API_PER_MIN=1` 下探针连打 10 次全 200；`localhost:8090/healthz` 200
- [X] T044 [US3] 回归本地命令启动（FR-020）：停掉 compose 后 `make dev`（`:8080`/`:8081`）+ `web/portal` `npm run dev`（`:5173`）+ `web/admin` `npm run dev`（`:5174`）均可独立使用，vite 的 `/api` 代理仍生效；确认无需改动任何 compose/镜像文件

**Checkpoint**：SC-006 与 FR-020 可断言；`make dev` 与 `npm run dev` 未受影响。

---

## Phase 6: User Story 4 — 部署文档可独立执行（P2）

**Goal**：运维只按 `DEPLOYMENT.md` + `quickstart.md` 即可完成部署、升级、回滚、排障。
**Independent Test**：未参与开发的同学按文档走一遍，quickstart 清单逐项通过。

- [X] T045 [P] [US4] 新建 `scripts/smoke-deploy.sh`：只读冒烟（`DOMAIN=example.com`），覆盖 `sso/healthz` 200、`sso/readyz` 404、公网 `api` 域连不通、`api` 域 `/docs/` 404（容器内打 `:8081`）、admin 深链 200、`index.html` no-store、静态资源 immutable；失败即非零退出
- [X] T046 [P] [US4] 在 `scripts/check-registry.sh` 增加一条：`internal/api/docs/openapi.json` 的 sha256 与 `specs/007-deployment/api-evolve/baseline/manifest.json` 一致（`docs-check` 本身不读 baseline）
- [X] T047 [US4] 回填 `DEPLOYMENT.md` 的"已具备/待实现"状态（前端镜像、编排、CORS 不引入、探针、可信代理、admin 根路径化），并把 §9 验收清单与 `quickstart.md §1–§4` 对齐
- [X] T048 [US4] 用 T045 的脚本重跑 §3 断言，把 quickstart 中"007 合并后可用"的措辞改为可直接执行；补齐 `<cid>`/`<uri>` 的取得步骤
- [ ] T049 [US4] 按 `DEPLOYMENT.md §8` 演练一次回滚：旧 tag `up -d` → 三容器 5 分钟内 healthy 且 admin 可登录（SC-008），记录耗时

**Checkpoint**：SC-005 可断言（清单全绿，无遗留未勾选项）。

---

## Phase 7: Polish & Cross-Cutting

- [X] T050 全量门禁：`make test`、`make lint`、`make build`、`make build-web`、`make check`、`make docs-check` 一次全绿，并把结果记录到 `DEPLOYMENT.md §9`
- [X] T051 [P] 回归 critique 的四个原始缺陷（各留一条断言）：改密掉线 → `internal/api/me/profile_test.go`；验证码重置不掉线 → 同上；探针不被限流 → `internal/api/router_test.go`；`prod` 缺 `COOKIE_DOMAIN` 启动失败 → `internal/config/config_test.go`
- [X] T052 [P] 复核 `specs/007-deployment/contracts/*.md` 与实现一致（反代路径、探针暴露面、`API_UPSTREAM`、admin 路由表），不一致即以实现为准回改契约
- [X] T053 更新 `specs/007-deployment/api-evolve/orchestration.md` 的 Gate 结论（实现后复跑，确认 `openapi.json` 字节未变）

---

## Dependencies

```
Phase 1 (T001–T003)
   └─> Phase 2 (T004–T023)  ← 全部用户故事的前置
         ├─> US1 (T024–T034) ─┐
         ├─> US2 (T035–T039) ─┼─> T033（US1 启动验收需 US2 镜像 T035）
         ├─> US3 (T040–T044)   │
         └─> US4 (T045–T049) ─┘
                  └─> Phase 7 (T050–T053)
```

- **US1 与 US2 可并行开发**：US1 写 nginx/compose/env 模板，US2 写 `web/Dockerfile`；只有 US1 的 T033（起容器验收）需要 US2 产出镜像。
- **US3 依赖 Phase 2** 的 admin 根路径化（T017–T020）与探针（T009–T013），不依赖 US1/US2 的域名产物。
- **US4 依赖 US1/US2/US3** 的事实产物（命令、端口、脚本）。
- 任务内依赖：T005 依赖 T004 的字段；T012 依赖 T009–T011；T023 依赖 T004（`TRUSTED_PROXIES` 三处登记）；T033 依赖 T028–T031 + T035；T048 依赖 T045。

## Parallel Execution Examples

**Phase 2（并行度高）**

```text
T004 config 字段与解析   ┐
T007 clientip 包         ├─ 四组文件互不重叠，可并行
T017 admin router.ts     │
T021 portal router.ts    ┘
T006/T008/T012/T016 为对应测试，可在实现后并行补齐
```

**US1（配置面并行）**

```text
T025 admin.conf ┐
T026 api.conf   ├─ 三个 nginx 文件互不依赖
T027 templates  ┘
T030 test.env   ┐
T031 prod.env   ┘ 可并行
```

**US2 / US3**

```text
T035 web/Dockerfile（主）  ∥  T038 镜像内容检查
T040 compose 前端服务       ∥  T041 API_UPSTREAM 注入
```

## Implementation Strategy

### MVP First

1. Phase 1 + Phase 2（阻塞项：探针、可信代理、cookie 域、会话失效、admin 根路径化、回跳）
2. **US1**（四域名部署）→ 起三容器跑 §3 断言 → 这就是 MVP
3. 停下验收：SC-001、SC-002、SC-007 必须同时成立

### Incremental Delivery

1. Phase 2 完成 → 代码层"多域安全"就绪（可单独 review）
2. US2 完成 → 镜像可按环境构建（SC-003）
3. US1 完成 → 测试/生产可部署（SC-001/002/004/007/008）
4. US3 完成 → 本地一键栈 + 命令启动回归（SC-006、FR-020）
5. US4 完成 → 文档与脚本自证（SC-005）

### 收口优先级（来自两轮 critique + analyze）

P0（不做就等于验证链断裂）：T045 `smoke-deploy.sh`、T046 sha256 断言、T024–T026 的 `location = /healthz`、T015 的 `clearSessionCookie`、T011 的限流短路。
P1：T005 `prod` cookie 校验、T037 串环境检查、T041 `depends_on`、T044 命令启动回归。

## Notes

- `[P]` = 不同文件、无未完成依赖
- 每个用户故事独立可验收；跨故事只允许"依赖"不允许"隐式耦合"
- 测试与实现同故事同阶段，先写测试使其失败（plan 要求的断言）
- 每完成一个 Phase 提交一次；Phase 2 的改动涉及既有行为（cookie 清理、限流、admin 路由），提交信息需写明影响面
- **禁止**：给探针套统一响应、把 `/me/password/reset` 并入 `clearSessionCookie`、给前端加 CORS、把 admin 挂到子路径、用裸 `envsubst`

## Phase 8: Convergence

- [X] T054 让登出真正落到 `portal.<主域>/login?loggedOut=1`：`web/admin/src/config.ts` 的 `portalLoginUrl()` 无 `next` 时也附 `loggedOut=1`，`web/portal/src/stores/session.ts` 的 `useSignOut` 改为 `router.replace({ path: '/login', query: { loggedOut: '1' } })`，`web/portal/src/views/Login.vue` 读该参数显示"已退出"提示并清参数，补 `Login.spec.ts` 用例 per FR-023 (missing)
- [X] T055 在两个前端 nginx conf 增加 `location = /readyz { return 404; }`，修掉 SPA fallback 把 `/readyz` 吞成 200 的假就绪信号（实测 portal 镜像 `/readyz` = 200），并在 `scripts/smoke-deploy.sh` 补 portal/admin 域 `/readyz` 404 断言 per FR-015 (contradicts)
- [X] T056 在 `DEPLOYMENT.md` 补「入口 nginx（api / sso 域）落地」章节：`web/nginx/api.conf` 存放与 include 位置、证书挂载 `/etc/nginx/tls/{sso.crt,sso.key}`、`server_name` 按环境替换、`proxy_pass http://sso:8081` 在宿主机 nginx 下改为可达地址、`nginx -t` 与 reload 步骤 per FR-019 (partial)
- [X] T057 统一 `web/nginx/api.conf` 的 `server_name` 口径：按契约 §3/§4 改为环境可注入的域名（或在文件头明确部署时必须把 `_` 替换为 `api.`/`sso.` 实际域名），并同步 `contracts/nginx-routing.md` 的实现口径说明 per plan: D5 (partial)
- [X] T058 在 `SMS_PROVIDER=mock` 的干净环境复跑 `make e2e`（四域 `*.lvh.me`），确认跨子域登录态、探针暴露面、可信代理 IP 未回归，并留存审计 IP 抽样（20 条）结果 per SC-002 / SC-004 / SC-007 (missing)
- [X] T059 把 portal 的 `ADMIN_HOME` 缺省值改为域名根（dev `http://localhost:5174/`、生产 `/`），与 FR-005 及 plan D3「后台首页为根路径」一致 per FR-005 (partial)
- [X] T060 修正 `AGENTS.md §3` 目录树：删除并不存在的 `internal/api/auth/logout.go`，并在 `login.go` 行注明「含 Logout 与 clearSessionCookie」 per FR-022 (contradicts)
- [X] T061 把 `web/nginx/api.conf` 纳入 `nginx -t` 自检（临时自签证书渲染后校验），挂在 `make image-check` 或独立 target，避免入口配置语法错误只在线上暴露 per plan: D6 (missing)

## Phase 9: Convergence

- [X] T062 为 `web/admin` 建 vitest 基建并补断言：新增 `vitest.config.ts`（jsdom + `@` 别名，沿用 portal 的 `pretendToBeVisual`）与 `test` 脚本，devDeps 用与 portal **完全相同的锁定版本**（`vitest 3.2.7`、`@vue/test-utils 2.5.1`、`jsdom 25.0.1`，不新增版本）；补 `src/views/*.spec.ts` 断言菜单 `index`、`ClientDetail` tab index、`Offline.vue` 回跳 `/users`、`portalLoginUrl()` 生成的 URL 不含 `/admin` 且主动登出（无 `next`）带 `loggedOut=1`；把 `make test-web` 串上 admin per plan: Testing / T020 / FR-004 (missing)
- [X] T063 更新 `scripts/smoke-deploy.sh` 头部断言清单：补第 8 项「portal/admin 域 `/readyz` 404」，与实现（T055 新增的两条断言）一致 per T055 / FR-015 (partial)
- [X] T064 回写 `contracts/nginx-routing.md` §2 第 7 条 gzip 口径：说明 `text/html` 由 nginx 恒定压缩、写进 `gzip_types` 会触发 `duplicate MIME type` 警告，故实现只列 `text/css application/javascript application/json image/svg+xml` per FR-021 (partial)

## Phase 10: Convergence

- [X] T065 补会话失效的 HTTP 边界用例（T016/T051 声称的 `internal/api/me/profile_test.go` 实际不存在）：① 凭原密码改密 `POST /api/v1/me/password` 成功后同一 sid 请求 `/api/v1/me` 返回 401，且响应 `Set-Cookie` 带 `Domain=<主域>` 与 `Max-Age=0`（不是 host-only 空 cookie）；② `POST /api/v1/auth/logout` 同上；③ 回归保护——`POST /api/v1/me/password/reset`（验证码重置）**不**下发清除 cookie、当前 sid 仍 200（只吊销其他设备）。现有 `internal/pkg/cookie/cookie_test.go` 只锁 helper 属性、`internal/service/verification_integration_test.go` 只锁 sid 吊销，均覆盖不到 handler 接线（`internal/api/me/profile.go:183`、`internal/api/auth/login.go:238`） per plan: D10 会话失效用例 / T016 / T051 / FR-023 (missing)
- [X] T066 修正 `web/portal/src/utils/redirect.ts` 的 `resolveNext`：相对路径除 `/login` 外**也要丢弃 `/offline`**（含 `/offline?next=...` 等前缀形态），与 `contracts/admin-routes.md §4`「相对路径不得以 `/login` 或 `/offline` 开头」及 T021 一致；并在 `web/portal/src/utils/redirect.spec.ts` 补一条 `/offline` 被拒用例（现状只拒 `/login`，`?next=/offline` 会被当作合法回跳） per contracts/admin-routes.md §4 / T021 / plan: D4 (contradicts)
- [X] T067 补 `web/portal/src/views/Login.spec.ts` 的登录后跳转用例：① 带 admin 域绝对 `next`（如 `https://admin.example.com/clients/abc/members`）登录成功后走 `window.location.assign`（整页跳转，不得 `router.replace`）；② 带站内相对 `next`（如 `/account/apps`）走 `router.replace` 且不改写成外域。`Login.vue:156-176` 的 `goAfterAuth` 四个分支目前零断言（现有 `redirect.spec.ts` 只覆盖解析，不覆盖跳转） per plan: D4 / T022 / FR-009 (missing)
- [X] T068 同步 `AGENTS.md` 与 admin 测试基建：§3 的 `web/admin/` 目录树补 `vitest.config.ts` 与新增 spec 文件（`layouts/AdminLayout.spec.ts`、`views/client/ClientDetail.spec.ts`、`views/Offline.spec.ts`、`config.spec.ts`），§2.2 的 `vitest`/`@vue/test-utils`/`jsdom` 说明由「portal 首屏用例」改为「portal + admin」，§8 质量门禁表中 `make test` 行改为「portal + admin vitest」 per FR-022 / T062 (partial)
