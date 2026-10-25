---

description: "Task list for 未登录 OIDC 授权统一跳转门户登录页"
---

# Tasks: 未登录 OIDC 授权统一跳转门户登录页

**Input**: Design documents from `/specs/009-portal-login-redirect/`

**Prerequisites**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**Tests**: 本项目不接受「测试可选」：宪法「开发工作流与质量门禁」要求新增或修改业务逻辑、端点、数据访问层时同步交付测试。因此**每条实现类任务自带对应测试任务**（同一次提交），测试不堆到最后。

**Organization**: 按用户故事分组，保证每个故事可独立实现与独立验证。

## 任务完成定义（DoD，来自 AGENTS.md §7.4）

一条任务只有在下列四项同时满足时才算完成，缺一不得勾选：

1. **代码**：行为符合 `spec.md` 与 `contracts/`，无 TODO / 空实现；
2. **测试**：同一提交内含单测或门控集成用例；
3. **登记**：新增/变更的路由、错误码、审计事件、环境变量、表结构、依赖版本同步写入 `AGENTS.md` §2 / §4.1·§4.6 / §5.3 / §6.7 / §9；
4. **门禁**：`make build && make test && make lint && make check` 全绿。

> **文档侧已先行完成**（本次会话）：`AGENTS.md`（§3 / §4.0 / §6.7 / §9 / 前端构建变量段）、`docs/prd.md`（P1 登录页说明、路由表）、`docs/integration-guide.md`（流程图、时序图、§9.1）、`docs/deployment.md`（§1 落点说明、§4.1/§4.2 变量、§8 发布顺序与回滚、§9 验收项、§11 排障）、`.env.example`、`deploy/env/{test,prod}.env.example`。
> 当前 `make check` 有 **3 处预期漂移**（事件常量、`config.go` 未读取 `PORTAL_LOGIN_URL` ×2），T001/T002 落地后归零。
>
> **已完成的 spec 项（无需任务）**：**FR-016**（文档记录新变量 / 行为变化 / 回滚方式）已由上述文档改动交付，Phase 7 由 T028–T030 复核即可。
> **约束型 FR 的验证方式**：**FR-017**（admin 前端策略不变）由 T028 的 admin 构建 + 既有单测覆盖；**FR-018**（登出链路不变）由 T030 的 `run.sh` 登出断言（A12/A13）覆盖。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属用户故事（US1–US4，与 spec.md 对齐）
- 每条任务含**确切文件路径**与可验收断言

## Path Conventions

单仓库：后端 `internal/`、前端 `web/portal` · `web/admin`、脚本 `scripts/`、文档 `docs/`。

---

## Phase 1: Setup（把注册表漂移清零）

**Purpose**：先让 `make check` 回到绿色，后续所有任务的 DoD 才能自证。

- [X] T001 [P] 在 `internal/config/config.go` 的 `Config` 结构体新增 `PortalLoginURL string`，并在 `Load()` 用 `env("PORTAL_LOGIN_URL", "")` 读取（**本任务不加校验逻辑**，仅接通读取）；验收：`make check` 的 2 条「环境变量未被 config.go 读取」消失
- [X] T002 [P] 在 `internal/store/model/audit_event.go` 新增常量 `EventSSOLoginSuccess = "sso_login_success"`（注释写明语义：凭既有会话直接完成授权）；验收：`make check` 审计事件 38/38

**Checkpoint**：`make check` 全绿（0 漂移），文档与代码重新对齐。

---

## Phase 2: Foundational（阻塞所有故事的前置）

**Purpose**：共享的判定纯函数、cookie 属性收敛、一体化编排变量。完成后各故事可并行开工。

