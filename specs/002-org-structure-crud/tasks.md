# Tasks: 管理后台组织架构增删改

**Input**: Design documents from `specs/002-org-structure-crud/`

**Prerequisites**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/admin-orgs-api.md](./contracts/admin-orgs-api.md)、[quickstart.md](./quickstart.md)

**Tests**: 本特性的测试由 plan.md 的 Testing 小节强制要求（纯函数单测、路由/文档一致性、`TEST_MYSQL_DSN` 门控集成测试），因此下列任务包含测试任务；无前端测试基建，界面通过 quickstart 场景 13 手工/脚本验收。

**Organization**: 按用户故事分组，便于逐故事独立实现与验收。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: US1 / US2 / US3 / US4
- 每个任务都带精确文件路径

## Path Conventions

Go 单模块 + 独立前端工程：后端 `internal/…`、`cmd/sso/main.go`；前端 `web/admin/src/…`；规范文档 `AGENTS.md`；生成物 `internal/api/docs/openapi.json`。

---

## Phase 1: Setup（共享基元，先登记后使用）

**Purpose**: 错误码、审计事件类型、DTO、路径工具等跨故事共享的基元。规范要求"错误码/事件类型先登记再使用"（`AGENTS.md` §5.3、§6.7）。

- [x] T001 [P] 先在 `AGENTS.md` §5.3 登记 4 个错误码：`ORG_NAME_ALREADY_EXISTS`(409)、`ORG_HAS_CHILDREN`(409)、`ORG_HAS_USERS`(409)、`ORG_INVALID_PARENT`(400)，message 分别固定为"同级已存在同名部门"/"部门下仍有子部门"/"部门下仍有账号"/"上级部门不存在或不可作为上级"；再在 `internal/api/response/errors.go` 落码（宪法「错误码必须先登记再使用」，登记与落码同一提交内完成，登记在前）
- [x] T002 [P] 先在 `AGENTS.md` §6.7 登记 3 个事件类型，再在 `internal/store/model/audit_event.go` 新增常量 `EventOrgCreated = "org_created"`、`EventOrgUpdated = "org_updated"`、`EventOrgDeleted = "org_deleted"`
- [x] T003 [P] 在 `internal/api/dto/dto.go` 新增 `OrgDetail{ID, ParentID *string, Name, Path, Sort, TenantID, UserCount, CreatedAt}` 与 `FromOrganization(o *model.Organization, userCount int64) OrgDetail`（`createdAt` 输出 RFC3339 UTC；根级 `parentId` 为 `null`）
- [x] T004 [P] 在 `internal/store/model/organization.go` 增加纯函数：`OrgDepth(path string) int`（`strings.Count(path,"/")-1`）、`JoinOrgPath(parentPath, id string) string`（根级 `"/"+id+"/"`）、`IsPathPrefix(prefix, p string) bool`（含相等，用于成环判定）、`OrgPathPattern(prefix string) string`（`prefix + "%"`，注释说明 ULID 字符集不含 `%`/`_`）

---

## Phase 2: Foundational（阻塞所有用户故事）

**Purpose**: store 读写原语、service 骨架、handler 迁移与装配链。**⚠️ 本阶段完成前不得开始任何用户故事。**

