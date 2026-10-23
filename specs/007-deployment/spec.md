# Feature Specification: 容器化多域名部署

**Feature Branch**: `007-deployment`

**Created**: 2026-10-23

**Status**: Draft

**Input**: User description: "正式/测试环境 Docker 容器化部署，portal/admin/backend 同主域的多个二级域名；不同环境用不同配置文件构建镜像；正式/测试使用内网云 MySQL/Redis；本地支持命令启动，也支持 compose 一键起全栈。"

## Clarifications

### Session 2026-10-23

- Q: 一共几个域名，各自承载什么？ → A: **四个**，都是同一主域（registrable domain）的二级域名：
  1. `portal.example.com` — 门户前端（登录页、账号中心、我的应用），浏览器直接访问；
  2. `admin.example.com` — 管理后台前端，浏览器直接访问；
  3. `api.example.com` — 管理 API（容器 `:8081`），**由前端 nginx 调用，浏览器不直连**；
  4. `sso.example.com` — OIDC 协议端点（容器 `:8080`），**给 SP（接入方应用）与授权跳转调用**。
- Q: `api` 域既然"给前端调用"，浏览器是不是直连它？ → A: **不是**。前端 XHR 一律打**同源**相对路径 `/api/v1/...`，由 portal/admin 的 nginx 反代到 `api` 域（`api` 域再指向 `sso:8081`）。浏览器视角只有两个源：`portal` 与 `admin`；`api` 域仅在服务端（nginx → backend）可见，因此**不需要 CORS**。
- Q: 那还要 `api` 域干什么？ → A: 它是 `:8081` 的**唯一暴露点**，与给 SP 用的 `sso` 域在网络边界上分开：`sso` 必须公网可达且 issuer 稳定；`api` 只在内网/VPC 解析，公网不可达，便于独立限流与收紧暴露面。前端容器通过内网地址（一体化 compose 用服务名 `sso:8081`）访问它。
- Q: 四个域名之间是什么关系？ → A: 同属一个主域，既有同站关系也有跨源关系：浏览器侧的 `portal` ↔ `admin` 是跨源同站（整页跳转，不涉及 XHR）；前端的 XHR 全部同源。因此会话 cookie 用 `Domain=.example.com` + `SameSite=Lax`，**不需要** `SameSite=None`，也**不需要** CORS。
- Q: admin 的访问路径形态？（2026-10-23 修正） → A: **根路径**，入口是 `https://admin.example.com/users`。此前「保持 `/admin/*` 前缀、`admin.example.com/admin/users` 可接受」的口径**作废**——多一层 `/admin` 纯属冗余，且与"admin 独立域名"自相矛盾。要求：`web/admin/src/router.ts`、菜单 `index`、详情页 tab、离线页回跳全部去掉 `/admin` 前缀；vite `base` 保持 `/`（根路径托管，不要改成子路径）。
- Q: 会话 cookie 由谁写入、谁能读到？ → A: 由登录接口写入（经前端 nginx 反代，仍落在**前端自己的域名**下，故必须显式 `Domain=.example.com`）；`portal`/`admin`/`sso` 三个域都要读到（SSO 判定在 `sso` 域的 `/login`）。`COOKIE_DOMAIN` 留空（host-only）会让 SSO 直接失效。
- Q: OIDC 授权流程未登录时落到哪个登录页？ → A: 落在 `sso.example.com/login`（后端 Go 模板页），**本期不改协议层的登录跳转**；`portal.example.com/login`（Vue 页）只服务"直接访问门户 / 会话失效回跳"的场景。两个登录页并存，能力与视觉对齐不在本期范围。
- Q: 前端 nginx 反代到 `api` 域的这段内网链路用 HTTP 还是 HTTPS？ → A: **HTTP**。该跳转只发生在同一 VPC 内、公网不可达；证书只覆盖 `portal`/`admin`/`sso` 三个公网域名，`API_UPSTREAM` 写成 `http://api.example.com`（一体化 compose 为 `http://sso:8081`）。后续要加密只改变量值，不改代码。
- Q: 健康探针允许公网访问吗？ → A: **只有存活探针放行**：`https://sso.example.com/healthz` 对公网返回 200（无依赖信息，供外部 LB 判断进程存活）；`/readyz`（含 MySQL/Redis 连通性）**仅在容器内/内网可用**，由 docker healthcheck 打 `127.0.0.1`，公网访问必须 404。
- Q: 用户主动登出后停在哪个页面？ → A: 统一回 **`https://portal.<主域>/login`**（带"已退出"提示）。跨域失效靠 `Domain=.example.com` 的 cookie 清除 + 服务端会话删除实现，`admin`/`sso` 域在下次请求时失效即可，不需要把用户带到协议域页面。
- Q: 前端镜像策略？ → A: **按环境分别构建**（同一份代码用不同 `VITE_*` 构建出 test / prod 镜像），不做运行时 `config.js` 注入。
- Q: 云 Redis 是否需要 TLS？ → A: **不需要**；只通过内网/VPC 白名单 + 密码保护。
- Q: 数据库迁移由谁执行？ → A: **运维手动执行**，一期不引入自动迁移任务；必须提供可重复核对的手动步骤与执行记录要求。
- Q: 本期是否做 CI 自动构建推送镜像？ → A: **暂不做**；提供本地/跳板机构建与推送的命令说明即可。
- Q: 是否要求一个 compose 起全栈？ → A: 是。除 test/prod 的「外部云 DB/Redis + 三容器」形态外，**必须再提供一个 compose 直接起全部组件（含 MySQL/Redis）的一体化形态**，用于本地/演示/联调。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 测试/生产四域名部署（Priority: P1）