- [ ] T003 [P] 在 `internal/pkg/cookie/cookie.go` 删除 `sameSiteMode` 分支，恒发 `http.SameSiteLaxMode`（注释写明：`Strict` 会让跨站整页跳转丢会话）；同步改 `internal/pkg/cookie/cookie_test.go`：删除「Strict 配置 → StrictMode」的旧断言，改为「无论入参如何都返回 Lax」；验收：`go test ./internal/pkg/cookie/...` 全绿
- [ ] T004 [P] 新建 `internal/oidc/login_flow.go`：定义 `type LoginStep int` 与 `StepComplete / StepPortal / StepTemplate / StepTemplateDegraded / StepRequestExpired / StepBadRequest` 六态，实现 `func decideStep(reqExists, hasSession, portalConfigured, portalMarker bool) LoginStep`（判定顺序：`!reqExists` → `hasSession` → `!portalConfigured` → `portalMarker`）与 `func portalRedirectURL(base, issuer, reqID, clientName string) (string, error)`（`next` 只做一次 URL-encode；保留 base 已有查询串；`clientName` 为空则不下发 `app`）；**同时在本任务内**于 `AGENTS.md §3` 的 `internal/oidc/` 段登记该文件（描述：登录落点判定纯函数 + 对应单测），避免目录树漂移
- [ ] T005 为 `internal/oidc/login_flow.go` 新增**同目录单元测试**（`_test.go`，表驱动）：覆盖 `decideStep` 全 16 种输入组合（重点：`hasSession && portalMarker` 必须为 `StepComplete`）、`portalRedirectURL` 的 5 条断言（`next` 形如 `<issuer>/login?id=X&portal=1`、`app` 缺失不下发、base 带 `?from=sso` 时保留、`id` 含特殊字符只编码一次、非法 base 报错）；验收：`go test ./internal/oidc/ -run 'TestDecideStep|TestPortalRedirectURL'` 全绿
- [ ] T006 [P] 在 `docker-compose.yml` 的 `sso` 服务 `environment` 增加 `PORTAL_LOGIN_URL=${PORTAL_LOGIN_URL:-}`（与 `.env.example`、`deploy/env/*.example` 对齐）；验收：`docker compose config` 解析通过、`make check` 无新增漂移

**Checkpoint**：判定逻辑已被纯函数单测锁定；`config.go`/`cookie`/`compose` 就绪，三条故事线可并行。

---

## Phase 3: User Story 1（P1）— 未登录的接入方访问由门户登录页接管

**Goal**：未登录用户从 SP 发起授权 → 落到 `PORTAL_LOGIN_URL` → 登录后回到 `/login?id=…&portal=1` → 302 回 SP；回跳后再无会话则降级内置页；未配置时保持现状；过期请求给出可继续路径。

**Independent Test**：清空 cookie 打开 `examples/go` → 落在门户登录页（副标题显示应用名）→ 登录 → 自动回到示例且已登录；再构造 `portal=1` 无会话访问 `/login` → 出现内置页而非再次跳门户。