- [x] T005 在 `internal/store/organization_store.go` 实现读原语：`LockAllOrganizations(ctx) ([]model.Organization, error)`（`SELECT id,parent_id,path FROM organizations ORDER BY id FOR UPDATE`，注释说明"无 tenant_id 索引 → 实际锁全表、全局串行"）、`GetOrganizationByName(ctx, tenantID string, parentID *string, name, excludeID string) (*model.Organization, error)`（`parent_id <=> ?` + `BINARY name = ?` + `id <> ?`）、`HasChildOrganizations(ctx, id string) (bool, error)`、`CountUsersByDeptID(ctx, id string) (int64, error)`、`ListSubtree(ctx, pathPrefix string) ([]model.Organization, error)`、`CountUsersInSubtree(ctx, pathPrefix string) (int64, error)`（`users JOIN organizations ON o.id=u.dept_id WHERE o.path LIKE ?`）
- [x] T006 在 `internal/store/organization_store.go` 实现写原语：`CreateOrganization(ctx, o *model.Organization) error`、`UpdateOrganizationFields(ctx, id string, fields map[string]any) error`、`UpdateSubtreePaths(ctx, oldPrefix, newPrefix string) (int64, error)`（`SET path = CONCAT(?, SUBSTRING(path, ?)) WHERE path LIKE ?`，参数为"新前缀 / 旧前缀字符数+1 / 旧前缀%"）、`DeleteOrganization(ctx, id string) error`
- [x] T007 在 `internal/service/org.go` 建立 `OrgService` 骨架：`maxOrgDepth = 10` 常量；`NormalizeOrgName`（trim）；`ValidateOrgName`（trim 后 rune 数 1–64、禁止空串/全空白/Unicode 控制字符）；`validateSort`（缺省 0、必须落在 `int32` 范围）；`withWriteTx(ctx, fn)`（`store.Transaction` 包裹，遇 MySQL `1213`/`1205` 重试一次、重试对调用方不可见、仍失败返回 500）
- [x] T008 把 `OrgNode`、`buildTree`、`OrgTree` 从 `internal/api/admin/users.go` 迁入新文件 `internal/api/admin/orgs.go`，`users.go` 中删除定义且行为不变（`GET /admin/orgs/tree` 报文零变化）；`OrgTree` 改调 `OrgService.Tree`，handler 不再直连 store（分层约束，见 plan.md Constraints）
- [x] T009 在 `internal/api/admin/users.go` 的 `Handler` 结构体与 `NewHandler` 增加 `orgs *service.OrgService` 字段/参数
- [x] T010 更新装配链：`internal/api/router.go` 的 `NewRouter` 增加 `orgs *service.OrgService` 参数、`admin.NewHandler(...)` 调用同步；`cmd/sso/main.go:122` 传入 `OrgService`；`internal/api/router_test.go` 的 `newTestRouter` 构造 `service.NewOrgService(st, audit, logger)` 并传入
- [x] T011 在 `internal/api/router_test.go` 增加 `TestOrgRoutesRegistered`：断言 `r.Routes()` 精确包含 `GET /api/v1/admin/orgs/tree`、`POST /api/v1/admin/orgs`、`PATCH /api/v1/admin/orgs/:id`、`DELETE /api/v1/admin/orgs/:id`，并断言四条写路由在无 `sid` cookie 时返回 `401 UNAUTHENTICATED` 且响应体为统一响应（`code/message/data/traceId`）

**Checkpoint**: 基元与装配就绪，可并行进入 US1/US2/US3（US4 依赖 US1–US3 的接口行为，但可同步开发）。

---

## Phase 3: User Story 1 - 新增组织节点 (Priority: P1) 🎯 MVP

**Goal**: 管理员可创建根级或子级部门；名称、深度、同级唯一、租户继承、审计全部生效。

**Independent Test**: `POST /api/v1/admin/orgs` 在根级建"技术中心"、其下建"平台组"，`GET /admin/orgs/tree` 可见两级且 `userCount=0`；同名/超长/第 11 级/非管理员均被拒且零写入。

### Tests for User Story 1

- [x] T012 [P] [US1] 在 `internal/service/org_test.go` 编写纯函数单测：名称 trim 与 1–64 rune 边界、空串/全空白/控制字符拒绝、`sort` 越界拒绝、`OrgDepth`（`/ID/`→1）、`JoinOrgPath`、`IsPathPrefix`（自身/后代/兄弟/无关四种）
- [x] T013 [P] [US1] 在 `internal/service/org_integration_test.go` 建立 `TEST_MYSQL_DSN` 门控骨架（为空 `t.Skip`；建库、执行 `migrations/0001_init.up.sql`、每用例前 `TRUNCATE organizations/users/audit_events`、禁止 `AutoMigrate`），并新增创建用例：同级同名拒绝、`Platform` vs `platform` 大小写不同可共存、第 10 级成功而第 11 级返回 `VALIDATION_FAILED`、空表并发创建（重试后一方成功一方 `ORG_NAME_ALREADY_EXISTS`）

