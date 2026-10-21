# Tasks: 应用详情子页面与应用内授权体系

**Input**: Design documents from `/specs/003-app-management-pages/`

**Prerequisites**: [plan.md](./plan.md)（必需）、[spec.md](./spec.md)（用户故事与优先级）、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**Tests**: 本特性**不采用 TDD**。下列测试任务来自 `plan.md` 的 Testing 节（纯函数单测、`claims_test.go`、`TEST_MYSQL_DSN` 门控集成测试、路由计数断言），在对应实现之后编写，不要求先写失败用例。

**Organization**: 按用户故事分阶段，每个阶段可独立实现与验收；批次划分与 `plan.md`「交付批次」一致。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可与同阶段其它任务并行（不同文件、无未完成依赖）
- **[Story]**: 该任务所属用户故事（US1–US6），Setup / Foundational / Polish 阶段不带
- 每条任务都给出精确文件路径

## Path Conventions

单仓库单体：Go 后端在 `internal/`、`cmd/`、`migrations/`；两个 Vue 前端在 `web/admin/`、`web/portal/`。

## ⚠️ 实现前必须知道的两点

1. **迁移编号已对齐为 `0003`**：`migrations/0002_clients_meta.*` 已被 002 特性占用，因此本特性迁移取 **`0003_app_auth.up.sql` / `0003_app_auth.down.sql`**。plan / data-model / quickstart 已于 2026-10-21 同步改为 `0003_app_auth`，实现时按 0003 落位即可。
2. **handler 构造函数位置**：`admin.NewHandler` 在 `internal/api/admin/users.go:28`（不是独立 `handler.go`）；`me` 包的 apps 列表 handler 在 `internal/api/me/profile.go`，没有 `me/apps.go`。

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 迁移、模型、错误码与审计事件的登记落位。规范要求"先登记、后使用"，因此本阶段全部在业务代码之前完成。

- [x] T001 创建 `migrations/0003_app_auth.up.sql`：按 [data-model.md](./data-model.md) 逐列建 6 张表（`client_members`、`client_roles`、`client_permissions`、`client_role_permissions`、`client_role_members`、`access_tokens`），全部 `utf8mb4`；时间列 `BIGINT` 毫秒且以 `_at` 结尾；`client_id VARCHAR(64)`、`client_roles.name`、`client_permissions.code` 显式 `COLLATE utf8mb4_0900_as_cs`；主键 `client_members(client_id,user_id)`、`client_roles.id`/`client_permissions.id` 为 `CHAR(26)`；唯一索引 `uk_client_roles_name (client_id,name)`、`uk_client_permissions_code (client_id,code)`；普通索引 `idx_client_members_user (user_id,client_id)`、`idx_client_roles_client (client_id,created_at)`、`idx_role_permissions_perm (permission_id)`、`idx_role_members_user (user_id)`、`idx_client_permissions_parent (client_id,parent_id,sort)`、`idx_access_tokens_exp (expires_at)`。禁止 `AutoMigrate`、禁止代码内 DDL
- [x] T002 创建 `migrations/0003_app_auth.down.sql`：按外键依赖逆序 `DROP TABLE`（`access_tokens` → `client_role_members` → `client_role_permissions` → `client_permissions` → `client_roles` → `client_members`）；文件头注明"执行后成员/角色/权限配置不可恢复"
- [x] T003 [P] 创建 `internal/store/model/app_auth.go`：`ClientMember`、`ClientRole`、`ClientPermission`、`ClientRolePermission`、`ClientRoleMember`、`AccessToken` 六个结构体 + `TableName()`，字段类型与 T001 的列一一对应（时间字段用 `int64` 毫秒，不用 `time.Time`）
- [x] T004 更新 `AGENTS.md`：§5.3 追加 5 个错误码（`APP_ROLE_NAME_EXISTS` 409、`APP_PERMISSION_CODE_EXISTS` 409、`APP_PERMISSION_HAS_CHILDREN` 409、`APP_PERMISSION_INVALID_PARENT` 400、`APP_RESOURCE_STALE` 409）；§6.7 的 `event_type` 枚举追加 10 个（`client_updated`、`app_member_added`、`app_member_removed`、`app_role_created`、`app_role_updated`、`app_role_deleted`、`app_permission_created`、`app_permission_updated`、`app_permission_deleted`、`app_grant_updated`）；§6 追加 6 张表的字段/索引摘要；§4 接口总览追加 10 条新 path（17 个操作）与 `GET /admin/clients` 的 `memberCount` / `hasMembers` 字段约定。**`AGENTS.md` 属受保护文件**，提交信息须带 `Protected-Change: AGENTS.md` 与 `Approved-By: <name>` 尾注
- [x] T005 [P] 更新 `internal/api/response/errors.go`：登记 T004 的 5 个错误码（`ErrAppRoleNameExists`、`ErrAppPermissionCodeExists`、`ErrAppPermissionHasChildren`、`ErrAppPermissionInvalidParent`、`ErrAppResourceStale`），message 用 `data-model.md`「错误映射」表中的固定文案（依赖 T004）
- [x] T006 [P] 更新 `internal/store/model/audit_event.go`：追加 T004 登记的 10 个事件常量，命名与枚举值逐字一致（依赖 T004）
- [x] T007 [P] 更新 `internal/api/dto/dto.go`：新增 `ClientDetail`（`GET /admin/clients/:id` 与 `PATCH` 共用：含 `status`、`accessTokenTtl`、`refreshTokenTtl`、`updatedAt`、`memberCount`，**不含** `client_secret_hash`）、`AppMemberItem`（含 `status`、`roleCount`）、`CandidateItem`、`AppRoleItem`（含 `permissionCount`/`memberCount`/`updatedAt`）、`RoleDetail`（含 `permissionIds`/`memberIds`）、`AppPermissionItem`、`GrantResult`，以及请求体 `AddMembersReq`（`userIds` **或** `deptId`+`includeSubDepts`，互斥）、`RemoveMembersReq`、`RoleUpsertReq`（含 `updatedAt *int64`）、`PutPermissionsReq`、`PutRoleMembersReq`、`UpdateClientReq`（全部指针字段 + `updatedAt *int64`）

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 打通 service/store 骨架与 fx 装配，使后续每个故事都只加自己的方法。

