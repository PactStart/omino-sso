---

description: "Task list for 多通道验证与账号安全"
---

# Tasks: 多通道验证与账号安全

**Input**: Design documents from `/specs/005-verification-code-auth/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/, quickstart.md

**Tests**: 本特性按 critique 结论纳入验证码全链路集成测试（E5），其余以单测为主，不强制 TDD。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- 后端：`internal/{config,store,service,api}`、`migrations/`
- 前端：`web/portal/src`、`web/admin/src`
- 文档：`specs/005-verification-code-auth/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 配置、迁移、审计/错误码登记、邮件模板等共享基础设施

- [x] T001 [P] 在 `internal/config/config.go` 新增短信/邮箱/验证码/清理配置项（`SMS_PROVIDER`、`SMS_CHUANGLAN_URL/ACCOUNT/PASSWORD/TEMPLATE`、`MAIL_PROVIDER`、`MAIL_SMTP_HOST/PORT/USERNAME/PASSWORD`、`MAIL_FROM`、`MAIL_TEMPLATE_PATH`、`VERIFY_CODE_TTL`、`VERIFY_RESEND_INTERVAL`、`VERIFY_MAX_ATTEMPTS`、`VERIFY_RECORD_RETENTION_DAYS`），按 provider 校验必填项，缺失启动失败
- [x] T002 [P] 创建 `migrations/0005_verification_auth.up.sql` 与 `.down.sql`：`CREATE TABLE send_records`（列与索引见 data-model.md，索引以 `created_at` 前置）+ `ALTER TABLE users ADD UNIQUE KEY uk_users_phone (phone)`；down 反向执行
- [x] T003 [P] 在 `internal/store/model/audit_event.go` 新增审计事件常量 `verification_code_sent`、`phone_bound`、`email_bound`、`mfa_bound`、`mfa_unbound`、`password_reset`，并登记到 AGENTS.md §6.7
- [x] T004 [P] 在 `internal/api/response/errors.go` 新增错误码 `AUTH_CODE_INVALID`、`VERIFY_TARGET_IN_USE`、`VERIFY_SEND_FAILED`、`MFA_ALREADY_BOUND`、`MFA_NOT_BOUND`，并登记到 AGENTS.md §5.3
- [x] T005 [P] 创建邮件验证码模板 `configs/verify-mail.tmpl`（`html/template` 语法，含验证码与有效期占位），由 `MAIL_TEMPLATE_PATH` 引用；`Makefile` 新增 `assets` 目标做存在性校验（`build`/`dev` 依赖），缺文件即失败。镜像打包方式待后续单独处理
- [x] T006 [P] 更新 `.env.example` 新增 T001 所列配置项及注释

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 发送抽象、验证码服务、发送记录存储、清理任务等所有用户故事的共同前置

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T007 创建 `internal/store/model/send_record.go`：`SendRecord` 模型（`id/channel/scene/target/result/provider/error/created_at`）与 `channel`(`sms`/`email`)、`scene`、`result`(`success`/`failed`)、`provider`(`chuanglan`/`smtp`/`mock`) 枚举常量
- [x] T008 创建 `internal/store/send_record_store.go`：`CreateSendRecord`、`ListSendRecords`（按 channel/scene/result + 时间范围分页）、`DeleteSendRecordsBefore`（分批 500 条物理删除）
- [x] T009 [P] 在 `internal/store/user_store.go` 新增 `GetUserByEmail`、`GetUserByPhone`、`UpdatePhone`、`UpdateEmail`（写入/清空）
- [x] T010 发送记录存储接入 DI：按项目既有惯例实现为 `internal/store/send_record_store.go` 中 `*store.Store` 的方法（`CreateSendRecord`/`ListSendRecords`/`DeleteSendRecordsBefore`），由已有的 `*store.Store` 注入；不引入 `SendRecordStore` 接口与 mock（理由见实施备注）
- [x] T011 [P] 创建 `internal/service/notification/provider.go`：定义 `NotificationProvider` 接口（`SendSms`/`SendEmail`）与场景/渠道常量
- [x] T012 [P] 创建 `internal/service/notification/chuanglan.go`：创蓝 HTTP 实现（3s 超时、无自动重试）
- [x] T013 [P] 创建 `internal/service/notification/smtp.go`：SMTP 实现（`net/smtp` + `html/template` 渲染模板 + `multipart/alternative` MIME，3s 超时）
- [x] T014 [P] 创建 `internal/service/notification/mock.go`：mock 实现（不调用真实供应商，验证码固定 `888888`）
- [x] T015 创建 `internal/service/notification/service.go`：验证码签发（6 位数字、Redis key `verify:{scene}:{channel}:{target}`、TTL 5 分钟、60s 重发间隔）、校验（一次性消费、连续错误 5 次失效）、发送记录**同步落库**、审计 `verification_code_sent`
- [x] T016 创建 `internal/service/cleanup.go`：内置 `time.Ticker` 每日清理 `send_records` 与 `audit_events` 超过 180 天数据（分批、结构化日志：删除行数/耗时/错误），在 fx 生命周期启动/停止
- [x] T017 在 `cmd/sso/main.go` 的 fx Provide 中装配通知服务（`newNotification` → `notification.New`）与 `CleanupService`，经 `fx.Invoke(runCleanup)` 挂生命周期后传给 `api.NewRouter`；仓库无 `internal/service/module.go`、`internal/api/module.go`，装配位置以 `cmd/sso/main.go` 为准