### Implementation for User Story 1

- [x] T014 [US1] 在 `internal/service/org.go` 实现 `Create(ctx, in CreateOrgInput) (*model.Organization, error)`：事务内先 `LockAllOrganizations` → 父节点存在校验（不存在 `ErrOrgInvalidParent`）→ 名称校验 → `depth(parent)+1 > 10` 拒绝（message 固定"层级不能超过 10 级"）→ `GetOrganizationByName` 同级唯一（`BINARY`）→ 租户继承（根级 `default`）→ `path = parentPath + id + "/"` → 写入；`path` 长度仅作内部断言（≤512）
- [x] T015 [US1] 在 `internal/api/admin/orgs.go` 实现 `CreateOrg` handler：绑定 `{parentId?, name, sort?}`（`tenantId` 为未知字段被忽略）、调用 service、`201` + `dto.OrgDetail`（`userCount=0`）；补 swag 注解（`@Tags 组织架构`、`@Security cookieAuth`、`@Success 201`、`@Failure` 覆盖 400/401/403/409/500/503，业务码逐字与契约一致）
- [x] T016 [US1] 在 `internal/api/router.go` 的 `adminGroup` 注册 `POST /orgs`，并在 handler 内按 FR-014 投递审计：成功 `org_created/success`，业务拒绝（同名/非法父节点/深度超限）`org_created/failure`，输入格式错误不写；`detail` 含 `orgId/name/parentId` 或 `{"message": ...}`

**Checkpoint**: US1 可独立验收；组织树出现新节点，非法输入零写入。

---

## Phase 4: User Story 2 - 重命名、调整排序与调整上级部门 (Priority: P2)

**Goal**: 支持局部更新名称/排序/上级；移动时整棵子树路径级联、禁止成环、深度不越界、排序不被隐式改写。

**Independent Test**: 只提交名称→父子/排序不变；把含两级子部门的 A 移到新父下→整棵子树出现在新位置且用户归属不变；把 A 移入其后代→400 且树不变；提交 `{"parentId":null}` → A 成为根节点；提交 `{}` → 200 且数据不变。

### Tests for User Story 2

- [x] T017 [P] [US2] 在 `internal/api/admin/orgs_test.go` 编写 `optionalString` 三态单测：字段缺失（`Set=false`）、显式 `null`（`Set=true, Value=nil`）、字符串值，并覆盖 `{"parentId":null,"sort":null}` 的绑定结果
- [x] T018 [P] [US2] 在 `internal/service/org_integration_test.go` 增加更新用例：只改名不动父子/排序、移动级联（含用户归属与 `userCount` 不变）、移入自身与移入后代均 `ORG_INVALID_PARENT`、移动导致深度 > 10 拒绝、同级改名撞名 `ORG_NAME_ALREADY_EXISTS`（排除自身）、`sort` 超 `int32` 返回 `VALIDATION_FAILED`、空 body 幂等

### Implementation for User Story 2

- [x] T019 [US2] 在 `internal/service/org.go` 实现 `Update(ctx, id string, in UpdateOrgInput) (*model.Organization, error)`：短事务内 `LockAllOrganizations` → 重读节点（不存在 `ErrOrgNotFound`）→ 若 `ParentIDSet`：校验新父存在、前缀判定拒绝自身/后代、校验租户一致、按"新父深度 + 子树高度 ≤ 10"校验 → 先 `UpdateSubtreePaths` 再改 `parent_id`；若提交名称则按创建同规则校验（同级唯一排除自身）；三个字段都未提交时不执行任何 UPDATE；`sort` 缺省不变
- [x] T020 [US2] 在 `internal/api/admin/orgs.go` 实现 `UpdateOrg` handler 与 `optionalString` 类型：区分"缺失/`null`/值"；`ParentID` 注解带 `extensions:"x-nullable"`，`@Description` 写明三态；`200` + `dto.OrgDetail`（`userCount` 为该节点及子部门汇总，一次子查询，不得 N+1）
- [x] T021 [US2] 在 `internal/api/router.go` 的 `adminGroup` 注册 `PATCH /orgs/:id`，并按 FR-014 投递 `org_updated` 审计（成功 success；业务拒绝 failure；输入格式错误不写）

