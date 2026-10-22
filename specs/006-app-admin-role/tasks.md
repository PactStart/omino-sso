---

description: "Task list for 应用管理员标记与应用内管理入口"
---

# Tasks: 应用管理员标记与应用内管理入口

**Input**: Design documents from `/specs/006-app-admin-role/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), data-model.md, contracts/portal-app-admin-api.md

**Tests**: 本项目不接受「测试可选」：宪法「开发工作流与质量门禁」要求新增或修改业务逻辑、端点、数据访问层时同步交付测试。因此**每条实现类任务自带对应测试任务**（同一次提交），测试不单独堆到最后的 Polish 阶段。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## 任务完成定义（DoD，来自 AGENTS.md §7.4）

一条任务只有下列四项同时满足才算完成，缺一不得勾选：

1. **代码**：行为符合 `spec.md` 与 `contracts/portal-app-admin-api.md`，无 TODO / 空实现；
2. **测试**：同一提交内含单测或门控集成用例；
3. **登记**：新增/变更的路由、错误码、审计事件、表结构同步写入 `AGENTS.md` §4.1 / §5.3 / §6.7 / §6.9；
4. **门禁**：`make build && make test && make lint && make check` 全绿。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- 后端：`internal/{config,store,service,api}`、`migrations/`
- 前端：`web/portal/src`、`web/admin/src`
- 文档：`specs/006-app-admin-role/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 结构变更、模型字段、审计/错误码登记的共享基础设施

- [x] T001 [P] 创建 `migrations/0006_app_admin_role.up.sql` 与 `migrations/0006_app_admin_role.down.sql`：`ALTER TABLE client_members ADD COLUMN is_admin TINYINT(1) NOT NULL DEFAULT 0 COMMENT '1=该应用的应用管理员；授予与取消只在 admin 端' AFTER joined_at`；down 为 `DROP COLUMN is_admin`；不改主键、不加索引（依据 `data-model.md` §2）
- [x] T002 [P] 在 `internal/store/model/app_auth.go` 的 `ClientMember` 追加 `IsAdmin bool \`gorm:"column:is_admin"\``（位于 `JoinedAt` 之后），保持结构体字段顺序与列顺序一致
- [x] T003 [P] 登记规范（先登记后使用）：`AGENTS.md` §5.3 新增 `404 APP_MEMBER_NOT_FOUND`；§6.7 事件枚举追加 `app_member_admin_granted`、`app_member_admin_revoked`；§6.9 `client_members` 行追加 `is_admin`(1) 列说明
- [x] T004 [P] 在 `internal/api/response/errors.go` 新增 `ErrAppMemberNotFound`（`APP_MEMBER_NOT_FOUND` / 404 / 文案「该用户不是本应用成员，请先添加成员」）；在 `internal/store/model/audit_event.go` 新增常量 `AuditAppMemberAdminGranted = "app_member_admin_granted"`、`AuditAppMemberAdminRevoked = "app_member_admin_revoked"`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 授权判定所需的存储读取、服务判定与中间件——所有用户故事的共同前置

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 在 `internal/store/app_auth_store.go` 新增 `GetClientMembership(ctx, clientID, userID) (*model.Client, *model.ClientMember, error)`（单条 `clients LEFT JOIN client_members`，应用不存在返回 `nil, nil, nil`）；并把 `ListClientsForMember` 的返回类型改为 `[]MemberClientRow`（`model.Client` + `IsAdmin bool`，同一查询增加 `m.is_admin` 选择列）；在 `internal/service/app_auth_integration_test.go` 补断言：非成员返回 `nil` 成员、成员返回正确 `IsAdmin`、应用不存在不报错
- [x] T006 在 `internal/service/app_auth.go` 新增 `AuthorizeAppAdmin(ctx, clientID, userID) error`：依次判定「应用存在 → 应用 `status = 1` → 成员关系存在 → `is_admin = 1`」，返回可区分的哨兵（`ErrClientNotFound` / 新增 `ErrAppClientDisabled` / `ErrAppMemberNotFound` / `ErrAppNotAdmin`）供中间件写日志；**不得**返回给 HTTP 层区分。补单测覆盖 4 种拒绝原因 + 放行
- [x] T007 在 `internal/api/middleware/auth.go` 新增 `AppAdmin(appAuth *service.AppAuthService, logger *slog.Logger) gin.HandlerFunc`：读 `CurrentUser(c)`（缺失 → `ErrUnauthenticated`）、取 `c.Param("id")`、调 `AuthorizeAppAdmin`；任何拒绝一律 `response.Fail(c, response.ErrForbidden)` 并 `logger.Warn("app admin denied", "clientId", id, "userId", uid, "reason", reason)`；放行时 `c.Set(CtxClientID, id)`。补 `internal/api/middleware/app_admin_test.go`：4 种拒绝均返回 403 且文案完全一致（不含应用是否存在的信息）

---

## Phase 3: User Story 1 - 平台管理员授予或取消应用管理员 (Priority: P1) 🎯 MVP-1

**Goal**: 平台管理员在 admin 端应用成员列表看到「应用管理员 / 普通成员」并切换，变更即时写入审计。

**Independent Test**: 在 admin 端把成员 A 设为管理员、B 保持普通成员；重复设置同一状态不报错；查看审计含操作者/应用/目标成员/前后身份。