---

## Phase 3: User Story 1 - 绑定与修改手机号、邮箱 (Priority: P1) 🎯 MVP

**Goal**: 员工在账号中心绑定/修改手机号与邮箱，均需验证码验证归属。

**Independent Test**: 登录后绑定手机号（获取验证码→提交）→显示脱敏；修改手机号→更新生效；绑定/修改邮箱同理。

- [x] T018 [US1] 在 `internal/service/user.go` 新增 `BindPhone`、`BindEmail`（验证码消费 + 唯一性校验 + 唯一冲突映射 `VERIFY_TARGET_IN_USE` + 审计 `phone_bound`/`email_bound`）
- [x] T019 [US1] 创建 `internal/api/me/contact.go`：`POST /me/phone`、`POST /me/email`（请求校验、脱敏响应）
- [x] T020 [US1] 在 `internal/api/router.go` 注册 `/me/phone`、`/me/email`
- [x] T021 [P] [US1] 在 `web/portal/src/api/index.ts` 新增 `bindPhone`/`bindEmail`，并在 `web/portal/src/views/Account.vue` 实现联系方式绑定/修改 UI（获取验证码倒计时、服务端脱敏展示）
- [x] T022 [US1] 补 `internal/api/me/contact.go` swag 注解并运行 `make docs` 同步 `internal/api/docs/openapi.json`

---

## Phase 4: User Story 2 - 多方式登录（密码 / 邮箱验证码 / 手机验证码） (Priority: P1)

**Goal**: 用户名/邮箱/手机号 + 密码登录；邮箱/手机验证码登录；登录页可切换。

**Independent Test**: 分别用「邮箱+密码」「手机号+密码」「邮箱+验证码」「手机+验证码」登录同一账号均成功。

- [x] T023 [US2] 在 `internal/service/user.go` 新增 `AuthenticateByIdentifier`（正则识别：`^1\d{10}$`→手机号、含 `@`→邮箱、其余→用户名；分别走对应唯一索引查询）
- [x] T024 [US2] 扩展 `internal/api/auth/login.go` 支持 `loginType`（`password`/`email_code`/`sms_code`）+ 验证码登录；验证码登录成功后**不触发** TOTP 二步（澄清 Q6），密码登录保持既有 MFA 流程
- [x] T025 [US2] 创建 `internal/api/auth/code.go`：`POST /auth/code`（按 scene 校验目标状态；`login`/`find_password` 对未绑定目标返回统一成功响应但不真实发送——防枚举；`bind_*` 对已占用目标返回 `VERIFY_TARGET_IN_USE`）
- [x] T026 [US2] 在 `internal/api/router.go` 注册 `/auth/code`
- [x] T027 [P] [US2] 在 `web/portal/src/views/Login.vue` 增加登录方式切换（密码/邮箱验证码/手机验证码）与验证码获取/倒计时
- [x] T028 [US2] 补 `internal/api/auth/login.go`、`code.go` swag 注解并更新 `openapi.json`

---

## Phase 5: User Story 3 - 找回与重置密码 (Priority: P1)

**Goal**: 未登录通过邮箱/手机验证码找回（重置）密码；已登录用户通过验证码重置密码。

**Independent Test**: 登录页「忘记密码」→邮箱验证码→设新密码→用新密码登录成功；已登录用户在账号中心通过验证码重置密码，其他设备会话失效。

- [x] T029 [US3] 在 `internal/service/user.go` 新增 `ResetPasswordByCode`（未登录，验证码校验 + 更新 password_hash + 审计 `password_reset`）与 `ResetPasswordWithCode`（已登录，重置后吊销该用户其他会话）
- [x] T030 [US3] 创建 `internal/api/auth/password.go`：`POST /auth/password/reset`（成功**不自动登录**，澄清 Q1）；已登录重置端点 `POST /me/password/reset`
- [x] T031 [US3] 在 `internal/api/router.go` 注册 `/auth/password/reset`、`/me/password/reset`
- [x] T032 [P] [US3] 前端：`web/portal/src/views/Login.vue` 增加「忘记密码」流程；`web/portal/src/views/Account.vue` 增加已登录重置密码入口
- [x] T033 [US3] 补 swag 注解并更新 `openapi.json`

---

## Phase 6: User Story 4 - MFA 绑定与解绑 (Priority: P2)

**Goal**: 账号中心安全状态区绑定/解绑 TOTP MFA（扫码 + 6 位动态码；解绑用短信/邮箱验证码）。

**Independent Test**: 未绑定时点击绑定→扫码→填 6 位动态码→状态变「已启用」；解绑→验证码→状态变「未启用」；绑定后密码登录需 TOTP 二步。

