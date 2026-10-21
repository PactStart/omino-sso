# Tasks: 图片上传与对象存储（头像与 Logo）

**Input**: Design documents from `/specs/004-object-storage-upload/`

**Prerequisites**: plan.md（技术栈与结构）、spec.md（用户故事与优先级）、research.md（决策）、data-model.md（实体）、contracts/file-upload-api.md（端点）

**Tests**: 项目宪法要求交付前通过 `make test` / `make lint`；故各阶段含最小必要测试任务，不采用严格 TDD 顺序。

**Organization**: 按用户故事分组，每个故事可独立实现、独立测试、独立交付。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 归属用户故事（US1/US2/US3/US4）
- 描述含精确文件路径

---

## Phase 1: Setup（共享基础设施）

**Purpose**: 依赖登记与基础设施配置

- [X] T001 更新 `AGENTS.md`：§2.1 登记 `github.com/aliyun/aliyun-oss-go-sdk/oss`（锁定版本 + MIT 许可证 + 理由）；§2.3 增加阿里云 OSS 外部基础设施；§5.3 登记审计事件类型 `file_uploaded`、`file_status_updated`；§9 增加 OSS 环境变量清单
- [X] T002 在根 `go.mod` 引入 `github.com/aliyun/aliyun-oss-go-sdk/oss` 并 `go mod tidy`（版本以解析的稳定补丁版为准）
- [X] T003 [P] 在 `.env.example` 增加 `OSS_PROVIDER=aliyun.oss`、`OSS_ENDPOINT`、`OSS_BUCKET`、`OSS_ACCESS_KEY_ID`、`OSS_ACCESS_SECRET`、`OSS_PUBLIC_BASE_URL`、`OSS_CREDENTIAL_TTL=300`

---

## Phase 2: Foundational（阻塞前置，所有故事依赖）

**Purpose**: 配置、数据表、FileStore 抽象、共享上传组件——任一故事开始前必须完成

**⚠️ 关键**: 本阶段未完成前不得开始任何用户故事

- [X] T004 在 `internal/config/config.go` 新增 `OSSConfig` 结构（Provider/Endpoint/Bucket/AccessKeyID/AccessSecret/PublicBaseURL/CredentialTTL），`Load()` 读取环境变量，`validate()` 按 provider 校验（`aliyun.oss` 必填 accessKeyId/accessSecret），必填缺失非零退出
- [X] T005 [P] 新增 `migrations/0004_files.up.sql` / `0004_files.down.sql`：`files` 表列 `id VARCHAR(26) PK`（ULID）、`provider VARCHAR(32) NOT NULL`、`scene VARCHAR(32) NOT NULL`、`format VARCHAR(16) NOT NULL`、`size BIGINT NOT NULL`、`url VARCHAR(512) NOT NULL`、`object_key VARCHAR(512) NOT NULL`、`status VARCHAR(16) NOT NULL`、`creator VARCHAR(26) NOT NULL`、`created_at BIGINT NOT NULL`、`updated_at BIGINT NOT NULL`；utf8mb4；索引 `(scene, created_at)` 与 `status`（见 data-model.md）
- [X] T006 [P] 新增 `internal/store/model/file.go` 定义 `File` 模型（gorm 列映射，`TableName()="files"`）
- [X] T007 新增 `internal/store/file_store.go`：`Create`、`List(scene,status,keyword,page,pageSize)` 分页、`UpdateStatus(id,status)`、`GetByObjectKey(key)`
- [X] T008 新增 `internal/store/filestore.go`：定义 `FileStore` 接口（`CreateUploadCredential`/`DeleteObject`/`ResolvePublicURL`）及阿里云 OSS 实现（PostObject 预签名：policy 约束 key 前缀、`Content-Type`、`Content-Length≤5MB`、过期，HMAC-SHA1 签名）
- [X] T009 新增 `internal/service/file.go`：`CreateUploadCredential(scene,format,size)`（校验枚举与大小）与 `ConfirmUpload(scene,key,format,size,targetID,actor)`（写 `files` 记录、按场景回写 `clients.logo_url` / `users.avatar_url`、替换时删除旧对象并置旧记录 `deleted`、发审计事件 `file_uploaded`）
- [X] T010 [P] 在 `internal/api/dto/dto.go` 新增 `UploadCredential`（key/uploadUrl/accessKeyId/policy/signature/expiresAt）与 `FileItem`（id/provider/scene/format/size/url/status/creator/createdAt）DTO 及构造器
- [X] T011 新增 `internal/api/files/` 包实现 `POST /api/v1/files/upload-credential`（需登录；`scene=app_logo` 需 admin），并在 `internal/api/router.go` 注册路由
- [X] T012 [P] 新增 `web/portal/src/components/ImageUpload.vue`：library-agnostic 单文件上传组件（原生 `<input type=file>` + Canvas 裁剪），props `scene/accept/maxSize/enableCrop(默认 false)/cropAspectRatio`，事件 `success(url)`/`error(msg)`；流程=选文件→（可选裁剪）→调 `upload-credential`→直传→调确认端点→回显
- [X] T013 [P] 新增 `web/admin/src/components/ImageUpload.vue`：与 portal 同源实现，属性与交互完全一致（FR-016）
- [X] T014 [P] 前端 api 客户端增加 files 相关类型与调用：`web/portal/src/api/client.ts` + `web/portal/src/api/types.ts`、`web/admin/src/api/*`（upload-credential、确认端点、文件分页/状态）