**⚠️ CRITICAL**: 本阶段完成前，任何用户故事都不能开始。

- [x] T008 创建 `internal/service/app_auth.go`：`AppAuthService` 结构体（字段 `st *store.Store`）+ `NewAppAuthService(st *store.Store) *AppAuthService`；定义哨兵错误（`ErrAppRoleNotFound`、`ErrAppPermissionNotFound`、`ErrAppMemberNotFound`、`ErrTooManyIDs`）；随后创建 `internal/store/app_auth_store.go`，先只放文件头注释与包声明（方法由各故事按需追加）
- [x] T009 装配打通（一次提交内完成）：`cmd/sso/main.go` 的 `fx.Provide` 增加 `service.NewAppAuthService`；`internal/api/router.go` 的 `NewRouter` 增加 `appAuth *service.AppAuthService` 参数；`internal/api/admin/users.go` 的 `NewHandler` 增加 `appAuth *service.AppAuthService` 参数（`Handler` 结构体加字段）；`internal/oidc/storage.go` 的 `NewStorage` 增加 `appAuth` 参数并存入 `Storage` 字段。完成后 `make build` 必须通过（此时尚无新路由）

---

## Phase 3: User Story 1 - 应用详情子页面（P1）🎯 MVP 前半

**Goal**: "详情"从抽屉改为独立子页面，五菜单固定，详情页可就地编辑允许字段。

**Independent Test**: 点"详情"后地址变为 `/clients/demo-app/overview`、无抽屉；刷新仍停同一菜单；改名称与 TTL 保存成功且列表同步；提交 `ftp://x` 被拒且原值不变。

- [x] T010 [US1] 更新 `internal/store/client_store.go`：新增 `UpdateClientFields(ctx, clientID string, patch model.ClientPatch, expectedUpdatedAt *int64) error`，事务内先 `SELECT ... FOR UPDATE` 读 `updated_at`，与 `expectedUpdatedAt` 比对不一致返回哨兵错误，写入后刷新 `updated_at`
- [x] T011 [US1] 更新 `internal/service/client.go`：新增 `Update(ctx, clientID string, in UpdateClientInput) (*AppDetail, error)`，校验规则逐条落实（`clientName` trim 后 1–128；`description` ≤255；`logoUrl` `http(s)` 绝对地址 ≤512；`redirectUris` 1–10 条、每条 ≤512、去重；`postLogoutUris` 0–10 条；`accessTokenTtl` 60–86400；`refreshTokenTtl` 300–31536000；`status` ∈ {0,1}），指针字段三态（缺失=不变），版本不符映射为 `response.ErrAppResourceStale`
- [x] T012 [US1] 更新 `internal/api/admin/clients.go`：新增 `GetClient`（`GET /admin/clients/:id`，供子页面刷新直达）与 `UpdateClient`（`PATCH /admin/clients/:id`）两个 handler，两者共用 `ClientDetail`（含 `status`、`accessTokenTtl`、`refreshTokenTtl`、`updatedAt`、`memberCount`，**不含** `client_secret_hash`）；`UpdateClient` 解析 T007 的 `UpdateClientReq`、调用 T011；补 swag 注解（`400 VALIDATION_FAILED` / `404 NOT_FOUND` / `409 APP_RESOURCE_STALE`）；写审计 `client_updated`，detail 记 `changed[]` 字段名，`redirectUris`/`postLogoutUris` 额外记 `before`/`after` 完整数组
- [x] T013 [US1] 更新 `internal/api/router.go`：在 `adminGroup` 注册 `GET` 与 `PATCH /clients/:id`（**1 条 path / 2 个操作**，`routes()` 计数变化起点）
- [x] T014 [P] [US1] 更新 `web/admin/src/api/types.ts` 与 `web/admin/src/api/index.ts`：新增 `getClient(id)`、`updateClient(id, payload)` 与 `ClientDetail` 类型（可编辑字段全部可选 + `updatedAt`；`ClientDetail` 由 T007 定义）
- [x] T015 [P] [US1] 更新 `web/admin/src/router.ts`：新增 `/clients/:clientId/:tab(overview|members|roles|permissions|grants)` 子路由，默认 `overview`，非法 `tab` 重定向到 `overview`，无需登录态以外的额外守卫
- [x] T016 [P] [US1] 创建 `web/admin/src/views/client/ClientDetail.vue`：左侧 `el-menu`（`router` 模式）+ 右侧 `<router-view>` + "返回应用列表"入口；五项顺序固定为 应用详情 / 应用成员 / 应用角色 / 应用权限 / 授权管理
- [x] T017 [US1] 创建 `web/admin/src/views/client/ClientOverview.vue`：进页即用 `getClient(clientId)` 取数（支持刷新与地址直达，FR-002）；只读展示应用标识、授权方式、可申请范围、创建时间；可编辑表单承载 T011 的 8 个字段；保存成功后就地回填并刷新列表缓存；字段级错误展示；TTL 变更提示"仅对此后签发的令牌生效，已签发令牌不受影响"；`status` 改为停用时二次确认并显示"停用后全员在门户不可见，成员与角色配置保留"；密钥只读且不展示明文（依赖 T014）
- [x] T018 [US1] 更新 `web/admin/src/views/Clients.vue`：删除详情抽屉及其状态，详情按钮改为 `router.push({ name: 'client-detail', params: { clientId, tab: 'overview' } })`（依赖 T015）
- [x] T019 [US1] 更新 `internal/api/router_test.go`：为新注册 path 增加"无 `sid` cookie → `401`"断言；`r.Routes()` 计数按**本批实际注册量**更新（批次 1 结束应为 **20 path / 25 操作**；该文件在批次 2 继续上调，见 T063）

