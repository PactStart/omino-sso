# Tasks: 应用访问按组织架构授权

**Input**: Design documents from `/specs/010-app-access-org-grant/`

**Prerequisites**: [plan.md](./plan.md)（必需）、[spec.md](./spec.md)（用户故事与优先级）、[research.md](./research.md)（R1–R12）、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)、[critiques/critique-20261101-2221.md](./critiques/critique-20261101-2221.md)（Must-Address 已并入 spec/plan）

**Tests**: 本项目不接受「测试可选」（宪法「开发工作流与质量门禁」+ 模板 DoD）。每条实现任务都自带对应测试，同一提交内完成；不采用 TDD，不要求先写失败用例。

**Organization**: 按用户故事分阶段，每阶段可独立实现与验收。

## 实现前必须知道的四点

1. **迁移编号为 `0007`**：`migrations/` 现有最大为 `0006_app_admin_role`，本特性取 `0007_client_org_grant.{up,down}.sql`。禁止 `AutoMigrate`、禁止代码内 DDL。
2. **AGENTS.md 属受保护文件**：按 §8 提交纪律，触碰它的提交信息须带 `Protected-Change: AGENTS.md` 与 `Approved-By: <name>` 尾注。
3. **operation 断言会中途变化**：`internal/api/router_test.go:181` 当前硬断言 `ops == 78`。US1 完成后为 **80**（admin 新增 `GET`/`POST .../org-grants` 两个 operation），US3 完成后为 **81**（`.../org-grants/remove`），US4 完成后为 **84**（门户 3 个）。三次都要在同一提交内改断言，不要留到最后。
4. **不改 `:8080`**：`internal/oidc/**` 零改动。本特性是门户可见性，不是访问控制（spec FR-029）；任何"顺手给 authorize 加成员校验"的改动都属越界。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可与同阶段其它任务并行（不同文件、无未完成依赖）
- **[Story]**: 所属用户故事（US1–US5）；Setup / Foundational / Polish 不带
- 每条任务给出精确文件路径与可验收断言

## Path Conventions

单仓库单体：Go 在 `internal/`、`cmd/`、`migrations/`；迁移在 `migrations/*.sql`；两个 Vue 前端在 `web/admin/`、`web/portal/`。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 迁移、模型、错误码、审计事件、DTO 的登记与落位。规范要求"先登记、后使用"，因此全部先于业务代码。

- [ ] T001 创建 `migrations/0007_client_org_grant.up.sql`：单表 `client_org_grants`，逐列按 [data-model.md](./data-model.md) §2——`client_id VARCHAR(64) COLLATE utf8mb4_0900_as_cs NOT NULL`、`org_id CHAR(26) NOT NULL`、`include_sub TINYINT(1) NOT NULL DEFAULT 1`、`granted_by CHAR(26) NULL`、`granted_at BIGINT NOT NULL`、`updated_at BIGINT NOT NULL`；`PRIMARY KEY (client_id, org_id)`；`KEY idx_client_org_grants_org (org_id)`；`ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`。不建物理外键（项目约定逻辑外键），不建 `org_path` 快照列
- [ ] T002 创建 `migrations/0007_client_org_grant.down.sql`：`DROP TABLE IF EXISTS client_org_grants;`，文件头注明「执行后全部组织授权配置不可恢复，回滚需先回滚服务镜像（critique E13）」
- [ ] T003 [P] 创建 `internal/store/model/client_org_grant.go`：`ClientOrgGrant` 结构体（字段与 T001 一一对应，时间用 `int64` 毫秒）+ `TableName() string { return "client_org_grants" }` + **唯一判定片段构造函数** `OrgGrantMatchSQL(grantAlias, grantOrgAlias, deptAlias string) string`，返回 `((g.include_sub = 1 AND d.path LIKE CONCAT(go.path, '%')) OR (g.include_sub = 0 AND d.id = g.org_id))`（片段只含列名与常量，不含任何外部输入）
- [ ] T004 [P] 创建 `internal/store/model/client_org_grant_test.go`：表驱动单测，断言片段对三种输入生成的 SQL 文本稳定（防列名漂移），并断言片段中不出现 `%s` 以外的用户可控插值
- [ ] T005 [P] 更新 `internal/api/response/errors.go` + `AGENTS.md` §5.3：新增 `ORG_HAS_GRANTS`（409「部门仍被应用的组织授权引用，请先撤销授权」）与 `APP_ORG_GRANT_LIMIT`（400「单个应用的组织授权数量已达上限（500）」）。**先改 AGENTS 再改代码**，两处 code 字符串逐字一致（`scripts/check-registry.sh` 双向比对）
- [ ] T006 [P] 更新 `internal/store/model/audit_event.go` + `AGENTS.md` §6.7 枚举：新增 `app_org_granted`、`app_org_grant_updated`、`app_org_grant_revoked` 三个常量，同批在 §6.7 的 `detail` 说明段追加字段约定 `{deptId, deptName, deptPath, includeSubDepts|from,to, coveredUsers|impactUsers}`
- [ ] T007 [P] 更新 `internal/api/dto/`：新增 `OrgGrantItem`（`orgId/deptName/deptPath/includeSubDepts/coveredUsers/coveredDisabledUsers/uniqueUsers/grantedBy/grantedAt/updatedAt`）、`AddOrgGrantsReq`（`grants[]`，每项 `deptId` + `includeSubDepts *bool`）、`AddOrgGrantsResult`（`added/updated/unchanged/coveredUsers`）、`RemoveOrgGrantsReq`（`orgIds[]`）、`RemoveOrgGrantsResult`（`removed/impactUsers/remainingVisibleUsers`）、`OrgGrantHint`（`orgId/deptName/memberCount`）；扩展 `AppMemberItem` 增 `orgGrants[]`、成员列表响应增 `visibleTotal *int64` 与 `orgGrantHints []OrgGrantHint`、`CandidateItem` 增 `orgCovered bool`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 打通 store 判定/聚合/事务原语与 service 方法，使后续每个故事只加自己的端点与界面。