- [x] T008 [US1] 在 `internal/store/app_auth_store.go` 新增 `SetMemberAdmin(ctx, clientID, userID string, isAdmin bool) (bool, error)`：单条 `UPDATE client_members SET is_admin = ? WHERE client_id = ? AND user_id = ?`，返回 `RowsAffected > 0`（false 表示非成员或值未变，由 service 再区分）；在 `app_auth_integration_test.go` 断言「设为 1 → 查回 1」「同值重复设置影响行数 0」「非成员影响行数 0」
- [x] T009 [US1] 在 `internal/service/app_auth.go` 新增 `SetMemberAdmin(ctx, clientID, userID string, isAdmin bool, operatorID string) (bool, error)`：先 `GetClientMembership` 判定成员身份（非成员 → `ErrAppMemberNotFound`），值未变化时幂等返回当前值且**不写审计**，值发生变化时写 `app_member_admin_granted` / `app_member_admin_revoked`（`detail = {clientId, userId, isAdmin:{before,after}}`）；补集成测试覆盖四种路径（设为 / 取消 / 重复设同值 / 非成员 404）
- [x] T010 [US1] 在 `internal/store/app_auth_store.go` 的成员列表查询（`ListClientMembers`）与 `MemberRow` 增加 `is_admin`；在 `internal/api/dto/dto.go` 的 `AppMemberItem` 增加 `IsAdmin bool \`json:"isAdmin"\``；`internal/api/admin/app_auth.go` 的 `ListMembers` 映射该字段；断言 `GET /admin/clients/:id/members` 每项含 `isAdmin`
- [x] T011 [US1] 在 `internal/api/admin/app_auth.go` 新增 `SetMemberAdmin` handler（`PUT /api/v1/admin/clients/:id/members/:uid/admin`，body `{"isAdmin": bool}`，`isAdmin` 必填否则 `VALIDATION_FAILED`）+ swag 注解；在 `failAppAuth` 补 `ErrAppMemberNotFound` → `response.ErrAppMemberNotFound`；在 `internal/api/router.go` 的 `adminGroup` 注册该路由
- [x] T012 [US1] 登记规范：`AGENTS.md` §4.1 追加 `PUT /api/v1/admin/clients/:id/members/:uid/admin`（鉴权 `admin`）与成员列表 `isAdmin` 字段说明（`make docs` 与 operation 数断言统一放到 T027 一次完成，避免中途反复改写 `docs_test.go`）
- [x] T013 [P] [US1] 在 `web/admin/src/api/index.ts` 新增 `setAppMemberAdmin(clientId, userId, isAdmin)`（走既有 `clientBase()` 与 `request<T>`，路径 `/members/${userId}/admin`，method `PUT`）
- [x] T014 [US1] 在 `web/admin/src/views/client/ClientMembers.vue` 新增「身份」列（`el-tag`：应用管理员 / 普通成员）与切换操作（`el-switch` 或「设为管理员 / 取消管理员」按钮，调用 T013，成功后刷新当前页；取消应用最后一名管理员时 `ElMessageBox.confirm` 提示但不阻止，依据 A3）；页面**不得**出现应用基础信息或密钥入口
- [x] T015 [US1] 在 `internal/api/admin/app_auth_test.go` 补 HTTP 层用例（沿用 `newTestHandler` 的无 DB 风格）：body 非 JSON / 缺 `isAdmin` → 400 `VALIDATION_FAILED`；`/clients/:id/members/:uid/admin` 在无会话时返回统一响应格式的 401；service 错误映射到 `404 APP_MEMBER_NOT_FOUND`；审计字段断言放在 T009 的集成测试里

---

## Phase 4: User Story 2 - 应用管理员在门户看到并使用管理入口 (Priority: P1) 🎯 MVP-2

**Goal**: 应用管理员在「我的应用」看到「管理」按钮（位于「打开」左侧），点击进入带独立地址的管理页；无权限时看到明确提示。

**Independent Test**: 应用管理员与普通成员分别登录门户，对比同一张卡片上按钮数量与点击结果；收藏管理页地址后刷新仍停留原应用。