- [ ] T007 [US1] 在 `internal/oidc/login.go` 的 `GET /login` 处理器接入 `decideStep`：`StepComplete` 走既有 `CompleteLogin` + 302 回调；`StepPortal` 调 `portalRedirectURL` 并 302；`StepTemplate` 渲染原模板；`StepTemplateDegraded` 渲染模板并写一条 `slog.Warn`（字段固定 `reason=portal_roundtrip_without_session`、`client_id`、`request_id`，**不含任何凭据**）；`StepBadRequest` 保持既有 400；验收：单测断言不含凭据、`LoginURL`（`internal/oidc/storage.go`）未被改动
- [ ] T008 [US1] 在 `internal/oidc/login.go` 实现 `StepRequestExpired`（contracts/login-entry.md C1b）：渲染过期提示页，含「返回接入系统重新发起」与「重新登录」两个可点击入口，**不自动跳门户**、不出现 `pending`/`TTL`/`sameSite` 等内部词；`StepBadRequest`（无 `portal=1`）仍返回既有 400 文本；验收：单测断言两分支响应码与文案差异
- [ ] T009 [US1] 在 `internal/oidc/login.go` 的**内置登录页（降级态）**数据中增加 `Degraded bool`，模板（`login.go` 内的 HTML 模板）在 `Degraded` 时渲染一行「登录状态未能在跳转中延续，请在此直接登录」；验收：C3（未配置门户地址）不出现该文案、C4（带 `portal=1` 回跳）出现
- [ ] T010 [US1] 把模板渲染抽成可测函数（如 `renderLoginTemplate(w io.Writer, data loginData) error`），并新增**同目录单元测试**（`_test.go`）：断言 ① C3 输出含 `<form` 且不含降级文案 ② C4 含降级文案 ③ C1b 含重新发起入口 ④ 三种输出都不含 `/login` 技术路径字样（SC-008）；验收：`go test ./internal/oidc/ -run TestLoginTemplate` 全绿
- [ ] T011 [P] [US1] 门户白名单：`web/portal/src/config.ts` 新增 `SSO_BASE_URL`（`VITE_SSO_BASE_URL`，dev 缺省 `http://localhost:8080`，解析失败视为空）；`web/portal/src/utils/redirect.ts` 的 `allowedOrigins()` 加入该 origin（解析失败则不加入）；新增/扩展 `web/portal/src/utils/redirect.spec.ts`：断言 IdP 域被允许、`//evil.com`/`javascript:`/白名单外域名仍被拒
- [ ] T012 [P] [US1] `web/portal/src/views/Login.vue`：`next` 指向 IdP 登录域时副标题显示「登录后返回 <应用名>」（取 `app`，`trim` 后 >64 截断、含控制字符则丢弃、缺失回落「所访问的应用」），其它情况保持现状；`app` 仅文本渲染；扩展 `web/portal/src/views/Login.spec.ts` 覆盖三种分支；验收：`cd web/portal && npm run test` 全绿
- [ ] T013 [US1] 门户构建链与安全响应头：① `web/portal/.env.example` 增 `VITE_SSO_BASE_URL=http://localhost:8080`、`.env.test.example` / `.env.production.example` 增对应 https 值；② `web/Dockerfile` 的 portal target 增 `ARG VITE_SSO_BASE_URL` + `ENV` + **构建期强制**（`test -n "$VITE_SSO_BASE_URL" || { echo "VITE_SSO_BASE_URL is required"; exit 1; }`）；③ `web/nginx/templates/portal.conf.template` 增 `add_header Referrer-Policy "same-origin" always;`（**FR-022**：登录页 URL 含 `app`，不得经 `Referer` 泄露给第三方），并确认登录页不把含 `app` 的完整地址写入应用日志/埋点；④ `docker-compose.yml` portal `build.args` 与 `Makefile` 的 `image-portal` 同步传入并从 `DOMAIN` 派生 `sso.<DOMAIN>`；验收：省略该参数构建应失败；`curl -sI http://<portal>/login` 含 `Referrer-Policy: same-origin`
- [ ] T014 [US1] 四域端到端 `scripts/e2e/run.sh`：`start_backend` 增 `PORTAL_LOGIN_URL="https://portal.$DOMAIN:$HOST_PORT/login"`；场景 7 改为「authorize → 302 门户登录页（断言 `next` 指回 sso 域登录入口）→ 门户页 200 → api 域登录建立会话 → GET `next` → 回调」；**保留一条表单 POST 登录断言**；新增 loop guard 断言（空 jar 访问带 `portal=1` 的 `/login` → 200 HTML，不 302 回门户）；按 `contracts/e2e-a18-a19.md §3.1` 记录「门户点击登录 → 收到 SP 回调 302」的耗时与跳转次数（目标 ≤3 秒 / ≤2 跳）；验收：`make e2e` 全绿且计时值打印在日志里

**Checkpoint**：US1 可独立演示——手动走 quickstart 场景 1/2/3 全部成立。

---

## Phase 4: User Story 2（P1）— SSO 快捷完成在审计中可追溯

**Goal**：每次「凭既有会话完成授权」产生 1 条 `sso_login_success`（含 `entry`/`amr`），与 `login_success` 互不替代，管理后台可按类型筛选。

**Independent Test**：已登录完成一次授权 → 审计新增 1 条且 `detail` 为 `{"amr":[…],"entry":"portal"}`；表单登录仍只产生 `login_success`。