**Checkpoint**: US1 + US2 均可独立验收；移动后树结构与人数一致。

---

## Phase 5: User Story 3 - 删除组织节点 (Priority: P2)

**Goal**: 仅在无子部门且无账号时物理删除；重复删除返回 404；删除与账号写入互斥，不产生悬空 `dept_id`。

**Independent Test**: 空叶子删除成功并从树消失；含子部门 `ORG_HAS_CHILDREN`；含账号 `ORG_HAS_USERS`；再次删除同 ID 返回 `NOT_FOUND`；并发"删部门 + 建用户"后 SQL 校验 `dangling = 0`。

### Tests for User Story 3

- [x] T022 [P] [US3] 在 `internal/service/org_integration_test.go` 增加删除用例：空叶子成功、含子部门/含账号两类拒绝码不同、重复删除 404、并发"删部门 + 建用户"（恰一成功，且失败原因不得是用户名冲突）、`PATCH /admin/users/:id` 携带不存在 `deptId` 返回 `VALIDATION_FAILED` 且原值不变

### Implementation for User Story 3

- [x] T023 [US3] 在 `internal/service/user.go` 改造账号归属写入：`CreateUser` 把参数校验与 `crypto.HashPassword` 保留在事务外，短事务内 `SELECT id FROM organizations WHERE id = ? FOR UPDATE` 锁目标部门并校验存在（不存在 `ErrDeptNotFound`）后插入；`UpdateUser` 在提交 `deptId` 时同样短事务加锁 + 校验（行为修正：过去静默写入）
- [x] T024 [US3] 在 `internal/service/org.go` 实现 `Delete(ctx, id string) error`：事务内锁表 → 重读节点（不存在 `ErrOrgNotFound`）→ 先 `HasChildOrganizations`（`ErrOrgHasChildren`）→ 再 `CountUsersByDeptID > 0`（`ErrOrgHasUsers`）→ 物理删除
- [x] T025 [US3] 在 `internal/api/admin/orgs.go` 实现 `DeleteOrg` handler（`200` + `data:null`，错误映射 404/409），在 `internal/api/router.go` 注册 `DELETE /orgs/:id`，并投递 `org_deleted` 审计（成功 success；两类保护拒绝 failure；输入错误不写）；删除前取审计用名称经 `OrgService.Name`，不直连 store

**Checkpoint**: US1–US3 全部可独立验收；组织树无孤儿、无悬空引用。

---

## Phase 6: User Story 4 - 管理后台组织树操作入口 (Priority: P3)

**Goal**: 管理员在浏览器内完成新增根部门/新增子部门/重命名/移动到/删除闭环，错误提示可读、非法操作不可选。

**Independent Test**: 只用浏览器完成"新增根部门 → 新增子部门 → 重命名 → 移动 → 删除"，树每次自动刷新；含账号部门删除被拒时提示数量并自动筛选；10 级节点的新增入口禁用并说明原因。