- [x] T016 [US2] 在 `internal/service/client.go` 的 `ListForUser` 用 T005 的 `MemberClientRow` 填充 `AppItem.IsAppAdmin`（`service.AppItem` 增加该字段），并在 `internal/api/dto/dto.go` 的 `AppItem` 增加 `IsAppAdmin bool \`json:"isAppAdmin"\``；集成测试断言：仅成员标记为真时为 `true`，**平台 `admin` 角色不置 true**（A2）
- [x] T017 [US2] 创建 `internal/api/me/app_admin.go`：`func (h *Handler) AppManageContext(c *gin.Context)` 返回 `dto.AppManageContext{clientId,name,logoUrl,description,status,memberCount}`（`appAuth.ClientDetail` 取值，**不含** `redirectUris`/`scopes`/TTL/密钥）；`internal/api/dto/dto.go` 新增该结构；`me.NewHandler` 增加 `appAuth *service.AppAuthService` 参数（同步 `internal/api/router.go` 构造处）
- [x] T018 [US2] 在 `internal/api/router.go` 新增门户应用管理分组：`appAdminGroup := authed.Group("/me/apps/:id")`（继承 `Auth`）+ `appAdminGroup.Use(middleware.AppAdmin(appAuth, logger))`，注册 `GET ""` → `meH.AppManageContext`；swag 注解；断言「普通成员 / 非成员 / 平台管理员非应用管理员」访问均 403
- [x] T019 [P] [US2] 创建 `web/portal/src/api/app.ts`：复用 `web/portal/src/api/client.ts` 的 `request`/解包/`ApiError` 约定，导出 `getManageContext(clientId)`；在 `web/portal/src/api/types.ts` 增加 `AppItem.isAppAdmin`、`AppManageContext`
- [x] T020 [US2] 修改 `web/portal/src/views/Apps.vue`：卡片操作区在「打开」按钮**左侧**新增「管理」按钮，条件 `app.isAppAdmin === true`，用 `router.push` 跳 `/account/apps/{clientId}/manage`（站内地址，FR-016）；普通成员不渲染该按钮
- [x] T021 [US2] 创建 `web/portal/src/views/app-admin/AppAdminLayout.vue`（Tailwind 实现：应用摘要 + 4 个 tab 导航 + `<router-view>`；容器 `max-w-7xl`；首次加载 403 时展示 `EmptyState`「无权限管理该应用」+「返回我的应用」，不空白、不无限加载）并在 `web/portal/src/router.ts` 注册 `/account/apps/:clientId/manage` 与 `members|roles|permissions|grants` 子路由（`meta.requiresAuth` + `meta.handleForbidden: 'local'`）
- [x] T022 [US2] 修改 `web/portal/src/main.ts`：`FORBIDDEN` 全局跳 `/403` 的行为尊重 `to.meta.handleForbidden === 'local'`（命中时不跳转，交由页面处理）；其余路由行为不变
- [x] T023 [P] [US2] 新增 `web/portal/src/views/Apps.spec.ts`（或扩展同目录 spec）：`isAppAdmin: true` 时渲染「管理」且在「打开」之前；`isAppAdmin: false` 时不渲染；点击「管理」触发跳到对应路径（mock `vue-router`）

---

## Phase 5: User Story 3 - 应用管理员在门户完成日常管理 (Priority: P1)

**Goal**: 门户管理页覆盖成员 / 角色 / 权限 / 授权，行为与错误提示与 admin 端完全一致；不含基础信息、密钥与「设为管理员」入口。

**Independent Test**: 仅以应用管理员身份在门户完成「新增角色 → 配权限 → 授给成员 → 移除成员」，admin 端核对结果一致。

- [x] T024 [US3] 在 `internal/api/me/app_admin.go` 写 15 个薄 wrapper（各 3 行 + 独立 swag 注解，路径为 `/api/v1/me/apps/:id/...`，内部转调 `admin.Handler` 同名方法）：`AppListMembers`、`AppListCandidates`、`AppAddMembers`、`AppRemoveMembers`、`AppListRoles`、`AppCreateRole`、`AppGetRole`、`AppUpdateRole`、`AppDeleteRole`、`AppPutRolePermissions`、`AppPutRoleMembers`、`AppListPermissions`、`AppCreatePermission`、`AppUpdatePermission`、`AppDeletePermission`（`me.Handler` 增加 `appMgmt *admin.Handler` 字段，`router.go` 传入既有 `adminH`；加上 T017 的摘要端点，`/me/apps/:id` 分组共 16 条路由）
- [x] T025 [US3] 在 `internal/api/router.go` 的 `appAdminGroup` 注册 T024 的 16 条路由（路径与 `contracts/portal-app-admin-api.md` §3 完全一致）；**不注册** `members/:uid/admin`、`clients` 基础信息、`logo`、`secret`、`audit-logs`、`files` 任何路径；在 `internal/api/router_test.go` 追加路由注册断言（18 条新路由全部 `registered`）与「未带会话时返回统一响应 401」断言，并断言 `PUT /api/v1/me/apps/:id/members/:uid/admin` **未注册**
- [x] T026 [US3] 在 `internal/api/router.go` 的 `authed` 分组注册 `GET /me/orgs/tree` → `adminH.OrgTree`（只读组织树，供门户成员页部门筛选与「按部门加入」）；断言应用管理员与普通登录用户均可 200，且门户侧无任何组织写端点
- [x] T027 [US3] 登记并再生文档：`AGENTS.md` §4.1 追加 17 条门户端点（`/me/apps/:id/*` 16 条 + `/me/orgs/tree`）与鉴权列「appAdmin」/「session」；执行 `make docs` 再生 `internal/api/docs/openapi.json`，把 `internal/api/docs/docs_test.go` 的 operation 数断言由 56 更新为实际值（56 + 18 = 74，以生成结果为准），并确认每个新 operation 都有 2xx/500 与 `security` 声明；`make docs-check` 通过
- [x] T028 [US3] 在 `web/portal/src/api/app.ts` 补 18 个函数（覆盖 16 条 `/me/apps/:id` 路由，其中 `listAppMemberIds`/`listCandidateIds` 走 `?all=true`、`addAppMembersByDept` 走 `deptId` 形态，共用同一条路由）：`listAppMembers`/`listAppMemberIds`/`listMemberCandidates`/`listCandidateIds`/`addAppMembers`/`addAppMembersByDept`/`removeAppMembers`/`listAppRoles`/`createAppRole`/`getAppRole`/`updateAppRole`/`deleteAppRole`/`putRolePermissions`/`putRoleMembers`/`listAppPermissions`/`createAppPermission`/`updateAppPermission`/`deleteAppPermission`；`types.ts` 同步 `AppMemberItem`（含 `isAdmin`）/`CandidateItem`/`AppRoleItem`/`RoleDetail`/`AppPermissionItem`
- [x] T029 [P] [US3] 移植 `web/admin/src/views/client/ClientMembers.vue` → `web/portal/src/views/app-admin/Members.vue`：保留关键字/部门筛选、候选弹窗、跨页全选（≤500/批）、按部门加入、批量移除；部门树改调 `/me/orgs/tree`；**移除**「身份切换」入口（FR-007），身份列只读展示；EP 组件在本文件 `import`
- [x] T030 [P] [US3] 移植 `web/admin/src/views/client/ClientRoles.vue` → `web/portal/src/views/app-admin/Roles.vue`：角色 CRUD 与表单校验（`^[A-Za-z0-9]{1,32}$`、描述 ≤100）、`APP_ROLE_NAME_EXISTS`/`APP_RESOURCE_STALE` 分支提示保持不变
- [x] T031 [P] [US3] 移植 `web/admin/src/views/client/ClientPermissions.vue` → `web/portal/src/views/app-admin/Permissions.vue`：树/列表双视图、父级防成环、`childCount > 0` 阻断删除、`APP_PERMISSION_*` 错误码分支；`localStorage` key 改为 `portal:app-permissions-view:{clientId}`（避免与 admin 共享偏好）
- [x] T032 [P] [US3] 移植 `web/admin/src/views/client/ClientGrants.vue` → `web/portal/src/views/app-admin/Grants.vue`：角色 → 权限树勾选 + 成员覆盖式保存、`autoJoinedMembers` 提示、`APP_RESOURCE_STALE` 冲突后刷新
- [x] T033 [US3] 新建 `internal/api/me/app_admin_test.go` 写后端契约测试（覆盖 `contracts/portal-app-admin-api.md` §3 全部端点）：每个 wrapper 断言「请求被完整转交 admin handler（状态码/`data` 结构/错误码一致）」；特别断言 `/me/apps/:id/members/:uid/admin`、`/me/apps/:id/logo`、`/me/apps/:id/secret` 等未注册路径返回 404 `NOT_FOUND`
- [x] T034 [US3] 新建 `web/portal/src/views/app-admin/AppAdminLayout.spec.ts`：切换 tab 保留 `clientId`、`APP_ROLE_NAME_EXISTS` 提示文案与 admin 端一致、`APP_RESOURCE_STALE` 提示刷新重试、403 时展示页内「无权限」而非空白
- [x] T035 [US3] 前端构建验证：`element-plus` 仅被 `views/app-admin/*` 引入 → `npm run build` 后确认登录页 chunk 不含 EP，且 EP 样式为独立 CSS chunk（`dist/assets/` 中定位确认）