- [x] T034 [US4] 在 `internal/service/mfa.go` 新增 `StartBind`（`totp.Generate` + 加密 `mfa_secret` + 返回 `otpauth://` URL 与 `setupTicket`）、`ConfirmBind`（`totp.Validate` 通过后置 `mfa_enabled=true` + 审计 `mfa_bound`）、`Unbind`（验证码校验后清空 secret + 置 disabled + 审计 `mfa_unbound`）
- [x] T035 [US4] 创建 `internal/api/me/mfa.go`：`POST /me/mfa/bind`、`POST /me/mfa/bind/confirm`、`POST /me/mfa/unbind`
- [x] T036 [US4] 在 `internal/api/router.go` 注册上述端点
- [x] T037 [P] [US4] 在 `web/portal/src/views/Account.vue` 安全状态区实现绑定/解绑按钮、绑定弹窗（`qrcode@1.5.4` 将 `otpauthUrl` 渲染为二维码，并保留 Base32 密钥与链接兜底）、解绑验证码流程
- [x] T038 [US4] 补 swag 注解并更新 `openapi.json`

---

## Phase 7: User Story 5 - 短信/邮箱发送记录 (Priority: P2)

**Goal**: 管理员在后台分页查看发送记录并按渠道/场景/结果/时间筛选。

**Independent Test**: 产生若干记录后，管理员在发送记录页分页浏览并筛选正确。

- [x] T039 [US5] 在 `internal/service/notification/service.go` 新增 `ListRecords`（分页 + channel/scene/result + 时间范围筛选，强制带时间范围以命中 `created_at` 索引）
- [x] T040 [US5] 创建 `internal/api/admin/send_records.go`：`GET /admin/send-records`
- [x] T041 [US5] 在 `internal/api/router.go` 注册 `/admin/send-records`
- [x] T042 [P] [US5] 创建 `web/admin/src/views/SendRecords.vue`（分页表格 + 筛选），并在 `web/admin/src/router.ts` 与菜单注册
- [x] T043 [US5] 补 swag 注解并更新 `openapi.json`

---

## Phase 8: User Story 6 - 验证码模拟发送（开发/测试配置） (Priority: P3)

**Goal**: 配置驱动短信/邮箱模拟发送，mock 时验证码固定 `888888`。

**Independent Test**: `SMS_PROVIDER=mock`/`MAIL_PROVIDER=mock` 时任意验证码请求返回 `888888` 且不调用真实供应商。

- [x] T044 [US6] 在 `internal/service/notification/service.go` 按 `SMS_PROVIDER`/`MAIL_PROVIDER` 选择 provider 实现（`mock` 分支固定 `888888`、不真实发送、`send_records.provider=mock`）
- [x] T045 [US6] 增加 `internal/service/notification/service_test.go` 单测：mock 分支验证码为 `888888`、不调用真实实现、记录 `provider=mock`

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: 集成测试、文档同步、构建验证

- [x] T046 新增验证码全链路集成测试 `internal/service/notification/integration_test.go`（签发→校验→错误计数 5 次失效→一次性消费→60s 重发限制→过期），覆盖 mock 分支；真实供应商需外部凭据，provider 选择由 `service_test.go` 的 `TestProviderSelection` 覆盖
- [x] T047 在 AGENTS.md 更新 §4.1 接口总览（新增 `/auth/code`、`/auth/password/reset`、`/me/phone`、`/me/email`、`/me/mfa/*`、`/admin/send-records`）与 §6 表结构（`send_records`、`users.uk_users_phone`）
- [x] T048 运行 `make docs` 校验 `openapi.json` 无漂移（`make docs-check`）
- [x] T049 运行 `make lint`（golangci-lint + 两套前端 eslint）、`make test`、`make build`（含 `docs-check`、`assets`）与两个前端 `npm run build`；并在真实 MySQL(3307)+Redis(6380) 上跑通集成测试与 30 项 HTTP 端到端冒烟（见实施备注）

---

## 实施备注（2026-10-22）

- **T005 落地方式**：模板随 `configs/` 目录交付，路径由 `MAIL_TEMPLATE_PATH` 指定；`Makefile` 的 `assets` 目标在 `build`/`dev` 前做存在性校验（缺失即失败），把"首次发信才报错"提前到构建期。容器镜像打包方式暂缓。
- **T010 调整为不启用接口抽象**：`store` 层在本项目统一以具体 `*store.Store` 提供（既有 `user_store.go` / `audit_store.go` 风格），`send_record_store.go` 沿用同一形态，未引入 `SendRecordStore` 接口与 mock，避免与既有分层惯例冲突。
- **T037 二维码已落地**：portal 引入 `qrcode@1.5.4`（+`@types/qrcode@1.5.6`，已登记 AGENTS §2.2），绑定弹窗用 `QRCode.toDataURL(otpauthUrl)` 生成 data URL 后交给 `<img>` 展示（qrcode 内部用 canvas 绘制再导出，页面无 canvas 节点），并保留下方密钥与链接作为兜底。
- **T049 前端 lint 已接门禁**：两套前端新增 `eslint.config.js` 与 `npm run lint`（eslint 10.13.0 / eslint-plugin-vue 10.11.1 / typescript-eslint 8.72.0 / @vue/eslint-config-typescript 14.9.0 / globals 17.13.0，均 `--save-exact`），`make lint` 已聚合 Go + 前端。
- **本地验证记录（2026-10-22）**：`make lint` / `make test` / `make build` 全绿；`TEST_MYSQL_DSN`+`TEST_REDIS_ADDR` 下 `TestVerifyLifecycle` 与全套 `TestOrg*` 通过；`bin/sso` 连测试库跑 30 项 HTTP 端到端冒烟全部通过（密码登录、绑定手机号、验证码登录、MFA 绑定/二步/解绑、越权与占用、发送记录脱敏与筛选、找回密码）。
- **脱敏范围**：绑定/解绑接口响应与 `send_records.target` 一律服务端脱敏；`GET /me`、`GET /admin/users` 仍返回明文联系方式（前者供本人维护，且 `/auth/code?scene=mfa_unbind` 需要与已绑定值比对），属对"澄清 Q4"的收窄实现，如需全站脱敏需另起变更。

