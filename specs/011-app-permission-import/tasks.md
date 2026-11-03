---
description: "Task list for 011-app-permission-import"
---

# Tasks: 应用权限点批量导入

**Input**: Design documents from `/specs/011-app-permission-import/`

**Prerequisites**: [plan.md](./plan.md)（必需）、[spec.md](./spec.md)（必需）、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**Tests**: 本项目不接受「测试可选」：宪法「开发工作流与质量门禁」要求新增或修改业务逻辑、端点、数据访问层时同步交付测试。因此每条实现类任务**自带对应测试（同一次提交）**，测试不单独堆到最后的 Polish 阶段。

**Organization**: 按 user story 分组，保证每个 story 可独立实现与独立验收。

## 任务完成定义（DoD，来自 AGENTS.md §7.4）

一条任务只有在下列四项同时满足时才算完成，缺一不得勾选：

1. **代码**：行为符合 `spec.md` 与 `contracts/`，无 TODO / 空实现；
2. **测试**：同一提交内含单测，或（涉及真库行为时）门控集成用例；
3. **登记**：新增/变更的路由、错误码、审计事件、环境变量、表结构、依赖版本同步写入 `AGENTS.md` §2 / §4.1·§4.6 / §5.3 / §6.7 / §9；
4. **门禁**：`make build && make test && make lint && make check` 全绿。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: US1…US6，对应 `spec.md` 的用户故事
- 所有路径均为仓库相对路径；后端 `internal/…`、前端 `web/admin/…` 与 `web/portal/…`

## 本特性的固定口径（写任务时已内联，实现不得自行改）

| 项 | 值 | 出处 |
|---|---|---|
| 条数上限 | `maxImportPermissions = 1000` | spec FR-013 |
| 体积上限 | `maxImportBodyBytes = 512 << 10`（512KB，`http.MaxBytesReader`，仅导入端点） | spec FR-013 / research R2 |
| 层级上限 | **复用** `maxPermissionDepth`（`internal/service/app_auth.go` 既有 100），不新增常量 | spec FR-012 / research R6 |
| 问题清单上限 | `maxImportIssues = 20`（另给 `issueTotal`）；`warnings` 同规则；`movedItems` ≤20 | spec FR-005 / FR-016 |
| 来源 | query `?source=paste\|file`，缺省与非法值一律 `paste` | research R1 |
| 错误码 | `APP_PERMISSION_IMPORT_INVALID`(400)、`APP_PERMISSION_IMPORT_TOO_LARGE`(413)；冲突复用 `APP_RESOURCE_STALE`(409) | spec FR-029 / research R7 |
| 预览语义 | 内容非法仍 **200 + `ok=false` + `issues`**；只有 Content-Type 不对/body 读不了才 400 | research R4 |
| 导入体积判定 | `importable.too_large` 用 **2 空格缩进** 序列化字节（与下载文件逐字节一致） | research R9 / spec FR-019 |
| 审计口径 | 成功仅当 `added>0\|\|updated>0` 才写 1 条；失败仅"业务拒绝"写；预览永不写 | spec FR-023 / FR-025 |

---

## 完成记录（2026-11-04）

40/40 全部完成，门禁全绿。复核命令与结果：

| 门禁 | 命令 | 结果 |
|---|---|---|
| 快速档 | `make verify-fast`（lint + check + go 单测 + portal/admin vitest） | ✅ exit 0 |
| 构建 | `make build`（含 `docs-check` 漂移比对） | ✅ |
| 集成 | `TEST_MYSQL_DSN=…/omino_sso_test TEST_REDIS_ADDR=… make test-integration` | ✅ service/store/auth/me 全 ok |
| 注册表 | `bash scripts/check-registry.sh` | ✅ 0 漂移（路由 / 错误码 / 事件 / 变量） |
| 产物引用 | `make check`（§3 目录树 + tasks.md 路径） | ✅ |

**关键落地物**

- 后端：`internal/service/app_permission_import.go`（解析 / `buildImportPlan` / 预览 / 确认 / 导出 / `evaluateImportable`）、`internal/api/admin/app_permission_import.go`（3 handler + `importFailureReason` / `importAuditFields`）、`internal/api/me/app_admin.go` 3 个 wrapper。
- 后端测试：`internal/service/app_permission_import_test.go`（纯函数，7 组）、`internal/api/admin/app_permission_import_test.go`（请求体边界 / 映射表 / 409 不降级）、`internal/service/app_permission_import_integration_test.go`（12 个端点级用例，含 `TestReimportNoopWritesNoAudit`、`TestImportWritesExactlyOneAuditEvent`、`TestImportPreviewWritesNoAudit`、`TestImportFailureReason`、`TestExportImportRoundTrip`、`TestPermissionImportPreviewParity`、`TestExportDoesNotTruncate`、并发 409）。
- 前端：两端 `utils/file.ts`（512KB 预检 + 2 空格缩进下载）、`PermissionImportDialog.vue`（示例下载 / 问题清单 / 提示 / 上级变更明细 / ok 门控 / 失败保留输入 / 409 自动重跑 / 413 重新校验）、权限页入口按钮（admin `ClientPermissions.vue`、portal `Permissions.vue`），共 4 个 spec。
- 登记与基线：`AGENTS.md` §3（新增文件 + `app_admin.go` 15→18）、§4.1（门户 3 + admin 3）、§4.6（3 条字段与错误码）、§5.3（2 个错误码）、§6.7（`app_permissions_imported` 全量口径）；`openapi.json` 84→90 operation、`docs_test.go`/`router_test.go` 断言 90、`manifest.json` 新 sha256。