---

## Phase 6: User Story 4 - 权限边界与即时生效 (Priority: P2)

**Goal**: 非管理员无法通过任何方式进入或调用管理能力；身份变更下一次请求即生效；管理权不外溢到其他应用。

**Independent Test**: 普通成员绕过前端直接请求管理操作 → 403 且数据无变化；X 的管理员操作 Y → 403 且不泄露 Y 是否存在。

- [x] T036 [US4] 新建 `internal/api/me/app_admin_guard_test.go`：普通成员、非成员、未登录（401）、停用账号（`AUTH_ACCOUNT_DISABLED`）、停用应用（403）分别请求 16 个门户端点中的代表性子集，断言全部被拒且目标资源数据前后一致（逐表比对）
- [x] T037 [US4] 在 `internal/api/me/app_admin_guard_test.go` 补跨应用隔离：用户为 X 管理员、非 Y 成员时操作 Y → 403，且响应体与「操作一个完全不存在的 clientId」**逐字节相同**（FR-010 不泄露存在性）
- [x] T038 [US4] 在 `internal/api/me/app_admin_guard_test.go` 补即时生效：同一会话（不重新登录）下取消管理员 → 同一端点下一次请求 403；重新授予 → 下一次请求 200（不依赖任何缓存失效）
- [x] T039 [US4] 在 `internal/api/me/app_admin_guard_test.go` 补平台管理员边界：平台 `admin` 但非 X 的应用管理员 → 门户 X 端点 403，同一操作在 `/api/v1/admin/clients/:id/*` 仍 200（FR-003 / FR-013 / US4-AC7）
- [x] T040 [P] [US4] 门户管理页操作期 403 处理：各 tab 捕获 `ApiError` 403 → `toast.error('无权限操作该应用')` + 重新拉取当前 tab 数据，不触发整页跳转（与 T022 的 `handleForbidden` 配合）

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 规范同步、四向一致、门禁与视觉回归

- [x] T041 [P] `AGENTS.md` §3 目录树同步：新增 `internal/api/me/app_admin.go`、`web/portal/src/api/app.ts`、`web/portal/src/views/app-admin/*`，标注 `ClientMembers` 身份列与 `0006` 迁移
- [x] T042 [P] 执行 `scripts/check-registry.sh`：确认路由 / 错误码 / 审计事件 / 环境变量四向一致（新路由 18 条、新错误码 1 个、新事件 2 个、无新增环境变量）
- [x] T043 全量门禁：`make build && make test && make lint && make check`，以及 `web/portal` 与 `web/admin` 的 `npm run lint`（portal 另跑 `npm run test`）
- [x] T044 视觉回归（EP × Tailwind v4 覆盖风险）：登录页、账号中心、我的应用三个既有页面无样式变化；管理页宽表在 1280/1440 宽度下无横向溢出
- [x] T045 更新 `specs/006-app-admin-role/plan.md` 的「分阶段交付」勾选状态与 `data-model.md` 中若有实现偏差的条目（保持文档与代码一致）