## Dependencies

- **Phase 1 (Setup)** → 无依赖，可先行。
- **Phase 2 (Foundational)** → 依赖 Phase 1（配置/迁移/审计码）。**阻塞所有用户故事**。
- **US1 / US2 / US3 / US4 / US5** → 均依赖 Phase 2；彼此之间代码路径独立（不同 API/文件），可在 Foundational 完成后并行。
- **US6 (mock)** → 依赖 Phase 2 的 provider 抽象；与 US1-US5 无强依赖。
- **Phase 9 (Polish)** → 依赖所有目标用户故事完成。
- 说明：US2 的验证码登录在运行时需要用户已绑定联系方式（US1 产出的能力），但代码实现只依赖 Foundational 的验证码服务，因此实现阶段可并行；验证阶段建议先完成 US1。

## Parallel Example: User Story 1

```bash
# US1 内部可并行（不同文件）：
Task: "T021 在 web/portal/src/views/Account.vue 实现联系方式绑定 UI"
Task: "T018 在 internal/service/user.go 实现 BindPhone/BindEmail"
```

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 1 Setup
2. 完成 Phase 2 Foundational（CRITICAL，阻塞所有故事）
3. 完成 Phase 3 US1（绑定/修改手机号邮箱）
4. **STOP and VALIDATE**：独立验证 US1
5. 可交付/演示

### Incremental Delivery

1. Setup + Foundational → 基础设施就绪
2. US1 → 验证 → 交付（MVP）
3. US3（找回密码）→ 验证 → 交付（与 US1 组合即可解决「忘记密码」核心痛点）
4. US2（验证码登录）→ 验证 → 交付
5. US4（MFA）→ US5（发送记录）→ US6（mock）→ 依次验证交付
6. Phase 9 收尾（集成测试 + 文档 + 构建）

### Parallel Team Strategy

1. 团队共同完成 Setup + Foundational
2. Foundational 完成后：
   - 开发者 A：US1（联系方式绑定）
   - 开发者 B：US3（找回密码）
   - 开发者 C：US2（验证码登录）
   - 开发者 D：US4（MFA）/ US5（发送记录）
3. 各故事独立完成并集成

---

## Notes

- [P] tasks = 不同文件、无依赖
- [Story] label 映射到 spec.md 的用户故事，便于追溯
- 每个用户故事应可独立完成与测试
- 验证码同步落库（E1 决策），不可改回异步
- 唯一索引 `uk_users_phone` 建表前须核对存量重复数据
- 提交粒度：一个逻辑变更一个提交

---

## Phase 10: Convergence

- [X] T050 为未登录的 `/auth/code`（`login`/`find_password` 场景）与 `/auth/login` 补齐图形验证码校验链路：新增签发/校验服务与端点、对应 DTO 与配置、`AUTH_CAPTCHA_INVALID` 的实际返回，并在 `web/portal/src/views/Login.vue`（含忘记密码流程）渲染与刷新图形验证码，消除当前 `captchaId/captchaCode` 仅透传、错误码无使用点的问题 per spec Edge Cases「未登录防刷」/ Assumptions（missing）
- [X] T051 验证码登录失败时按 spec 给出统一提示并附绑定指引「如未绑定手机号/邮箱，请用密码登录后到账号中心绑定」，不再复用 `web/portal/src/views/Login.vue` 的「用户名或密码错误」横幅（区分密码登录与验证码登录两条分支，且不泄露账号是否存在）per US2/AC5（partial）
- [X] T052 让 `GET /api/v1/me` 返回脱敏后的手机号/邮箱（`internal/api/dto` + `internal/api/me/profile.go`，复用 `notification.MaskTarget`），并移除前端二次脱敏（`web/portal/src/views/Password.vue` 的 `maskedTarget`）与明文展示（`web/portal/src/views/Account.vue`）；如坚持返回明文，须在 `spec.md`/`tasks.md` 显式记录该偏离 per Clarifications Q4「联系方式脱敏」（contradicts）
- [X] T053 新增服务端镜像构建：多阶段 `Dockerfile` 编译 `cmd/sso` 并 `COPY configs/verify-mail.tmpl`（`MAIL_TEMPLATE_PATH` 可命中），`docker-compose.yml` 增加 sso 服务（依赖 mysql/redis 健康检查），使 FR-012 的「镜像构建时一并复制模板」可验证 per FR-012（missing）
- [X] T054 引入前端测试：`web/portal` 加 `vitest` + `@vue/test-utils`（精确版本并登记 AGENTS.md §2.2）、`npm run test` 脚本与首屏用例（登录方式切换、验证码获取倒计时、忘记密码流程），并接入 `make test` 或 `make lint` 聚合门禁 per plan.md Testing 决策（missing）
- [X] T055 为未绑定手机号/邮箱的存量账号在账号中心增加一次性、可关闭的绑定引导（「建议绑定联系方式以启用验证码登录与找回」），关闭状态本地持久化、不强制 per spec Assumptions「存量账号绑定引导」（missing）
- [X] T056 同步任务与目录记录：已登录改密入口已由 `web/portal/src/views/Account.vue` 迁至 `web/portal/src/views/Password.vue`（原密码 / 验证码双方式，`POST /me/password` 与 `POST /me/password/reset`），在本文件实施备注与 AGENTS.md §3 目录说明中更正 T032 的落点描述 per tasks.md T032 / plan.md 结构（contradicts）