---

## Phase 4: User Story 2 - 应用成员管理与门户可见性（P1）🎯 MVP

**Goal**: 成员页可筛选、批量增删、跨页全选、按部门整批加入；成员关系成为门户"我的应用"唯一可见性依据。

**Independent Test**: 新建应用 B 不加成员 → 普通员工门户看不到 B；把他加入 B → 可见；移除 → 再次不可见；成员 500 人时跨页全选反选 1 人，移除数与界面一致。

- [x] T020 [US2] 更新 `internal/store/app_auth_store.go`：成员分页查询（联 `users`/`organizations` 取用户名、姓名、部门名；`roleCount` 用子查询聚合；`ORDER BY u.username`；`keyword` 对 `username` 前缀 + `display_name` 包含、长度 ≤64；`deptId` 含子部门走 `organizations.path` 前缀）；`all=true` 返回 `userIds` 且**上限 5,000**，超限返回 `ErrTooManyIDs`
- [x] T021 [US2] 更新 `internal/store/app_auth_store.go`：候选查询用 `NOT EXISTS` 反连接排除已成员，`pageSize ≤ 50`，排序同样 `u.username`；`all=true` 同样受 5,000 上限
- [x] T022 [US2] 更新 `internal/store/app_auth_store.go`：批量添加用 `INSERT IGNORE`，以 `RowsAffected` 得 `added`、其余计 `skipped`；按部门加入用单条 `INSERT IGNORE ... SELECT`（含子部门、`u.status=1` 计入 added、已存在计入 existing、停用计入 disabled）
- [x] T023 [US2] 更新 `internal/store/app_auth_store.go`：批量移除在同一事务内先统计并删除 `client_role_members`、再删 `client_members`，返回 `removed`/`roleBindingsRemoved`/`notMember`
- [x] T024 [US2] 更新 `internal/service/app_auth.go`：成员规则——`userIds` 去重后 1–500（空数组或超 500 → `VALIDATION_FAILED`，message 含"单次最多 500 个，请分片提交"）；`userIds` 与 `deptId` 互斥（同时提交 → `VALIDATION_FAILED`）；`deptId` 不存在 → `VALIDATION_FAILED`；**新增**成员（含部门整批）时跳过停用账号（`status≠1`）并计入 `disabled`/`skipped`，**已存在**的停用成员 MUST 保留成员关系、列表标记"已停用"（FR-014），仅禁止再授予新角色（见 T050）；`all=true` 超限映射为 `VALIDATION_FAILED` 并带"请先按部门或关键字缩小范围"
- [x] T025 [US2] 更新 `internal/store/client_store.go` 与 `internal/service/client.go`：新增 `ListClientsForMember`（`JOIN client_members` 且 `clients.status=1`），`ListForUser` 由"全部启用应用"改为调用它（返回字段与排序不变）；新增 `memberCount` 聚合与 `hasMembers` 过滤（`true` → `EXISTS`、`false` → `NOT EXISTS`、缺省不加条件）
- [x] T026 [US2] 更新 `internal/api/admin/app_auth.go`：4 个成员端点 handler（列表、候选、批量添加、批量移除）+ swag 注解；审计 `app_member_added`（`clientId`、`added`、`skipped`，部门形态另记 `existing`/`disabled`/`deptId`）与 `app_member_removed`（`removed`、`roleBindingsRemoved`）；并创建 `internal/api/admin/app_auth_test.go`：`userIds` 去重、空数组拒绝、超 500 拒绝、`userIds` 与 `deptId` 互斥的请求体解析单测
- [x] T027 [US2] 更新 `internal/api/router.go`：注册 **3 条 path / 4 个操作**（`GET`+`POST /clients/:id/members`、`POST /clients/:id/members/remove`、`GET /clients/:id/members/candidates`）
- [x] T028 [US2] 更新 `internal/api/admin/clients.go`：`GET /clients` 列表项**新增** `memberCount` 字段、Query **新增** `hasMembers`（只接受 `true`/`false`），既有字段与排序不变（FR-032）
- [x] T029 [P] [US2] 更新 `web/admin/src/api/index.ts` 与 `types.ts`：`listAppMembers`/`listMemberCandidates`/`addAppMembers`/`removeAppMembers` + `AppMember`/`Candidate` 类型
- [x] T030 [US2] 创建 `web/admin/src/views/client/ClientMembers.vue`：成员表（含"已停用"标记与角色数）+ 候选弹窗（用户名/姓名/部门三条件）+ "按部门加入"（显示 新增/已存在/已停用 三类计数）+ 跨页全选（`all=true` 拉 ID 集合，再本地反选）+ 提交时按 500 分片顺序调用并汇总计数 + 移除确认显示"将移除 N 名成员，并解除其 X 项角色绑定" + 无成员时提示"当前无成员，员工在门户将看不到该应用"（依赖 T029）
- [x] T031 [US2] 更新 `web/admin/src/views/Clients.vue`：新增"成员数"列与"仅看无成员"开关（置 `hasMembers=false`），无成员行给出醒目提示（依赖 T029）
- [x] T032 [P] [US2] 更新 `web/portal/src/views/Apps.vue`：无可见应用时展示空状态说明与"联系管理员开通"引导，不得只显示空白列表（FR-035）
- [x] T033 [US2] 创建 `internal/service/app_auth_integration_test.go`（`TEST_MYSQL_DSN` 门控）：批量添加幂等与计数、重复添加不重复插入、移除成员级联解除角色绑定、部门整批加入三类计数、`memberCount` 与 `hasMembers=false` 过滤、门户可见性过滤（非成员不可见、停用应用不可见）
- [x] T034 [US2] 修订既有测试：检索所有断言"新用户可见 N 个应用"的用例（`internal/service/*_test.go`、`internal/api/router_test.go` 等），改为先建 `client_members` 再断言；新增一条"非成员看不到应用"的回归用例（E8）