- [X] T015 [P] 新增 `internal/config/config_test.go` 的 OSS 校验用例（`aliyun.oss` 缺 accessKeyId/accessSecret 启动失败）
- [X] T016 [P] 新增 `internal/store/filestore_test.go`：预签名凭证 policy/signature 正确性（固定时间与密钥断言）
- [X] T017 [P] 新增 `internal/service/file_test.go`：`ConfirmUpload` 写记录 + 回写字段 + 替换删旧对象 + 状态转移

---

## Phase 3: User Story 1 — 管理员上传与管理应用 Logo (Priority: P1)

**Goal**: 管理员在应用管理内上传/替换/清除 logo，无需外部托管 URL（FR-001/002）

**Independent Test**: 管理员上传一张合规图片作为 logo，保存后「我的应用」列表立即显示；替换与清除即时生效，无需外部托管

- [X] T018 [US1] 新增 `internal/api/admin/clients_logo.go` 实现 `POST /api/v1/admin/clients/:id/logo`（admin；请求 `{key,format,size}`，调用 service `ConfirmUpload(scene=app_logo, targetID=clientId)`），在 `internal/api/router.go` 注册
- [X] T019 [US1] 在 `web/admin/src/views/Clients.vue` 与 `web/admin/src/views/client/ClientOverview.vue` 将 logo 文本框替换为 `ImageUpload.vue`（scene=app_logo，enableCrop 默认 false），上传成功后回显并提交 logoUrl
- [X] T020 [US1] 新增 `internal/service/file_test.go` 中 app_logo 场景用例：确认后 `clients.logo_url` 更新、旧 logo 对象被删除、审计事件发出

---

## Phase 4: User Story 2 — 员工自助头像 + 管理员代管 (Priority: P2)

**Goal**: 员工自助上传/替换/清除头像；管理员可后台代管（FR-003/004/011）

**Independent Test**: 员工在账号中心裁剪并上传头像，页面立即更新；管理员在后台为某用户设置/清除头像生效