**⚠️ CRITICAL**: 本阶段完成前不得开始任何用户故事。

- [ ] T008 创建 `internal/store/org_grant_store.go`（列表部分）：`ListOrgGrants(ctx, clientID)` 一次查询返回全部授权（JOIN `organizations` 取 `name`/`path`），并为每条算出 `coveredUsers`（命中 ∧ `u.status = 1`）、`coveredDisabledUsers`（命中 ∧ `u.status <> 1`）、`uniqueUsers`（命中 ∧ 非成员 ∧ 未被其它授权命中 ∧ `status = 1`）——三者共用 T003 的片段，**不得**另写一套判定
- [ ] T009 [P] 创建 `internal/service/org_grant_integration_test.go`（MySQL 门控，`TEST_MYSQL_DSN` 未设置即 skip）：断言 ① `include_sub=0` 时子孙部门人员不计入 ② 同一人多条授权时 `coveredUsers` 各自独立、`uniqueUsers` 只归其中一条 ③ 停用账号只进 `coveredDisabledUsers`
- [ ] T010 更新 `internal/store/org_grant_store.go`（写入部分）：`AddOrgGrants(ctx, clientID, grants, actorID, now)` 在 `Transaction` 内先 `LockOrganization`（不存在 → `ErrAppDeptNotFound`）、再按 `(client_id, org_id)` 读既有行分流为 insert / update（仅 `include_sub` 真变化才刷新 `updated_at`）/ no-op，最后在同事务内 `COUNT` 校验 ≤ 500（超限 → `ErrOrgGrantLimit`）。**MUST NOT** 用 `INSERT ... ON DUPLICATE KEY UPDATE` 的影响行数区分三态（critique E3）
- [ ] T011 更新 `internal/store/org_grant_store.go`（撤销部分）：`RemoveOrgGrants(ctx, clientID, orgIDs)` 在 `Transaction` 内**先**算 `impactUsers`（命中待撤销授权 ∧ 非成员 ∧ 未被其它保留授权命中 ∧ `status=1`）与 `remainingVisibleUsers`，**再** `DELETE ... WHERE client_id = ? AND org_id IN (...)` 并用 `RowsAffected` 回填 `removed`；计数失败即整次失败（critique E5）
- [ ] T012 [P] 更新 `internal/service/org_grant_integration_test.go`：断言 ① 撤销后 `impactUsers` 不为 0 且等于撤销前"仅因此授权可见"的人数 ② 同一用户仍为手动成员时不计入 `impactUsers` ③ 撤销不存在授权返回 `removed=0` 不报错 ④ 并发两次写入同一部门只产生 1 行
- [ ] T013 [P] 更新 `internal/store/organization_store.go` + `internal/service/org.go` + `internal/api/admin/orgs.go`：新增 `HasOrgGrants(ctx, orgID) (bool, error)`（走 `idx_client_org_grants_org`），在 `OrgService.Delete` 的既有 `withWriteTx`+`LockAllOrganizations` 内排在"有子部门/有账号"之后校验，命中返回 `ErrOrgHasGrants` 并映射为 `409 ORG_HAS_GRANTS`
- [ ] T014 [P] 更新 `internal/service/org_grant_integration_test.go`：断言授权存在时删除部门返回 `ORG_HAS_GRANTS`；撤销授权后同一部门可删除；有子部门的部门删除仍先报 `ORG_HAS_CHILDREN`（顺序不变）
- [ ] T015 更新 `internal/store/app_auth_store.go` 的 `ListClientsForMember`：由 `INNER JOIN client_members` 改为 `LEFT JOIN` + 组织授权 `EXISTS`（片段同 T003），条件为 `c.status = 1 AND (m.user_id IS NOT NULL OR EXISTS(...))`，按用户当前 `dept_id` 的 `organizations.path` 判定；返回行保持 `is_admin` 语义（仍只来自成员标记）
- [ ] T016 [P] 更新 `internal/service/org_grant_integration_test.go`：断言非成员但被授权覆盖的用户可列出该应用且 `is_admin=false`；应用停用后两种来源均不返回
- [ ] T017 [P] 更新 `internal/store/app_auth_store.go`（成员页数据部分）：`ListClientMembers` 增加每行 `orgGrants`（一条 `JOIN client_org_grants + organizations` 查询，限当页 ≤50 个 `user_id`，无 N+1）；`ListCandidates` 增加 `orgCovered`（同样的 `EXISTS` 标记，候选集合本身不变）；新增 `CountVisibleUsers(ctx, clientID) (int64, error)`（成员 ∪ 授权覆盖去重 ∧ `status=1`）与 `ListOrgGrantHints(ctx, clientID, threshold=20, limit=3)`（部门成员数 ≥20 且该部门及祖先均未被授权）
- [ ] T018 更新 `internal/service/app_auth.go`：新增 `ListOrgGrants / AddOrgGrants / RemoveOrgGrants` 三个方法（`ensureClient` 前置、参数校验：`grants` 1–500 且去重、`includeSubDepts` 缺省 true、`orgIds` 1–500），新增哨兵错误 `ErrOrgGrantLimit`、`ErrOrgHasGrants`；本服务**不持有** `*store.Store` 之外的依赖（宪法 III），审计由 handler 写
- [ ] T019 创建 `internal/service/org_grant_test.go`（纯函数/边界单测）：断言空 `grants`、>500 条、重复 `deptId` 去重、`includeSubDepts` 缺省为 true、`orgIds` 超限各自返回 `ErrAppValidation`
- [ ] T020 创建 `internal/service/org_grant_integration_test.go`：**口径等价性门禁**（critique E1）——同一组 fixtures 下 ① `ListForUser` 的可见集合 == 授权列表覆盖人数去重后的并集 ② `include_sub=0` 时子孙部门人员既不可见也不计入覆盖 ③ 调岗一步（改 `users.dept_id`）后两者同步变化。测试名固定为 `TestOrgGrant_VisibilityMatchesCoverage`，不得删除或改名