**与 quickstart 的差异说明**：§7.3「403 与 404 逐字节相同」属认证中间件行为（既有 006 用例覆盖），本特性集成用例只断言两端 404 语义一致、`data=null`；§7.4 的"admin 403 文案与应用停用"已由 `TestImportFailureReason/client_disabled` 覆盖。

---

## Phase 1: Setup（共享骨架）

**Purpose**: 错误码、审计事件、传输 DTO 三件事先立起来，后续所有 story 都引用它们。

- [x] T001 新增两个错误码常量 `ErrAppPermissionImportInvalid`(400, `APP_PERMISSION_IMPORT_INVALID`, 文案含"内容不合法，请先校验预览") 与 `ErrAppPermissionImportTooLarge`(413, `APP_PERMISSION_IMPORT_TOO_LARGE`, 文案含"单次最多 1000 条 / 512KB，请裁剪后重试")，放在 `internal/api/response/errors.go` 的 `APP_PERMISSION_*` 分组内；同步在 `AGENTS.md` §5.3 错误码表登记这两行（code / HTTP / 说明），使 `scripts/check-registry.sh` 的 `errors.go ⊆ §5.3` 双向校验通过
- [x] T002 [P] 新增审计事件常量 `EventAppPermissionsImported = "app_permissions_imported"`，放在 `internal/store/model/audit_event.go` 既有 `app_*` 常量块内；同步把该值加入 `AGENTS.md` §6.7 的 `event_type` 枚举**同一行**（`check-registry.sh` 只解析含"新增需登记"的那一行），此时事件说明段落留到 T036 补齐
- [x] T003 [P] 在 `internal/api/dto/dto.go` 新增传输结构：`PermissionImportDocument{Version *int, Mode string, Permissions []PermissionImportNode}`、`PermissionImportNode{Code, Name, Description string, Sort *int, Children []PermissionImportNode}`、`PermissionImportIssue{Path, Field, Code, Message string, Line *int}`、`PermissionImportPreview{Ok bool, Added, Updated, Unchanged, Moved, Total int, Issues []..., IssueTotal int, Warnings []..., WarningTotal int, MovedItems []PermissionImportMoved{Code, FromParent, ToParent}}`、`PermissionImportResult{Added, Updated, Unchanged, Moved, Total int, Source string}`、`PermissionExportDocument{Version int, Mode string, Importable PermissionExportable, Permissions []PermissionImportNode}`、`PermissionExportable{Ok bool, Reasons []string}`；JSON 字段一律 camelCase，可选字段加 `omitempty`

---

## Phase 2: Foundational（阻塞所有 story）

**Purpose**: store 批量读写 + 文档解析纯函数。这里的三组能力被 US1–US6 共同依赖，必须先落地。

**⚠️ 关键约束**：`internal/store/app_auth_store.go` 的三个任务改同一个文件，**必须串行**（T004 → T005 → T006），不得并行。

- [x] T004 在 `internal/store/app_auth_store.go` 新增 `LockClientRow(ctx, clientID) error`（`SELECT ... FROM clients WHERE client_id = ? FOR UPDATE`，无行时返回既有 `ErrClientNotFound`）与 `LockClientPermissions(ctx, clientID) ([]model.ClientPermission, error)`（同一事务内 `SELECT ... WHERE client_id = ? FOR UPDATE`，按 `sort, created_at` 排序）。注释里写明：先锁 `clients` 行是为了在"该应用尚无任何权限点"时得到确定的记录锁，避免 002 记录过的空表间隙锁死锁
- [x] T005 在 `internal/store/app_auth_store.go` 新增 `CreateClientPermissions(ctx, perms []*model.ClientPermission) error`（`CreateInBatches(..., 100)`）与 `UpdateClientPermissionFields(ctx, clientID, id string, fields map[string]any, now time.Time) error`（无乐观锁，注释注明"调用方必须已持有 T004 的行锁"；`fields` 只允许 `name/description/sort/parent_id/updated_at`）
- [x] T006 在 `internal/store/app_auth_store.go` 新增 `ListClientPermissionsForExport(ctx, clientID) ([]model.ClientPermission, error)`：**不带 `LIMIT`**、**不带 `child_count`/`role_count` 子查询**，按 `sort ASC, created_at ASC` 排序；注释引用 spec FR-019，说明不得复用 `ListClientPermissions`
- [x] T007 新建 `internal/service/app_permission_import.go`：定义 `AppPermissionImportService`（依赖 `*AppAuthStore`）、常量 `maxImportPermissions = 1000` / `maxImportBodyBytes = 512 << 10` / `maxImportIssues = 20`，以及纯函数 `parseImportDocument(data []byte) (*importDoc, []dto.PermissionImportIssue, []dto.PermissionImportIssue)`。解析要求：用 `json.Decoder` 逐层解到 `map[string]json.RawMessage` 比对白名单键（根 `{version, mode, permissions}`，节点 `{code, name, description, sort, children}`），**未知字段一律报 `unknown_field` 并指出父级路径**；`version` 仅接受 `1`（缺省视为 1）、`mode` 仅接受 `"merge"`（缺省视为 merge，`replace` 文案含"暂不支持全量对齐"）；`code` 先 `TrimSpace` 再匹配既有 `permCodePattern`（`^[A-Za-z0-9_.:-]{1,64}$`）；`name` 必填且 `len([]byte) <= 64`；`description` `<= 255` 字节；`sort` 必须是整数；`children` 必须是数组；语法错误用 `json.SyntaxError.Offset` 换算行号写入 `issue.Line`。同时在同一目录新增测试文件（`app_permission_import_test.go`）做表驱动单测，覆盖：空内容→`empty`、非法 JSON→`syntax_error`+行号、`version:2`→`bad_version`、`mode:"replace"`→`bad_mode`、未知字段 `id`→`unknown_field` 且 `path="permissions[0].id"`、`code` 含空格→`invalid_code`、`name` 65 字节→`too_long`、缺 `name`→`missing_field`、`children` 不是数组→`bad_type`
- [x] T008 [P] 新建 `web/admin/src/utils/file.ts`：`readJsonFile(file: File): Promise<string>`（校验 `file.size`，>512KB 抛错并给出"超过 512KB 上限"文案）与 `downloadJson(name: string, data: unknown): void`（`JSON.stringify(data, null, 2)` → `Blob` → `URL.createObjectURL` → `a[download]` → `revokeObjectURL`）；同时交付 `web/admin/src/utils/file.spec.ts`（断言下载调用了 `createObjectURL` 一次且 `revokeObjectURL` 被调用、序列化为 2 空格缩进）