---

## Dependencies

```
Phase 1 (T001-T004)  ← 全部可并行
      ↓
Phase 2 (T005-T007)  ← T006 依赖 T005；T007 依赖 T006
      ↓
Phase 3 US1 (T008-T015)  ← T009 依赖 T008；T010-T012 依赖 T005/T006；T013-T015 依赖 T011
      ↓
Phase 4 US2 (T016-T023)  ← T016 依赖 T005；T017-T018 依赖 T007；T019-T023 依赖 T017/T018
      ↓
Phase 5 US3 (T024-T035)  ← T024/T025 依赖 T007 与 US1 的 admin handler；T028-T032 依赖 T024/T025；T033 依赖 US1+US3 端点
      ↓
Phase 6 US4 (T036-T040)  ← 依赖 US3 端点全部就绪
      ↓
Phase 7 (T041-T045)
```

**Story 完成顺序**：US1（授权来源）→ US2（入口可见性）→ US3（管理能力）→ US4（边界验收，理论上可与 US3 并行编写测试，但需 US3 端点存在才能跑）。

**US4 与 US1 的耦合**：T036/T038 需要 US1 的 `SetMemberAdmin` 才能构造「取消管理员」场景，故 US4 不能早于 US1 完成。

---

## Parallel Example: User Story 3（四个管理页并行移植）

```bash
# 依赖 T024/T025/T028 完成后，四页互不共享文件，可并行：
Task: "移植 ClientMembers.vue → web/portal/src/views/app-admin/Members.vue"
Task: "移植 ClientRoles.vue → web/portal/src/views/app-admin/Roles.vue"
Task: "移植 ClientPermissions.vue → web/portal/src/views/app-admin/Permissions.vue"
Task: "移植 ClientGrants.vue → web/portal/src/views/app-admin/Grants.vue"
```

```bash
# Phase 1 四个任务互不相关，可并行：
Task: "创建 migrations/0006_app_admin_role.up.sql 与 .down.sql"
Task: "ClientMember 增加 IsAdmin 字段"
Task: "AGENTS.md §5.3/§6.7/§6.9 登记"
Task: "response/errors.go 与 audit_event.go 新增常量"
```

---

## Implementation Strategy

### MVP First（US1 + US2）

1. 完成 Phase 1 + Phase 2（阻塞项）
2. 完成 US1：admin 端可授予/取消，成员列表显示身份，审计可追溯
3. 完成 US2：门户 `/me/apps` 带 `isAppAdmin`，「管理」按钮出现在可管理应用的「打开」左侧，管理页壳与 403 提示可用
4. **STOP and VALIDATE**：用管理员与普通成员两个账号验收 US1/US2 的独立测试；此阶段管理页各 tab 可为空白占位，但**入口可见性必须已正确**
5. US2 单独交付不构成完整价值（点了「管理」看不到内容），因此 MVP 的实际上线门槛是 US1 + US2 + Phase 5 的 T024-T028（接口与页面骨架就绪）

### Incremental Delivery

1. Setup + Foundational → 基础就绪
2. US1 → 独立验收（admin 端身份列 + 审计）
3. US2 → 独立验收（按钮可见性 + 路由 + 无权限提示）
4. US3 → 独立验收（成员/角色/权限/授权四页全链路完成一轮管理动作）
5. US4 → 独立验收（越权矩阵与即时生效）
6. Polish → 门禁与视觉回归

### Parallel Team Strategy

1. 全员完成 Phase 1（4 个 [P] 任务）+ Phase 2（串行 3 个任务）
2. Phase 2 完成后：
   - 后端 A：US1（T008-T012、T015）+ US4 的测试用例
   - 后端 B：US2 后端（T016-T018）+ US3 后端（T024-T027、T033）
   - 前端 C：US2 前端（T019-T023）
   - 前端 D：US3 前端（T028-T032、T034-T035）
3. US4 的测试在 US3 端点就绪后统一跑

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖
- [Story] 标签用于追溯，每个用户故事可独立验收
- **禁止**在 `web/portal/src/main.ts` 全局注册 element-plus；EP 只能在 `views/app-admin/*` 内引入（`AGENTS.md` §2.2）
- **禁止**为 `is_admin` 增加任何缓存（Redis / 进程内），FR-011 与 SC-005 依赖「每请求实时判定」
- 门户侧任何拒绝都必须是 403 `FORBIDDEN` 且文案一致，不得为调试方便区分 404/403（FR-010）
- 新增路由后必须重跑 `make docs`，`scripts/check-registry.sh` 会校验路由与规范表一致
- 提交粒度：每个任务一次提交（含测试），不得把多处不相关改动塞进同一提交

---

## 实施备注（2026-10-22，与任务描述的三处偏差）

