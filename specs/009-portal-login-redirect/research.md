# Research: 未登录 OIDC 授权统一跳转门户登录页

**Feature**: `009-portal-login-redirect` | **Date**: 2026-10-25

本文件记录 Phase 0 的 8 个决策点（R1–R8）。每条都给出结论、理由与被否掉的备选，避免后续实现或评审重复讨论。无遗留 NEEDS CLARIFICATION。

---

## R1 跳转决策放在哪一层

**Decision**：保持 `op.Client.LoginURL(id)` 恒为 `<issuer>/login?id=<reqID>`，把「送去门户 / 展示内置页 / 直接完成授权」的三态判定放进 `internal/oidc/login.go` 的 `GET /login` 处理器。

**Rationale**：

- `github.com/zitadel/oidc/v3` 的授权端点对**每一次** `GET /oauth2/authorize` 都会调用 `LoginURL`（它不读我们的会话），所以 `LoginURL` 拿不到「用户是否已登录」。现有 e2e 也据此注释：「authorize 一律把浏览器送到 `/login?id=`；有没有表单取决于同一浏览器里是否已有 IdP 会话」。
- 只有 `/login` 处理器持有 `sessions.Get(cookie)`，能可靠区分「已登录 → 直接完成」（FR-009）与「未登录 → 落点选择」（FR-001）。
- 三态集中在同一处，降级与审计也在这里，避免逻辑分散到 `clientAdapter`。

**Alternatives considered**：

- *在 `LoginURL` 直接返回门户地址*：会把已登录用户也送去门户，破坏 SSO 快捷完成，直接违反 FR-009。
- *在 `op.Storage.AuthRequestByID` 里塞会话判定*：`op.Storage` 只允许「数据存取 + 业务校验」，塞跳转策略属于把协议适配层变成业务层（宪法 I）。
- *新写一个 `:8080` 的 `/oauth2/authorize` 包装层*：等于重写协议端点，禁止。

---

## R2 loop guard 的载体

**Decision**：使用回跳 URL 上的**无状态标记** `portal=1`。IdP 生成 `next=<issuer>/login?id=<reqID>&portal=1` 交给门户；门户登录成功整页跳回该地址；`/login` 在「无会话 + 带标记」时停止二次跳转，改渲染内置登录页（含说明文案）并写一条降级日志。

**Rationale**：

- 标记由 IdP 生成、随一次性授权请求 ID 绑定；不携带凭据；不落库、不占 Redis 键、不新增 cookie。
- 幂等且自愈：重复带标记 → 恒定内置页；用户手动去标记 → 多一次门户往返但不成环（门户每次跳转都会重新带标记）。
- 与「待认证请求在进程内存」的既有设计一致（单副本 MVP），不引入新的多副本一致性问题。

**Alternatives considered**：

- *Redis 键 `portal_skip:<reqID>`（TTL 60s）*：多一次写 + 需要为多副本考虑；收益仅是「标记不可见」，而标记本身无害。
- *一次性 cookie（`portal_roundtrip`）*：跨子域要设 Domain，且与既有 `sid` 的写入/清除路径产生第二套 cookie 生命周期，容易在登出时留残留。
- *Referer 判定*：不可靠（隐私策略可能不发送），且会被伪造。

---

## R3 审计事件命名与粒度

**Decision**：新增 `sso_login_success`（`EventSSOLoginSuccess`），`result=success`，`detail = {"entry":"portal"|"template","amr":["pwd","otp"]}`。**不复用** `login_success`。

**Rationale**：

- 管理员需要区分「输入凭据的新登录」与「凭会话复用完成授权」；混在同一事件里会让登录次数统计失真（每次进任意应用都 +1）。
- 事件粒度 = 每次授权完成一条（FR-010 / SC-004），与 `token_issued` 的粒度一致，便于按应用排查。
- 仍需登记 AGENTS §6.7 与 `web/admin/src/utils/format.ts` 的 `EVENT_LABEL`（审计页筛选由该表自动生成），否则 `scripts/check-registry.sh` 与筛选都会缺口。

**Alternatives considered**：

- *复用 `login_success` + `detail.entry`*：注册表零改动，但统计口径被污染，且无法在筛选下拉里单独命中。
- *每条 `token_issued` 视为等价*：粒度在 token 而非授权，无法覆盖「拿到 code 但没换 token」的场景。

---

## R4 门户登录页的白名单入口