---

## Phase 3: User Story 1 - 一份 JSON 一次性导入大量权限点（P1）🎯 MVP

**Goal**: 粘贴/选文件 → 校验预览（三态计数 + 问题清单）→ 确认导入（单事务写入），全程一条处理路径。

**Independent Test**: 空权限应用粘贴 50 条 JSON → 校验显示"将新增 50 条"且列表仍为空 → 确认后 50 条全部出现，字段逐条与输入一致。

- [x] T009 [US1] 在 `internal/service/app_permission_import.go` 实现 `ValidateImport(ctx, clientID string, data []byte) (*dto.PermissionImportPreview, error)`：`ensureClientActive`（存在且 `status==1`）→ `parseImportDocument` → 一次 `LockClientPermissions` 之外的只读全量读（复用 `ListClientPermissions` 会被 `LIMIT 1000` 截断，改用 T006 的 `ListClientPermissionsForExport` 只读调用或新增只读变体）→ 内存 diff 得 `added/updated/unchanged` → 截断 `issues`/`warnings` 到 20 条并给总数。**`ok=false` 时四项计数一律归零**。单测：同一份 50 条文档两次调用返回相同计数且库里仍为 0 条
- [x] T010 [US1] 在同一文件实现 `ConfirmImport(ctx, clientID string, data []byte, source string) (*dto.PermissionImportResult, error)`：`store.Transaction` 内按序执行 ① `LockClientRow` ② `LockClientPermissions` ③ 重算 diff（不信任预览）④ `CreateClientPermissions` 写新增、`UpdateClientPermissionFields` 写变更；任一步失败整事务回滚。集成测试（`TEST_MYSQL_DSN`，沿用 `app_auth_integration_test.go` 的 `newAppAuthEnv`）：导入 50 条后库里恰好 50 条，`code/name/description/sort` 与输入逐条相等；中途构造一条非法 parent 使事务失败时库里仍是 0 条（无半成品）
- [x] T011 [US1] 新建 `internal/api/admin/app_permission_import.go`，实现 `ValidatePermissionImport` 与 `ConfirmPermissionImport`：读取 `source` query（白名单 `switch`，`default` 落 `paste`）、用 `http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)` 读 body，命中 `*http.MaxBytesError` → 413 `ErrAppPermissionImportTooLarge`；`Content-Type` 非 `application/json` → 400 `VALIDATION_FAILED`；预览恒 200；确认导入遇 service 的 `ErrAppPermissionImportInvalid` → 400、`ErrAppResourceStale` → 409。同时在 `internal/api/admin/` 下新增测试文件（`app_permission_import_test.go`）断言：坏 JSON 走确认导入返回 400 且 `data=null`；body 超 512KB 返回 413 且 `code=APP_PERMISSION_IMPORT_TOO_LARGE`；预览对非法内容返回 **200 + `ok=false` + 非空 `issues`**
- [x] T012 [US1] 在 `internal/api/router.go` 的 `adminGroup` 注册 `POST /admin/clients/:id/permissions/import/validate` 与 `POST /admin/clients/:id/permissions/import`（挂在 `h.appAuth` 上，与既有权限端点同组）；同步在 `AGENTS.md` §4.6 登记这两行（方法 / 路径 / 说明 / 鉴权档位）
- [x] T013 [US1] 在 `web/admin/src/api/index.ts` 增加 `validatePermissionImport(clientId, doc, source)` 与 `confirmPermissionImport(clientId, doc, source)`（`POST`，query 带 `source`），在 `web/admin/src/api/types.ts` 增加与 T003 对齐的类型（`PermissionImportPreview` / `PermissionImportResult` / `PermissionImportIssue` / `PermissionImportNode`）
- [x] T014 [US1] 新建 `web/admin/src/views/client/PermissionImportDialog.vue`：`el-dialog` + `el-tabs`（粘贴 / 选择文件，文件走 T008 的 `readJsonFile`）、"下载示例"按钮（静态 3 层 5 条 JSON + 字段对照 + "表格行 → 嵌套 JSON"三行对照）、"校验预览"按钮、预览结果区（`ok` 三态计数、`issues` 列表显示 `path`+`message`、超 20 条显示"共 N 条"）、"确认导入"仅 `ok=true` 时可用、失败后**输入框内容必须保留**、前端仅做空值与 `JSON.parse` 校验。同时交付 `PermissionImportDialog.spec.ts`：非法 JSON 不发请求、`issues` 渲染、`ok=false` 时按钮 disabled、请求失败后 textarea 内容不变
- [x] T015 [US1] 在 `web/admin/src/views/client/ClientPermissions.vue` 头部加"批量导入"按钮并挂载 `PermissionImportDialog`（导入成功后 `refresh` 权限树，并提示实际三态计数）