- [X] T021 [US2] 新增 `internal/api/me/avatar.go` 实现 `POST /api/v1/me/avatar`（需登录；请求 `{key,format,size}`，调用 service `ConfirmUpload(scene=user_avatar, targetID=当前用户)`），在 `internal/api/router.go` 注册
- [X] T022 [US2] 新增 `internal/api/admin/users_avatar.go` 实现 `POST /api/v1/admin/users/:id/avatar`（admin；调用 service `ConfirmUpload(scene=user_avatar, targetID=目标用户, actor=管理员)`），在 `internal/api/router.go` 注册
- [X] T023 [US2] 在 `web/portal/src/views/Account.vue` 将只读头像替换为 `ImageUpload.vue`（scene=user_avatar，enableCrop=true 默认比例 1:1），上传/替换/清除即时生效
- [X] T024 [US2] 在 `web/admin/src/views/Users.vue` 用户行/抽屉接入头像管理（上传/清除，`ImageUpload.vue` + `POST /admin/users/:id/avatar`）
- [X] T025 [US2] 新增 `internal/service/file_test.go` 中 user_avatar 场景用例：自助与代管两条路径回写 `users.avatar_url` 均正确

---

## Phase 5: User Story 3 — 图片失联优雅降级 (Priority: P3)

**Goal**: logo/头像不可访问时回退占位符，无破损图（FR-007）

**Independent Test**: 清除或使某 logo/头像失效后，门户与账号中心显示首字/姓名占位而非破损图标

- [X] T026 [US3] 在 `web/portal/src/views/Apps.vue` 与 `web/portal/src/views/Account.vue` 确保 `@error` 回退占位（logo=名称首字方块、头像=姓名首字），并覆盖 `disabled`/`deleted` 状态的隐藏/占位逻辑
- [X] T027 [US3] 在 `web/admin/src/views/Clients.vue`、`web/admin/src/views/Users.vue` 列表的 logo/头像展示处同样接入 `@error` 占位降级

---

## Phase 6: User Story 4 — 管理员查看与管理上传文件记录 (Priority: P3)

**Goal**: 管理员分页查看上传文件记录并更新状态（FR-014/015）

**Independent Test**: 管理员分页浏览记录（含全部字段），更新某条状态后列表与展示方相应生效

- [X] T028 [US4] 新增 `internal/api/admin/files.go` 实现 `GET /api/v1/admin/files`（admin；分页 + scene/status/keyword 筛选）与 `PATCH /api/v1/admin/files/:id`（admin；请求 `{status}`，置 `disabled` 清空业务字段保留对象、置 `deleted` 删除对象并清空业务字段，且发 `file_status_updated` 审计事件），在 `internal/api/router.go` 注册
- [X] T029 [US4] 新增 `web/admin/src/views/Files.vue` 上传文件管理页（分页表格展示 provider/scene/format/size/url/status/creator/createdAt + 状态更新操作），在 `web/admin/src/router.ts` 注册路由
- [X] T030 [US4] 新增 `internal/service/file_test.go` 中状态转移用例：`active→disabled→deleted` 且 `deleted` 触发对象删除与关联字段回退

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 全链路回归与文档

- [X] T031 全链路回归：`make build`、`make test`、`make lint`、`make build-web` 全绿；`make docs` 更新 `internal/api/docs/openapi.json`（新增 6 个端点注解），`make docs-check` 通过
- [X] T032 [P] 按 `quickstart.md` 场景 A–D 手工端到端验证（logo 上传/裁剪头像/文件管理/失联降级），并核对 `api-evolve` 基线在实现后被 `speckit.api-evolve.diff` 正确识别为 additive 变更

---

## Dependencies

- **Foundational（Phase 2）** 阻塞所有用户故事。
- **US1（P1）** 可在 Foundational 完成后开始；**US2（P2）** 同；US1 与 US2 相互独立，可并行。
- **US3（P3）** 依赖 US1/US2 的展示接线，但可独立实现占位降级；**US4（P3）** 依赖 Foundational 的 files 存储，可独立于 US1/US2。
- 建议顺序：Foundational → US1（MVP）→ US2 → US3 → US4 → Polish。

### 故事内顺序

- Foundational 内：config → migration/model → store → filestore → service → dto/endpoint → 前端组件。
- 各故事内：endpoint → 前端接线 → 测试。

### 并行机会