- [ ] T015 [US2] 在 `internal/oidc/login.go` 的 `StepComplete` 分支发审计：`service.Auditor` 发 `model.EventSSOLoginSuccess`，`result=success`，`detail={entry, amr}`（`entry` 由是否带 `portal=1` 决定 `portal`/`template`），`user_id`/`username`/`client_id`/`ip`/`user_agent` 取当前请求上下文；发失败只 `slog.Error`，**不得阻断 302**；验收：`grep -rn "EventSSOLoginSuccess" internal/` 仅出现在 `login.go` 与 `model`
- [ ] T016 [P] [US2] 在 `web/admin/src/utils/format.ts` 的 `EVENT_LABEL` 增加 `sso_login_success: 'SSO 快捷登录'`（审计页筛选下拉由此表生成，无需改 `Audit.vue`）；验收：`npm run build`（admin）通过、审计页筛选出现该项
- [ ] T017 [US2] 为 `internal/oidc/login.go` 的审计分支新增**同目录单元测试**（`_test.go`）：用内存 sink（或 `service.NewAuditor` 注入可捕获实现）断言「一次 `StepComplete` 恰好产生 1 条事件」「`detail` 不含 token/密码/验证码字段」「连续两次产生 2 条不合并」；验收：`go test ./internal/oidc/ -run TestSSOLoginAudit` 全绿
- [ ] T018 [US2] 端到端核对：在 `scripts/e2e/examples.sh` 的 A4 之后，复用 `scripts/e2e/lib/admin.sh` 的管理员客户端（脚本已缓存管理员令牌）查询审计接口，断言「最近一次 `sso_login_success` 的 `client_id` 等于本次示例客户端且 `detail.entry` 为 `portal`」，且条数较 A3 时 **+1**；`examples.sh` 不引入 MySQL 客户端（无连接参数），审计查询一律走管理 API；验收：断言失败时打印实际条数与最近一条的 detail

**Checkpoint**：US2 独立可验——审计页可单独筛出「SSO 快捷登录」，与「登录成功」分开计数。

---

## Phase 5: User Story 3（P2）— 三环境行为一致且配置错误立即暴露

**Goal**：`PORTAL_LOGIN_URL` 在 dev/test/prod 同一套行为；`SameSite=Strict`、非法地址、prod 缺失一律启动失败；镜像不串环境。

**Independent Test**：按 quickstart 场景 5 逐条启动，5 种错配全部非零退出且日志点名变量；正例正常启动。

- [ ] T019 [US3] 在 `internal/config/config.go` 的 `validate()` 实现门户地址矩阵（contracts/config-and-env.md）：非空须为带 scheme+host 的绝对 URL、不允许 fragment；`prod` 必填 + 必须 https；`test` 非空必须 https；`dev` 允许 http；错误信息必须包含变量名与期望形态
- [ ] T020 [US3] 在 `internal/config/config_test.go` 增表驱动用例：空(dev/test 通过、prod 失败)、`http://`(test/prod 失败)、无 scheme、无 host、含 fragment、合法 https（三环境全通过）；验收：`go test ./internal/config/...` 全绿且每条错误断言变量名
- [ ] T021 [US3] 在 `internal/config/config.go` 把 `COOKIE_SAME_SITE` 收敛为白名单（`Lax` 大小写不敏感、空视为 Lax）；`Strict` 与未知值启动失败并给出明确文案；`internal/config/config_test.go` 增 `Strict`/`None`/`laxx`/空 四例；验收：`go test ./internal/config/...` 全绿
- [ ] T022 [US3] 扩展 `scripts/check-build-env.sh`：断言 portal 镜像内出现 `sso.<本环境域>` ≥1 次、`sso.<外来环境域>` 0 次（参数命名与既有 `EXPECT_DOMAIN`/`FOREIGN_DOMAIN` 风格一致）；验收：故意用错环境构建时脚本非零退出
- [ ] T023 [US3] **只做核对，不再改构建参数**（构建参数归 T013）：确认 `Makefile` 的 `images` / `image-portal` 已从 `DOMAIN` 派生 `VITE_SSO_BASE_URL`（与 `VITE_ADMIN_HOME` 同款），并核对 `docs/deployment.md` §8 三步与 `quickstart.md` §9 表述一致；验收：`make images DOMAIN=test.example.com` 构建出的 portal 产物含 `sso.test.example.com`（来源：critique E7/E9）