**Checkpoint**: US1 可独立验收（spec US1 的 6 条验收场景 + SC-001）。

---

## Phase 4: User Story 2 - 层级用嵌套表达，导入后直接成树（P1）

**Goal**: `children` 递归解析、层级深度校验、跨层重复、前缀提示、父级改写（含 `movedItems`），保证"父先子后落库、不改未出现的节点、不成环"。

**Independent Test**: 三层 `order → order.write → order.write.refund` 导入后树形正确；101 层被校验拒绝并指出路径；把 `order.read` 从 `order` 挪到 `pay` 后预览显示 `moved=1` 且确认后上级变更。

- [x] T016 [US2] 扩展 `parseImportDocument`（`internal/service/app_permission_import.go`）：递归 `children` 建立节点树并记录每节点的 `path`（`permissions[i].children[j]…`）；`code` 全局唯一性检测（跨层重复报 `dup_code` 并在 `message` 里列出**两处以上** `path`）；深度超过 `maxPermissionDepth` 报 `too_deep`（文案含路径与实际深度，且**不再深入该子树**避免刷屏）；`prefix_hint`（子 code 不以父 code 为前缀，仅 `warnings`，不阻断）与 `case_hint`（忽略大小写相同，仅提示、仍作为两个权限点导入）。单测：12 条三层样例成功；101 层报 `too_deep`；两条 `order.read` 跨层重复报 `dup_code` 且两处 path 都在；`pay.refund` 挂在 `order` 下只产生 `prefix_hint` 且 `ok=true`；`order.read` 与 `Order.Read` 只产生 `case_hint`
- [x] T017 [US2] 在 `internal/service/app_permission_import.go` 新增 `buildImportPlan(doc, existing []model.ClientPermission) (*importPlan, []dto.PermissionImportIssue)`：以 `code` 建 `CodeToID`（既有行 + 本次新增的 ULID）、按输入树 BFS 父先子后，根级 `parent_id=NULL`；`moved` 与 `movedItems`（`fromParent`/`toParent` 用 **code** 表达，根级为空串，最多 20 条）在 **code 空间**比较（不得比较内部 ID）。单测：`A→B` 改为 `B→A` 时计划里 `B.parent=NULL`、`A.parent=B` 且不成环；未出现在输入中的既有节点不在 `Update` 列表内
- [x] T018 [US2] 把 `internal/service/app_permission_import.go` 里 `ConfirmImport` 的写入改为按 T017 的层序执行（父先子后），未出现在输入中的节点一律不动。集成测试：① 三层 12 条导入后父子关系正确；② 子节点写在父节点之前的数组顺序也正确；③ `order.read` 从 `order` 挪到 `pay` 后 `parent_id` 变更且既有角色绑定（`client_role_permissions`）行数不变；④ `A/B` 互换后树为 `B → A` 且无环
- [x] T019 [US2] 在 `PermissionImportDialog.vue` 的预览区增加 `movedItems` 明细表（权限编码 / 原上级 / 新上级，空值显示"根级"）与 `warnings` 区（区分"提示，不阻断"），并在 `movedItems` 超 20 条时显示"共 N 条，仅列前 20"；`PermissionImportDialog.spec.ts` 补三条断言（`movedItems` 渲染、警告区不阻断确认、超 20 条文案）

**Checkpoint**: US2 可独立验收（spec US2 的 8 条验收场景）。

---

## Phase 5: User Story 3 - 重复导入不产生重复数据（P2）

**Goal**: 按 `code` 幂等合并；全量无变化不写审计；并发下不产生重复或半成品，冲突给"请刷新后重试"。

**Independent Test**: 20 条导入后原样再导一次 → `新增 0 / 更新 0 / 无变化 20`，总数仍 20，审计无新增记录。