运维在测试或生产环境，用容器方式部署 backend（`sso.example.com` 承载协议、`api.example.com` 承载管理 API）、门户（`portal.example.com`）与后台（`admin.example.com`）。数据库与 Redis 使用内网可达的云实例。浏览器只访问 `portal`/`admin`/`sso` 三个域名，前端所有 API 请求同源；SP 通过 `sso` 域完成 OIDC 授权码流程。

**Why this priority**：这是本期部署诉求的主体，决定系统能否从「本机能跑」变成「可交付环境」。

**Independent Test**：在一套空环境按部署文档完成构建、迁移、启动后：门户登录 → 打开后台 `admin.example.com/users` 无需二次登录 → 第三方应用走 OIDC 授权码流程可完成登录；同时用公网 curl 打 `api.example.com`（http/https 皆试）应连不通。

**Acceptance Scenarios**：

1. **Given** 已配置四域名 DNS 与证书，**When** 执行部署文档中的启动步骤，**Then** 三个容器均通过健康检查，门户/后台首页可访问，`sso.example.com/.well-known/openid-configuration` 返回的 issuer 与外网地址完全一致。
2. **Given** 用户在门户完成账号密码登录，**When** 浏览器打开 `https://admin.example.com/users`，**Then** 直接进入后台「用户管理」，不出现二次登录页，且地址栏**不含** `/admin` 段。
3. **Given** 后台处于任意深层页面（如 `admin.example.com/clients/abc123/members`），**When** 刷新浏览器，**Then** 页面正常渲染，不 404、不被重定向回首页。
4. **Given** 门户/后台页面向 `/api/v1/me` 发起请求，**When** 检查浏览器网络面板，**Then** 请求 URL 是当前域名的相对路径（`portal.example.com/api/v1/me`），**同源**、**无 OPTIONS 预检**、携带 `sid` cookie。
5. **Given** 前端 nginx 反代 `/api/` 到 `api` 域，**When** 在公网直接访问 `http://api.example.com/api/v1/me`，**Then** 连接失败（`api` 域只在 VPC/内网解析，且不配证书）。
6. **Given** 用户从后台会话失效被重定向到门户登录页，**When** 完成登录，**Then** 浏览器回到 `admin.example.com` 原来的后台路径，而不是停留在门户域名下。
7. **Given** 第三方应用发起授权码流程，**When** 跳转到 `sso.example.com/oauth2/authorize` 且已有门户会话，**Then** 无需再次输入密码即可完成授权并回跳应用。
8. **Given** 请求经由 nginx/LB 进入，**When** 查看审计日志中的 IP，**Then** 记录的是真实客户端 IP（而非代理地址或前端容器 IP），且限流按真实来源生效。

---

### User Story 2 - 按环境构建镜像（Priority: P1）

同一份代码针对测试与生产分别构建三套镜像（backend / portal / admin）。环境差异只体现在构建参数与运行时环境变量/配置文件，不通过改代码区分。镜像可用于首次部署，也可用于后续版本更新与回滚。

**Why this priority**：没有环境化构建，测试与生产只能手工改配置，无法保证「测试验证过的行为在生产可复现」。