**Checkpoint**：US3 独立可验——三类配置错配全部启动失败，镜像串环境被脚本拦下。

---

## Phase 6: User Story 4（P2）— 端到端验证覆盖新链路

**Goal**：`examples.sh` 增加门户前置与 A18/A19，既有 A0–A17 仍绿，前置不满足时退出码 2。

**Independent Test**：被测 IdP 已配门户地址时 `bash scripts/e2e/examples.sh` 全绿；把 `E2E_PORTAL_LOGIN_URL` 改成别的地址 → 退出码 2 且提示前置不满足。

- [ ] T024 [US4] 在 `scripts/e2e/examples.sh` 头部增加 `E2E_PORTAL_LOGIN_URL="${E2E_PORTAL_LOGIN_URL:-http://localhost:5173/login}"` 与用法说明；A2 之后插入「门户前置」步骤（`POST $E2E_API/api/v1/auth/login` 写入 `$WORK/portal.cookie`，`mfaRequired`/非 200/网络错误 → `exit 2`）；A4–A6 改为复制该 jar 起步并断言 `LOGIN_SAW_IDP_FORM=0`；验收：日志打印「门户前置：已建立 IdP 会话」
- [ ] T025 [US4] 新增 A18（contracts/e2e-a18-a19.md §3）：空 jar 请求 `/oauth2/authorize`，断言 ①`302` ②`Location` 的 scheme+host+path 等于 `E2E_PORTAL_LOGIN_URL` ③`next` 解码后匹配 `^<issuer>/login\?id=[^&]+&portal=1$` ④`app` 非空 ⑤不是 `<issuer>/login?id=`；并按 §3.1 记录「有会话直达」段耗时（A4–A6 各一次，目标 ≤2 秒），打印到断言日志；失败时打印实际 `Location` 与期望值
- [ ] T026 [US4] 新增 A19（§4）：`GET <A18 的 next>` 空 jar → `200` + 含 `<form` + 含降级文案；`POST /login` 完成登录 → `302` → 回调拿到 code → 换 token 成功；对 go/java/python **各跑一次**表单 POST（仅断言 302 到各自回调）；验收：A19 失败信息能区分「未降级到内置登录页（降级态）」与「降级态页面不能完成登录」两种情况
- [ ] T027 [US4] 更新 `--self-test` 的函数清单与脚本文档：表头/用法注释改为 A0–A19，函数清单加入新增 stage；同步核对 `AGENTS.md §3` 与 `docs/integration-guide.md` 的 A0–A19 表述（文档已改，本任务只做一致性核对）；验收：`bash scripts/e2e/examples.sh --self-test` 通过，缺函数时自检报错

**Checkpoint**：US4 独立可验——A0–A19 全绿，前置错配以退出码 2 结束。

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T028 [P] 跑 `make build && make lint && make test && make check`（Go + 两端前端 + 三个一致性脚本），确认第 1 阶段的 3 处漂移已归零且无新增
- [ ] T029 [P] 跑 `make build-web` 并核对 `web/portal` 产物体积未异常增长（element-plus 仍只在 `views/app-admin/`，本特性未引入新依赖）
- [ ] T030 跑 `make e2e`（四域）与 `bash scripts/e2e/examples.sh`（A0–A19），按 quickstart 场景 1–10 逐条留证（含场景 5 的 5 条配置反证）
- [ ] T031 提交前自检：`git diff` 中无未登记的新环境变量 / 审计事件 / 路由；`internal/api/docs/openapi.json` 未变（`check-registry` 输出的文档基线 sha256 仍为 `7a5cb8d19b43…`，且与 `specs/009-portal-login-redirect/api-evolve/baseline/manifest.json` 一致）；确认未新增第三方依赖（`git diff go.mod web/*/package.json` 为空）；**并按 `quickstart.md §10` 把三条观测 SQL 各跑一次**（验证 SC-009 的三项信号确实可从既有数据源算出，不新增接口）；来源：门禁 + critique P6