- [x] T026 [P] [US4] 在 `web/admin/src/api/types.ts` 增加 `OrgDetail`、`CreateOrgPayload{parentId?:string|null;name:string;sort?:number}`、`UpdateOrgPayload{name?:string;sort?:number;parentId?:string|null}`；在 `web/admin/src/api/index.ts` 增加 `createOrg/updateOrg/deleteOrg`
- [x] T027 [P] [US4] 在 `web/admin/src/utils/format.ts` 的 `EVENT_LABEL` 增加 `org_created: "创建组织"`、`org_updated: "更新组织"`、`org_deleted: "删除组织"`（审计页下拉由该常量生成）
- [x] T028 [US4] 在 `web/admin/src/views/Users.vue` 组织树面板顶部增加"新增根部门"（空树/未选中时可用），并实现新增/重命名共用的 `el-dialog`（名称 + 排序；排序前端限制 `0..1_000_000`）；节点深度由 `path` 斜杠数推导，达 10 级时禁用"新增子部门"并以 tooltip/title 说明"已达最大层级 10 级"
- [x] T029 [US4] 在 `web/admin/src/views/Users.vue` 实现"移动到…"弹窗：`el-tree-select` 列出全部节点 + "根级"选项，候选排除该节点自身、其全部后代（`path` 前缀）以及"新父深度 + 子树高度 > 10"的目标并置灰说明；提交 `PATCH {parentId}`，成功后 `loadTree()` 并保持选中态
- [x] T030 [US4] 在 `web/admin/src/views/Users.vue` 实现删除入口：`ElMessageBox.confirm` 二次确认；收到 `ORG_HAS_USERS` 时展示含账号数量的提示并自动按该部门筛选右侧用户列表；删除当前选中节点后选中态回到"全部用户"并刷新列表
- [x] T031 [US4] 在 `web/admin/src/views/Users.vue` 完成树体验与可访问性：默认展开 3 层；节点数 > 50 时显示关键字过滤且命中路径自动展开；五个入口补 `title`/`aria-label`；所有失败路径使用既有 `errorMessage(e)` 展示、不暴露堆栈

**Checkpoint**: 浏览器闭环可用；后端接口行为与 US1–US3 一致。

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 规范同步、文档生成、门禁与端到端验收。

- [x] T032 [P] 复核 `AGENTS.md` 与实现逐条一致：§4.1 接口总览 3 行（并注明 `PATCH /admin/users/:id` 增加 `deptId` 存在性校验）、§4.6 字段约定 3 行、§5.3 4 个错误码、§6.7 3 个事件类型。**错误码与事件类型已在 T001/T002 先行登记**，本任务只做终检，不得等到此处才首次登记（宪法「先登记后使用」）
- [x] T033 执行 `make docs` 重生成 `internal/api/docs/openapi.json`（14 → **16** 条 path、16 → **19** 个操作；3 个新端点只新增 2 条 path），并把 `internal/api/router_test.go:104` 与 `internal/api/docs/docs_test.go:269-270` 的 `ops != 16` 断言改为 **19**
- [x] T034 [P] 校验 `internal/api/docs/openapi.json` 的文档细节：三个新 op 具备 summary/tags/500/`security: cookieAuth`；`UpdateOrgRequest.ParentID` 呈现 `nullable: true` 或 description 完整写明三态；`TestAnnotationErrorCodesAreRegistered` 通过
- [x] T035 按 [quickstart.md](./quickstart.md) 逐场景验收：场景 1–11（接口）、场景 12（审计边界两条断言）、场景 13（浏览器七条）、场景 14（并发 + 悬空 SQL 校验 + 用户 `deptId` 400）、场景 15（门禁）
- [x] T036 运行门禁并修复问题：`make docs-check`、`make test`、`make build`、`make lint`、`make build-web`；`TEST_MYSQL_DSN=… go test ./internal/service/ -run 'TestOrg'`（实际 13 个用例，该 `-run` 命中 12 个）
- [x] T037 清理与回归确认：按 [quickstart.md](./quickstart.md) §16 删除产生的测试节点与 `/tmp/sso-cookie.txt`、`/tmp/*.json` 临时文件，确认组织树恢复为"总公司/技术中心/平台组"、`dangling = 0`、审计无残留测试记录

---

## Phase 8: 分析后补充（`/speckit.analyze`，2026-10-21）

**Purpose**: 收口分析报告中 SC-003/SC-004 与 FR-014 前端验收的覆盖缺口，不新增产品范围。