---

## Phase 5: User Story 3 - 应用角色管理（P2）

**Goal**: 应用内角色 CRUD，名称唯一且仅字母数字，描述 ≤100，删除时连带解除两类绑定。

**Independent Test**: 新建 `opsAdmin` 成功；重复 `opsAdmin` → 409；`ops-admin` → 400；`Admin` 与 `admin` 可共存；删除前确认框显示"3 项权限绑定与 5 名成员绑定"。

- [x] T035 [US3] 更新 `internal/store/app_auth_store.go`：角色 CRUD + 列表（`permissionCount`/`memberCount` 聚合）+ 详情（`permissionIds`/`memberIds`）+ 写操作携带 `updated_at` 版本比对，冲突返回哨兵错误
- [x] T036 [US3] 更新 `internal/service/app_auth.go`：角色名校验 `^[A-Za-z0-9]{1,32}$`（trim 后）、描述 ≤100；MySQL `1062` 映射为 `APP_ROLE_NAME_EXISTS`；删除在事务内先删 `client_role_permissions` 与 `client_role_members` 再删角色（不删成员本身）
- [x] T037 [US3] 更新 `internal/api/admin/app_auth.go`：5 个角色端点（列表、创建、详情、改名/改描述、删除）+ swag 注解；审计 `app_role_created`/`app_role_updated`（`changed[]`）与 `app_role_deleted`（`removedPermissionIds`/`removedUserIds` 快照，超 20 项记数量 + 前 20 项）
- [x] T038 [US3] 更新 `internal/api/router.go`：注册 **2 条 path / 5 个操作**（`GET`+`POST /clients/:id/roles`、`GET`+`PATCH`+`DELETE /clients/:id/roles/:rid`）
- [x] T039 [P] [US3] 更新 `web/admin/src/api/index.ts` 与 `types.ts`：角色 5 个函数 + `AppRole`/`RoleDetail` 类型
- [x] T040 [US3] 创建 `web/admin/src/views/client/ClientRoles.vue`：角色表（权限数/成员数）+ 新建/编辑弹窗 + 删除确认显示受影响数量 + 页面副标题"应用内角色仅在本应用生效，与平台角色（roles）无关"（FR-036，依赖 T039）
- [x] T041 [US3] 更新 `internal/service/app_auth_test.go`：角色名校验单测（大小写敏感、非法字符、1/32/33 长度边界、trim 行为）

---

## Phase 6: User Story 4 - 应用权限管理（P2）

**Goal**: 权限点 CRUD + 父子层级，树形与列表双视图同一份数据，删除有子节点被拒、被角色引用时提示并解除。

**Independent Test**: 建 `order:read` 与其子 `order:read:detail`；删父 → 409 并提示"1 个子权限"；把父级设为自身 → 400；树/列表切换条数与层级一致。