**实施备注（Phase 10，2026-10-22）**

- **T032 落点更正**：已登录的「验证码重置密码」入口已从 `web/portal/src/views/Account.vue` 迁至 `web/portal/src/views/Password.vue`（原密码 / 验证码双方式）；Account 页「安全状态」仅保留 MFA 启用/解绑的行内按钮。
- **T050**：`internal/service/captcha.go`（Redis `captcha:` 前缀、120 秒、`GETDEL` 一次性消费、SVG data URL）＋ `GET /api/v1/auth/captcha`；`/auth/code` 的 `login`/`find_password` 与 `/auth/login` 的 `email_code`/`sms_code` 强制校验，`AUTH_CAPTCHA_INVALID` 已实际返回；portal 的验证码登录与找回密码两处渲染并在失败后自动换图。
- **T052**：`GET /api/v1/me` 返回 `notification.MaskPhone`/`MaskEmail` 脱敏值；`/auth/code` 已登录的 `mfa_unbind`/`find_password` 忽略请求体 `target`、改由服务端按会话解析；`Password.vue` 删除 `maskedTarget`，`Account.vue` 解绑不再传 `target`。
- **T053**：`Dockerfile`（`golang:1.26-alpine` → `alpine:3.20`，非 root 用户）COPY `configs/verify-mail.tmpl` 并设 `MAIL_TEMPLATE_PATH`；`docker-compose.yml` 新增 `sso` 服务（依赖 mysql/redis 健康检查）。
- **T054**：`vitest 3.2.7` + `@vue/test-utils 2.5.1` + `jsdom 25.0.1`（精确版本，已登记 AGENTS.md §2.2）；`Login.spec.ts` 现为 12 条用例，覆盖登录方式切换与不兼容账号清空、图形验证码拦截/透传/失效、发送成功后换图、验证码登录失败提示（含绑定指引）、字段级旧错误清除、轻提示自动消失；`make test` = `test-go` + `test-web`。
- **T055**：Account 页顶部可关闭引导（`localStorage` 键 `omino.portal.bind-hint-dismissed`），仅在手机号与邮箱均未绑定时出现，含「去绑定」锚点跳转。

---

## Phase 11: Convergence

- [X] T057 让图形验证码无法被脚本直接解析：当前 `internal/service/captcha.go` 把 4 位码以 `<text>` 明文写入 SVG 并 base64 成 data URL，任何脚本解码即可读出答案，未达成「避免批量探测账号」；改为位图（`image/png` 标准库绘制）或路径化字符渲染，禁止在可解析节点中保留明文码，并同步调整 `Login.vue` 的 `<img src>` 取图与 `Login.spec.ts` 断言（不得仅靠 mock 图片证明可用）per spec Edge Cases「未登录防刷」（partial）
- [X] T058 对齐图形码的一次性消费与前端刷新时机：服务端 `Verify` 每次 `GetDel` 即消费，而 `web/portal/src/views/Login.vue` 的 `ensureCaptcha` 只在 `captcha.id` 为空时拉取、`refreshCaptcha` 只在 `AUTH_CAPTCHA_INVALID` 分支触发，导致发送成功后再重发或验证码登录失败后重试时，第一次提交必然先失败一次；改为「发送成功后刷新」「登录失败（`AUTH_CODE_INVALID`/`AUTH_INVALID_CREDENTIALS`）后刷新」，或在服务端改为仅成功路径消费，并补一条用例覆盖重试路径 per T050（partial）
- [X] T059 让 `make test` 在干净检出环境可用：`test-web` 目前只跑 `npm run test`，而 `web/portal/node_modules` 不入库（实测缺失时报 `sh: vitest: command not found`）；改为 npm 依赖缺失时按 `$(NPM_INSTALL)` 安装（或把前端依赖安装纳入 `deps`），确保新克隆仓库直接 `make test` 通过 per Constitution「开发工作流与质量门禁」（partial）

**实施备注（Phase 11，2026-10-22）**