- [x] T038 [P] 在 `quickstart.md` 增补"§15.1 规模与响应时间（SC-003 / SC-004）"：以 10 个根部门 × 50 个子部门构造 500 节点（纯接口调用生成，不新增种子文件），断言组织树查询与单次写操作的 `time_total ≤ 1.0s`，并断言写操作后下一次树查询立即可见（无二次刷新）
- [x] T039 [P] 登记 FR-014 前端验收口径：仓库无前端测试基建且本次不新增依赖，故"中文标签 + 可按事件类型筛选"以 [quickstart.md](./quickstart.md) §12 的浏览器断言为唯一验收手段，并在 PR 说明中显式记为手工验收项

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖，可立即开始；T001–T004 互相独立（不同文件）
- **Foundational (Phase 2)**: 依赖 Setup；**阻塞所有用户故事**
  - T005、T006 同文件 → 顺序执行
  - T007 依赖 T005/T006 的方法签名（可先写骨架）
  - T008、T009 同属 `internal/api/admin/`，T009 依赖 T008（`Handler` 迁移后再加字段）
  - T010 依赖 T007/T009；T011 依赖 T010
- **US1 (Phase 3)**: 依赖 Phase 2；T012/T013 可并行；T014 依赖 T005–T007；T015 依赖 T014；T016 依赖 T015
- **US2 (Phase 4)**: 依赖 Phase 2；T017/T018 可并行；T019 依赖 T014（复用创建校验）；T020 依赖 T019；T021 依赖 T020
- **US3 (Phase 5)**: 依赖 Phase 2；T022 与 T023–T025 可先写测试后实现；T023 独立于组织删除实现（可与 T024 并行，不同文件）；T024 依赖 T005/T006；T025 依赖 T024
- **US4 (Phase 6)**: 依赖 US1–US3 的接口契约（可先按契约开发，联调时再对齐）；T026/T027 可并行；T028–T031 同文件 `Users.vue` → 顺序执行
- **Polish (Phase 7)**: 依赖全部用户故事；T032/T034 可并行；T033 依赖 T032（注解已补）；T035–T037 顺序执行

### User Story Dependencies

- **US1 (P1)**: Phase 2 完成后即可独立交付（MVP）
- **US2 (P2)**: 复用 US1 的名称/深度校验，但不依赖 US1 的 handler；可独立验收
- **US3 (P2)**: 与 US1/US2 无代码依赖；T023 会改动既有用户接口，需单独回归登录与用户管理
- **US4 (P3)**: 依赖三个端点存在，但可按契约先做，接口就绪后联调

### Within Each User Story

- 测试先写并确认失败（T012/T013、T017/T018、T022）
- store → service → handler → route → 审计
- 故事完成并通过 Independent Test 后再进入下一优先级

### Parallel Opportunities

- 首批可并行：T001、T002、T003、T004
- Phase 2 内：T005 与 T008/T009 可并行（不同文件）；T006 紧随 T005
- 故事级并行：US1（T012/T013）、US2（T017/T018）、US3（T022/T023）可由不同人同时推进（除共享的 `org_integration_test.go` 需按追加顺序合并）
- Polish：T032 与 T034 可并行

---

## Parallel Example: User Story 1

```bash
# 同时启动 US1 的两组测试（T012 纯函数、T013 集成骨架）
Task: "在 internal/service/org_test.go 编写名称/深度/路径纯函数单测"
Task: "在 internal/service/org_integration_test.go 建立 TEST_MYSQL_DSN 门控骨架与创建用例"
```

## Parallel Example: User Story 3