---

## Dependencies & Execution Order

### Phase 依赖

```text
Phase 1 (T001–T002)
      │  make check 归零
      ▼
Phase 2 (T003–T006)  ◄── 判定纯函数 / cookie / compose
      │
      ├──────────────┬───────────────┬──────────────┐
      ▼              ▼               ▼              ▼
Phase 3 US1      Phase 4 US2     Phase 5 US3    Phase 6 US4
(T007–T014)      (T015–T018)     (T019–T023)    (T024–T027)
      │              │               │              │
      └──────────────┴───────────────┴──────────────┘
                             ▼
                    Phase 7 Polish (T028–T031)
```

- **US2 依赖 US1 的 T007**（`StepComplete` 分支存在才能发审计）→ 严格串行；其余故事彼此独立。
- **US4 依赖 US1 + US3**（需要真实跳转行为与可配置的门户地址）。
- **US3 与 US1 无代码耦合**（校验只读配置），可并行。
- **T011/T012 与 T007 并行安全**（不同文件、不同语言）。
- **T013 → T014 是硬依赖**（四域 e2e 需要带 `VITE_SSO_BASE_URL` 与白名单的 portal 产物）；若并行派工，T014 会在门户丢弃回跳处失败，容易被误判为后端缺陷。

### 关键任务内部顺序

- T004（纯函数）必须先于 T005（单测）与 T007（接线）。
- T007 必须先于 T008/T009（同一函数的分支）与 T015（审计）。
- T013 必须先于 T014（四域 e2e 需要带白名单的 portal 产物）。
- T024 必须先于 T025/T026（A18/A19 依赖门户前置的会话与变量）。

---

## Parallel Example: Phase 3（US1）

```bash
# 后端串行：T007 → T008 → T009 → T010
# 前端可同时开工（不同文件）：
Task: "T011 [P] [US1] portal config.ts + redirect.ts 白名单 + redirect.spec.ts"
Task: "T012 [P] [US1] portal Login.vue 副标题 + Login.spec.ts"
# 构建链与 e2e 在 T007 完成后并行准备：
Task: "T013 [US1] portal env 模板 + Dockerfile 强制校验 + compose build.args"
```

## Parallel Example: Phase 5（US3）

```bash
Task: "T019 [US3] config.validate 门户地址矩阵"
Task: "T021 [US3] COOKIE_SAME_SITE 白名单（不同函数/不同测试用例）"
Task: "T022 [US3] check-build-env.sh sso 域断言（脚本文件独立）"
```

---

## Implementation Strategy

### MVP First（仅 US1）

1. Phase 1（T001–T002）→ `make check` 归零；
2. Phase 2（T003–T006）→ 判定逻辑被单测锁定；
3. Phase 3（T007–T014）→ **STOP & VALIDATE**：manual 走 quickstart 场景 1/2/3；
4. 此时功能已可用（审计、配置守护、e2e 断言尚未补齐）。

### Incremental Delivery

1. US1 上线即可让员工"从应用点进去看到门户登录页"——用户价值已完成；
2. US2 补齐审计（安全合规要求，建议与 US1 **同批发布**）；
3. US3 补齐启动校验与镜像防串环境（生产发布前必备）；
4. US4 补齐自动断言（回归防线，可与 US3 同批）。

### 不值得拆细的合并点

- T007/T008/T009 都在同一函数的三个分支，建议一次提交完成（避免中间态出现"半接线"）。
- T019/T021 都在 `validate()`，建议一次提交 + 一份表驱动测试。

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖；同文件的多条任务一律串行。
- 每个故事结束都有 **Checkpoint**，可停在任一 Checkpoint 交付。
- 提交前确认 `make check` 的文档基线 sha256 仍为 `7a5cb8d1…`（本特性不改管理 API）。
- 禁止在 `internal/oidc/storage.go` 的 `LoginURL` 里判断登录态（research.md R1）。