---

## Phase 3: User Story 1 - 管理员按组织授权，员工无需被逐个添加即可使用应用（P1）🎯 MVP

**Goal**: admin 端可列/加组织授权，保存后该部门树内员工在门户立即可见该应用。

**Independent Test**: 新建应用 C，只做一次"授予研发部（含子部门）"，用 `dev1/2/3` 与 `mkt1` 各登录一次门户：前者可见、后者不可见；全程未添加任何单个成员。

- [ ] T021 [US1] 更新 `internal/api/admin/app_auth.go`：新增 `ListOrgGrants`（`GET /admin/clients/:id/org-grants`，返回 `{list,total}`）与 `AddOrgGrants`（`POST .../org-grants`）两个 handler，复用既有 `failAppAuth` 错误映射（补 `ErrOrgGrantLimit` → `APP_ORG_GRANT_LIMIT`）；补 swag 注解含 `@x-auth-level "admin"`；写审计 `app_org_granted` / `app_org_grant_updated`（按条，`detail` 含 `deptId/deptName/deptPath/includeSubDepts/coveredUsers`），失败写 1 条 `result=failure` 且 `detail.reason ∈ {dept_not_found, limit_exceeded, validation}`
- [ ] T022 [US1] 更新 `internal/api/router.go`：在 `adminGroup` 注册 `GET` 与 `POST /clients/:id/org-grants`；同步 `AGENTS.md` §4.1 追加 2 行
- [ ] T023 [US1] 更新 `internal/api/router_test.go`：新增断言"无 `sid` cookie → `401`"与"非平台管理员 → `403`"；operation 断言由 **78 改为 80**
- [ ] T024 [P] [US1] 更新 `web/admin/src/api/types.ts` 与 `web/admin/src/api/index.ts`：新增 `listOrgGrants(clientId)`、`addOrgGrants(clientId, payload)` 与 `OrgGrant`/`AddOrgGrantsResult` 类型；**删除** `addAppMembersByDept` 的导出与调用（FR-027）
- [ ] T025 [P] [US1] 更新 `web/admin/src/views/client/ClientMembers.vue`（第一刀：组织授权卡片）：新增"组织授权"卡片（表格列：部门 / 层级 / 含子部门（可切） / 覆盖人数（列头标注"在职启用账号数"） / 授权人 / 授权时间 / 撤销）；"添加组织授权"对话框用 `el-tree-select` 多选部门，选择后生成待提交列表并可逐行关闭"含子部门"；保存后刷新卡片与成员列表
- [ ] T026 [US1] 更新 `web/admin/src/views/client/ClientMembers.vue`（第二刀）：授予组织树根节点（`parentId == null`）时二次确认并显示"将覆盖全部 N 名在职员工"（N 取该节点 `coveredUsers` 预览或组织树人数的既有字段）；删除"按部门加入"按钮与对话框（FR-027）
- [ ] T027 [US1] 创建/更新 `web/admin/src/views/client/ClientMembers.spec.ts`（vitest）：断言 ① 组织授权卡片渲染部门与覆盖人数 ② 勾选根节点时出现二次确认文案 ③ 保存后调用 `addOrgGrants` 且请求体含 `includeSubDepts` ④ 页面不再渲染"按部门加入"按钮
- [ ] T028 [US1] 创建 `scripts/e2e/org-grant.sh` 的 US1 段（或在 `scripts/e2e/run.sh` 增用例）：用 API 建 `AppC` + 授权研发部（含子部门），断言 `dev1` 的 `/api/v1/me/apps` 含 `AppC`、`mkt1` 不含；把授权改为不含子部门后 `dev1` 不含、`dev3` 含