- **T057**：`internal/service/captcha.go` 改为 `image/png` 位图输出——`basicfont.Face7x13` 逐字绘制到 7x13 槽位、最近邻放大 2 倍、逐行剪切叠加，再叠 3 条干扰线与 160 个噪点，最终 `data:image/png;base64,…`；响应体不再包含任何可解析的明文码。新增 `TestCaptchaImageCarriesNoPlaintext`（解码 PNG、断言尺寸 120x40、断言原始字节不含验证码字符串、断言深色像素数）。新增依赖 `golang.org/x/image v0.20.0`（仅 `font/basicfont` 与 `math/fixed`），已登记 AGENTS.md §2.1 并说明理由，`go.mod` 其余锁定版本未变动。
- **T058**：`Login.vue` 在「发送验证码成功」与「验证码登录提交失败」后立即 `refreshCaptcha()`（服务端 `GetDel` 已消费），`AUTH_CAPTCHA_INVALID` 分支改为只提示不重复拉取；新增 2 条用例覆盖「发送成功后换图」「登录失败后换图，重试不再多失败一次」，前端用例累计 12 条（`Login.spec.ts`）。
- **T059**：`make test-web` 增加 `test -d web/portal/node_modules || $(NPM_INSTALL)`，干净检出可直接 `make test`；本机已有依赖时跳过安装，仍走 `npm ci` 的锁文件一致性语义。
- **T057 缺陷修复（2026-10-22，人工验收反馈「只能看到 3 个字符」）**：原布局用 `slide = 6 + (宽-6-4*字宽)/3` 当字符步长（120 画布下步长 39），第 4 个字符起点落到 x=121，整字被画出画布。改为 `glyphOrigins()` 按真实字宽反推间隙，并新增两条回归：`TestCaptchaGlyphLayoutFitsCanvas`（各字符含右边界必须落在留白内）与图片用例的「最右字符区域必须有笔画」。同时把画布提到 132x44、字模放大 3 倍并加粗，字母表去掉位图下易与 8/5 混淆的 `B`/`S`；前端 `img` 容器改为 `132x44` + `object-contain`（原 `object-cover` 在 120x42 容器里还会横向裁掉约 3px）。

---

## Phase 12: Convergence

- [X] T060 对齐 `login` 场景未绑定目标的提示与 spec 的「统一、不泄露账号存在性」：`internal/api/auth/code.go` 现返回 `404 VERIFY_TARGET_NOT_FOUND` 并回显「该邮箱/手机号未绑定任何账号」，与 spec Edge Cases（不泄露存在性 + 附指引）、`contracts/verification-api.md:38`、T025（统一成功响应）相冲突；须把 2026-10-22 的显式提示取舍写回 `spec.md` 的 Edge Cases 与契约条款（或改回统一响应），并把文案补齐为「如未绑定手机号/邮箱，请用密码登录后到账号中心绑定」per spec Edge Cases / T025 (contradicts)
- [X] T061 恢复邮件发送的超时与失败语义：`internal/service/notification/smtp.go` 改用 `email.SendWithTLS`/`SendWithStartTLS`/`Send` 后不再有 3 秒上限（库内部 `tls.Dial` 无超时、不接受 `ctx`），SMTP 卡住时既不返回可理解提示也不写 `send_records` 失败记录；须补回超时（外层 watchdog 或可设 deadline 的投递路径），或在 T013 与 spec Edge Cases 中显式记录该偏离及其风险 per T013「3s 超时」/ spec Edge Cases「供应商不可用须给出提示并记录失败」(partial)
- [X] T062 让邮件正文的有效期跟随配置：`internal/service/notification/smtp.go` 的 `ExpireMinutes` 硬编码为 5，而验证码 TTL 取自 `VERIFY_CODE_TTL`（允许 0–30 分钟，默认 300 秒），改配置后邮件仍写「5 分钟内有效」，与 `configs/verify-mail.tmpl` 的 `{{.ExpireMinutes}}` 占位语义不符；改为按实际 `CodeTTL` 渲染并补一条断言 per FR-012 / plan「邮件模板含有效期占位」(partial)
- [X] T063 补齐 005 配置清单文档：`specs/005-verification-code-auth/quickstart.md` 的短信环境变量仍只有 `SMS_CHUANGLAN_TEMPLATE`，缺新必填项 `SMS_CHUANGLAN_TEMPLATE_ID`（创蓝 V2 模板接口缺它必返回 `code=130 templateId 不能为空`），且未说明「模板 ID 与内容文案二选一」的新校验语义，照文档配置将无法启动 per FR-011 / T001 (partial)
- [X] T064 更新 plan/research 的依赖决策描述：`plan.md`「Primary Dependencies」与 `research.md`「决策 3」仍声明「短信与邮箱发送不新增 SDK、邮箱走 Go 标准库 `net/smtp` + 手写 MIME」，与实际的 `github.com/jordan-wright/email`（AGENTS §2.1 已登记、仅用于组装 MIME + 调用其 `Send*`）不符，须同步描述并写明不再手写 MIME 的理由 per plan「Primary Dependencies」/ research 决策 3 (contradicts)
- [X] T065 记录登录页交互约定：`web/portal/src/views/Login.vue` 新增的「提示走 3.2 秒自动消失的轻提示」「切换登录方式时清空与目标格式不兼容的账号」「账号/密码/图形码/验证码输入即清除对应错误」属 spec 与 T027/T032 未定义的行为（本次为人工体验要求），须写入 `spec.md` 的登录场景或 `quickstart.md` 的交互说明，并明确 `VERIFY_TARGET_NOT_FOUND` 提示的展示形态 per FR-008 / T027 (unrequested)

---

## Phase 13: Convergence