- [x] T042 [US4] 更新 `internal/store/app_auth_store.go`：权限 CRUD + 扁平列表（上限 1,000，`ORDER BY sort, created_at`）+ 子节点计数 + 引用角色计数；写操作带 `updated_at` 版本比对
- [x] T043 [US4] 更新 `internal/service/app_auth.go`：编码校验 `^[A-Za-z0-9_.:-]{1,64}$`、名称 1–64、描述 ≤255；父级成环检测（沿 `parent_id` 向上、步数上限 100，命中自身或后代 → `APP_PERMISSION_INVALID_PARENT`，跨应用同样拒绝）；删除有子节点 → `APP_PERMISSION_HAS_CHILDREN`（message 含子节点数量）；被引用时事务内解除引用后删除
- [x] T044 [US4] 更新 `internal/api/admin/app_auth.go`：4 个权限端点（列表、创建、修改、删除）+ swag 注解；审计 `app_permission_created`/`app_permission_updated`/`app_permission_deleted`（删除记 `removedRoleIds` 快照）
- [x] T045 [US4] 更新 `internal/api/router.go`：注册 **2 条 path / 4 个操作**（`GET`+`POST /clients/:id/permissions`、`PATCH`+`DELETE /clients/:id/permissions/:pid`）
- [x] T046 [P] [US4] 更新 `web/admin/src/api/index.ts` 与 `types.ts`：权限 4 个函数 + `AppPermission` 类型
- [x] T047 [US4] 创建 `web/admin/src/views/client/ClientPermissions.vue`：树/列表双视图切换（同一份数据，视图偏好存 `localStorage`，不入库）+ CRUD 表单 + 父级选择器禁用自身与后代 + 超过 3 层默认折叠（依赖 T046）
- [x] T048 [US4] 更新 `internal/service/app_auth_test.go`：编码校验（大小写、`_.:-` 字符集、1/64/65 长度）与成环判定单测（自身、直系后代、深层后代、无环）

---

## Phase 7: User Story 5 - 授权管理（P2）

**Goal**: 角色↔权限、角色↔成员两类覆盖式写入，支持批量勾选与计数展示，授权时自动补成员。

**Independent Test**: 给角色勾 3 个权限保存 → 计数 3，重复提交不变；把非成员 U 加入角色 → 提示"已同时加入 1 名成员"且成员列表出现 U；移除角色成员 → 该人仍在成员列表。

- [x] T049 [US5] 更新 `internal/store/app_auth_store.go`：两类覆盖式写入（事务内先算差集删除、再插入新增）+ 自动补成员（`INSERT IGNORE client_members`，`added_by=NULL`）+ 返回 `added`/`removed`/`autoJoinedMembers`
- [x] T050 [US5] 更新 `internal/service/app_auth.go`：`permissionIds` ≤1,000、`userIds` ≤5,000（**均不分片**，超限 → `VALIDATION_FAILED`）；每个 ID 必须属于同一应用；写入角色成员时校验 `users.status=1`，停用账号拒绝并计入 `skipped`（FR-014"不得分配新角色"）；`updatedAt` 与角色当前值比对，不符 → `APP_RESOURCE_STALE`
- [x] T051 [US5] 更新 `internal/api/admin/app_auth.go`：`PUT /clients/:id/roles/:rid/permissions` 与 `PUT /clients/:id/roles/:rid/members` + swag 注解 + 审计 `app_grant_updated`（`permissionsAdded/Removed`、`membersAdded/Removed/autoJoined`）
- [x] T052 [US5] 更新 `internal/api/router.go`：注册 2 条授权 path
- [x] T053 [P] [US5] 更新 `web/admin/src/api/index.ts` 与 `types.ts`：`putRolePermissions`/`putRoleMembers` + `GrantResult` 类型
- [x] T054 [US5] 创建 `web/admin/src/views/client/ClientGrants.vue`：角色搜索 + 每行显示"X 个权限 / Y 名成员" + 权限树勾选与成员勾选两个面板 + 提交后展示 added/removed/autoJoined 结果 + `409` 时提示"已被他人修改，请刷新后重试"并重新拉取（依赖 T053）
- [x] T055 [US5] 更新 `internal/service/app_auth_integration_test.go`：覆盖语义幂等、自动补成员、跨应用 ID 拒绝、停用账号被拒绝/计入 `skipped`、`updatedAt` 冲突返回 409

---

## Phase 8: User Story 6 - 用户信息接口返回应用内角色与权限（P2）

**Goal**: `GET /oauth2/userinfo` 按令牌所属应用返回 `app_roles` 与 `permissions`，空集返回 `[]`。

**Independent Test**: 员工在应用 B 有角色 `opsAdmin`（含 2 权限），用 B 的令牌调用 → 两字段正确；移出应用后立即再调用 → 均为 `[]`；用另一应用令牌调用 → 互不混淆。