- [x] T020 [US3] 在 `internal/service/app_permission_import.go` 的 `ConfirmImport` 中实现审计成功事件：`added>0 || updated>0` 时写 1 条 `app_permissions_imported`（`Result="success"`，`Fields{clientId, added, updated, unchanged, moved, source, total}`）；**`added==0 && updated==0` 时一律不写**。集成测试 `TestReimportNoopWritesNoAudit`：连续两次导入 → `audit_events` 中该类型恰好 1 条；第二次返回 `unchanged=20`
- [x] T021 [US3] 在 `internal/service/app_permission_import.go` 的 `ConfirmImport` 增加并发冲突映射：捕获 1062 后经既有 `store.isDuplicate` 判定，映射为 `ErrAppResourceStale`（不得新增错误码）。集成测试：两个 goroutine 并发导入同一份 20 条文档，断言最终 `client_permissions` 恰好 20 条、无重复 `code`、至少一方成功；失败方得到的错误是 `ErrAppResourceStale`
- [x] T022 [US3] 在 `internal/api/admin/app_permission_import.go` 的 `failPermissionImport` 中把 `ErrAppResourceStale` 映射为 `409 APP_RESOURCE_STALE`，并补 handler 单测：service 返回 stale 时响应 409 且 `data=null`；`409` **不得**被映射成 400
- [x] T023 [US3] 在 `PermissionImportDialog.vue` 增加三类处置：`409` → 提示"已被他人修改，请刷新后重试"并**自动重跑一次预览**；`413` → 提示裁剪并给"重新校验"按钮；确认导入返回计数 ≠ 预览计数 → 提示"数据在预览后有变化，已按最新状态写入"。`PermissionImportDialog.spec.ts` 补三条断言

**Checkpoint**: US3 可独立验收（spec US3 的 5 条验收场景 + SC-003）。

---

## Phase 6: User Story 4 - 导出后再导入，跨环境搬运（P2）

**Goal**: 导出一份与导入同构的 JSON（无内部标识），并自证"能否回导"（`importable`，体积按下载文件字节判定）。

**Independent Test**: A（三层 30 条）导出 → B（空权限）导入 → B 的条数、编码、层级与 A 完全一致。

- [x] T024 [US4] 在 `internal/service/app_permission_import.go` 实现 `ExportPermissions(ctx, clientID) (*dto.PermissionExportDocument, error)`：用 T006 的 `ListClientPermissionsForExport` 读全量 → 建树（每层按 `sort, created_at`）→ 组装 `{version:1, mode:"merge", permissions}` → 计算 `importable`：`empty`（空）/`too_many`（>1000）/`too_deep`（>100）/`too_large`（**`json.MarshalIndent({version,mode,permissions}, "", "  ")` 的字节数 > 512KB**）。同时交付 `TestImportableMatchesDownloadBytes`：断言判定字节数 == `MarshalIndent` 长度、≤512KB→`ok=true`、>512KB→`ok=false`、空文档→`reasons=["empty"]`
- [x] T025 [US4] 在 `internal/api/admin/app_permission_import.go` 实现 `ExportPermissions` handler（统一响应包裹 `data`），并补 handler 单测：直插 1201 条后导出返回 `permissions` 长度 1201（证明未复用 `LIMIT 1000`）且 `importable={ok:false, reasons:["too_many"]}`
- [x] T026 [US4] 在 `internal/api/router.go` 的 `adminGroup` 注册 `GET /admin/clients/:id/permissions/export`；同步在 `AGENTS.md` §4.6 补第 3 行
- [x] T027 [US4] 在 `web/admin/src/api/index.ts` 增加 `exportPermissions(clientId)`（返回 `PermissionExportDocument`）；在 `ClientPermissions.vue` 增加"导出"按钮：`importable.ok=false` 时**下载前**按 `reasons` 提示（空 / 过多 / 过大 / 过深），`too_many` 附带"列表页最多展示 1000 条"，用户可继续下载；下载用 T008 的 `downloadJson`，文件名 `permissions-<clientId>-<yyyyMMdd>.json`，**下载内容只含 `{version, mode, permissions}`**（剔除 `importable`）。`PermissionImportDialog.spec.ts` 或 `ClientPermissions.spec.ts` 断言：下载对象不含 `importable` 键、`ok=false` 时先提示后下载
- [x] T028 [US4] 在 `internal/service/app_auth_integration_test.go` 同目录新增集成用例 `TestExportImportRoundTrip`：A 造 30 条三层（含 `order.item:read` 这类特殊字符编码）→ 导出 → 导入空应用 B → 断言 B 的 `code` 集合、父子关系与 A 完全一致；再把整份导出文档原样回导 A，断言 `added=0, updated=0, unchanged=30`

**Checkpoint**: US4 可独立验收（spec US4 的 3 条验收场景 + SC-004）。

---

## Phase 7: User Story 5 - 门户应用管理员具备同等能力（P2）

**Goal**: 门户侧 3 个端点复用同一 handler，仅加 `appAdmin` 鉴权；两端结论逐字一致。

**Independent Test**: 应用管理员在门户导入 20 条后 admin 端立即可见；非该应用管理员被 403；同一份 JSON 两端结论一致。

