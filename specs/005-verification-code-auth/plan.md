# Implementation Plan: 多通道验证与账号安全

**Branch**: `005-verification-code-auth` | **Date**: 2026-10-22 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/005-verification-code-auth/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

为门户与 IdP 增加多通道验证能力：短信（创蓝）与邮箱（SMTP）双渠道发送验证码，支撑手机号/邮箱绑定与修改、验证码登录、找回/重置密码、TOTP MFA 绑定与解绑，并沉淀发送记录与审计。核心通过服务端 `NotificationProvider` 抽象实现发送（短信实现创蓝、邮箱实现 SMTP，均支持 mock 模拟发送）；验证码用 Redis 短时存储（TTL + 一次性消费 + 错误计数）；发送记录落 MySQL（参考 `files` 模型），由内置定时任务按 6 个月清理。登录侧扩展账号识别（用户名/邮箱/手机号）与验证码登录，复用既有 MFA 二步验证机制（仅密码登录强制 TOTP，验证码登录豁免）。

**分阶段交付**：建议按「短信/邮箱基础设施与联系方式绑定 → 找回密码 → 验证码登录 → MFA 绑定/解绑」分阶段落地，便于分阶段交付、验证与回滚（阶段划分在 `tasks.md` 中细化）。

## Technical Context

**Language/Version**: Go 1.26.0（后端）；TypeScript 5.7.2 + Vue 3.5.13（前端）

**Primary Dependencies**: 既有 gin v1.10.0 / fx v1.23.0 / gorm v1.25.12 / go-redis v9.7.0 / ulid v2.1.0 / validator v10.23.0 / pquerna-otp v1.4.0（TOTP）/ argon2（x/crypto v0.31.0）；短信与邮箱发送不新增 SDK（创蓝走 HTTP API、邮箱走 Go 标准库 `net/smtp`），如需引入第三方库须先登记 AGENTS.md §2.1

**Storage**: MySQL 8.4（新增 `send_records` 表 + `users.phone` 唯一索引）；Redis 7.4（验证码短时存储 + 频率限制）

**Testing**: `github.com/stretchr/testify`（Go 单测/集成）；前端 vitest（登录方式切换、验证码流程）

**Target Platform**: Linux server（Docker Compose）+ 浏览器（portal / admin）

**Project Type**: web-service（Go IdP，`:8080` 协议面 + `:8081` 管理 API）+ 两个 web 前端

**Performance Goals**: 验证码签发/校验 < 200ms；发送记录写入不阻塞主流程（异步）；短信/邮件发送失败不影响验证码主链路（可降级为失败提示 + 记录）

**Constraints**: 分层 `api → service → store`；结构变更走手写 SQL 迁移；错误码登记 AGENTS.md §5.3；审计事件登记 §6.7；短信/邮箱供应商密钥仅服务端持有；邮件模板文件随 Docker 镜像打包

**Scale/Scope**: 企业内部 IdP；2 渠道（`sms`/`email`）+ 多场景；新增 1 张表 + 1 个唯一索引 + 若干审计事件类型

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 说明 |
|------|------|------|
| I. 协议层单一来源 | N/A | 本特性不触碰 OIDC/OAuth 协议面（登录会话仍走既有 `:8081` 管理 API 与 `:8080` 协议面衔接） |
| II. 端口与信任边界物理分离 | PASS | 仅新增 `:8081` 管理 API 端点，不触碰 `:8080` 协议面 |
| III. 分层与数据一致性 | PASS | 验证码/发送走 `NotificationProvider`（service 层），`send_records` 存取与 `users` 联系方式更新走 store 层，经 `api → service → store` |
| IV. 凭证不出服务端 | PASS | 短信/邮箱供应商密钥仅服务端持有；验证码校验在服务端完成；前端仅拿到「已发送」状态 |
| V. 结构变更只走手写 SQL 迁移 | PASS | 新增 `0005_verification_auth.up/down.sql`；时间列 `BIGINT`（Unix 毫秒）；主键 ULID |
| 技术与接口约束 | PASS（有条件） | 供应商抽象与配置由 `internal/config` 统一加载校验；新增审计事件类型须登记 AGENTS.md §6.7；新增错误码登记 §5.3 |

## Project Structure

### Documentation (this feature)

```text
specs/005-verification-code-auth/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── verification-api.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/
├── config/
│   └── config.go                # 新增短信/邮箱/验证码/清理配置（provider、密钥、模板路径、TTL、mock 开关、保留期）
├── store/
│   ├── model/send_record.go     # SendRecord 模型（send_records 表）
│   ├── send_record_store.go     # 发送记录 CRUD + 分页 + 按时间清理
│   └── user_store.go            # 新增按邮箱/手机号查询、联系方式写入/清空
├── service/
│   ├── notification.go          # NotificationProvider 抽象 + 短信(创蓝)/邮箱(SMTP)/mock 实现 + 验证码签发与校验
│   ├── user.go                  # 账号识别（用户名/邮箱/手机号）、绑定/修改联系方式、重置密码
│   └── mfa.go                   # MFA 绑定（生成 secret + 二维码 + 验证）与解绑（验证码验证）
├── api/
│   ├── auth/
│   │   ├── login.go             # 扩展：账号识别 + 验证码登录入口
│   │   ├── code.go              # POST /auth/code（发送验证码，含 mock 分支）
│   │   ├── password.go          # POST /auth/password/reset（找回密码，邮箱/手机验证码）
│   │   └── mfa.go               # 扩展：MFA 绑定/解绑（或拆分到 me）
│   ├── me/
│   │   ├── contact.go           # POST /me/phone、/me/email（绑定/修改）
│   │   └── mfa.go               # MFA 绑定/解绑（账号中心安全状态区）
│   └── admin/
│       └── send_records.go      # GET /admin/send-records（分页 + 筛选）

migrations/
├── 0005_verification_auth.up.sql
└── 0005_verification_auth.down.sql

web/
├── portal/src/views/Login.vue       # 登录方式切换 + 验证码登录 + 找回密码入口
├── portal/src/views/Account.vue     # 联系方式绑定/修改 + 安全状态区（MFA 绑定/解绑 + 二维码弹窗）
└── admin/src/views/SendRecords.vue  # 发送记录列表（分页 + 筛选）
```

**Structure Decision**: 遵循既有分层与目录（`internal/{config,store,service,api}`）；验证码与发送走 service 层的 `NotificationProvider` 抽象（与 004 的 `FileStore` 同构），`send_records` 存取落 store 层；MFA 绑定/解绑与联系方式维护走 `me/*`（账号中心）与 `auth/*`（登录侧）两组端点；前端 portal 复用既有 `ImageUpload` 之外的登录/账号组件模式。

## Complexity Tracking

> 无宪法违规需豁免。短信与邮箱发送不新增第三方 SDK（创蓝走 HTTP API、邮箱走 `net/smtp`），避免依赖膨胀；供应商密钥由 `internal/config` 统一加载并校验。若实施阶段确需引入邮箱/短信 SDK，须先在 AGENTS.md §2.1 登记并说明理由，不构成违规。