- [x] T056 [US6] 更新 `internal/store/app_auth_store.go`：`Snapshot` 两条查询（角色名、去重权限编码，均按 `client_id` + `user_id` 过滤）；`InsertAccessToken`（主键插入）；限频清理（进程内时间戳，≥1 分钟执行一次 `DELETE ... WHERE expires_at < ? LIMIT 500`，失败仅 `slog.Warn`，不影响签发）
- [x] T057 [US6] 更新 `internal/service/app_auth.go`：`Snapshot(ctx, clientID, userID) (roles, permissions []string, err error)`，去重并集、`name`/`code` 升序、无数据返回空切片而非 `nil`
- [x] T058 [US6] 更新 `internal/oidc/storage.go`（`Storage` 已含 `appAuth` 字段，见 T009）：`CreateAccessToken` / `CreateAccessAndRefreshTokens` 写入 `(tokenID, subject, clientID, exp)`；插入失败 MUST 返回 error 且不签发令牌，并打 `slog.Error("access token mapping insert failed", "client_id", …, "sub", …)`；MUST NOT 降级为空数组
- [x] T059 [US6] 更新 `internal/oidc/claims.go`：`SetUserinfoFromToken` 用 `tokenID` 反查 `client_id` → `AppAuthService.Snapshot` → 填 `app_roles`/`permissions`（空集为 `[]`，不得为 `null` 或省略）；映射缺失或用户已非成员时打 `slog.Warn("userinfo app context miss", "token_id", …, "sub", …, "client_id", …)`；成员无绑定不打日志
- [x] T060 [P] [US6] 创建 `internal/oidc/claims_test.go`：用假 `AppAuthService` 覆盖三态——① 命中且有绑定（返回角色与权限）② 映射缺失 ③ 映射存在但非成员；②③ 返回空数组且不报错
- [x] T061 [US6] 更新 `internal/service/app_auth_integration_test.go`：移除成员后同一枚未过期令牌的 userinfo 立即返回空数组；两个角色权限有交集时并集去重
- [x] T062 [US6] 更新 `AGENTS.md` §4 的 userinfo 字段说明：并列给出 `roles`（平台角色）与 `app_roles`（应用内角色）的对比示例，并声明 `permissions` 与应用 `scopes` 无映射关系（与 [contracts/userinfo-claims.md](./contracts/userinfo-claims.md) 一致；受保护文件，提交带尾注）

---

## Phase 9: Polish & Cross-Cutting Concerns

- [x] T063 执行 `make docs` 重生成 `internal/api/docs/openapi.json`，并把 `internal/api/router_test.go`、`internal/api/docs/docs_test.go` 的计数断言上调至 **26 path / 36 操作**；确认 `TestDocsPathsMatchRouterRoutes`、`TestAnnotationErrorCodesAreRegistered`、`TestEmbeddedSpecStructure` 通过
- [x] T064 [P] 更新 `web/admin/src/views/Audit.vue`：`EVENT_LABEL` 增加 10 个应用事件的中文名，并确保按应用（`clientId`）与事件类型筛选可用（此文件被多个故事共用，故集中在此一次改完，避免并发冲突）
- [x] T065 执行全量门禁：`make build && make test && make lint && make build-web`（命令定义见 `Makefile`），全部通过
- [ ] T066 [P] 按 [quickstart.md](./quickstart.md) §1–§9 做端到端手工验收（含 userinfo 三段 curl）；实测偏离写入 commit message
- [x] T067 [P] 结构自查：`git grep -n "h\.st\." internal/api/admin/` 无新增命中（分层约束）；`migrations/` 只新增 `0003_app_auth.*` 两个文件；`git grep -n "0002_app_auth" specs/` 无残留（plan/data-model/quickstart 已统一为 `0003_app_auth`）
- [ ] T068 发布前检查（quickstart §10.1）：用 `hasMembers=false` 列出全部 0 成员启用应用并完成补员；与 `ListForUser` 成员制切换在**同一次发布**内完成；记录回滚口径为"先回退二进制，≥1 个发布周期后才考虑 `0003_app_auth.down.sql`"
- [ ] T069 协议一致性验收（宪法 + `AGENTS.md` §8.5，**不可跳过**）：`docker run --rm -p 8080:8080 -e CONFORMANCE_SERVER=http://host.docker.internal:8080 openidentity/conformance-suite`，UI 中选择 basic OP + config OP profile，全部用例通过方可发布；结果（镜像版本、通过数）写入 `specs/003-app-management-pages/tasks.md` 本任务与 commit message（US6 改动了 `/oauth2/userinfo` 的 claim 输出，属协议面）
- [ ] T070 [P] 性能验收（SC-002 / SC-004）：按 [quickstart.md](./quickstart.md) §11 在测试库造 10,000 用户 + 500 成员 + 200 权限节点，记录成员列表 / 候选 / `memberCount` 三个接口的响应时间（均 ≤1s），并对这三条 SQL 跑 `EXPLAIN` 确认无 `Using filesort` / `Using temporary`；权限树在浏览器切树/列表并记录耗时（≤1s）；结果写入 `specs/003-app-management-pages/tasks.md` 本任务与 commit message

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1（Setup）**：无依赖；T002 依赖 T001；T005/T006/T007 依赖 T004（先登记后使用）
- **Phase 2（Foundational）**：依赖 Phase 1 全部完成；T009 依赖 T008
- **Phase 3（US1）**：依赖 Phase 2；T010 → T011 → T012 → T013 为同一链路，T014/T015/T016 可并行，T017 依赖 T014，T018 依赖 T015，T019 依赖 T013
- **Phase 4（US2）**：依赖 Phase 2；T020–T023 同文件需顺序执行，T024 依赖 T020–T023，T025 可与 T020–T023 并行（不同文件），T026 依赖 T024，T027 依赖 T026，T028 依赖 T025，T029 起的前端任务依赖对应接口就绪，T033 依赖 T020–T025，T034 依赖 T025
- **Phase 5（US3）**：依赖 Phase 2；可与 US4 并行
- **Phase 6（US4）**：依赖 Phase 2；可与 US3 并行
- **Phase 7（US5）**：依赖 US3 + US4（要引用角色与权限）
- **Phase 8（US6）**：依赖 US3 + US4 + US5（快照读角色与权限）；T056 → T057 → T058 → T059，T060/T061 依赖 T059
- **Phase 9（Polish）**：依赖全部故事完成