- [x] T029 [US5] 在 `internal/api/me/app_admin.go` 新增 3 个薄 wrapper（`AppValidatePermissionImport` / `AppConfirmPermissionImport` / `AppExportPermissions`），各自带 swag 注释（`@Tags 门户应用管理`、`@x-auth-level appAdmin`、`@Param source query string false`），body 直接转调 `h.appMgmt` 的对应方法
- [x] T030 [US5] 在 `internal/api/router.go` 的 `appAdminGroup` 注册 3 条门户路由；同步在 `AGENTS.md` §4.1 登记，并补 `internal/api/router_test.go` 断言：3 条路由已注册、未登录访问返回 401、非应用管理员返回 403
- [x] T031 [US5] 在 `web/portal/src/api/app.ts` 增加 3 个方法（`/me/apps/:id/permissions/import*` 前缀），`web/portal/src/api/types.ts` 复用与 admin 相同的类型定义；新建 `web/portal/src/utils/file.ts`（内容与 `web/admin/src/utils/file.ts` 一致，不跨工程共享）
- [x] T032 [US5] 复制 `PermissionImportDialog.vue` 到 `web/portal/src/views/app-admin/PermissionImportDialog.vue`（EP 组件**局部注册**，对齐同目录 `Permissions.vue` 的写法），在 `web/portal/src/views/app-admin/Permissions.vue` 挂载按钮与对话框；同时交付 `web/portal/src/views/app-admin/PermissionImportDialog.spec.ts`，断言与服务端返回同构的 issues 文案与新三态计数渲染
- [x] T033 [US5] 在 `internal/service/app_auth_integration_test.go` 同目录新增集成用例 `TestPermissionImportPreviewParity`：同一份含 3 处非法（非法编码、跨层重复、未知字段）的文档，分别对 admin 端点与门户端点调用，断言 `data.ok` 相同、`issues` 的 `path`/`code`/`message` 逐一相等；再断言门户端对"应用不存在"与"非管理员"返回**同一份响应体**（不泄露存在性）

**Checkpoint**: US5 可独立验收（spec US5 的 4 条验收场景 + SC-007）。

---

## Phase 8: User Story 6 - 批量导入可追溯（P3）

**Goal**: 审计可回答"谁、何时、为哪个应用、新增/更新多少、来源、结果与失败分类"，且误导入留有可处置线索。

**Independent Test**: 成功导入 1 条事件（含计数/来源/操作者）、预览 0 条、内容非法失败 0 条、业务拒绝失败 1 条（含 `reason`）。

- [x] T034 [US6] 在 `internal/service/app_permission_import.go` 的 `ConfirmImport` 失败路径与成功路径补齐审计字段：失败仅在 **业务拒绝**时写（`ErrAppPermissionImportTooLarge`→`to_large`、`ErrAppResourceStale`→`stale`、应用不存在→`client_not_found`、应用停用→`client_disabled`），`Fields{clientId, reason}`、`Result="failure"`；成功事件追加 `addedIds`（本次新增 ID，**>20 项只记前 20**）、`addedCount` 与 `docSha256`（`sha256.Sum256(data)` 十六进制）；`APP_PERMISSION_IMPORT_INVALID` 与 401/403 **不得**写事件
- [x] T035 [US6] 在 `internal/service/app_auth_integration_test.go` 同目录新增集成用例三件套：`TestImportWritesExactlyOneAuditEvent`（一次 1000 条导入恰好 1 条事件、`detail.addedIds` 长度 ≤20 且 `addedCount=1000`、`docSha256` 为 64 位十六进制）、`TestImportPreviewWritesNoAudit`（连续 5 次预览、含非法内容，事件数 +0）、`TestImportFailureReason`（四类业务拒绝分别落 `reason`，内容非法 +0）
- [x] T036 [US6] 在 `AGENTS.md` §6.7 补齐 `app_permissions_imported` 的事件说明：成功/失败同 Type 用 `result` 区分；**全量无变化不写**、**内容非法与 401/403 不写**、**预览不写**；`reason` 取值枚举；`source` 由 query 声明**仅供统计、不得作为安全依据**；`addedIds` 的 20 条截断规则

**Checkpoint**: US6 可独立验收（spec US6 的验收场景 + SC-008）。

---

## Phase 9: Polish（跨 story 收口）

- [x] T037 运行 `make docs` 重新生成 `internal/api/docs/openapi.json`（新增 6 个 operation，84 → 90），更新 `internal/api/docs/docs_test.go` 的 operation 断言为 90，并更新 `specs/007-deployment/api-evolve/baseline/manifest.json` 的 `openapi.json` sha256
- [x] T038 补齐 `AGENTS.md` §3 目录树（新增 `internal/service/app_permission_import.go`(+test)、`internal/api/admin/app_permission_import.go`(+test)、`web/admin/src/utils/file.ts`、`web/admin/src/views/client/PermissionImportDialog.vue`、`web/portal/src/utils/file.ts`、`web/portal/src/views/app-admin/PermissionImportDialog.vue`），并跑 `make check`（`check-registry` / `check-artifact-refs` / `check-docs-commands` 全绿）
- [x] T039 按 `specs/011-app-permission-import/quickstart.md` §1–§9 逐条走查并记录结果（含 §5 的 1205 条导出、字节一致性、空应用 `empty`），发现与文档不符先改文档再改代码
- [x] T040 按 `Makefile` 与 `scripts/ci-local.sh` 的全门禁目标收尾：`make build && make test && make lint && make check` + `TEST_MYSQL_DSN=<专用测试库> make test-integration`（**必须指向专用测试库**，脚手架会 `DROP DATABASE`）

---