---

## Phase 4: User Story 2 - 组织变动后门户可见性自动跟随（P1）

**Goal**: 调岗/入职/停用后无需任何应用侧操作，可见性在下一次请求即正确。

**Independent Test**: 只授权"研发部（含子部门）"，把一名市场部员工调入研发部下的平台组 → 刷新门户即可见；调回后消失。

> 本故事**不需要新增生产代码**：判定在 T015 已按当前 `dept_id` 实时完成。本阶段的价值是把"自动跟随"变成不可回退的测试与性能证据。

- [ ] T029 [US2] 扩展 `internal/service/org_grant_integration_test.go`：断言 ① 调岗进入授权子树后 `/me/apps` 立即包含该应用（同一 `ctx` 内改 `dept_id` 后重查） ② 调出后立即消失 ③ 新建账号分配到授权部门首次登录即可见 ④ 账号 `status=0` 后即使部门仍被授权也不可见 ⑤ 全程 `client_org_grants` 行数不变（证明是实时判定而非同步）
- [ ] T030 [US2] 在 `quickstart.md` §2 的表格旁补一行"判定无缓存"的验证方法（连续两次请求 + 直接改库后立即重查），确保实现者不会加缓存（FR-013）
- [ ] T031 [US2] 按 `quickstart.md` §7 对覆盖人数聚合与可见性 `EXISTS` 各做一次 `EXPLAIN` 与固定 fixtures 计时（2000 人子树 / 20 条授权），把结果与结论贴进 PR；若超 SC-005，按 data-model §7 注记的"祖先 ID 集合 `IN`"改写并在 PR 说明（critique E8）

---

## Phase 5: User Story 3 - 授权来源可见，撤销时结果可预期（P2）

**Goal**: 成员页能看出每人来源；撤销前说清"多少人将不再可见"，撤销后不动手动成员与角色。