- [X] T066 实现供应商连续失败熔断：`research.md` 决策 11 要求「连续失败进入短时熔断，熔断期内请求直接返回失败提示」，但 `internal/service/notification/service.go` 在发送失败时仅释放重发间隔、落 `failed` 记录并返回 `ErrSendFailed`，全仓库无任何熔断/连续失败计数（`grep 熔断|breaker|circuit` 为空）；须用 Redis 计数 + 冷却窗口实现（阈值/时长可配置），冷期内直接返回 `VERIFY_SEND_FAILED` 且不调用供应商，并补单测；若判定不实现，须在 `research.md` 决策 11 显式降级记录 per research 决策 11 / critique E3（供应商慢响应或宕机会雪崩）(missing)
- [X] T067 更新 plan 的性能目标表述：`plan.md`「Performance Goals」仍写「发送记录写入不阻塞主流程（异步）」，与实现及 `research.md` 决策 9、本文件 Notes「验证码同步落库（E1 决策），不可改回异步」相反，须改为同步落库（并说明写库失败仅告警不掩盖供应商已发送事实）per plan: Performance Goals (contradicts)
- [X] T068 更新 plan 的依赖表述（第二处）：`plan.md`「Complexity Tracking」仍声明「短信与邮箱发送不新增第三方 SDK（创蓝走 HTTP API、邮箱走 `net/smtp`）」，与实际的 `github.com/jordan-wright/email` 不符（T064 仅更新了「Primary Dependencies」一处），须同步改写并保留「新增依赖须先在 AGENTS §2.1 登记」的约束说明 per plan: Complexity Tracking (contradicts)


**实施备注（Phase 13，2026-10-22）**

- **T066**：熔断落在 `internal/service/notification/service.go`——Redis key `verify:cb:<channel>`（连续失败计数，窗口 = 冷却时长）与 `verify:cb:<channel>:open`（开闸标记，TTL = 冷却窗口）。发送前先查开闸标记，命中则直接返回 `ErrSendFailed`，**不写验证码、不写 `send_records`**（本次未发起发送，写记录会让审计面失真），只落一条 `send skipped: provider circuit open` 警告日志；供应商成功即清零计数，失败则计数并在达阈值时开闸。配置项 `VERIFY_CIRCUIT_THRESHOLD`（默认 5，0 关闭）/ `VERIFY_CIRCUIT_COOLDOWN`（默认 60 秒），已在 AGENTS §9 与 `.env.example` 登记；阈值 > 0 而冷却为 0 视为启动错误（否则开闸即永久短路）。Redis 异常时不阻断发送，避免保护措施变成新的单点。集成用例 `TestCircuitBreaker` 覆盖「两次失败→开闸→第三次不调用供应商→冷却后恢复」，已在真实 MySQL+Redis 上通过。
- **T067 / T068**：`plan.md` 的「Performance Goals」改为同步落库（引用决策 9 与 E1），「Complexity Tracking」改为「短信不引 SDK、邮箱用 `jordan-wright/email`」并保留「再引依赖须按 §2.0 提案确认」的约束。

---

## Phase 14: Convergence

- [X] T069 为 005 新增的业务逻辑补单元测试：`internal/service/user.go` 的 `bindContact`/`BindPhone`/`BindEmail`（唯一性冲突映射 `VERIFY_TARGET_IN_USE`、验证码消费失败不写库、审计 `phone_bound`/`email_bound`）、`AuthenticateByCode`（未绑定目标统一错误、验证码错误计数）、`ResetPasswordByCode`/`ResetPasswordWithCode`（不自动登录、吊销全部会话 vs 保留当前会话），以及 `internal/service/mfa.go` 的 `StartBind`/`ConfirmBind`/`Unbind`（已绑定拒绝、ticket 一次性、动态码错误不改变绑定状态、未绑定解绑返回 `MFA_NOT_BOUND`）；测试不得依赖外部基础设施，或沿用 `TEST_MYSQL_DSN`+`TEST_REDIS_ADDR` 门控并在 `make test` 可见 per Constitution「开发工作流与质量门禁：改动必须伴随测试」(missing)
- [X] T070 统一图形验证码的三方口径：`specs/005-verification-code-auth/contracts/verification-api.md` 的「统一约定」把 `/auth/password/reset` 列为「需图形验证码防刷」，而 `internal/api/auth/password.go` 与 AGENTS.md §4.1 的注释只要求 `/auth/code` 的 `login`/`find_password` 与 `/auth/login` 的 `email_code`/`sms_code`；须把契约改为与实现一致（若选择补实现，则须同步 AGENTS §4.1、`web/portal/src/views/Login.vue` 忘记密码最后一步与用例）per contracts §统一约定 / T050 (contradicts)
- [X] T071 补齐契约 §1 中 `target` 的真实必填语义：`internal/api/auth/code.go` 的 `SendCodeRequest.Target` 为 `binding:"omitempty"`，已登录会话的 `mfa_unbind`/`find_password` 忽略请求体并改由服务端按会话解析（`code.go:64-75`），前端 `Account.vue`、`Password.vue` 均不传该字段，契约却写「必填=是」per contracts §1 / T025 (contradicts)
- [X] T072 让 `find_password` 静默成功分支的返回策略取自配置：`internal/api/auth/code.go` 现硬编码 `SendCodeResult{ExpiresIn: 300, ResendAfter: 60}`，与 `VERIFY_CODE_TTL`/`VERIFY_RESEND_INTERVAL` 解耦，改配置后前端倒计时与实际策略不一致；改为从 `notification.Service` 暴露并读取实际值 per T001 / T025 (partial)
- [X] T073 在 AGENTS.md §3 目录树的 `internal/api/me/` 下补登记实际存在的 `contact.go`（`/me/phone`、`/me/email`）与 `mfa.go`（MFA 绑定/解绑），使目录清单与仓库一致 per T019 / T035 (partial)
- [X] T074 修正 `web/portal/src/api/types.ts` 中 `CaptchaResult.image` 的注释：仍写「SVG data URL」，而 `internal/service/captcha.go` 自 T057 起返回 `data:image/png;base64,…` per T057 (partial)
- [X] T075 更正本文件实施备注中的过时记录：Phase 10 备注「`Login.spec.ts` 5 条用例」与 Phase 11 备注「前端用例共 7 条」实测为 12 条（`npx vitest run` 输出 `Tests 12 passed`）；`T037` 备注写「canvas 渲染二维码」，实际实现为 `QRCode.toDataURL` + `<img>`（`Account.vue`），须按实际事实改写 per T037 / T054 / T057 (partial)

