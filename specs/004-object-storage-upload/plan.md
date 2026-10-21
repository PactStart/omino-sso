# Implementation Plan: 图片上传与对象存储（头像与 Logo）

**Branch**: `004-object-storage-upload` | **Date**: 2026-10-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-object-storage-upload/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command; its definition describes the execution workflow.

## Summary

为应用 logo 与用户头像提供「系统内上传」能力：前端复用一致的单文件上传组件（可选裁剪），通过服务端签发的**预签名直传凭证**将图片直接上传到阿里云 OSS，绕过服务端字节流；上传完成后前端调用后端接口写入一条文件记录（`files` 表）并回写业务字段（`clients.logo_url` / `users.avatar_url`）。服务端定义 `FileStore` 抽象（provider 驱动的可插拔配置，本期实现阿里云 OSS），管理后台提供文件记录分页查看与状态更新。

## Technical Context

**Language/Version**: Go 1.26.0（后端）；TypeScript 5.7.2 + Vue 3.5.13（前端）

**Primary Dependencies**: 新增 `github.com/aliyun/aliyun-oss-go-sdk/oss`（对象存储 SDK，**须先登记 AGENTS.md §2.1**）；既有 gin v1.10.0 / fx v1.23.0 / gorm v1.25.12 / go-redis v9.7.0 / ulid v2.1.0 / validator v10.23.0；前端既有 vue/vite/element-plus(admin)/tailwindcss(portal)/axios

**Storage**: MySQL 8.4（新增 `files` 表，迁移 `0004_files`）；阿里云 OSS（新增外部基础设施）

**Testing**: `github.com/stretchr/testify`（Go 单测/集成）；前端 vitest（上传组件与裁剪）

**Target Platform**: Linux server（Docker Compose）+ 浏览器（portal / admin）

**Project Type**: web-service（Go IdP，`:8080` 协议面 + `:8081` 管理 API）+ 两个 web 前端

**Performance Goals**: 图片字节不经服务端（直传 OSS）；凭证签发 < 200ms；单图 ≤ 5MB

**Constraints**: 凭证不出服务端（OSS master accessKeyId/accessSecret 仅服务端持有）；分层 `api → service → store`；结构变更走手写 SQL 迁移；错误码登记 AGENTS.md §5.3

**Scale/Scope**: 企业内部 IdP；场景 2 类（`app_logo` / `user_avatar`）；新增 1 张表 + 1 个 SDK 依赖 + 1 个外部基础设施

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 说明 |
|------|------|------|
| I. 协议层单一来源 | N/A | 本特性不触碰 OIDC/OAuth 协议面 |
| II. 端口与信任边界物理分离 | PASS | 仅新增 `:8081` 管理 API 端点，不触碰 `:8080` 协议面 |
| III. 分层与数据一致性 | PASS | `FileStore` 抽象落 service 层、`files` 表存取落 store 层，经 `api → service → store` 调用 |
| IV. 凭证不出服务端 | PASS | OSS master 密钥仅服务端持有；客户端仅拿预签名直传凭证；文件记录不含密钥 |
| V. 结构变更只走手写 SQL 迁移 | PASS | 新增 `0004_files.up/down.sql`；时间列 `BIGINT`（Unix 毫秒）；主键 ULID |
| 技术与接口约束 | PASS（有条件） | 新增 OSS SDK 依赖须**先更新 AGENTS.md §2.1** 并写明理由；配置由 `internal/config` 统一加载并校验；错误码登记 §5.3 |

## Project Structure

### Documentation (this feature)

```text
specs/004-object-storage-upload/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── file-upload-api.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/
├── config/
│   └── config.go            # 新增 OSS 配置（provider/endpoint/bucket/accessKeyId/accessSecret/publicBaseUrl/credentialTTL）
├── store/
│   ├── model/file.go        # File 模型（files 表）
│   ├── file_store.go        # files 表 CRUD（分页、状态更新）
│   └── filestore.go         # FileStore 接口 + 阿里云 OSS 实现（预签名凭证签发、删除对象）
├── service/
│   └── file.go              # 上传凭证签发、上传确认（写记录 + 回写业务字段）、生命周期
└── api/
    ├── files/               # POST /files/upload-credential（通用凭证签发）
    ├── me/avatar.go         # POST /me/avatar（本人头像确认）
    └── admin/
        ├── users_avatar.go  # POST /admin/users/:id/avatar（管理员代管头像）
        ├── clients_logo.go  # POST /admin/clients/:id/logo（logo 确认）
        └── files.go         # GET /admin/files（分页）+ PATCH /admin/files/:id（状态）

migrations/
├── 0004_files.up.sql
└── 0004_files.down.sql

web/
├── portal/src/components/ImageUpload.vue   # 共享单文件上传组件（含可选裁剪，library-agnostic）
└── admin/src/components/ImageUpload.vue    # 与 portal 一致的实现（行为/属性完全一致）
```

**Structure Decision**: 遵循既有分层与目录（`internal/{config,store,service,api}`）；新增 `files` 表与 `FileStore` 抽象分别落在 store 与 service 层；上传组件作为 library-agnostic 的 Vue SFC 在 portal 与 admin 各落一份源码，保持属性与交互完全一致（详见 research.md 决策 4）。

## Complexity Tracking

> 无宪法违规需豁免。新增阿里云 OSS SDK 属本特性必需的外部依赖，按宪法「技术与接口约束」先更新 AGENTS.md §2.1 再实施，不构成违规。