## Dependencies

### Phase 依赖

- **Phase 1（T001–T003）**：三者互相独立，可并行
- **Phase 2（T004–T008）**：`T004 → T005 → T006`（同一文件，严格串行）；`T007` 依赖 T001+T003（错误码 / DTO），与 T004 无编译依赖但语义上应先读 T004 的锁语义；`T008` 完全独立（前端）
- **Phase 3（US1）**：T009 依赖 T007+T006（只读全量）；T010 依赖 T009+T004+T005；T011 依赖 T009/T010+T001；T012 依赖 T011；T013→T014→T015 串行，仅依赖 T003 的类型口径
- **Phase 4（US2）**：T016 依赖 T009；T017 依赖 T016；T018 依赖 T010+T017+T005；T019 依赖 T014
- **Phase 5（US3）**：T020 依赖 T010（审计是 ConfirmImport 的收尾动作）；T021 依赖 T020；T022 依赖 T021；T023 依赖 T014+T022
- **Phase 6（US4）**：T024 依赖 T006+T007；T025 依赖 T024；T026 依赖 T025；T027 依赖 T008+T013+T025；T028 依赖 T010+T024
- **Phase 7（US5）**：T029 依赖 T011+T025（复用同一 handler）；T030 依赖 T029；T031 依赖 T008；T032 依赖 T031+T029；T033 依赖 T011+T029
- **Phase 8（US6）**：T034 依赖 T020；T035 依赖 T034；T036 依赖 T002
- **Phase 9**：全部依赖前面完成

### Story 完成顺序

US1（P1）→ US2（P1）→ US3（P2）→ US4（P2）→ US5（P2）→ US6（P3）

US3 与 US5 在 US1 完成后即可与技术无关地并行推进（前者只改 service/api，后者只改 me/router/portal 三处）；US4 依赖 US1 的解析器与 US2 的建树语义，建议在 US2 之后。

### 不算并行的三类"伪并行"

1. **`internal/store/app_auth_store.go`**：T004/T005/T006 同文件，必须串行
2. **`internal/api/router.go`**：T012/T026/T030 同文件，必须串行（且各自随所属 story 提交）
3. **`internal/service/app_permission_import.go`**：T007/T009/T010/T016/T017/T018/T020/T021/T024/T034 同文件，跨 story 时必须按顺序提交，否则冲突

---

## Parallel Example: Phase 1

```bash
# 三个互不相关的文件，可同时开工
Task: "T001 两个错误码 + AGENTS §5.3"          # internal/api/response/errors.go
Task: "T002 审计事件常量 + AGENTS §6.7"        # internal/store/model/audit_event.go
Task: "T003 导入/导出 DTO"                      # internal/api/dto/dto.go
```

## Parallel Example: US1 + 前端

```bash
# 后端 handler 落地后，前端两条线可并行
Task: "T013~T015 web/admin 的 API + 对话框 + 页面挂载"   # web/admin/**
Task: "T022 US3 的 409 映射（后端）"                     # internal/api/admin/app_permission_import.go
```

## Parallel Example: US5 + US6

```bash
# US1–US4 完成后，门户与审计两条线互不冲突
Task: "T029~T033 门户 3 wrapper + portal UI + parity 测试"   # internal/api/me/**、web/portal/**
Task: "T034~T036 审计字段 + 三条断言 + AGENTS §6.7"           # internal/service/app_permission_import.go、AGENTS.md
```

---

## Implementation Strategy

### MVP First（US1 only）

1. Phase 1 + Phase 2（错误码 / DTO / store 批量方法 / 解析器 / 前端文件工具）
2. Phase 3（US1）：预览 + 确认导入 + admin 对话框
3. **STOP and VALIDATE**：跑 spec US1 的 6 条验收场景 + quickstart §1
4. 此时"批量新建权限点"已可用（不含层级），可先给内部试用

### Incremental Delivery

| 增量 | 交付价值 | 验收 |
|---|---|---|
| + US2 | 层级成树（本特性的核心价值才完整） | spec US2 的 8 条场景 |
| + US3 | 可反复重导、不产生重复 | SC-003 |
| + US4 | 跨环境搬运闭环 | SC-004 |
| + US5 | 业务侧自助（不再走 IT） | SC-007 |
| + US6 | 可追溯 + 误导入可处置线索 | SC-008 |

### 提交粒度

每个 T 一条提交（含其测试与登记）。跨 story 的同文件任务（见"伪并行"）按 T 编号顺序提交，不做"先写完全部 service 再写 handler"的横向切分——否则 US1 无法独立验收。

---

## Notes

- **[P] 只标真正无冲突的任务**：同文件任务一律不加 [P]，即使它们属于不同 story
- **测试必须写在实现之前**（表驱动/集成夹具先红后绿），尤其 T007/T016/T017/T024 的纯函数用例
- **集成测试门控**：`TEST_MYSQL_DSN` 未设置时整组 skip，因此 CI 无库时不能作为"通过"依据，净检出必须跑 `make test-integration`
- **零迁移、零新依赖、零新环境变量**：任何任务若需要新增表、库或 env，先停下来说明——那说明设计被偏离了
- **不需要动的文件**（避免顺手优化）：`internal/oidc/**`、`internal/api/admin/app_auth.go`（已有 1049 行，新 handler 一律进 `app_permission_import.go`）、`internal/service/app_auth.go` 的既有单条 CRUD

