# Research: 图片上传与对象存储（技术选型）

本文件沉淀 Phase 0 的技术决策，所有规格中的开放技术点在此落定。

## 决策 1 — 对象存储 SDK：`github.com/aliyun/aliyun-oss-go-sdk/oss`

- **Decision**: 采用阿里云 OSS 官方 Go SDK（`aliyun-oss-go-sdk/oss`）作为本期唯一对象存储实现，版本以实施时 `go get` 解析的稳定补丁版为准，落定后登记 AGENTS.md §2.1。
- **Rationale**: 首选供应商已确认为阿里云 OSS；官方 SDK 提供 bucket 管理、对象上传/删除、预签名 URL 等能力，与「provider 驱动的可插拔抽象」相符——本期只实现 `aliyun.oss`，后续 OBS/COS/S3/MinIO 各加一个实现即可。
- **Alternatives considered**:
  - S3 兼容 SDK（minio-go / aws-sdk-go-v2 指向 OSS 的 S3 兼容端点）：可一次覆盖多家，但配置语义与用户给定的 `provider=aliyun.oss + accessKeyId/accessSecret` 不一致，且 S3 兼容层对 OSS 的原生能力（如 PostObject 表单直传、ACL）支持不完整 → 拒绝。
  - 自研 HTTP 客户端封装 OSS REST API：无 SDK 依赖，但要自维护签名/错误处理 → 拒绝（违反「协议/凭证类缺陷成本最高，不自研」的精神）。

## 决策 2 — 直传机制：预签名直传（PostObject 表单直传）

- **Decision**: 服务端用自己的 `accessKeyId`/`accessSecret` 签发**预签名上传凭证**，客户端据此直传 OSS；优先采用 **PostObject 表单直传**（可在 policy 中约束 key 前缀、`Content-Type`、`Content-Length` 上限与过期时间），并回传 `uploadUrl`、`key`、`policy`、`signature`、`accessKeyId`、`expiresAt`。
- **Rationale**: 与用户给定的配置模型一致（服务端持有 master 密钥，客户端只拿到短时签名，不拿 master 密钥，满足宪法「凭证不出服务端」）；policy 可把单文件 ≤5MB、格式、场景前缀等约束前置到 OSS 侧；图片字节不流经服务端，规避内存/带宽开销。
- **Alternatives considered**:
  - 预签名 PUT（`SignURL`，SDK 原生）：实现更简，但约束（类型/大小/前缀）依赖客户端自觉，且跨域 PUT 配置更繁 → 作为降级备选，主推 PostObject。

> **修订（2026-10-21）**：由「服务端 master AK/SK 直签」改为 **STS AssumeRole**——服务端用 RAM 用户长期凭证调用 STS AssumeRole（经 `ramossrole` 角色）获取临时凭证（AccessKeyId/AccessKeySecret/SecurityToken），再用临时密钥签发 PostObject policy 并回传 `securityToken`（客户端表单字段 `x-oss-security-token`）。master 密钥不再进入直传签名路径，凭证粒度更细且到期自动续期。

## 决策 3 — `FileStore` 接口边界

- **Decision**: 抽象定义在 service 层，方法仅含对象存储必需能力，不含文件记录 CRUD（记录走 store 层 `file_store.go`）：
  - `CreateUploadCredential(ctx, scene, format, size) (*UploadCredential, error)` — 签发直传凭证；
  - `DeleteObject(ctx, objectKey) error` — 替换/清除时删除旧对象（FR-008）；
  - `ResolvePublicURL(objectKey) string` — 生成公开访问地址。
  - `UploadCredential` 携带 `{key, uploadUrl, accessKeyId, policy, signature, expiresAt}`。
- **Rationale**: 把「对象存储供应商操作」与「业务元数据（files 表）」分离，前者可插拔（provider 驱动），后者走既有 `api → service → store` 分层；上传确认时 service 同时写 `files` 记录并回写业务字段。
- **Alternatives considered**:
  - 把文件记录也塞进 FileStore 接口：会让「供应商抽象」与「业务持久化」耦合，后续加供应商时被迫复写记录逻辑 → 拒绝。

## 决策 4 — 上传组件共享方式：library-agnostic 的 Vue SFC

- **Decision**: 上传组件用不依赖 Element Plus / Tailwind 的 **library-agnostic Vue SFC**（原生 `<input type="file">` + Canvas 裁剪 + axios），在 `web/portal` 与 `web/admin` 各落一份**同源实现**，属性与交互完全一致（FR-016）。组件属性：`scene`、`accept`、`maxSize`、`enableCrop`（默认 `false`）、`cropAspectRatio`；事件：`success(url)`、`error(msg)`。
- **Rationale**: portal（Tailwind）与 admin（Element Plus）是两个独立 Vite 应用，无法轻易共享单文件模块；用不绑 UI 库的组件保证「完全一致」的交互与裁剪行为，同时避免引入跨端共享包的构建复杂度。
- **Alternatives considered**:
  - 抽成 npm 私有包/workspace 共享：行为最一致，但需引入 monorepo/私有源基建，超出本期范围 → 拒绝。
  - 各端各写一套：行为易漂移，违反 FR-016「完全一致」 → 拒绝。

## 决策 5 — 裁剪实现：客户端 Canvas 裁剪，默认关闭

- **Decision**: 裁剪在客户端完成（读取本地图片 → Canvas 按矩形区域裁剪 → 产出 Blob 再直传），由组件属性 `enableCrop` 控制，默认关闭（FR-018）。裁剪比例默认 `1:1`（头像），logo 场景可由调用方指定。
- **Rationale**: 裁剪发生在直传之前，避免把原图与裁剪图都传上去浪费存储与带宽；纯前端 Canvas 即可满足，无需服务端图像处理。
- **Alternatives considered**:
  - 服务端裁剪（上传原图后服务端裁剪）：与「直传、服务端不碰字节」矛盾，且引入图像处理依赖 → 拒绝。

## 决策 6 — 枚举取值与配置

- **Decision**:
  - 场景枚举 `scene`: `app_logo` / `user_avatar`（字符串）。
  - 状态枚举 `status`: `active` / `disabled` / `deleted`（字符串）。
  - 配置（`internal/config`，环境变量）：`OSS_PROVIDER`（默认 `aliyun.oss`）、`OSS_ENDPOINT`、`OSS_BUCKET`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_SECRET`、`OSS_PUBLIC_BASE_URL`、`OSS_CREDENTIAL_TTL`（默认 300s）。provider 不同则所需配置项不同，由 `validate()` 按 provider 校验（如 `aliyun.oss` 必填 accessKeyId/accessSecret）。
- **Rationale**: 满足用户「配置由 provider 决定」的要求；必填项缺失启动失败（宪法「环境变量统一加载并校验」）。

## 遗留（Non-blocking，交 tasks/实现）

- PostObject 的 policy 签名需手工 HMAC-SHA1（SDK 无现成表单签名助手），实现阶段落地并补单测。
- 存量外部 URL 不迁移，仅新上传走新链路（规格 Assumptions 已定）。