**Independent Test**: 应用 C 有 1 名手动成员 `dev3` 与"研发部"授权（部门内 10 人），撤销后确认框显示影响 9 人，`dev3` 仍在成员列表且仍可见。

- [ ] T032 [US3] 更新 `internal/api/admin/app_auth.go`：新增 `RemoveOrgGrants`（`POST .../org-grants/remove`），返回 `{removed,impactUsers,remainingVisibleUsers}`；写审计 `app_org_grant_revoked`（`detail.impactUsers` 取删除前计算值）
- [ ] T033 [US3] 更新 `internal/api/admin/clients.go` + `internal/api/dto/`（接入 T017）：成员列表响应返回每行 `orgGrants`、`visibleTotal`（**仅 `page=1` 返回数字，其余页 `null`**）、`orgGrantHints`（仅 `page=1`，≤3 条）；候选列表返回 `orgCovered`
- [ ] T034 [US3] 更新 `internal/api/router.go` + `AGENTS.md` §4.1 + `internal/api/router_test.go`：在 admin 组注册 `POST /clients/:id/org-grants/remove`（同 path 追加方法，path 数不变、operation +1）；operation 断言由 **80 改为 81**
- [ ] T035 [US3] 新增 `internal/api/admin/app_auth_test.go` 用例（或扩展既有）：断言成员列表 `page=1` 有 `visibleTotal`、`page=2` 为 `null`；`visibleTotal` ≥ `total`；`orgGrants` 为空数组而非 `null`
- [ ] T036 [US3] 更新 `web/admin/src/views/client/ClientMembers.vue`（第三刀）：成员表新增"来源"列（`手动` + 部门标签，>2 个折叠为"手动 + N 个组织"，可展开）；页头统计改为"手动成员 N 人 / 实际可见 M 人"；撤销确认框显示"撤销后 X 人将不再可见"+"另有 D 个停用账号被覆盖"+"Y 人仍可通过其它途径访问"
- [ ] T037 [US3] 更新 `web/admin/src/views/client/ClientMembers.vue`（第四刀）：`orgGrantHints` 以只读提示条展示（"研发部有 118 名成员来自历史批量加入，可改为组织授权"），可逐条关闭，**不得**触发任何写操作
- [ ] T038 [US3] 更新 `web/admin/src/views/client/ClientMembers.spec.ts`：断言 ① 撤销确认框文案含三个数字 ② 来源列在 3 个来源时折叠 ③ `visibleTotal` 与 `total` 同时渲染 ④ 迁移提示不发起写请求
- [ ] T039 [US3] 扩展 `scripts/e2e/org-grant.sh`：断言撤销返回 `impactUsers` 非 0 且等于期望值；撤销后手动成员仍可见、角色绑定数不变

---

## Phase 6: User Story 4 - 门户应用管理员具备同等能力（P2）

**Goal**: 应用管理员在门户完成同样的组织授权操作，结果与 admin 端一致。

**Independent Test**: 应用管理员在门户新增"市场部（含子部门）"，admin 端刷新看到同一条记录与相同覆盖人数；对未授权应用发起同样请求返回 403。

- [ ] T040 [US4] 更新 `internal/api/me/app_admin.go`：新增 `AppListOrgGrants` / `AppAddOrgGrants` / `AppRemoveOrgGrants` 三个薄 wrapper（`h.appMgmt.<Method>(c)`），补 swag 注解 `@x-auth-level "appAdmin"`，描述写明"与 admin 端同一实现"
- [ ] T041 [US4] 更新 `internal/api/router.go` + `AGENTS.md` §4.1：在 `appAdminGroup` 注册 3 个门户 operation（`GET/POST /me/apps/:id/org-grants`、`POST /me/apps/:id/org-grants/remove`）
- [ ] T042 [US4] 更新 `internal/api/router_test.go`：断言 `GET /me/apps/:id/org-grants` 在"普通登录用户（非应用管理员）"下返回 `403 FORBIDDEN`（critique E7）；operation 断言更新为 **84**
- [ ] T043 [P] [US4] 更新 `web/portal/src/api/index.ts` 与 `types.ts`：新增三个 org-grants 调用与类型；删除门户侧 `addAppMembersByDept` 的使用
- [ ] T044 [P] [US4] 更新 `web/portal/src/views/app-admin/Members.vue`：按 T025/T026/T036/T037 的实现同构复制（组织授权卡片、根节点确认、来源列、撤销确认、迁移提示、移除"按部门加入"）；确认未把 `element-plus` 引用扩散到 `views/app-admin/` 之外
- [ ] T045 [US4] 更新 `web/portal/src/views/app-admin/Members.spec.ts`：同 T027/T038 的断言集（两端行为一致的最小证明）
- [ ] T046 [US4] 更新 `scripts/e2e/examples.sh`：新增一次"同一操作两端返回结构一致"的断言——同一应用分别用平台管理员与应用管理员调用组织授权列表，断言 `data.list` 的字段集合与覆盖人数一致（critique E16）

