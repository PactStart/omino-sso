# Implementation Plan: 未登录 OIDC 授权统一跳转门户登录页

**Branch**: `009-portal-login-redirect` | **Date**: 2026-10-25 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/009-portal-login-redirect/spec.md`

**Note**: 本文件由 `/speckit.plan` 产出；任务拆分见后续 `/speckit.tasks`。

## Summary

未登录用户从接入系统（SP）发起授权时，不再看到统一身份（IdP）内置的 HTML 登录表单页，而是被 302 到门户登录页（`PORTAL_LOGIN_URL`）；用户在门户完成登录（含验证码 / MFA）后，浏览器回到 IdP 的 `/login?id=<reqID>&portal=1`，IdP 认出共享会话后完成授权并 302 回 SP。为保证这条跨子域跳转链不会来回弹跳，新增**无状态 loop guard**：回跳时若仍无会话，IdP 立即降级展示内置登录页（带一行说明 + 服务端降级日志），同一授权请求最多触发一次门户跳转。同时补齐「凭既有会话直接完成授权」的审计事件（新增 `sso_login_success`），把 `SameSite=Strict` 列为启动失败项，并在接入示例端到端验证中加入门户前置与 A18/A19 两条断言。

技术要点（详见 [research.md](./research.md)）：

- 跳转决策只能落在 `/login?id=` 处理器（协议库的 `LoginURL` 在每次授权时都会调用，拿不到会话），因此 `op.Client.LoginURL` 保持 `issuer/login?id=` 不变，由处理器决定「门户 / 内置页 / 直接完成」。
- loop guard 用回跳 URL 上的 `portal=1` 标记实现，不引入 Redis 键、不新增表、不加 cookie（见 R2）。
- 门户侧只新增「允许回跳到 IdP 登录域」的白名单项与「来源应用名」展示，不改登录逻辑。

## Technical Context

**Language/Version**: Go `1.26.0`（后端）、TypeScript `5.7.2` + Vue `3.5.13`（门户）

**Primary Dependencies**: `github.com/zitadel/oidc/v3 v3.28.0`（OP，唯一协议实现）、`gin v1.10.0`（仅 :8081）、`go.uber.org/fx v1.23.0`、`redis/go-redis/v9 v9.7.0`（会话）、`element-plus 2.9.1`（**本特性不使用**）、`vitest 3.2.7`。**本特性不新增任何第三方依赖**（§2.0 提案流程不触发）。

**Storage**: MySQL 8.4（`audit_events.event_type` 为枚举字符串，新增取值**无需迁移**）；Redis 7.4（会话，本特性**不新增键**）；待认证授权请求仍在进程内存（`internal/oidc/storage.go` 的 `pending sync.Map`）。

**Testing**: `go test ./...`（`testify`）、`vitest`（portal / admin）、`scripts/check-registry.sh`（四向注册表一致性）、`scripts/e2e/run.sh`（四域 HTTPS）、`scripts/e2e/examples.sh --self-test`（脚本自身校验）

**Target Platform**: Linux 容器（`nginx:1.27-alpine` 托管前端）；浏览器侧需支持 `SameSite=Lax` 的整页跳转

**Project Type**: Web 服务（协议层 :8080 + 管理 API :8081）+ 两个独立前端产物（portal / admin）

**Performance Goals**: 新增一次 302（IdP → 门户）+ 一次回跳（门户 → IdP），不引入额外网络往返到后端；`/login` 单次处理仍为 O(1) 内存查询（`PendingRequest`）

**Constraints**: 不得改动 `/oauth2/*` 报文与 `op.Storage` 协议状态流转；不得在 `:8080` 上挂 CORS / 业务鉴权 / 统一响应；`openapi.json` 必须字节不变（未改 `/api/v1` 路由，`check-registry.sh` 的 baseline sha256 门禁不得被触发）

**Scale/Scope**: 与既有登录链路同量级（企业内网数百至数千员工）；改动面 = 后端 1 个 handler + 1 个配置项 + 1 个事件枚举 + cookie 属性收敛；前端 1 个页面文案 + 白名单；脚本 3 个 + 文档 4 份

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 结论 | 依据 |
|---|---|---|
| I. 协议层单一来源 | PASS | 只改「浏览器落点」，不碰授权码/令牌/状态机；`LoginURL` 仍由既有 `clientAdapter` 提供 |
| II. 端口与信任边界物理分离 | PASS | 改动全在 `internal/oidc`（`:8080` 的 `net/http`），不引入 Gin 中间件、CORS、统一响应包装 |
| III. 分层与数据一致性 | PASS | 不新增 SQL、不改授权码原子消费；审计仍走 `service.Auditor` 异步通道 |
| IV. 凭证不出服务端 | PASS | 会话仍只在 `HttpOnly` cookie；门户登录页拿不到任何令牌；URL 上只出现一次性授权请求 ID（现状即如此），不含 token / secret；审计 detail 不写凭据 |
| V. 结构变更只走手写 SQL 迁移 | PASS | 无表结构变更（`event_type` 是 VARCHAR 枚举） |
| 技术与接口约束 | PASS | 新环境变量进 `internal/config` 并登记 AGENTS §9；无新增错误码；`/api/v1/**` 报文不变 → `openapi.json` 不变 |
| 质量门禁 | PASS（计划内） | 新增纯函数单测 + 配置校验单测 + cookie 单测；前端补 `redirect.spec.ts` / `Login.spec.ts`；`make check` / `make test` / `make build-web` / `make e2e` / `examples.sh` 全绿 |

**Gate 结论**：无违反项，无需 Complexity Tracking 例外。

## Project Structure

### Documentation (this feature)

```text
specs/009-portal-login-redirect/
├── plan.md              # 本文件
├── research.md          # Phase 0：R1–R8 决策与备选
├── data-model.md        # Phase 1：授权请求状态 / 审计事件枚举 / 配置项
├── quickstart.md        # Phase 1：可复现验证步骤
├── contracts/
│   ├── login-entry.md          # /login?id= 的四种分支（含 loop guard）
│   ├── portal-login-redirect.md# 门户登录页的 next / app 契约与白名单
│   ├── config-and-env.md       # 新配置项与启动校验矩阵
│   └── e2e-a18-a19.md          # 门户前置 + A18/A19 断言口径
├── checklists/requirements.md
└── tasks.md             # Phase 2（/speckit.tasks 产出，非本命令）
```

### Source Code (repository root)

```text
internal/
├── config/config.go              # +PortalLoginURL；COOKIE_SAME_SITE 白名单校验
├── config/config_test.go         # +门户地址三态（空 / 非法 / prod 缺失）；+Strict 拒绝
├── oidc/login.go                 # 核心：/login 的「门户 / 内置页 / 直接完成」三态 + 降级提示 + 降级日志（slog）
├── oidc/login_flow.go            # 新增：纯函数（回跳 URL 构造、落点判定），便于单测
├── oidc/login_flow_test.go       # 新增：纯函数单测
├── oidc/server.go                # 不变（handler 装配已在 login.go）
├── oidc/storage.go               # 不变（LoginURL 仍指 issuer/login?id=）
├── store/model/audit_event.go    # +EventSSOLoginSuccess = "sso_login_success"
└── pkg/cookie/cookie.go          # SameSite 恒为 Lax；cookie_test.go 同步

web/
├── portal/src/config.ts          # +SSO_BASE_URL（VITE_SSO_BASE_URL）
├── portal/src/utils/redirect.ts  # 白名单 + IdP 登录域
├── portal/src/utils/redirect.spec.ts
├── portal/src/views/Login.vue    # "登录后返回 <应用名>"（app 参数），隐藏技术路径
├── portal/src/views/Login.spec.ts
├── portal/.env.example / .env.test.example / .env.production.example  # +VITE_SSO_BASE_URL
├── Dockerfile                    # portal target +ARG/ENV VITE_SSO_BASE_URL，并做构建期强制校验（缺则 exit 1）
└── admin/src/utils/format.ts     # +EVENT_LABEL.sso_login_success（审计筛选自动出现）

scripts/
├── e2e/examples.sh               # +门户前置（A2 之后）+A18/A19；表头改 A0–A19；A19 覆盖三语言表单 POST
├── e2e/lib/admin.sh              # 可选：复用 admin::login 的 cookie jar 逻辑做门户前置
├── e2e/run.sh                    # 场景 7 改走门户跳转；+loop guard 断言；保留一条表单路径断言
└── check-build-env.sh            # +sso.<本环境> 必须出现、sso.<外来环境> 0 次（串环境防线）

.env.example                      # +PORTAL_LOGIN_URL（空 = dev 回落内置页）
deploy/env/test.env.example       # +PORTAL_LOGIN_URL=https://portal.<域>/login
deploy/env/prod.env.example       # 同上（prod 必填）
docker-compose.yml                # sso 服务 +PORTAL_LOGIN_URL；portal 构建 +VITE_SSO_BASE_URL
AGENTS.md                         # §6.7 事件、§9 变量、§3 examples.sh 行、§9 COOKIE_SAME_SITE 说明
docs/deployment.md                # 变量表 + 排障（未登录落点不对怎么查）
docs/prd.md                       # P1 登录页「SSO 不走前端」的旧说明改写
docs/integration-guide.md         # 流程图与「未登录会看到什么」两处
```

**Structure Decision**：沿用既有分层，不新建包。唯一新增文件是 `internal/oidc/login_flow.go`（纯函数 + 单测），目的是把「落点判定」从 `http` handler 里剥离出来做确定性单测——现有 `loginHandler` 依赖 `*Storage`（内存 map）+ `*service.*`（需 DB/Redis），直接对 handler 写单测成本高，而 e2e 已覆盖整链。

## 设计要点（Phase 1 提炼）

### D1 落点判定（`/login?id=` 处理器）

| 输入 | 输出 |
|---|---|
| 授权请求不存在/过期 + **不带** `portal=1` | `400` + 既有文案「登录请求已过期，请重新发起授权」（保持现状） |
| 授权请求不存在/过期 + **带** `portal=1` | `200` 过期提示页：说明会话/请求已失效，并提供**可继续的入口**（返回接入系统重新发起 / 重新登录），不是裸文本（FR-021） |
| 存在会话 | `CompleteLogin` → 审计 `sso_login_success` → `302` 到 `op.AuthCallbackURL` |
| 无会话 + 未配置 `PORTAL_LOGIN_URL` | `200` 内置登录页（等价改造前） |
| 无会话 + 已配置 + 无 `portal=1` 标记 | `302` 到 `PORTAL_LOGIN_URL?next=<issuer>/login?id=<id>&portal=1&app=<clientName>` |
| 无会话 + 已配置 + 有 `portal=1` 标记 | `200` 内置登录页 + 降级说明 + `slog.Warn`（loop guard 触发） |

> 判定顺序固定为「会话优先」：已登录用户**永远**不会被送去门户（FR-009），即便 URL 上带 `portal=1`。

**纯函数接口（`internal/oidc/login_flow.go`，便于确定性单测）**：

```go
type LoginStep int

const (
    StepComplete          LoginStep = iota // 有会话 → 完成授权并 302 回调
    StepPortal                              // 无会话 + 已配置 + 无标记 → 302 门户
    StepTemplate                            // 无会话 + 未配置 → 内置页
    StepTemplateDegraded                    // 无会话 + 已配置 + 有标记 → 内置页 + 降级说明
    StepRequestExpired                      // 请求不存在/过期 + 有标记 → 过期提示页
    StepBadRequest                          // 请求不存在/过期 + 无标记 → 400
)

func decideStep(reqExists, hasSession, portalConfigured, portalMarker bool) LoginStep
func portalRedirectURL(base, issuer, reqID, clientName string) (string, error)
```

handler 只做 IO（读 cookie / 查 pending / 渲染 / 重定向），**所有分支判定必须走 `decideStep`**，避免逻辑回流到 handler 里变得不可测。

### D2 loop guard 载体

回跳目标由 IdP 生成并随 `next` 传递，标记即 `portal=1`（**不带任何凭据**）。用户手动去掉标记只会多一次门户往返，不会形成环（每次门户跳转都会重新带上标记）；重复带标记则恒定走内置页。选择理由与备选见 [research.md](./research.md) R2。

**防御说明（标记可被剥离）**：若浏览器扩展/隐私策略剥离查询参数导致标记丢失，最坏结果是"多一次门户往返"，**不会**成为无限环——每次 `StepPortal` 都由 IdP 重新生成带标记的完整地址，标记随之恢复。因此不引入服务端计数或 cookie 兜底；该结论写入 A19 的注释，避免后续被当作缺口反复讨论。

### D3 审计事件

`model.EventSSOLoginSuccess = "sso_login_success"`，`result=success`，`Fields{entry: "portal"|"template", amr: [...]}`，`user_id`/`username`/`client_id`/`ip`/`user_agent` 取当前请求上下文（IP 走 `clientip.FromRequest`，与表单登录一致）。表单登录成功仍写 `login_success`，两者互不替代。

**上线观测三项（SC-009，均可用既有数据源计算，不新增埋点）**：

| 信号 | 计算方式 | 判读 |
|---|---|---|
| 主路径占比 | `count(sso_login_success) / count(login_success)`（按日） | 远低于预期 → 门户地址未生效或被降级 |
| 失效占比 | `/login` 过期分支响应数 / `/login` 总请求数 | > 1% → 请求有效期或配置问题（P2/E2） |
| 高频被送门户账号数 | 按 `client_id + user` 聚合的"门户跳转"次数 ≥3/日 | 异常升高 → 标记丢失或回跳失败（E3） |

> 前两项由审计表与日志聚合，"门户跳转次数"若无法从现有数据取得，则以"同一 `user+client` 当日多次 `sso_login_success` 之外的入口请求"近似，**不新增指标端点**（不扩 `/api/v1`，避免 baseline sha256 变化）。

### D4 配置与校验

新增 `PORTAL_LOGIN_URL`（默认空）：

- 非空时必须为 `http(s)://host[/path]` 绝对地址（有 scheme + host，无 fragment）；
- `APP_ENV=prod`：必填，且必须 `https`；
- `APP_ENV=test`：可空，非空必须 `https`；
- `APP_ENV=dev`：可空，`http` 允许。

`COOKIE_SAME_SITE` 收敛为白名单 `Lax`（大小写不敏感，空视为 Lax）：`Strict` 及其它取值一律启动失败并指名变量。

### D5 前端（portal）

- `config.ts` 增加 `SSO_BASE_URL`（`VITE_SSO_BASE_URL`，dev 缺省 `http://localhost:8080`，test/prod 由构建参数注入）；
- `utils/redirect.ts` 的 `allowedOrigins()` 增加 IdP origin；
- `Login.vue` 副标题：`next` 的 origin 等于 IdP 域时显示「登录后返回 <app>」（`app` 为空/超长/含控制字符时退回「所访问的应用」），其它情况沿用现状；`next` 是 IdP 登录入口时**不显示** `/login` 路径（SC-008）。

### D6 端到端验证

- `examples.sh`：A2 之后插入「门户前置」（用与门户登录页相同的后端登录接口建立会话，写 `$WORK/portal.cookie`），A4–A6 改从该会话起步（走 SSO 快捷完成，`LOGIN_SAW_IDP_FORM=0`）；新增 A18（未登录授权 302 到 `E2E_PORTAL_LOGIN_URL`，`next` 含 `portal=1` 与 `app`）与 A19（带标记回落内置页 + 该页仍能完成登录）。
- **覆盖补偿（E5）**：A4–A6 的语义由「登录闭环」变为「SSO 闭环」，表单 POST 路径改由 A19 承载，且 A19 对 go / java / python 三个客户端各跑一次（成本约 3 次 curl），避免该路径从"三语言覆盖"退化为"单语言覆盖"。
- **计时口径（E5）**：008 的 SC-001「进程启动 → 首次 `/api/me` 200」因省掉一次 argon2 校验而变快，跨版本不可比；契约中标注口径自 2026-10-25 起变化，禁止与 008 数据直接比较。
- `run.sh`：场景 7 改走真实四域链路（authorize → 门户登录页 → 前置登录 → 回跳完成），并加 loop guard 断言；**保留至少一条表单 POST 登录断言**（四域是唯一能验证 `Domain=.主域` + `Lax` 同时生效的地方）。
- ⚠️ 前置不满足（`E2E_PORTAL_LOGIN_URL` 与被测实例实际跳转不一致、示例账号启用了 MFA）→ 退出码 `2` 并指明原因，不得 SKIP。

### D7 文档与注册表

`AGENTS.md` §6.7（事件）+ §9（`PORTAL_LOGIN_URL`、`COOKIE_SAME_SITE` 说明）+ §3（examples.sh 描述 A0–A19）；`.env.example` 与 `deploy/env/*.example` 同步（否则 `check-registry.sh` 直接失败）；四份交付文档同步行为变化。

### D8 发布顺序与兼容矩阵

后端一旦开启跳转，就要求门户产物已具备「白名单含 IdP 域 + 显示应用名」。反序发布会在滚动窗口内造成**全员"登录成功却进不去任何系统"**（门户丢弃回跳 → 停在门户首页 → 再点应用又被送来）。因此：

| 步骤 | 动作 | 失败表现 |
|---|---|---|
| 1 | 发 **portal** 产物（含 `VITE_SSO_BASE_URL`，白名单含 IdP 域） | 旧门户仍可用（后端还没跳） |
| 2 | 在 `deploy/env/*.env` 填入 `PORTAL_LOGIN_URL` | 未填 → 新副本 CrashLoop（prod 必填） |
| 3 | 滚动 **后端** 镜像 | — |

**兼容矩阵**（写入 `contracts/portal-login-redirect.md`）：

| 门户版本 | 后端配置 | 结果 |
|---|---|---|
| 旧（白名单无 IdP 域） | 未开跳转 | 可用（改造前行为） |
| 旧 | **已开跳转** | **禁止上线**：登录后回不到应用 |
| 新 | 未开跳转 | 可用（内置页；门户白名单多一项无副作用） |
| 新 | 已开跳转 | 目标状态 |

**回滚**：dev/test 清空 `PORTAL_LOGIN_URL` 并重启（回到内置页）；prod 因必填，只能回退版本，且回退时**保留**该变量（清空会启动失败）。

## Phase 0 / Phase 1 产物

- [research.md](./research.md)：R1–R8，全部 NEEDS CLARIFICATION 已消除（本计划无遗留未知项）。
- [data-model.md](./data-model.md)：授权请求状态机（含 `portal=1` 标记语义）、审计事件枚举新增、配置项约束。
- [contracts/login-entry.md](./contracts/login-entry.md)、[contracts/portal-login-redirect.md](./contracts/portal-login-redirect.md)、[contracts/config-and-env.md](./contracts/config-and-env.md)、[contracts/e2e-a18-a19.md](./contracts/e2e-a18-a19.md)
- [quickstart.md](./quickstart.md)：9 个可复现验证场景（含反证、配置守护、上线顺序与回滚）。

## Complexity Tracking

无违反项，无需例外登记。以下为**主动拒绝的复杂度**（避免下游误加）：

| 被拒方案 | 理由 |
|---|---|
| 新增 Redis 键记录「已跳过门户」 | 会在多副本场景引入键失效/漂移，且 `pending` 授权请求本就进程内；URL 标记零状态即可满足 FR-006 |
| 本期把 `pending` 授权请求迁到 Redis（解决多副本） | 属独立改造（序列化 `authRequest`、TTL/清理、并发写），与"跳门户"无因果关系；但**触发条件已明确**：一旦 IdP 需要多副本且 LB 无粘性，本特性的回跳会以"请求已过期"失败，届时必须先做该迁移。当前以 A12（单副本/粘性）作为部署前提 |
| 在协议层 `LoginURL` 里直接返回门户地址 | `LoginURL` 在每次授权时都被调用且拿不到会话，会把已登录用户也送去门户，违反 FR-009 |
| 门户登录页改由后端渲染 | 违背 005/007 定的门户 SPA 形态，且会重写登录/MFA/验证码，成本与回归面远超收益 |
| 为 A18/A19 引入浏览器自动化 | 现有 e2e 体系为无头 HTTP 驱动；门户 SPA 登录由「同一后端登录接口」等价替代（A6 假设） |