- Phase 1/2 内所有 `[P]` 任务可并行（不同文件）。
- Foundational 完成后 US1/US2/US4 可由不同成员并行。
- 前端组件（T012/T013/T014）与后端 store/service 可并行。

---

## 并行示例：User Story 1

```bash
# 后端端点与前端接线可并行（不同文件）：
Task: "新增 internal/api/admin/clients_logo.go 实现 POST /admin/clients/:id/logo"
Task: "在 web/admin/src/views/Clients.vue 接入 ImageUpload.vue"
```

---

## Implementation Strategy

### MVP First（仅 User Story 1）

1. 完成 Phase 1（Setup）+ Phase 2（Foundational，关键阻塞）。
2. 完成 Phase 3（US1 管理员 Logo）。
3. **停止并验证**：按 US1 的 Independent Test 独立验收。
4. 可部署/演示（MVP 达成：管理员系统内管理 logo）。

### 增量交付

1. Setup + Foundational → 地基就绪。
2. + US1 → 独立验证 → 演示（MVP）。
3. + US2 → 独立验证 → 演示（员工自助头像）。
4. + US4 → 独立验证 → 演示（文件记录管理）。
5. + US3 → 独立验证 → 演示（失联降级）。
6. 每步不破坏既有故事。

### 并行团队策略

- 团队共同完成 Setup + Foundational。
- Foundational 完成后：开发者 A=US1、B=US2、C=US4，各自独立集成。

---

## Notes

- [P] = 不同文件、无依赖，可并行。
- [Story] 标签映射任务到用户故事，便于追踪。
- 每个故事独立可完成、可测试。
- 每个任务/逻辑组完成后提交（`/speckit.git.commit`）。
- 避免：模糊任务、同文件冲突、破坏故事独立性的跨故事依赖。

---

## Phase 8: Convergence

- [ ] T033 完善「清除 logo」：清除时删除 OSS 对象并更新 `files` 记录状态（而非仅清空 `logo_url`），确保旧图不再对外可访问 per FR-002/FR-008 (partial)
- [ ] T034 补齐「清除头像」入口：员工自助（`Account.vue`）与管理员代管（`Users.vue`）均支持清除头像并回退姓名首字占位 per FR-004/FR-011 (partial)
- [ ] T035 统一 provider 枚举值：spec/契约用 `aliyun.oss`，代码与 AGENTS.md §9 用 `aliyun_oss`，需对齐为单一取值 per FR-013/contracts (contradicts)
- [ ] T036 同步 admin 与 portal 的 `ImageUpload.vue` 组件行为（`confirm` 返回结构与事件集完全一致） per FR-016 (contradicts)
- [ ] T037 替换上传时旧文件清理（置 `deleted` + 删对象）补发 `file_status_updated` 审计事件 per FR-010 (partial)

---

## Phase 9: Convergence

- [X] T038 在 `web/admin/src/views/Users.vue` 用户编辑抽屉接入「清除头像」入口（调用已存在的 `clearUserAvatar`），使管理员可在后台清除任意用户头像并回退姓名首字占位 per FR-011/US2-AC6 (partial)
- [X] T039 统一对象存储 provider 枚举取值为单一值，消除运行时（`service/file.go` 写入、`internal/config/config.go` 校验、`AGENTS.md` §6/§9、`.env.example`、`data-model.md` 现为 `aliyun_oss`）与契约/文档（`dto.go` 示例、`openapi.json`、`spec.md`、`contracts/file-upload-api.md`、`research.md`、`quickstart.md` 现为 `aliyun.oss`）并存 per FR-013/contracts (contradicts)
- [X] T040 补齐图片加载失败（`@error`）占位降级：`web/portal/src/views/Account.vue` 头像、`web/admin/src/views/Users.vue` 列表头像、`web/admin/src/views/Clients.vue` 与 `client/ClientOverview.vue` logo 预览在 URL 存在但加载失败时回退占位，不显示破损图 per FR-007 (partial)