**Independent Test**：用同一 commit 分别按 test、prod 配置构建镜像，检查构建产物中的站点名、后台入口地址、登录回跳地址均指向各自环境；后端容器仅通过环境变量切换云 DB/Redis 与 OIDC issuer。

**Acceptance Scenarios**：

1. **Given** 同一 commit，**When** 分别执行 test 与 prod 的镜像构建命令，**Then** 产出可区分的镜像标签，且不需要修改仓库文件。
2. **Given** test 与 prod 的配置文件模板，**When** 核对差异项，**Then** 四个域名、issuer、cookie 域、云 DB/Redis 地址、短信/邮件/OSS 凭据均可独立配置（API 基地址不随环境变化，恒为同源 `/api/v1`）。
3. **Given** 生产环境缺少必填配置（如 issuer、cookie 域、签名私钥），**When** 启动容器，**Then** 进程明确失败并打印缺失项，不静默降级。
4. **Given** 生产环境 `APP_ENV=prod`，**When** 校验配置，**Then** issuer 必须为 https、cookie 必须 Secure、短信/邮件不得为 mock，否则拒绝启动。

---

### User Story 3 - 本地一键起全栈（Priority: P2）

开发/联调人员不满足于分别启动后端与两个前端，希望用一条 compose 命令起包含 MySQL、Redis、backend、portal、admin 的完整栈；同时保留原有的 `go run` / `npm run dev` 方式。

**Why this priority**：降低新人上手与联调成本，但不是对外交付能力，故低于 P1。

**Independent Test**：在干净机器上克隆仓库，执行一体化 compose 启动命令，浏览器访问本机若干端口可完成门户登录与后台操作；随后单独用 `go run` + `npm run dev` 也能正常工作。

**Acceptance Scenarios**：

1. **Given** 干净检出（无 `.env`、无本地镜像），**When** 执行一体化 compose 命令，**Then** MySQL/Redis 健康后 backend（:8080/:8081）、portal、admin 依次可用。
2. **Given** 一体化 compose 正在运行，**When** 访问门户与后台，**Then** 两者共享会话（四个端口共用 `localhost` 主机名，cookie 不区分端口），功能与测试环境一致。
3. **Given** 开发者改用 `go run` / `npm run dev`，**When** 启动，**Then** 无需改动 compose 或镜像即可继续开发（dev 用 vite `/api` 同源代理）。

---

### User Story 4 - 部署文档可独立执行（Priority: P2）

运维拿到仓库后，仅按根目录部署文档即可完成：环境准备、配置填写、镜像构建、数据库迁移、启动、健康检查、发布与回滚、常见故障处理，不需要口头补充。

**Why this priority**：文档是交付的一部分；缺少它会让部署依赖个人经验。

**Independent Test**：让未参与开发的同学按文档在测试环境走一遍，记录卡点；卡点清零即通过。

**Acceptance Scenarios**：

1. **Given** 文档与仓库，**When** 运维按步骤执行，**Then** 不需要询问开发者即可完成一次完整部署。
2. **Given** 发布新版本，**When** 按文档执行回滚，**Then** 能在不重建数据库的前提下回到上一镜像版本。
3. **Given** 常见故障（登录后另一域名未登录、`/api/` 反代 502、OIDC 回跳失败、上传失败、健康检查不通过），**When** 查阅文档，**Then** 能找到定位步骤与配置检查项。

---

### Edge Cases