**Decision**：门户新增构建期变量 `VITE_SSO_BASE_URL`（dev 缺省 `http://localhost:8080`；test/prod 由构建参数注入 `https://sso.<主域>`），`utils/redirect.ts` 的 `allowedOrigins()` 增加该 origin。

**Rationale**：

- 现状 `allowedOrigins()` 只信任「门户自身 + `ADMIN_HOME`」，IdP 域会被丢弃 → 用户登录成功却停在门户首页（spec Edge Cases 的第 2 条，必须被断言拦住）。
- 复用既有白名单机制而非新增跳转通道，改动面最小；`resolveNext` 的协议校验（拒绝 `//evil`、`javascript:`、白名单外域名）保持不变。

**Alternatives considered**：

- *信任任意 `*.主域` 子域*：会把任意子域纳入信任面（含未来新增的不可信子域），等于放宽开放重定向防护。
- *把 IdP 回调写死成相对跳转*：跨域不可行。

---

## R5 「同一授权请求最多一次门户跳转」如何保证

**Decision**：由 D2 的标记法保证——门户跳转只发生在「无标记且无会话」时；带标记一律走内置页。因此对同一请求，门户跳转次数上界为 1（SC-003）。

**Rationale**：判定所需状态全部在 URL 上，不依赖时序或服务端计数；`pending` 请求过期后 `/login` 直接 400，也不会跳门户。

**Alternatives considered**：服务端计数（新增字段/键）—— 收益为零，成本与失效模式更高。

---

## R6 端到端如何取得期望的门户地址

**Decision**：`examples.sh` 新增 `E2E_PORTAL_LOGIN_URL`（默认 `http://localhost:5173/login`）；A18 硬断言 `Location` 的 scheme+host+path 与该值一致，`next` 解码后含 `portal=1` 与非空 `app`；不一致 → 退出码 `2`（前置不满足），不允许 SKIP。

**Rationale**：`examples.sh` 连的是「已在运行」的 IdP，脚本无权改它的配置；显式声明期望值能让「IdP 配置与脚本假设不一致」立即暴露，而不是把断言退化成永远为真的软断言（spec US4 场景 4）。

**Alternatives considered**：

- *脚本自起 stub 门户*：需要脚本同时掌控 IdP 的 `PORTAL_LOGIN_URL`（重启 IdP），与「连已运行的 IdP」定位冲突。
- *未配置就 SKIP*：等于最关键的两条断言可被环境静默跳过。

---

## R7 `COOKIE_SAME_SITE` 的取值策略

**Decision**：白名单化——只接受 `Lax`（大小写不敏感）与空（视为 `Lax`）；`Strict` 及任何未知值启动失败并指名变量。代码侧不再按配置分支，恒发 `SameSite=Lax`。

**Rationale**：`Strict` 会让「从第三方系统整页跳到 IdP」的请求不带会话，直接表现为「刚登录又被要求登录」；拼写错误（`laxx`）在旧实现里会被静默当成 Lax，同样是不可诊断的隐患。收敛为白名单后，配置错误在启动期暴露（SC-005）。

**Alternatives considered**：

- *保留 `Strict` 仅提示告警*：与 FR-012 冲突，且告警在容器日志里极易被忽略。
- *支持 `None`*：需要 `Secure` + 跨站场景，spec 已明确不做跨 registrable domain。

---

## R8 接口文档与 `openapi.json` 是否需变更

**Decision**：不变。`/login`、`/oauth2/*` 属协议层，不在 `openapi.json`（管理 API）范围内；本次不新增/修改任何 `/api/v1` 路由与 DTO。

**Rationale**：`scripts/check-registry.sh` 会比对 `specs/007-deployment/api-evolve/baseline/manifest.json` 中的 `openapi.json` sha256，任何字节变化都会 fail。既然没有管理 API 变更，就不应触发 `make docs` 重生成（也就无需 `api-evolve` 重新 baseline）。

**Alternatives considered**：把 `/login` 行为写进 `openapi.json` —— 会污染管理 API 文档并破坏 baseline 门禁，且协议端点本就由 `/.well-known/openid-configuration` 描述。

---

## 依赖与版本

**无新增依赖**。后端复用 `zitadel/oidc`、`gin`、`redis`、`gorm`、`slog`；前端复用 `vue-router`、`vitest`。因此 §2.0 的「提案 → 确认 → 登记」流程不触发，§2.1/§2.2 版本表保持不变。