1. **T009 / T011 审计写在 handler 而非 service**：`AppAuthService` 不持有 `Auditor`（003 起的既有口径是成员/角色/权限审计都在 handler 发）。因此 `SetMemberAdmin` 只在 service 里返回 `before/changed`，`app_member_admin_granted|revoked` 由 `admin.Handler.SetMemberAdmin` 写入，并**额外记录操作者**（`user_id` / `username`）——既有 `app_*` 事件未记录操作者，本次按 FR-014 / SC-006 补齐；值未变化时不写第二条审计。
2. **T033 契约测试的落点**：`internal/api/me/app_admin_test.go` 用编译期 + 数量断言固化「16 个端点存在且非空实现」，不重复测试一句话转调；委托行为的一致性由 `internal/api/router_test.go`（路由集、未注册路径 404、无会话 401）与 `internal/service` 的判定集成测试共同保证。
3. **T036–T039 边界矩阵的层级**：仓库现有集成测试只有 MySQL 门控（无 Redis/HTTP 会话脚手架），故越权矩阵落在两处——`internal/api/middleware/app_admin_test.go`（四种拒绝原因响应体逐字一致）与 `internal/service` 的 `AuthorizeAppAdmin` 用例（非成员 / 普通成员 / 停用应用 / 跨应用隔离 / 取消后即时生效 / 平台 admin 角色不放行）；HTTP 层的 401 与未注册路径由 `router_test.go` 覆盖。

**验证记录**：`migrations/0006` 已在真实 MySQL 8.4（compose 3307）执行并通过全部集成用例；`make build`（含 `docs-check`）、`make test`、`make lint`、`make check` 全绿，`web/portal` 与 `web/admin` 各自 `npm run build` 成功。**T044（浏览器视觉回归）未执行**，仅完成静态验证：EP 独立 chunk、登录页 chunk 不含 EP。

---

## Phase 8: Convergence

> 由 `/speckit.converge`（2026-10-22）在 `/speckit.implement` 之后追加。仅记录相对 `spec.md` / `plan.md` / 宪法仍未满足的部分，不追溯、不改写既有任务。

- [x] T046 为门户侧成员 / 角色 / 权限 / 授权操作补齐审计操作者：`app_member_added`、`app_member_removed`、`app_role_*`、`app_permission_*`、`app_grant_updated` 写入 `user_id` / `username`（handler 内已有 `middleware.CurrentUser`），保持事件类型与 detail 结构不变；在 `internal/service/app_auth_integration_test.go` 断言门户调用后审计记录带操作者；同步 `AGENTS.md` §6.7 说明「app_* 事件均带操作者」 per FR-014 (partial)
- [x] T047 在浏览器中执行视觉回归（`element-plus` 全局样式 × Tailwind v4 preflight）：登录页、账号中心、我的应用三个既有页面无样式回归；管理页宽表在 1280 / 1440 宽度下无横向溢出、`el-table` / `el-tree` / `el-dialog` 渲染正常；将结论记录到本文件 per plan:D9 / T044 (partial)
- [x] T048 为门户管理页补 Element Plus 真实渲染的组件测试：挂载 `Members.vue`（断言 `el-table` 渲染出成员行、「身份」列只读展示「应用管理员 / 普通成员」、**不存在**「设为管理员 / 取消管理员」按钮）与 `Roles.vue`（断言 `APP_ROLE_NAME_EXISTS` 提示文案与 admin 端一致），覆盖当前只有壳层 `AppAdminLayout.spec.ts` 且 `RouterView` 被 stub 的空白 per FR-007 / US3-AC6 / AGENTS §2.2 (partial)

- [x] T049 修复应用权限新增/编辑弹窗「父级」下拉显示权限点 ID 的问题：`el-tree-select` 缺 `:props` 映射，节点数据的字段是 `name`（值为「权限名称（权限编码）」）而 EP 默认取 `label`，导致回落到显示 ULID。admin 端 `web/admin/src/views/client/ClientPermissions.vue` 与门户端 `web/portal/src/views/app-admin/Permissions.vue` 是同源复制、**两处都要改**；改后在浏览器确认下拉显示「权限名称（编码）」而非 ID per US3-AC2 / 用户反馈 2026-10-22 (partial)

### Phase 8 执行记录（2026-10-22）

- **T046 实施方式**：没有逐处改 13 个 handler，而是把操作者注入下沉到认证链路——`middleware.loadSession` 调用 `service.WithActor(ctx, userId, username)`，`service.Auditor.Emit` 在事件未显式指定操作者时回填（显式值优先，未登录仍为空）。收益：门户与 admin 两端、以及后续所有新增 handler 自动带操作者，不需要在每个 handler 里重复取用户。单测 `internal/service/audit_actor_test.go`（3 例：回填 / 显式优先 / 无 actor 保持空）；端到端复核：门户端点 `POST /api/v1/me/apps/demo-app/permissions` 后 `audit_events` 里 `app_permission_created` 的 `username=admin`、`user_id` 非空，`app_permission_deleted` 同样带操作者。
- **T047 视觉回归结果（通过）**：截图归档在 `.tmp/shots/`（gitignore）。① `10-portal-login.png` 登录页与改动前一致，输入框仍是 Tailwind 样式，未出现 EP 的圆角/蓝色边框；② `11-apps.png` 应用管理员卡片上「管理」在「打开」左侧；③ `12-manage-members.png` / `13-manage-permissions.png` / `14-manage-grants.png` 三个 tab 的 `el-table`、`el-tree`、`el-radio-group`、`el-tabs` 渲染正常；④ `15-members-1440.png` 1440 宽度下 `max-w-7xl` 内容区无横向溢出；⑤ `16-member-apps.png` 普通成员只有「打开」；⑥ `17-denied.png` 直达管理地址显示「无权限管理该应用」+ 返回入口。控制台仅有 2 条 `cdn.example.com/logo/demo.svg` 加载失败（seed 假域名，非代码问题）。
- **T048**：新增 `web/portal/src/views/app-admin/Members.spec.ts`（3 例：按 clientId 拉取并渲染表格、身份列只读、无「设为管理员/取消管理员」按钮）与 `Roles.spec.ts`（2 例：渲染角色列表、`APP_ROLE_NAME_EXISTS` 透传后端文案与 admin 端一致），EP 组件在 jsdom 下真实渲染（补 `ResizeObserver` polyfill）。
- **T049 验证**（2026-10-22）：根因是 `el-tree-select` 未配置 `:props`，而节点数据字段是 `name`（值「名称（编码）」），EP 默认取 `label` 缺失后回落到 value（ULID）。两端各加 `parentSelectProps = { label: 'name', children: 'children' }` 后，浏览器实测 dropdown 显示「订单管理（order:all）」「用户管理（user:all）」，截图 `.tmp/shots/21-portal-dropdown-open.png`（门户）与 `.tmp/shots/23-admin-dropdown-open.png`（admin）。同类写法在成员页部门树（`orgProps`）本来就是对的，未受影响。