---

## Phase 7: User Story 5 - 组织授权变更可追溯（P3）

**Goal**: 审计页能按类型筛出组织授权的新增/调整/撤销，每条含操作者、部门路径、影响人数。

**Independent Test**: 做一次新增与一次撤销，审计页两类事件各出现 1 条，字段齐全；批量提交 3 条产生 3 条事件。

- [ ] T047 [US5] 复核并补齐 `internal/api/admin/app_auth.go` 的审计组装：三条事件按条写入、`deptPath` 用授权时的路径快照（不依赖实时 JOIN）、失败记 `result=failure` 且不写成功事件；字段名与 §6.7 说明逐字一致（**不新增 `entry` 字段**，critique E15）
- [ ] T048 [US5] 在 `scripts/e2e/org-grant.sh` 增审计断言段：断言 ① 一次提交 3 条授权产生 3 条 `app_org_granted` ② `include_sub` 变化产生 `app_org_grant_updated` 且 `detail.from/to` 正确 ③ 重复提交无变化时**不产生**事件 ④ 部门不存在时只有 1 条 `failure` 事件
- [ ] T049 [US5] 在 `scripts/e2e/org-grant.sh` 增审计断言：按 `event_type` 过滤 `app_org_granted`/`app_org_grant_revoked`，校验 `detail` 字段与 `userId` 非空

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: 文档、注册表、门禁与边界文案的收口，缺一即 `make check` 失败。

- [ ] T050 执行 `make docs`：重新生成 `internal/api/docs/openapi.json`（新增 6 个 operation），确认 `internal/api/docs/docs_test.go` 与新 path 的 `@Summary`/`@Tags`/2xx 注解齐全
- [ ] T051 更新 `AGENTS.md` 一次性收口：§3 目录树（新增 `internal/store/model/client_org_grant.go`、`internal/store/org_grant_store.go`、`migrations/0007_*`、`scripts/e2e/org-grant.sh`）、§4.1 的 6 行路由、§6.12 新表字段/索引、§6.7 `detail` 说明、§5.3 两个错误码、§2 无新增依赖（显式说明）
- [ ] T052 全量门禁：`make build && make test && make lint && make check`；`make test-integration` 需在 `TEST_MYSQL_DSN` 下通过（含 `TestOrgGrant_VisibilityMatchesCoverage`）
- [ ] T053 平台管理员与应用管理员两个角色各走一遍 `quickstart.md` §1–§6.5（含 §6.5 的边界验证：撤销后已登录用户仍可直连 SSO、降级时 `visibleTotal` 显示"—"）
- [ ] T054 兼容性复核：`POST /admin/clients/:id/members` 的 `{deptId, includeSubDepts}` 形态仍可用，OpenAPI 描述已标注"已由组织授权取代（010），仅为兼容保留"；两端 UI 均无"按部门加入"残留入口（FR-026/FR-027）
- [ ] T055 回滚演练（在 test 环境）：先回滚镜像 → `make migrate-down` → 确认服务正常启动且组织授权接口返回 404/5xx 的预期行为记录在案；`quickstart.md` §0 的说明与实际一致（critique E13）

---

## Dependencies

```
Phase 1 (T001–T007) ──► Phase 2 (T008–T020) ──► US1 (T021–T028)
                                              ├─► US3 (T032–T039)   # 依赖 US1 的 handler 与卡片
                                              ├─► US4 (T040–T046)   # 依赖 US1 + US3 的实现（复制）
                                              └─► US2 (T029–T031)   # 只需 Foundational，可与 US3/US4 并行
Phase 2 ──► US5 (T047–T049)                                          # 依赖 US1/US3 的写入路径
全部 ──► Phase 8 (T050–T055)
```