**实施备注（Phase 14，2026-10-22）**

- **T069**：新增 `internal/service/verification_integration_test.go`（`package service_test`，与 `org_integration_test.go` 共用 `openTestDB`/`schemaOnce` 脚手架），沿用 `TEST_MYSQL_DSN` + `TEST_REDIS_ADDR` 双门控（缺一即整组 `t.Skip`，`make test` 不依赖外部基础设施）。5 个用例覆盖：`BindPhone`/`BindEmail`（脱敏返回、库内落值、占用冲突 `ErrTargetInUse`、错误验证码不写库）、`AuthenticateByCode`（成功、连续 5 次错误后正确码亦失效、未绑定号码统一 `ErrInvalidCredentials`）、`ResetPasswordByCode`（弱口令先拒、改密后可登录且不产生会话、未绑定目标统一验证码错误）、`ResetPasswordWithCode`（同密码拒绝、保留当前会话并吊销其他设备、未绑定渠道 `ErrTargetNotBound`）、MFA 全链路（错误动态码不改状态、ticket 一次性、有效码确认后 `mfa_enabled=true` 且密钥可解密、重复启动 `ErrMFAAlreadyBound`、未绑定渠道与未绑定状态解绑的错误分支）。已在本地 MySQL(3307)+Redis(6380) 实测全部通过；未引入 mock 接口，避免为测试重构分层（宪法「不重构既有目录结构与分层」）。
- **T070 / T071**：`contracts/verification-api.md` 的「统一约定」改为与 `internal/api/auth/password.go`、AGENTS §4.1 一致的图形码口径（仅 `/auth/code` 的 `login`/`find_password`（未登录）与 `/auth/login` 的 `email_code`/`sms_code` 强制校验；`/auth/password/reset` 靠验证码错误上限与端点限流）；§1 的 `target` 改标「条件必填」并写明已登录 `mfa_unbind`/`find_password` 由服务端按会话解析。
- **T072**：`notification.Service` 新增 `Policy() (expiresIn, resendAfter int)`，`internal/api/auth/code.go` 的 `find_password` 静默成功分支改用它，响应阈值不再硬编码 300/60。
- **T073 / T074 / T075**：AGENTS §3 补登记 `me/contact.go`、`me/mfa.go`；`web/portal/src/api/types.ts` 的图形码注释改为 PNG data URL；更正本文件三处过时备注（前端用例 12 条、二维码实现方式）。

---

## Phase 15: Convergence

- [X] T076 修订 `specs/005-verification-code-auth/quickstart.md` 使其与仓库实际可执行状态一致：第 9 行的 `make migrate` 目标不存在（Makefile 只提供 `migrate-up` / `migrate-down`，执行即报 `No rule to make target 'migrate'`）；配置段缺 T066 引入的 `VERIFY_CIRCUIT_THRESHOLD` / `VERIFY_CIRCUIT_COOLDOWN`（已在 AGENTS §9 与 `.env.example` 登记，缺失时读者无从调整熔断）；第 42 行「编译 bin/sso（含邮件模板复制）」表述不准确——`make build` 只做模板存在性校验（`assets`），模板复制发生在 `Dockerfile`；「自动化校验」一节未说明 `internal/service/verification_integration_test.go` 等 005 业务集成测试需 `TEST_MYSQL_DSN` + `TEST_REDIS_ADDR` 才会真正执行（否则 `t.Skip`）per T001 / T063 / T066 / T069 (partial)

**实施备注（Phase 15，2026-10-22）**

- **T076**：`quickstart.md` 四处修订——① 前置条件的 `make migrate` 改为 `make migrate-up`（并注明回退用 `make migrate-down`），两目标已用 `make -n` 验证存在；② 配置段补 `VERIFY_CIRCUIT_THRESHOLD` / `VERIFY_CIRCUIT_COOLDOWN`（含「阈值 0 关闭、阈值 > 0 时冷却必须 > 0」的启动校验说明）；③ `make build` 的真实行为改为「assets 模板存在性校验 + docs-check + 编译」（模板复制实际发生在 `Dockerfile`）；④「自动化校验」补集成测试的 `TEST_MYSQL_DSN` + `TEST_REDIS_ADDR` 双门控说明与示例命令（并警示该 DSN 指向的库会被 DROP 重建）、前端 12 条用例与提交前全量门禁命令。