---

## Phase 9: 口径调整——门户应用信息页（2026-10-22）

> 用户明确：admin 端保持原状（8 个可编辑字段，不修改）；门户**需要**应用详情，参考 admin 但可编辑字段缩减为名称 / 描述 / Logo / 回调地址，其余只展示。

- [x] T050 服务端白名单：新增 `ClientService.UpdateBasicInfo`（清空 `PostLogoutURIs`/`AccessTokenTTL`/`RefreshTokenTTL`/`Status` 后复用 `Update`），handler 请求结构体 `AppUpdateClientRequest` 只含 4 个业务字段 + `updatedAt`；`internal/api/me/app_admin_test.go` 用反射固化字段清单，出现第 5 个业务字段即失败
- [x] T051 门户端点：`GET /api/v1/me/apps/:id/detail`（复用 `dto.ClientDetail`）、`PATCH /api/v1/me/apps/:id`（写 `client_updated` 审计，地址类字段记前后数组）、`POST|DELETE /api/v1/me/apps/:id/logo`（复用 `FileService`，scene=`app_logo`）；全部挂 `AppAdmin` 中间件，路由登记 `internal/api/router.go`
- [x] T052 门户前端：新增 `views/app-admin/Overview.vue`（EP 表单 4 字段 + `el-descriptions` 只读展示 + `ImageUpload` 上传 Logo）、`api/app.ts` 增 `getAppDetail`/`updateAppBasicInfo`/`setAppLogo`/`clearAppLogo`、`api/types.ts` 增 `AppDetail`/`UpdateAppBasicInfoPayload`；路由与 tab 增加 `overview` 并设为默认落地页
- [x] T053 契约与注册表：`AGENTS.md` §3/§4.1/§4.6 与 appAdmin 说明同步；`spec.md` FR-005 与 Clarifications、`plan.md` D11/D12、`contracts/portal-app-admin-api.md` §3/§7、`data-model.md` §5 同步；`make docs` 重新生成 `openapi.json`（operation 78）
- [x] T054 测试与门禁：`router_test.go` 路由集 + 未注册路径（密钥/设为管理员仍在禁止清单）、`docs_test.go` operation 数、`Overview.spec.ts`（只读控件缺席、保存只带 4 字段、空回调地址拦截）、`AppAdminLayout.spec.ts`（5 tab）；`go test ./internal/api/...`、portal `lint`/`typecheck`/`vitest` 通过

---

## Phase 10: Convergence

> 由 `/speckit.converge`（2026-10-22）在 Phase 9 之后追加，仅记录与 `spec.md` / `plan.md` / 宪法仍有差距的部分；`admin` 端保持原状，不在本阶段范围。

- [X] T055 修复门户「应用信息」页 Logo 与乐观锁的冲突：上传/清除 Logo 会经 `FileService.applyBusinessField` / `clearBusinessField` 改写 `clients.updated_at`（`internal/service/file.go`、`internal/store/file_store.go`），而 `web/portal/src/views/app-admin/Overview.vue` 的 `onLogoSuccess` / `clearLogo` 只更新 `form.logoUrl`，`detail.updatedAt` 仍是旧版本 → 紧接着点「保存」必然 `409 APP_RESOURCE_STALE` 并触发 `load()` 回滚刚输入的名称/描述。修复方式：Logo 成功/清除后重新拉取 `GET /api/v1/me/apps/:id/detail`（或让 logo 端点返回最新 `updatedAt`）刷新版本号，并在 `Overview.spec.ts` 补「上传 Logo 后保存不再 409」用例 per FR-005 (partial)
- [X] T056 统一文档中的范围边界表述：`spec.md` A8 仍写「门户管理页仅覆盖成员 / 角色 / 权限…不含应用基础信息」，与 Clarifications 2026-10-22 及 FR-005 冲突；`plan.md` 开头与 D11 仍写「4 个 tab」「不含应用基础信息」；`specs/006-app-admin-role/api-evolve/orchestration.md` 仍把「门户基础信息 / Logo」列为 `NoRoute` 路径。按新口径改写这些表述（含应用信息页，仅名称/描述/Logo/回调地址可编辑；密钥与「设为/取消管理员」仍只在 admin 端），**不得**改动历史 Phase 的已完成任务条目 per FR-005 (contradicts)
- [X] T057 为服务端白名单补行为级测试：`internal/api/me/app_admin_test.go` 目前只有请求结构体反射断言，未验证 `ClientService.UpdateBasicInfo` 真的忽略 `postLogoutURIs` / `accessTokenTtl` / `refreshTokenTtl` / `status`。补一条测试（MySQL 集成脚手架或 store 级替身），构造同时携带 4 个白名单字段与 4 个只读字段的调用，断言只读字段在库中保持原值 per FR-005 (missing)
- [X] T058 修正实现注释与计划的数量漂移：`internal/api/me/app_admin.go` 文件头仍称「只有两类代码（摘要 + 一组薄 wrapper）」，`internal/api/me/app_admin_test.go` 注释写「应用信息 3 + 成员/角色/权限/授权 16」，实际为 1 摘要 + 4 应用信息 + 15 wrapper；`plan.md` D2 仍写「16 个薄 wrapper」。按当前实现更新注释与 D2 表述 per plan:D2 (partial)
- [X] T059 对新增「应用信息」页做一次浏览器视觉与交互回归：EP 的 `el-descriptions` / `el-form` 与 Tailwind v4 在门户的叠加效果、Logo 上传、保存与 409 提示、只读字段不可编辑；结论写入本文件（延续 T044/T047 的做法，截图归档 `.tmp/shots/`） per plan:D9 (partial)