- **User Story 边界**：US1 是 MVP；US2 无生产代码（纯测试与性能证据），可与 US3 并行；US3 依赖 US1 的组织授权卡片；US4 是 US1+US3 的复制（严格按同构实现）；US5 依赖 US1/US3 的写入路径。
- **同文件冲突提示**：`internal/store/org_grant_store.go`（T008/T010/T011）、`internal/api/admin/app_auth.go`（T021/T032/T047）、`web/admin/src/views/client/ClientMembers.vue`（T025/T026/T036/T037）为串行改造，**不得并行**。

## Parallel Example: Phase 2

```bash
# 可并行：不同文件、无相互依赖
Task: "T009 [P] org_grant_store 列表聚合的 MySQL 集成断言"
Task: "T013 [P] HasOrgGrants + 部门删除保护接入"
Task: "T016 [P] 可见性改造的 store 断言"
Task: "T017 [P] 成员来源 / visibleTotal / orgGrantHints 查询"
```

## Implementation Strategy

### MVP First (US1)

1. Phase 1 + Phase 2（严格顺序，T020 的口径等价性测试是后续所有断言的地基）
2. Phase 3（US1）→ 独立验证：一次根级授权后 `dev1` 可见、`mkt1` 不可见
3. **STOP & 验证**：跑 `T028` 的 e2e 段 + `make check`，再决定是否继续

### Incremental Delivery

1. US1 交付：管理员能按部门授权，员工立即可见（本特性 80% 价值）
2. US2 交付：自动跟随的回归防线（无新代码，把风险固化为测试）
3. US3 交付：来源与撤销可预期（管理员敢用）
4. US4 交付：门户侧同等能力（IT 不再是瓶颈）
5. US5 交付：审计闭环（安全可追溯）
6. Phase 8：文档、注册表、门禁、回滚演练

## Notes

- **不要动 `:8080`**：本特性不涉及 OIDC 协议层，任何 authorize 侧校验都属越界（spec FR-029）。
- **不要加缓存**：可见性与覆盖人数均按请求实时计算（FR-013）；性能靠索引与 EXPLAIN 留证解决。
- **不要用 `ON DUPLICATE KEY` 判三态**、**不要在删除后算 `impactUsers`**：这两处是 critique 明确点名的坑（E3/E5）。
- 每完成一个 Phase 至少跑一次 `make test`；T051 触碰 `AGENTS.md` 时记得带 `Protected-Change` / `Approved-By` 尾注。

---

## Phase 9: Convergence

> 由 `/speckit.converge` 于 2026-11-01 追加。判据：`spec.md`（FR-001–FR-031、SC-001–SC-007、US1–US5 共 23 条验收场景）、`plan.md`（含 critique E1/E3/E5/E7/E8/E9/E14/E16 的落地项）、`constitution.md` 1.1.0，对照当前实现逐条核对。**只追加，不改动既有任务与代码**；宪法 5 条核心原则无 MUST 违例，故本阶段无 CRITICAL 项。