- **四个域名不属于同一主域**：本期不支持；`SameSite`/cookie 域策略按同站假设实现，若跨主域需另立需求。
- **`COOKIE_DOMAIN` 留空（host-only）**：`portal` 域写入的 cookie 不会被 `admin`/`sso` 域携带，表现为"门户登录成功、进后台又要登录"；必须作为部署后验收项显式检查。
- **`api` 域公网可达**：反代本意是让 API 只在 VPC 内暴露，若误开公网解析，等于把管理 API 直接暴露，属安全事故；验收项须包含"公网连不通"。
- **前端 nginx 解析不到 `api` 域**：表现为 `/api/*` 502；文档须给出"容器内 `getent hosts` / 直连 `sso:8081` 兜底"的排查顺序。
- **测试环境使用 HTTP**：允许，但必须显式关闭 Secure cookie，并在文档中标注风险；生产强制 HTTPS。
- **证书/域名未就绪**：部署文档给出先内网 hosts 验证再切正式 DNS 的顺序。
- **云 MySQL/Redis 白名单未放通**：容器启动应快速失败并给出连接错误，不无限重试卡住编排。
- **迁移漏执行或顺序错误**：服务启动前的迁移检查步骤必须能让运维确认「当前库版本」与「待执行文件」。
- **迁移重复执行**：`0002` 起为裸 ALTER，不幂等；文档必须要求按记录只执行未应用文件，并在执行前备份。
- **同一浏览器同时访问多个环境**：cookie 域不同（或本地端口不同）不得互相覆盖；文档给出隔离建议。
- **登出**：门户登出后，`sso` 域的 OIDC 会话与 `admin` 域访问能力均失效；用户统一落回 `portal.<主域>/login`，`sso.<主域>/logged-out` 只作为协议层兜底页存在。
- **Redis 不可用**：限流与会话功能降级为报错；健康检查应体现为 not ready。
- **管理后台深链刷新**：nginx 必须做 SPA fallback（admin 为根路径托管，`/clients/:id` 刷新不 404）。
- **前端静态资源缓存**：升级后旧 `index.html` 不得长期缓存，避免引用已删除的 hash 资源。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**：系统 MUST 以四个二级域名对外服务——`portal`、`admin`、`api`、`sso`——四者同属一个主域；`portal`/`admin`/`sso` 经 HTTPS 对公网提供，`api` MUST 只在 VPC/内网可达；容器端口 `8080/8081` MUST NOT 直接对公网开放。`api` 域是 `:8081` 的**唯一命名入口**，实际网络隔离 MUST 由内网 DNS + 安全组/网络 ACL 强制——不得以"域名存在与否"作为安全验收依据。
- **FR-002**：`sso` 域 MUST 承载 OIDC 端点（`/oauth2/*`、`/.well-known/*`）与 `/login`、`/logout`、`/logged-out`；未登录的授权请求 MUST 由 `sso.<主域>/login` 承接（不改 `op.AuthCallbackURL` 链路），`portal.<主域>/login` 只服务直接访问门户与会话失效回跳两种场景；`OIDC_ISSUER` MUST 恒等于 `https://sso.<主域>`，`api` 域 MUST NOT 提供任何 OIDC 端点（避免出现第二个 issuer）。
- **FR-003**：`api` 域 MUST 承载管理 API（`:8081` 的 `/api/v1/*`），是 `:8081` 的**唯一暴露点**；`/docs/*` 在 prod 仍为 404（沿用既有行为）；`api` 域 MUST NOT 对公网开放。
- **FR-004**：admin 前端 MUST 以**根路径**托管：`https://admin.example.com/users`、`/clients`、`/clients/:clientId/:tab`、`/audit`、`/files`、`/send-records`；MUST NOT 再出现 `/admin/*` 前缀，vite `base` MUST 保持 `/`。
- **FR-005**：`VITE_ADMIN_HOME`（门户侧「去后台」入口）MUST 指向 `https://admin.<主域>/`（后台首页为根路径下的首个页面）；缺省值 MUST 与 FR-004 一致。
- **FR-006**：会话 cookie MUST 为 `Domain=<主域>`（形如 `.example.com`，**不得**为 host-only）、`Secure`、`HttpOnly`、`SameSite=Lax`；`SameSite=None` 禁用。
- **FR-007**：portal 与 admin 的 nginx MUST 把 `/api/` **同源反代**到 `api` 域（一体化 compose 直接指向 `sso:8081`），反代目标 MUST 可通过配置注入（`API_UPSTREAM`）而无需改镜像；该内网链路 MUST 使用 **HTTP**（TLS 只终止在三个公网域名的入口，证书不覆盖 `api` 域）；浏览器侧 MUST NOT 出现任何跨源请求，因此后端 **MUST NOT** 承担 CORS 职责（不实现 CORS 中间件，不新增 `CORS_ALLOWED_ORIGINS`）。
- **FR-008**：两个前端的 `VITE_API_BASE_URL` MUST 保持同源相对值 `/api/v1`（不随环境变化）；`npm run dev` 继续用 vite 的 `/api` 代理，语义与生产一致。
- **FR-009**：admin 未登录时 MUST 跳转门户登录页并能**回到 admin 域的原路径**；`next` 与 `post_logout` 回跳地址 MUST 校验主机属于允许列表（覆盖四个域及其本地端口形态），禁止开放重定向。
- **FR-010**：portal 与 admin 的镜像 MUST 按环境分别构建，环境差异由 `VITE_*` 构建参数注入（至少：门户地址、后台入口地址、登录页地址、站点名）。
- **FR-011**：backend 镜像 MUST 保持单一产物，环境差异全部由运行时环境变量注入（issuer、cookie 域、云 DB/Redis、短信/邮件/OSS、签名私钥路径）。
- **FR-012**：系统 MUST 提供 test / prod 两套部署编排（不启动 MySQL/Redis，指向内网云实例），并额外提供一套**一体化 compose**（含 MySQL/Redis + 三容器）。
- **FR-013**：仓库 MUST 提供各环境配置模板（不含真实密钥），真实密钥/私钥 MUST 通过挂载或环境注入，禁止写入镜像与仓库。
- **FR-014**：backend MUST 支持可信代理配置：仅对来自可信网段的请求解析 `X-Forwarded-For`/`X-Forwarded-Proto`（`api` 与 `sso` 各有一条反代链路），确保审计 IP、限流拿到真实客户端信息。
- **FR-015**：两个 HTTP 服务 MUST 提供 `GET /healthz`（存活，不探依赖）与 `GET /readyz`（就绪，覆盖 MySQL 与 Redis 连通性）；容器健康检查 MUST 打 `127.0.0.1` 的 `/healthz`。暴露面：公网只放行 `sso.<主域>/healthz`（供外部 LB 判断存活），`/readyz` MUST 仅容器内/内网可达，公网访问 MUST 返回 404。
- **FR-016**：数据库迁移 MUST 由运维手动执行；文档 MUST 给出：迁移文件清单、执行顺序、当前库版本核对方法、备份与失败回滚步骤；系统 MUST NOT 使用 AutoMigrate。
- **FR-017**：Redis MUST 仅以内网/白名单 + 密码方式接入（不要求 TLS）；MySQL 连接 MUST 具备连接/读写超时与连接池参数，默认走内网。
- **FR-018**：本期 MUST NOT 引入 CI 自动构建与推送；文档须给出在跳板机/本地构建并推送镜像的命令与标签规范。
- **FR-019**：仓库根目录 MUST 提供独立的部署文档，覆盖四域名拓扑、域名/端口矩阵、cookie 与反代约束、配置项、构建、首次部署、升级、回滚、健康检查、安全清单与排障。
- **FR-020**：本地开发方式 MUST 保持不变：`go run ./cmd/sso`、`npm run dev` 可独立使用；一体化 compose 为可选路径。
- **FR-021**：nginx MUST 提供 SPA fallback（admin 根路径同样适用）、静态资源长缓存 + `index.html` 不缓存、gzip、请求体大小与超时限制，并在反代 `/api/` 时透传 `X-Forwarded-For`/`X-Forwarded-Proto`/`X-Forwarded-Host`。
- **FR-022**：变更 MUST 同步更新 `AGENTS.md`：部署拓扑（§8.6）、基础设施（§2.3）、环境变量表（§9，`COOKIE_DOMAIN` 示例改为 `.example.com`）、目录结构（§3）。新增变量须同时出现在代码、`.env.example`、AGENTS，保持 `make check` 全绿。
- **FR-023**：登出 MUST 同时失效门户会话与 `sso` 域的 OIDC 会话、`admin` 域的访问能力；登出后浏览器 MUST 落到 `https://portal.<主域>/login?loggedOut=1`（页面读该参数显示"已退出"提示并清除参数），不得停在协议域页面；回跳地址同样受 FR-009 白名单约束。**改密同样属于会话失效路径**：`/me/password` 成功后 MUST 同时 revoke 服务端会话并清除带 `Domain=<主域>` 的 `sid` cookie（不得只清 host-only cookie）。**例外**：`/me/password/reset`（凭验证码重置）MUST 保留当前会话、仅吊销其他设备会话，不得与凭原密码改密合并处理（两者是不同语义）。