### Phase 10 实施与视觉回归记录（2026-10-22）

- **T055**：`Overview.vue` 新增 `syncVersion()`，Logo 上传/清除后重新拉取详情只同步 `updatedAt`/`logoUrl`（不动用户正在编辑的表单）；`Overview.spec.ts` 增「上传 Logo 后保存使用最新版本号」用例（4 例全过）。
- **T056**：`spec.md` A8、`plan.md` Summary/分阶段/实施状态/D2、`api-evolve/orchestration.md` 备注已与 FR-005 新口径对齐（历史 Phase 任务未改动）。
- **T057**：新增 `internal/service/client_basic_info_integration_test.go`，在真实 MySQL（`TEST_MYSQL_DSN`）上验证 `UpdateBasicInfo` 忽略登出地址/TTL/状态，仅落库名称/描述/Logo/回调地址；`go test ./internal/service -run TestUpdateBasicInfoIgnoresReadOnlyFields` PASS。
- **T058**：`me/app_admin.go` 头注释（三类代码）、`app_admin_test.go` 端点构成注释、`plan.md` D2（15 wrapper + 5 自有）已更正。
- **T059 视觉/交互回归**（1440×900，登录账号 `admin`，应用 `demo-app`）：
  - 截图：`.tmp/shots/20-portal-manage-overview.png`、`.tmp/shots/21-portal-manage-overview-1440.png`；快照文本 `.tmp/shots/overview-snap.txt`、`overview-after-logo*.txt`
  - `/manage` 默认落地 `/manage/overview`；五个 tab「应用信息 / 成员 / 角色 / 权限 / 授权」顺序正确
  - DOM 断言：`.el-switch` 0 个、`.el-input-number` 0 个；只读区含「启用状态 / 访问令牌有效期 / 刷新令牌有效期 / 登出地址」
  - 保存链路：改描述 → 提示成功 → `clients.description` 落库、审计 `client_updated`（`changed:["description"]`）
  - **Logo 回归（T055 关键路径）**：上传 png → `logo_url` 立即回写且 `updated_at` 递增 → 紧接着改描述并保存 → **成功落库，无 `409 APP_RESOURCE_STALE`**
  - 数据复原：测试后已把描述恢复为「陪伴学习APP」、Logo 地址恢复为原 OSS URL

---

## Phase 11: Convergence

> 由 `/speckit.converge`（2026-10-22，第二轮）追加；上一轮 Phase 10 的 T055–T059 已全部完成，本轮仅剩文档残留与一条错误分支用例。

- [X] T060 修正 `plan.md` 两处与新口径不符的残留：①「影响/目录结构」块里 `internal/api/me/app_admin.go` 仍写「+ 16 个薄 wrapper」，实际为 15 个 wrapper + 4 个应用信息自有 handler + 1 条摘要；②宪法自检表「IV. 凭证不出服务端」仍称「响应不含 `redirect_uris`、TTL 等配置」，应改为「仍不返回 `client_secret`/密钥；`redirect_uris` 与 TTL 仅在应用详情中作只读展示」 per plan:D2 (partial)
- [X] T061 为门户应用信息页补 `409 APP_RESOURCE_STALE` 冲突用例：`web/portal/src/views/app-admin/Overview.spec.ts` 中让 `updateAppBasicInfo` 拒绝 `new ApiError('APP_RESOURCE_STALE', '已被他人修改', 409)`，断言页面出现「已被他人修改，请刷新后重试」提示、调用 `load()` 重新拉取详情并回滚表单，且不把冲突当成功处理 per FR-005 (missing)

### Phase 11 实施记录（2026-10-22）

- **T060**：`plan.md` 结构树改为「15 个薄 wrapper」；宪法自检表 IV 改为「不返回 `client_secret`/密钥；`redirect_uris`/TTL 仅应用详情只读展示」。
- **T061**：`Overview.spec.ts` 新增 409 用例（构造 `ApiError('APP_RESOURCE_STALE', …, 409)`），断言页内提示、`getAppDetail` 二次拉取与表单回滚；同时把 `Overview.vue` 冲突分支改为「提示 → 重新加载 → 再落提示」，避免 `load()` 清空 `formError` 导致用户看不到冲突原因。