- [X] T056 两端成员页的「移除成员」确认框补齐组织授权提示：`web/admin/src/views/client/ClientMembers.vue` 的 `removeSelected()` 现文案只有角色绑定（`并解除其在本应用的 N 项角色绑定`），须按选中成员的 `orgGrants` 汇总，追加「其中 X 人仍可通过 N 个组织授权访问」；`web/portal/src/views/app-admin/Members.vue` 同构修改；两端 `*.spec.ts` 各补一条断言 per FR-017 (missing)
- [X] T057 补齐组织授权的鉴权断言：`internal/api/router_test.go` 的 `TestOrgGrantRoutesRequireAuth` 目前只覆盖 401，须新增「平台管理员以外的登录用户访问 `GET /api/v1/admin/clients/:id/org-grants` → `403 FORBIDDEN`」与「普通登录用户（非该应用的应用管理员）访问 `GET /api/v1/me/apps/:id/org-grants` → `403 FORBIDDEN`」，断言错误码而非仅状态码 per US4/AC2、US4/AC3、critique E7 (partial)
- [X] T058 在 `scripts/e2e/org-grant.sh` 增员工侧可见性段（现头部注释明确「不做」）：建应用 C 并授予根部门（含子部门）后，断言子树内账号的 `GET /api/v1/me/apps` 含 C、树外账号不含；把该条授权改为 `includeSubDepts=false` 后，本级账号仍含、子部门账号不含；全段不调用任何成员写接口（SC-001「额外人工操作次数为 0」的自动化证据）per US1/AC1、US1/AC2、US1/AC3、SC-001 (missing)
- [X] T059 给兼容期的「按部门加入」补弃用标注：`internal/api/admin/app_auth.go` 的 `AddMembers` 与 `internal/api/me/app_admin.go` 的 `AppAddMembers` 在其 swag `@Description` 写明「`deptId`/`includeSubDepts` 形态已由组织授权取代（010），仅为兼容保留，新接入请改用 `/org-grants`」，随后 `make docs` 重新生成 `internal/api/docs/openapi.json` 并同步 `specs/007-deployment/api-evolve/baseline/` 的 sha256 锚点 per FR-027、T054 (contradicts)
- [X] T060 新增成员列表的响应断言（`internal/api/admin/` 下新建或扩展用例）：`page=1` 时 `visibleTotal` 为数字、`page=2` 时为 `null`；`visibleTotal >= total`；成员行无组织授权时 `orgGrants` 序列化为 `[]` 而不是 `null`；候选行的 `orgCovered` 与实际覆盖一致 per T035、critique E9 (missing)
- [X] T061 扩展 `scripts/e2e/org-grant.sh` 的撤销段：授权前先给部门内某账号建立手动成员并授予 1 个应用角色，撤销该部门授权后断言 ① 该账号仍在成员列表 ② 其 `/api/v1/me/apps` 仍含该应用 ③ 角色绑定数与撤销前一致 ④ `impactUsers` 不含该账号 per FR-008、US3/AC3、T039 (partial)
- [X] T062 把 `scripts/e2e/org-grant.sh` 的审计段从「计数 ≥1」升级为按条断言：一次提交 3 个部门 → 恰 3 条 `app_org_granted`；同一部门翻转 `includeSubDepts` → 恰 1 条 `app_org_grant_updated` 且 `detail.from/to` 与提交值一致；重复提交无变化 → 不产生新事件；提交不存在的部门 → 恰 1 条 `result=failure` 且 `detail.reason=dept_not_found`；按 `eventType` 过滤后校验 `detail` 的 `clientId`/`deptName`/`deptPath` 与 `userId` 均非空 per FR-023、FR-024、T048、T049 (partial)
- [X] T063 在 `scripts/e2e/examples.sh` 增「两端一致」断言（critique E16 要求的约 20 行）：同一应用分别以平台管理员（`/api/v1/admin/clients/:id/org-grants`）与该应用的应用管理员（`/api/v1/me/apps/:id/org-grants`）读取组织授权列表，逐条比对 `data.list` 的元素字段集合与 `coveredUsers`/`uniqueUsers` 数值相等，任一处不等即 fail per FR-020、T046 (missing)
- [X] T064 补齐两端成员页用例（`web/admin/src/views/client/ClientMembers.spec.ts`、`web/portal/src/views/app-admin/Members.spec.ts`）：① 只选组织树顶层节点时点保存出现「将覆盖其整棵子树」二次确认，取消则不调用 `addOrgGrants` ② 正常保存的请求体含 `grants[].includeSubDepts` 且默认 `true` ③ 撤销确认框文案同时出现 `impactUsers`、停用账号数、其它来源人数三个数字 ④ 某成员有 3 个来源时渲染为「手动 + 2 个组织」并可展开查看全部 ⑤ 迁移提示（`orgGrantHints`）点击关闭不发起任何写请求 per FR-006、FR-007、FR-016、FR-031、T027/T038/T045 (partial)
- [X] T065 在 `docs/deployment.md` 增加「组织授权与访问边界」小节：写明组织授权只决定门户「我的应用」可见性与入口，撤销不回收既有会话与已签发令牌、不阻止用户直接访问接入方，离职/越权处置必须以停用账号为准；措辞与 `AGENTS.md` §6.12 的语义边注、`ClientMembers.vue` 的撤销确认文案保持一致 per FR-029 (missing)
- [X] T066 补齐 SC-005 的性能留证：在「单个部门子树含 2000 名在职员工 + 该应用 20 条组织授权」的固定 fixtures 下，按 `quickstart.md` §7 对覆盖人数聚合与可见性 `EXISTS` 各做一次 `EXPLAIN`，记录服务端 p95（保存 ≤3s、列表 ≤2s），把执行计划与结论写入 `quickstart.md` §7（或 `research.md` 新增小节）；若超阈值，按 `data-model.md` §7 的「祖先 ID 集合 `IN`」方案改写并在同处说明对外契约不变 per SC-005、critique E8、T031 (missing)