```bash
# 账号侧与组织侧互不依赖，可分头实现
Task: "改造 internal/service/user.go：事务外哈希 + 短事务锁部门 + UpdateUser dept 校验"
Task: "在 internal/service/org.go 实现 Delete 及两类删除保护"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 1 + Phase 2（基元 + 装配，缺一不可）
2. 完成 Phase 3（US1：创建）
3. **STOP & VALIDATE**：`make test` + quickstart 场景 1–4、场景 9（并发同名）、场景 12 的层级审计断言
4. 可先行交付：管理员已能在组织树新增部门

### Incremental Delivery

1. Setup + Foundational → 骨架就绪
2. US1 → 验收 → 交付（MVP：可新增）
3. US2 → 验收 → 交付（可改名/调序/移动）
4. US3 → 验收 → 交付（可删除，且账号归属不再悬空）
5. US4 → 验收 → 交付（浏览器闭环）
6. Polish → 规范/文档/门禁一次收口

### Parallel Team Strategy

1. 全员完成 Phase 1 + Phase 2
2. 之后：A 做 US1、B 做 US2、C 做 US3（共享 `org_integration_test.go` 时按"骨架→追加"顺序合并）
3. US4 由前端同学按契约先行，接口就绪后联调
4. 最后统一 Polish：`make docs`、两处 ops 断言、quickstart 全量验收

---

## Notes

- [P] = 不同文件、无未完成依赖
- 迁移文件**零改动**；禁止 `AutoMigrate`
- 所有写操作必须走 `store.Transaction`，事务内顺序固定为"锁 → 读 → 校验 → 写"
- 审计划分按条件而非状态码：业务拒绝（含层级超限）留痕，输入格式错误不留痕
- 每个故事结束即可独立验收，勿把 US2/US3 的必要工作推到最后

---

## Phase 9: Convergence

- [x] T040 **CRITICAL**（已完成：新增 `UserService.List` / `Auditor.List` 与过滤类型，`Handler` 去掉 `st` 字段）：让 `internal/api/admin/` 的读路径全部经 service——在 `UserService` 与 `AuditService` 增加 List 方法（含过滤入参类型），`ListUsers` / `ListAuditEvents` handler 改调 service，并移除 `Handler.st` 字段；若评审决定保留既有结构，则须在评审记录中显式登记该偏差与替代方案（宪法 Governance「未说明即视为缺陷」） per `Constitution III` (contradicts)
- [x] T041（已完成：`AGENTS.md` §2.1 新增「MySQL 错误码」行；plan 两处「无新增依赖」已改为「无版本升级、无新增库」+ 直接依赖说明） 在 `AGENTS.md` §2.1 登记 `github.com/go-sql-driver/mysql v1.7.0`（用途：组织写事务识别 MySQL 1213/1205 后重试，版本保持不变），并把 `plan.md` 中「无新增依赖 / 零新增依赖」改为「无版本升级；`go-sql-driver/mysql` 由间接依赖转为直接依赖并已登记 §2.1」 per `Constitution 技术与接口约束` (contradicts)
- [x] T042（已完成：三项均补 `title` + `aria-label`，`npm run build` 通过） 为组织树下拉的「重命名 / 移动到… / 删除」三项补 `title` 与 `aria-label`，与「新增子部门」保持一致，补齐五入口可访问性 per `FR-017` / `T031` (partial)
- [x] T043（已完成：plan 的 Source Code 清单补 `service/audit.go`、`views/Audit.vue` 两行） 在 `plan.md` 的 Source Code 清单与对应 tasks 中登记两处已完成但未列出的改动：`internal/service/audit.go`（`Event.Fields` 结构化 detail）与 `web/admin/src/views/Audit.vue`（detail JSON 解析渲染修正），使实现面与设计文档一致 per `FR-014` (partial)
- [x] T044（已完成：510 节点 + 3 节点实测 `tree` 7.3–8.8ms、`create` 12.9–39.4ms，均 ≤1s；SC-003 写后即刻可见；清理后 `dangling=0`、组织审计无残留） 在具备 MySQL 的环境执行 `quickstart.md` §15.1，记录 500 节点（10×50）规模下组织树查询与单次写操作的 `time_total` 实测值；若超过 1s 则定位并修复（索引/查询/序列化）后重测 per `SC-003` / `SC-004` (partial)