### 批次与故事的关系（对齐 plan.md「交付批次」）

| 批次 | 范围 | 说明 |
|---|---|---|
| 批次 1（M1） | Phase 3 + Phase 4 | 子页面 + 详情 + 成员 + 门户可见性，可独立上线；交付后计数应为 **20 path / 25 操作**（16+4 / 19+6） |
| 批次 2（M2） | Phase 5 + 6 + 7 + 8 | 角色 + 权限 + 授权 + userinfo，兑现对外鉴权；完成后 **26 path / 36 操作** |

### 同一文件被多个任务修改的顺序（不可并行）

`internal/api/router.go`：T013 → T027 → T038 → T045 → T052
`internal/api/router_test.go`：T019 → T063（先按批次 1 的 20/25 断言，批次 2 再上调）
`AGENTS.md`：T004 → T062（受保护文件，两次提交都要带 `Protected-Change` / `Approved-By` 尾注）
`internal/api/admin/app_auth.go`：T026 → T037 → T044 → T051
`internal/store/app_auth_store.go`：T020 → T021 → T022 → T023 → T037(角色) → T042(权限) → T049(授权) → T056(快照)
`internal/service/app_auth.go`：T024 → T036 → T043 → T050 → T057
`internal/service/app_auth_test.go`：T041 → T048
`internal/service/app_auth_integration_test.go`：T033 → T055 → T061
`web/admin/src/api/index.ts` + `types.ts`：T014 → T029 → T039 → T046 → T053
`web/admin/src/views/Clients.vue`：T018 → T031

---

## Parallel Example: 批次 1（US1 + US2）

```bash
# 后端链路顺序执行（同文件）：
T010 client_store.go → T011 service/client.go → T012 admin/clients.go → T013 router.go

# 前端可并行（不同文件）：
Task: "T014 在 web/admin/src/api/index.ts 增加 updateClient"
Task: "T015 在 web/admin/src/router.ts 增加 /clients/:clientId/:tab 子路由"
Task: "T016 创建 web/admin/src/views/client/ClientDetail.vue"

# US2 的后端存储层完成后，前端与门户并行：
Task: "T029 在 web/admin/src/api/index.ts 增加成员接口"
Task: "T032 更新 web/portal/src/views/Apps.vue 空状态"
```

## Parallel Example: 批次 2（US3 / US4）

```bash
# US3 与 US4 由两人并行（不同文件，除 app_auth_store.go 需约定追加顺序）：
Developer A: T035 → T036 → T037 → T038 → T040
Developer B: T042 → T043 → T044 → T045 → T047
# 汇合后 US5 依赖两者，US6 依赖三者
```

---

## Implementation Strategy

### MVP First（批次 1）

1. 完成 Phase 1 + Phase 2（迁移、模型、登记、装配）
2. 完成 Phase 3（US1）→ 独立验收：URL 直达、刷新保持、编辑与校验
3. 完成 Phase 4（US2）→ 独立验收：可见性闭环 + 跨页全选（SC-005）+ 零成员筛选（SC-007）
4. **STOP and VALIDATE**：`make build && make test`，按 quickstart §1–§3、§7 验收后即可上线批次 1

### Incremental Delivery

1. 批次 1 上线（US1 + US2）→ 观察一周"员工看不到应有应用"工单（SC-008）
2. 批次 2 的 US3 → US4（可并行）→ US5 → US6，每完成一个故事跑一次对应独立验收
3. 全部完成后进入 Phase 9（docs 重生成、计数上调、全量门禁、quickstart 全跑）

### 提交节奏

按 `开发流程规范.md` 第 6 节：一个逻辑变更加对应任务勾选合成一个提交，conventional commits；`AGENTS.md` 改动需 `Protected-Change` + `Approved-By` 尾注。

---

## Notes

- [P] 任务 = 不同文件、无未完成依赖；同文件任务一律串行（见上文"同一文件被多个任务修改的顺序"）
- 迁移编号必须用 `0003_app_auth`（`0002` 已被 `0002_clients_meta` 占用）；plan / data-model / quickstart 已同步为 `0003_app_auth`
- `access_tokens` 写入失败即签发失败，不降级（plan Constraints）
- 所有写接口的 `updatedAt` 为可选参数，缺省不校验；前端 MUST 始终携带
- 增量成员接口上限 500（前端分片）；覆盖式授权接口上限 5,000/1,000 且不分片

---

## 验收记录

### 2026-10-21：前端应用子页面补齐（T014–T018 / T029–T032 / T039–T040 / T046–T047 / T053–T054 / T064）

**新增文件（`web/admin/src/`）**