### Key Entities

- **域名矩阵**：`portal.<主域>`（门户静态，浏览器直达）/ `admin.<主域>`（后台静态，根路径，浏览器直达）/ `api.<主域>`（管理 API，**仅前端 nginx 可达**）/ `sso.<主域>`（OIDC 协议 + 登录页，SP 可达）；四行都要写清"谁调用、承载哪些路径、反代到哪个容器端口"。
- **环境配置**：test / prod 两套；包含四个域名、issuer、cookie 域、云 DB/Redis、通道凭据、镜像标签。
- **部署单元**：backend（单镜像双端口）、portal、admin 三个镜像；一体化 compose 额外包含 MySQL 与 Redis。
- **前端构建参数**：`VITE_PORTAL_HOME` / `VITE_PORTAL_LOGIN_URL` / `VITE_ADMIN_HOME` / `VITE_SITE_NAME`（`VITE_API_BASE_URL` 恒为 `/api/v1`，无需按环境设置）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**：测试与生产环境各自通过一份编排启动三容器，5 分钟内全部健康检查通过；外部 `https://sso.<主域>/healthz` 返回 200，而 `/readyz` 公网访问返回 404。
- **SC-002**：门户登录后访问 `admin.<主域>/users`，0 次额外登录；后台会话过期后经门户登录，100% 回到原路径；地址栏 0 处出现 `/admin/` 冗余段。
- **SC-003**：同一 commit 构建的 test 与 prod 镜像，站点名、后台入口、登录回跳三项检查全部指向正确环境（0 处串环境）。
- **SC-004**：经反代后审计日志中的客户端 IP 与真实来源一致（抽样 20 条，0 条为代理地址）。
- **SC-005**：未参与开发的同学仅按根目录部署文档，在测试环境完成一次部署，且 [quickstart.md](./quickstart.md) 的验收清单逐项通过（无遗留未勾选项）。
- **SC-006**：干净机器执行一体化 compose 一条命令即可起全栈，门户登录与后台操作可用；「去后台」落到 `http://localhost:8090/`，会话失效回跳落到 `http://localhost:8088/login`（不得指向生产域名或 5173/5174）。
- **SC-007**：浏览器端 0 条跨源 XHR（网络面板无 OPTIONS 预检）；公网访问 `api.<主域>` 0 次成功。
- **SC-008**：用旧 tag 重新 `up -d` 后，三容器在 5 分钟内恢复 healthy 且 admin 可正常登录；触发回滚的判定条件写在部署文档中（而非临时决定）。