---

## Phase 10: Convergence

> 2026-11-04 收敛评估（append-only）：对照 `spec.md`（FR-001…FR-031、SC-001…SC-008、6 个 story 共 30 条验收场景）、`plan.md`（架构落点 + 实施要点）与上表 40 条任务复核现有代码。门禁全绿但仍有 8 处「规范要求、代码或用例未覆盖」的缺口，按严重度排列。

- [x] T041 让门户端「确认导入」在应用被停用或不存在时同样写 1 条 `app_permissions_imported` 失败事件：当前 `middleware.AppAdmin`（`internal/api/middleware/auth.go` 的 `AuthorizeAppAdmin` 分支）在进入 handler 前就返回 403，请求到不了 `failPermissionImport`，导致 admin 端会记 `client_disabled` / `client_not_found` 而门户端 0 条；修法：为门户 3 条导入导出路由改用「只校验应用管理员、不校验应用状态」的中间件变体，把存在性/启用状态判定下沉到 service，并在门户 wrapper 里把 `ErrClientNotFound` 统一改写为 403（仍不泄露存在性）后再落 failure 审计 per FR-025 (partial)
- [x] T042 补集成用例固定「导入更新/改写父级不影响既有角色绑定」：预置角色并授予 `order.read`（写 `client_role_permissions`），导入把 `order.read` 换爹后断言绑定仍在、条数不变、角色权限列表仍含该点；覆盖 `UpdateClientPermissionFields` 只改 `code/name/description/parent_id/sort` 的既有语义 per FR-017, US2/AC7 (missing)
- [x] T043 补 SC-005 的可复现验证：用 gorm 回调或 `Session` 计数断言 1000 条 / 5 层输入的校验预览只产生固定次数的数据库往返（O(1)，不随条数增长），并加 `Benchmark` 记录预览与确认导入耗时基线 per SC-005, plan: Performance Goals (partial)
- [x] T044 补「写入中途失败整批回滚」的确定性用例：现有并发用例只覆盖唯一键冲突，需构造第二条写入必失败（注入失败 store 或事务内制造超长列值）后断言权限点集合与导入前逐条一致、条数不变 per FR-004, SC-006 (partial)
- [x] T045 补边界用例「被移动节点的既有子节点不被顺带移动」：预置 `order`→`order.read`→`order.read.self`，导入只声明 `order`/`order.read` 且把 `order.read` 挂到 `pay` 下，断言 `order.read.self` 的 `parent_id` 仍指向 `order.read`（只改输入中出现过的节点） per US2 边界条件 (partial)
- [x] T046 补体积上限的最坏合法载荷守护用例：构造 1000 条填满 `code`(64)/`name`(64)/`description`(255) 的文档，断言其 2 空格缩进序列化 < `MaxImportBodyBytes` 且校验通过，防止常量或字段上限漂移后「合法 1000 条被 413 拒」 per FR-013 (missing)
- [x] T047 把「下载示例文档」按钮补到权限页头部（admin `ClientPermissions.vue` 与门户 `Permissions.vue`），与「批量导入」「导出」并列，对话框内的入口可保留；当前仅在对话框内提供，与计划落点不一致 per plan: Source Code（`ClientPermissions.vue` 头部三按钮） (contradicts)
- [x] T048 修正 `TestPermissionImportPreviewParity` 的 404 断言口径：该用例直接调用门户 handler、绕过了 `AppAdmin` 中间件，断言「两端 404 一致」固化了生产路径不会出现的语义（生产门户对不存在应用统一 403）；改为经中间件+路由断言门户 403 且文案与 admin 的「应用不存在」不泄露存在性，或把断言降级为 handler 契约注释 per FR-021, plan: 两端一致性测试口径 (contradicts)

---

## Phase 11: Convergence

> 2026-11-04 复核（append-only）。本轮收敛识别出 3 处缺口，均已在随后的 `/speckit.implement` 中完成并联调通过。T049 实现过程中另发现并修复一处 FR-002/FR-025 违规：admin 端预览「应用不存在/已停用」原先会经 `failPermissionImport` 写失败审计（预览必须零留痕），现改走 `failPermissionPreview` 并补集成断言。

- [x] T049 按超限成因区分 413 文案：条数超限带本次实际条数（`本次共 N 条，超过单次上限 1000 条`），体积超限不声称知道条数、给上限并提示「可能是描述或名称过长所致」；顺带把预览失败改为零审计（`failPermissionPreview`） per FR-013, FR-002/FR-025 (partial)
- [x] T050 补门户权限页页面级用例 `web/portal/src/views/app-admin/Permissions.spec.ts`：三个头部入口并存、「下载示例」产出 3 层示例、导出文件只含 `version/mode/permissions`、不可回导先提示后下载（取消则不下载） per FR-020, SC-007 (partial)
- [x] T051 为 1000 条 / 5 层输入补基准：`BenchmarkImportPreview1000x5`（解析 + 建计划，约 7.5ms/op）与 `BenchmarkImportConfirmUnchanged1000x5`（全量无变化 diff，约 7.3ms/op），锁住 SC-005 的耗时基线 per SC-005, T043 (partial)