| 文件 | 对应任务 | 要点 |
|---|---|---|
| `views/client/ClientDetail.vue` | T016 | 左侧固定五菜单（`el-menu router`）+ 右侧 `<router-view>`，头部展示名称/状态/成员数，"返回应用列表"入口；`provide` 供子页保存后刷新摘要 |
| `views/client/ClientOverview.vue` | T017 | `getClient` 进页取数（支持刷新直达）；只读字段 + 8 个可编辑字段；字段级校验（`http(s)` 绝对地址、1–10 条、512 长度、TTL 区间）；TTL 变更提示；停用二次确认；保存始终携带 `updatedAt`，`409` 提示"已被他人修改，请刷新后重试"并重载 |
| `views/client/ClientMembers.vue` | T030 | 筛选（关键字 + 部门含子部门）；候选弹窗只含非成员；按部门整批加入展示 新增/已存在/已停用；跨页全选（`all=true` 拉 ID 集合后本地反选）；移除确认显示人数与角色绑定数；`userIds` 按 500 分片顺序提交并汇总；无成员提示 |
| `views/client/ClientRoles.vue` | T040 | 角色表（权限数/成员数）+ 新建/编辑/删除；`^[A-Za-z0-9]{1,32}$` 与描述 ≤100；`409 APP_ROLE_NAME_EXISTS` 就地提示；删除确认显示两类绑定数量；副标题"应用内角色仅在本应用生效，与平台角色（roles）无关" |
| `views/client/ClientPermissions.vue` | T047 | 树/列表双视图（同一份扁平数据，视图偏好存 `localStorage`）；父级选择器禁用自身与后代；超过 3 层默认折叠；有子节点时删除被拒并提示数量；被引用时确认框提示角色数 |
| `views/client/detail-context.ts` | — | 子页保存后刷新外层摘要的注入键 |

**改动文件**

- `api/types.ts`、`api/index.ts`：`ClientDetail` / 成员 / 角色 / 权限 / 授权共 17 个操作的函数与类型；`ClientRow.memberCount`；`listClients` 支持 `hasMembers`
- `router.ts`：`/admin/clients/:clientId/{overview|members|roles|permissions|grants}` 子路由，非法 `tab` 回落 `overview`（T015）
- `views/Clients.vue`：详情改为跳转子页面（抽屉删除）、新增"成员数"列（0 人醒目提示）、"仅看无成员"开关（T018 / T031）
- `layouts/AdminLayout.vue`：子页面仍高亮"应用管理"
- `utils/format.ts`：`EVENT_LABEL` 增加 10 个应用事件中文名（T064）
- `Makefile`：`migrate-up` / `migrate-down` 改为按文件名顺序全量执行 `migrations/*.up.sql` / 逆序 `*.down.sql`（原先只跑 `0001`，与 quickstart §0 不符）

### 门禁与实测结果

| 项 | 命令 | 结果 |
|---|---|---|
| 类型检查 | `cd web/admin && npm run typecheck` | 通过 |
| 前端构建 | `make build-web NPM_INSTALL=true` | 通过（本机 `npm ci` 触发 npm 的 `SAFE_DELETE_BULK_CONFIRM_REQUIRED` 保护，按 Makefile 注释用 `NPM_INSTALL=true` 跳过安装步骤；锁文件未改动） |
| 后端构建 | `make build` | 通过 |
| 单测 | `go test ./...` | 全 `ok`（无 DSN 时集成测试跳过） |
| 集成测试 | `TEST_MYSQL_DSN=…/omino_sso_test go test ./internal/service/` | `ok`，7 个 `TestAppAuth*` 全 PASS |
| 静态检查 | `go vet ./...`、`golangci-lint run ./internal/...` | 通过 |
| 文档漂移 | `make docs-check` | 通过（26 path / 36 操作） |
| 结构自查（T067） | `git grep "h\.st\." internal/api/admin/`、`git grep "0002_app_auth" specs/` | 均无命中 |

**浏览器冒烟（`demo-app`，真实后端 + MySQL + Redis，playwright-cli）**

- 列表 → "查看详情"跳转 `/admin/clients/demo-app/overview`，无抽屉；五个菜单顺序与地址一致；刷新保持当前菜单；非法 tab 回落 overview
- 详情页：改名 + TTL 1800 保存成功且头部摘要即时同步；`ftp://x` 被字段级拦截、原值不变；TTL 变更提示出现；停用开关弹二次确认（取消后仍为启用）
- 成员页：候选列表不含已加入用户；添加 1 人 → "新增 1 名成员"；全选全部 3 人 → 反选 1 人剩 2；移除确认显示"将移除 N 名成员，并解除其在本应用的 X 项角色绑定"；移除后总数回到 2
- 角色页：列表显示权限数/成员数；重名 `opsAdmin` → "角色名已存在"；`ops-admin` →"只能由字母与数字组成"
- 权限页：新建子权限后父节点显示"子权限 1"；删除父节点被拒并提示数量；切列表视图父级列正确；刷新后视图偏好保持
- 授权页：角色计数"2 个权限 / 1 名成员"与勾选态一致；覆盖式保存显示"新增/移除/当前"；外部改版本后保存 → 提示"该角色已被他人修改，请刷新后重试"并自动重载；成员面板勾选、保存后计数与自动补员提示正确
- 审计页：新事件显示中文（更新应用 / 变更应用授权 等）

### 仍未完成

- **T066**：quickstart §1–§5 已按上述浏览器冒烟覆盖；§6–§9 的 curl/userinfo 三段只做过接口级验证，未按文档逐条留证
- **T068**：发布前补员流程与回滚口径尚未执行（需真实环境）
- **T069**：OIDC conformance-suite 未执行（需 Docker 与外部镜像，属发布前门禁）
- **T070**：`TestAppAuthPerfFixture`（10,000 用户造数、`EXPLAIN`、响应时间）未编写/未执行
