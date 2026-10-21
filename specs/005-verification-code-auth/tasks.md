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
- **T037 二维码已落地**：portal 引入 `qrcode@1.5.4`（+`@types/qrcode@1.5.6`，已登记 AGENTS §2.2），绑定弹窗用 canvas 渲染 `otpauthUrl`，并保留下方密钥与链接作为兜底。
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
