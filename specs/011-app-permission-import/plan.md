# Implementation Plan: 应用权限点批量导入

**Branch**: `011-app-permission-import` | **Date**: 2026-11-03 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/011-app-permission-import/spec.md`

## Summary

在应用内权限点（`client_permissions`）之上增加一条"批量导入 / 导出"通道：管理员把一份以 `children` 嵌套表达层级的 JSON 文档（`{version?, mode?, permissions[]}`）交给服务端，先做一次**只读的校验预览**（返回"将新增 / 将更新 / 无变化"三态计数、其中"上级将变更"的条数与至多 20 条可核对的明细、以及可分位置的问题清单），确认后在**单个事务**内按 `code` 幂等合并（新增 + 更新，不删除），**有实际写入时**写 1 条聚合审计事件（全量无变化不写，见 research R8）。同一套能力在 admin 端与门户应用管理员端各暴露一组端点（门户侧复用同一 handler 实现，只加鉴权包装）。

关键约束来自既有实现，而非本特性：`client_permissions` 已有 `uk_client_permissions_code(client_id, code)`（`utf8mb4_0900_as_cs`）天然充当幂等键；既有写入路径是**单条** `Create` + 逐层 `ensureParent` 查库，1000 条输入若沿用它会产生十万级查询——因此必须新增"事务内一次读全量 + 内存 diff + 分层批量写入"的专用路径，而不是循环调 `CreatePermission`。导出一侧同理：既有 `ListClientPermissions` 带 `LIMIT 1000` 且带两个计数子查询，**不能复用**，否则大应用导出会被静默截断。

**本特性不改协议层**（`:8080` 零改动），**不新增数据库表**（无迁移），**不新增第三方依赖**（JSON 解析用标准库，下载/上传用浏览器原生能力），**不新增环境变量**（各上限以常量固化，与 `internal/service/app_auth.go` 既有 `maxMemberBatch` 等一致）。

## Technical Context

**Language/Version**: Go `1.26.0`；前端 Vue `3.5.13` + TypeScript `5.7.2`（`strict`）

**Primary Dependencies**: `gin v1.10.0`、`go.uber.org/fx`、`gorm v1.25.12`、`oklog/ulid/v2`（新记录主键）、`zitadel/oidc/v3`（本特性**不触碰**）；前端 `element-plus 2.9.1`（admin 全局、portal 仅 `views/app-admin/` 局部注册）。**本特性不新增任何第三方依赖**

**Storage**: MySQL `8.4`，**无结构变更**（复用 `client_permissions`）；导入在单事务内完成，事务经 `internal/store` 的 tx-in-context（`db.WithContext(ctx)`）

**Testing**: `make test`（Go 单测 + 前端 vitest）、`make test-integration`（`TEST_MYSQL_DSN` 真库）、`make lint`、`make docs-check`、`make check`

**Target Platform**: Linux 容器；`:8081` Gin 管理 API + `web/admin`、`web/portal` 两个静态前端

**Project Type**: web-service + 双 Vue 前端

**Performance Goals**: SC-005 —— 1000 条 / 5 层嵌套：校验预览 p95 ≤ 3s、确认导入 p95 ≤ 5s；预览对单应用的数据库查询次数为 **O(1)**（一次全量读），不随条目数增长

**Constraints**: `/api/v1/**` 必须走统一响应（成功 `data` 非空、失败 `data` 为 null）；导入端点请求体上限 **512KB**（`http.MaxBytesReader`，仅作用于导入端点；需覆盖 1000 条填满编码/名称/描述的最坏体积 ≈430KB）；单次条数上限 1000；层级上限**沿用既有 `maxPermissionDepth`（100 层）**，不另设导入专用上限；前端只做"空 / 合法 JSON"检查，语义结论一律以服务端为准（spec FR-028）

**Scale/Scope**: 单应用权限点 1e2–1e3 属正常规模（列表端点既有 `LIMIT 1000`，导出**不得**复用它）；单次导入 ≤ 1000 条；导出为**单次全量读、无分页**，1e4 条以内实测可接受（内存与耗时线性增长，超过则需改流式输出，本期不做，已在 Operational Notes 记录观察项）；新增 3 个 operation × 2 端 = 6 个 operation，文档 operation 数 84 → 90

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 说明 |
|---|---|---|
| I. 协议层单一来源 | PASS | `internal/oidc/**` 与 `op.Storage` 零改动；权限点导入只影响 `client_permissions`（管理面字典），不参与令牌签发路径（spec FR-031 明确不改变用户权限集合的既有计算） |
| II. 端口与信任边界物理分离 | PASS | 6 个 operation 全部落在 `:8081` Gin；`:8080` 不挂任何新中间件；导入的 body 上限用 `http.MaxBytesReader` 只包在导入 handler 内，不做全局中间件（避免污染 `/oauth2/*`） |
| III. 分层与数据一致性 | PASS | 新增代码遵循 `api → service → store`；解析与 diff 在 `service`，SQL 在 `store`；写入在单个 `store.Transaction` 内，事务经 tx-in-context 传递；不会出现"先查后改"的授权码式竞态——导入的读与写在同一事务且先锁 `clients` 行串行化 |
| IV. 凭证不出服务端 | PASS | 不涉及令牌/密钥/`client_secret`；审计 detail 只含计数、来源与错误分类，不含权限点全文 |
| V. 结构变更只走手写 SQL 迁移 | PASS | 本特性**无表结构变更**，不新增迁移；复用既有 `client_permissions`（`code` 已 `utf8mb4_0900_as_cs`，唯一键已存在） |
| 技术与接口约束 | PASS | 统一响应体；两个新错误码先登记 AGENTS §5.3 再使用；错误码不得在 handler 内拼字符串；新增环境变量：**无** |
| 开发工作流与质量门禁 | PASS（有强制动作） | 必须重跑 `make docs` 并把 `internal/api/docs/docs_test.go` 的 operation 断言 84 → 90、更新 `specs/007-deployment/api-evolve/baseline/manifest.json` 的 sha256；新增 6 条路由同步登记 AGENTS §4.1（门户 3）与 §4.6（admin 3）；新增审计事件登记 §6.7 同一行枚举；§3 目录树补新文件（`check-artifact-refs.sh` 会校验）；新增业务逻辑必须带单测与集成测试 |

**Gate 结论**：通过，无违规项，`Complexity Tracking` 只记录"有意识的不做"，不需要豁免。

## Project Structure

### Documentation (this feature)

```text
specs/011-app-permission-import/
├── plan.md              # 本文件
├── research.md          # Phase 0：10 个决策点（R1–R10）
├── data-model.md        # Phase 1：输入文档模型、三态计分、校验规则、写入计划
├── quickstart.md        # Phase 1：可执行的验证剧本
├── contracts/
│   ├── admin-permission-import.md   # admin 端 3 个端点（唯一实现）
│   └── portal-permission-import.md  # 门户端 3 个端点（同一 handler，仅鉴权差异）
├── checklists/
│   └── requirements.md  # 已生成（spec 阶段，16/16）
└── tasks.md             # 由 /speckit.tasks 生成，本命令不产出
```

### Source Code (repository root)

```text
internal/
├── service/
│   ├── app_permission_import.go          # 新增：文档解析（纯函数）/ 校验预览 / 确认导入 / 导入计划构建
│   ├── app_permission_import_test.go     # 新增：纯函数单测（20 份样例，无需 DB）
│   ├── app_auth.go                       # 改：新增 ensureClientActive；导入计划复用既有字段规则常量
│   └── app_permission_import_integration_test.go  # 新增：幂等 / 回导闭环 / 父级变更 / 并发（TEST_MYSQL_DSN）
├── store/
│   ├── app_auth_store.go                 # 改：LockClientRow / LockClientPermissions / CreateClientPermissions / UpdateClientPermissionFields / ListClientPermissionsForExport（无 LIMIT、无计数子查询）
│   └── client_store.go                   # 改：GetClient 已存在（复用）；不加逻辑
├── api/
│   ├── dto/dto.go                        # 改：PermissionImportIssue / PermissionImportPreview / PermissionImportResult / PermissionExportDocument
│   ├── admin/app_permission_import.go    # 新增：3 个 handler + failPermissionImport（两端共用的唯一实现）
│   ├── admin/app_permission_import_test.go # 新增：坏 JSON → IMPORT_INVALID、超体积 → 413、预览 200 带 issues
│   ├── me/app_admin.go                   # 改：3 个薄 wrapper（@x-auth-level appAdmin）
│   ├── response/errors.go                # 改：APP_PERMISSION_IMPORT_INVALID / APP_PERMISSION_IMPORT_TOO_LARGE
│   ├── router.go                         # 改：admin 3 path + 门户 3 path
│   ├── router_test.go                    # 改：门户 3 条路由已注册 + 未登录 401 断言
│   └── docs/openapi.json                 # 改：make docs 重新生成
web/admin/src/
├── api/{index.ts,types.ts}               # 改：validatePermissionImport / confirmPermissionImport / exportPermissions + 类型
├── utils/file.ts                         # 新增：readJsonFile / downloadJson（仅本工程内复用）
└── views/client/
    ├── ClientPermissions.vue             # 改：头部"批量导入""导出""下载示例"按钮 + 挂对话框
    ├── PermissionImportDialog.vue        # 新增：粘贴/选文件、预览面板（含 movedItems）、确认导入
    └── PermissionImportDialog.spec.ts    # 新增：vitest
web/portal/src/
├── api/{app.ts,types.ts}                 # 改：同 admin（`/me/apps/:id` 前缀）
├── utils/file.ts                         # 新增：同上（跨工程不共享，006 既定）
└── views/app-admin/
    ├── Permissions.vue                   # 改：与 admin 同构（006 决策：复制实现，不抽公共组件）
    ├── PermissionImportDialog.vue        # 新增：admin 版复制 + EP 局部注册
    └── PermissionImportDialog.spec.ts    # 新增：vitest
AGENTS.md                                 # 改：§3 目录树、§4.1 门户 3 条、§4.6 admin 3 条、§5.3 两个新码、§6.7 一个事件
specs/007-deployment/api-evolve/baseline/manifest.json  # 改：openapi.json 新 sha256（007 流程登记）
```

**Structure Decision**: 沿用既有单仓库分层与"两端共用一份实现"的落点。权限点的全部读写在 003 建立的 `internal/api/admin/app_auth.go` + `internal/service/app_auth.go` + `internal/store/app_auth_store.go` 三件套里；本特性**新增独立文件**而不继续加厚 `app_auth.go`（后者已承载成员/角色/权限/授权四类逻辑），解析逻辑与既有业务规则解耦、可纯函数单测。门户侧沿用 006 的"复制 UI、共享 handler"做法：`internal/api/me/app_admin.go` 只加 wrapper，`web/portal/src/views/app-admin/PermissionImportDialog.vue` 是 admin 版的逐行复制（仅 EP 注册方式与 API 前缀不同）。

## Phase 0: Outline & Research

输入（spec 未预设、必须在 plan 落地）：① 导入通道形态（纯 JSON body 还是 multipart 上传）；② 体积上限（512KB）在哪一层执行、如何与统一响应共存；③ 各上限是常量还是环境变量；④ "校验预览"对语法/语义错误返回 200 还是 400（FR-005 与 FR-029 的表面冲突）；⑤ 1000 条如何写入（既有 `ensureParent` 会放大成十万级查询）；⑥ 并发导入的串行化点与冲突码（是否新增错误码）；⑦ 层级上限是否与既有 `maxPermissionDepth=100` 对齐（对齐后"导出→回导"无例外）；⑧ 导出是否走统一响应、是否携带 `sort`、如何表达"能否回导"；⑨ 审计事件的粒度、no-op 与失败的记录口径；⑩ 前端从哪里复用（现有无任何下载/文件读取工具）。

输出：[research.md](./research.md)（R1–R10，逐条给出 Decision / Rationale / Alternatives considered，无遗留 NEEDS CLARIFICATION）。

## Phase 1: Design & Contracts

- [data-model.md](./data-model.md)：输入文档结构（`version`/`mode`/`permissions[]`/`children[]`）、三态计数口径（新增/更新/无变化/上级变更）、问题条目（`path`/`code`/`message`/`line`）、校验规则表、导入计划（拓扑分层）、写入语义与幂等性。
- [contracts/admin-permission-import.md](./contracts/admin-permission-import.md)：`POST /api/v1/admin/clients/:id/permissions/import/validate`、`POST .../permissions/import`、`GET .../permissions/export` 的完整报文（含成功/失败样例与错误码矩阵）。
- [contracts/portal-permission-import.md](./contracts/portal-permission-import.md)：`/api/v1/me/apps/:id/permissions/import*` 三端点，鉴权差异与失败语义（统一 403，不泄露应用存在性）。
- [quickstart.md](./quickstart.md)：从 `make dev` 到两端手工验证 + 断言清单的可执行剧本。

**Post-Design Constitution Re-check**：设计未触碰协议层（I）、未改 `:8080`（II）、未跨层调用且写入在单事务内并用 `FOR UPDATE` 串行化（III）、不涉及凭证（IV）、无结构变更（V）。新增的 `http.MaxBytesReader` 只包在导入 handler 内，不是全局中间件，不改变既有端点的请求体语义。结论：**PASS**，与 Phase 0 前一致。

## 实施要点（设计阶段已定的硬约束）

- **预览永远 200（业务失败也是）**：`POST .../import/validate` 对"内容非法"返回 `200 + issues[]`，`ok=false`；只有鉴权失败（401/403）、应用不存在（404）、请求体超限（413 `APP_PERMISSION_IMPORT_TOO_LARGE`）走非 2xx。否则 FR-005（可分位置清单）与 FR-029（失败响应 `data` 必须为 null）会互相打架（R4）。
- **确认导入只给码不给明细**：`POST .../import` 校验不通过 → `400 APP_PERMISSION_IMPORT_INVALID`（`data: null`），超限 → `413 APP_PERMISSION_IMPORT_TOO_LARGE`，并发冲突 → 复用 `409 APP_RESOURCE_STALE`（message 已含"请刷新后重试"，满足 FR-018）。
- **写入不得循环调用 `CreatePermission`**：既有 `ensureParent` 每节点最多 100 次单查，1000 条会退化。改为事务内 **① `SELECT ... FOR UPDATE` 锁 `clients` 行 → ② 一次读全量 `client_permissions` → ③ 内存 diff → ④ 按输入树 BFS 逐层写（父先子后）**。父级合法性由内存映射保证（父只可能是本次输入节点或库内既有节点），不再逐层查库（R5）。
- **无环性靠构造保证**：输入是树（每 `code` 全局唯一、单一父），写入的 `parent_id` 只取自输入树或既有行；父先子后写 → 任何中间态也不成环，无需二次环检测（R5）。
- **不新增错误码以外的语义**：不引入"校验票据 / 预览有效期"。预览与导入各算一次 diff（导入在事务内重算），并发下计数可能不同——spec FR-003 已声明"并发改动除外"（R4）。前端在"确认导入返回的计数 ≠ 预览计数"时提示"数据在预览后有变化，已按最新状态写入"，而不是假装没发生。
- **审计口径（对齐 SC-003 / US3-IT / 010 惯例）**：`app_permissions_imported` 成功与失败同 Type、用 `result` 区分。
  - **成功**：仅当 `added > 0 || updated > 0` 时写 1 条；**全量无变化（no-op）不写**。
  - **失败**：仅"业务拒绝"写 1 条（`stale` / `too_large` / `client_disabled` / 应用不存在）；**内容非法（`APP_PERMISSION_IMPORT_INVALID`）与 401/403 不写**（与既有"输入格式错误不记"一致，避免审计页被试探动作淹没）。
  - `reason` 只记分类枚举，不记原文（避免把用户 JSON 写进审计）；`source` 由 query 参数 `?source=paste|file` 声明（非法值回退 `paste`），**仅供统计，不得作为安全依据**。
  - **可处置线索**：成功事件另记 `addedIds`（本次新增权限点 ID，**超过 20 项只记前 20 个 + `addedCount`**，与既有删除快照的截断规则一致）与 `docSha256`（导入文档哈希，不存原文）。v1 不做"撤销导入"，这是误导入后唯一的批量化处置入口（一条按 ID 清理的 SQL）。
- **层级上限与单条创建对齐**：导入直接复用 `maxPermissionDepth`（100），不新设 `maxImportDepth`。这样"库里能导出的树，回导一定过得了深度校验"，闭环无例外。
- **导出必须可回导或明确不可回导**：导出不复用 `ListClientPermissions`（`LIMIT 1000` + 两个计数子查询，会静默截断），改用 `ListClientPermissionsForExport`（无 `LIMIT`、无计数）。响应带 `importable{ok, reasons[]}`，`reasons ∈ {empty, too_many, too_large, too_deep}`：
  - `too_many`：条数 > 1000；
  - **`too_large`：`{version, mode, permissions}` 经 `json.MarshalIndent(v, "", "  ")` 后的字节数 > 512KB**——必须用与下载文件一致的格式化序列化，**不得用 compact**（3 层嵌套下 compact 小 20–30%，会得出"能回导"的假承诺）；
  - `too_deep`：深度 > 100；`empty`：`permissions` 为空（回导必被 `empty` 拒，不能判 `ok=true`）。
  `ok=false` 时前端在下载前提示原因；`too_many` 附带"列表页最多展示 1000 条"（列表端点既有 `LIMIT 1000`，否则会被误认为导出出错）。
- **两端一致性靠共享实现 + 各自测试**：handler 单点实现；两端各写一份 `PermissionImportDialog.spec.ts`，断言"同一份输入在两端产生同一组 issues 文案"（SC-007）。
- **导入期间的并发单条编辑会被覆盖（已知且符合语义）**：导入以文档为准（FR-016），事务内 `FOR UPDATE` 保证读到最新提交状态后再算 diff，但另一个管理员在导入提交前改的名字会被文档覆盖。`409 APP_RESOURCE_STALE` **仅表示唯一键冲突**，不表示"有人改过"；实现者不得为此加逐条乐观锁（那会让 1000 条导入因一条被改而整批失败）。
- **解析不得自研**：用 `encoding/json`（自带嵌套深度上限兜底，超限报语法错误），解析后立即做 100 层检查给出 `too_deep`；禁止自己写递归下降解析器。

## Operational Notes

- **首次上线无需配置项**：不新增环境变量，`internal/config` 加载与启动校验不变；`.env.example` 与 AGENTS §9 不动。
- **无迁移、无回填**：本特性不改表结构；回滚只需回滚镜像（新端点消失，已导入的权限点是正常业务数据，保留）。
- **观察指标（上线后一周）**：`app_permissions_imported` 的 `result=failure` 占比与 `reason` 分布、导入 p95、**导出 p95 与导出条数分布**（导出是单次全量读 + 两次序列化，无分页；若 p95 随体量明显劣化，说明接近容量边界）、`APP_PERMISSION_IMPORT_TOO_LARGE` 触发次数（判断 512KB 是否合适）、`APP_RESOURCE_STALE` 在导入上的触发次数、导出响应里 `importable.ok=false` 的比例（判断是否需要提高条数/体积上限或明确产品上限）。
- **产品侧度量（上线后一个月，判断"这个特性有没有用"）**：使用过导入的应用数 / 权限字典规模 ≥50 的应用总数、导入次数、单次导入条数的中位数与 P90、"预览通过但未点确认"的放弃率（若过高说明预览规则过严或提示不可核对）。
- **审计可观测性**：审计写入是"队列满即丢"的异步通道（`internal/service/audit.go`），导入恰是写入放大最大的操作。丢弃日志必须带 `event_type`，运维可用 `grep 'audit queue full' + app_permissions_imported` 判断"这次导入是否丢了审计"；若实测频繁丢弃，再评估把导入事件改为同事务同步落库（本期不做）。
- **限流风险（本期不加）**：导入是"一次 1000 条、单事务"的放大操作，反复提交会持续压 DB；现有 `RateLimit` 是全局面板，未针对该端点收紧。本期记录风险并观察，若出现滥用再加端点级令牌桶（会新增 env → §9 三向同步，故不在本特性内做）。
- **已知边界**：单应用权限点总量仍不设上限（沿用现状），导出不再受列表 `LIMIT 1000` 影响；但 >1000 条或 >512KB 的导出文件**无法回导**，由 `importable.ok=false` 显式告知，属产品上限而非缺陷。

## Complexity Tracking

无宪法违例，无需豁免说明。以下属于「有意识的不做」，记录备查：

| 决策 | 选择的简单方案 | 被否的更"优雅"方案及原因 |
|---|---|---|
| 上传通道 | 单一 `application/json` body；选文件时前端 `file.text()` 后按同一 body 提交 | `multipart/form-data` 上传：需新增临时存储/大小处理分支，而导出仍要 JSON，两种形态会让"同一路径同一校验"（FR-001）出现两套入口 |
| 体积上限 | `http.MaxBytesReader`（512KB）只包在导入 handler | 全局 body-limit 中间件：会改变所有 `/api/v1/**` 端点的既有行为（含既有图片凭证等），超出本特性范围且回归面大 |
| 层级上限 | 与单条创建共用 `maxPermissionDepth`（100） | "导入专用 10 层"：会让库里更深的树"能导出、导不回"，制造闭环例外；而"字典应该只有三层"是命名习惯，应由提示引导而非硬限制 |
| 导出可回导性 | 允许导出任意规模，响应给 `importable{ok,reasons}` 判定 | 超限直接拒绝导出：剥夺"备份/人工参考"这一正当用途；静默产出不可回导的文件：最坏结果 |
| 端点级限流 | 不做，沿用全局限流并记录风险 | 为导入单开令牌桶需新增 env（触发 §9 三向同步），本期收益不明确 |
| 校验票据 | 不做；导入时事务内重算 diff | 预览后签发一次性 token + 结果缓存（Redis）：多一次状态与过期语义，1000 条 diff 是纯内存操作（一次全量读），收益不抵复杂度 |
| 全量对齐（删除） | 不做；`mode` 只接受 `merge`，`replace` 明确拒绝 | 实现 `replace`：删除会连带在事务内解绑角色权限（既有 `DeleteClientPermission` 语义），需要额外的影响面预览与二次确认，属独立特性 |
| 部分成功 | 不做；整批要么全成功要么全回滚 | 逐条尽力而为 + 返回部分成功清单：与 FR-004（单事务）直接冲突，且管理员面对"1000 条里成功 987 条"无法决策 |
| 导入即授权 | 不做；导入只写字典，不改任何角色绑定 | "导入时顺带把权限点授予某角色"：混淆两份关注点，且角色授权已有独立的覆盖式端点（`PUT .../roles/:rid/permissions`） |
| 前端公共组件 | 两端各复制一份对话框（006 既定做法） | 抽共享组件包：跨两个独立 Vite 工程，构建与版本耦合成本高于一次重复 |
| 导出格式 | JSON only | 同时提供 CSV/Excel：CSV 无法表达 `children` 层级，与 FR-019 的"同构"冲突 |