## Assumptions

- **A1**：四个域名同属一个 registrable domain（同站），因此无需 `SameSite=None`；前端 XHR 全部同源，因此无需 CORS。
- **A2**：TLS 在负载均衡或宿主机 nginx 终止，容器内可继续用 HTTP；证书由运维提供（`portal`/`admin`/`sso` 可用同一张通配符证书）。
- **A3**：单副本运行（延续 AGENTS 非目标 3），本期不做多副本与共享会话方案。
- **A4**：MySQL/Redis 为云托管实例，网络经 VPC/白名单打通，Redis 不启用 TLS。
- **A5**：数据库迁移由运维在发布窗口手动执行，应用启动不自动迁移。
- **A6**：本期不引入 CI/CD 与镜像仓库自动化，构建推送在跳板机执行。
- **A7**：admin 以根路径托管（`admin.<主域>/users` 为正常入口），`/admin/*` 前缀在本期移除；该改动属代码变更（路由/菜单/tab/离线页），不是纯部署配置。
- **A8**：前端按环境分别构建镜像，不做运行时配置注入。
- **A9**：继续遵守 `:8080` 协议层与 `:8081` 管理 API 的物理分离；两者通过两个不同域名（`sso` 公网 / `api` 内网）暴露，可指向同一容器的两个端口。
- **A10**：AGENTS 中的「非目标 3（不写 K8s/Helm、不做多副本）」仍然有效，本期只做 Compose 形态。
- **A11**：`api` 域与 `sso` 域可共享同一容器与同一进程，但反代规则、可达性与证书策略独立配置；若后续需要物理拆开，仅调整编排不改代码。

## Non-Goals

- 不做 Kubernetes / Helm / 多副本 / 蓝绿或金丝雀发布。
- 不做 CDN、WAF、API 网关。
- 不做跨 registrable domain 的 SSO（`SameSite=None` 方案）。
- 不统一 `sso` 域（Go 模板）与 `portal` 域（Vue）两个登录页，不做两者的能力/视觉对齐（另立需求）。
- 本期不支持 SP 的绝对地址登出回跳（OIDC RP-Initiated Logout）：`/logout` 的 `next` 仍只接受相对路径，接入方按 client 登记的 `post_logout_uris` 自动回跳不在本期范围；需要时另立需求并单独评估白名单。
- 不做跨源调用与 CORS（前端一律同源反代；若将来确有第三方调用管理 API 的需求，另立需求并单独评估）。
- 不做运行时前端配置注入（config.js）。
- 不做自动迁移与迁移回滚工具。
- 不做镜像漏洞扫描与签名（可在后续安全需求中提出）。
