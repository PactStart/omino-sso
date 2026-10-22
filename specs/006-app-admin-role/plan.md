# Implementation Plan: 应用管理员标记与应用内管理入口

**Branch**: `006-app-admin-role` | **Date**: 2026-10-22 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/006-app-admin-role/spec.md`

## Summary

在应用成员关系上增加「应用管理员」标记（`client_members.is_admin`），由平台管理员在 admin 端成员列表中切换；门户侧新增 `/api/v1/me/apps/:id/*` 分组，用新的 `AppAdmin` 中间件按 `(clientId, 当前用户)` 实时判定，把**既有 admin 端的成员 / 角色 / 权限 / 授权能力原样开放**给该应用的应用管理员（复用同一组 handler 与 `AppAuthService`，不复制业务逻辑，满足 A7「单一实现来源」）。门户「我的应用」按 `/me/apps` 新增的 `isAppAdmin` 字段渲染「管理」按钮，管理界面是带独立地址的 5 个 tab 子页（应用信息 / 成员 / 角色 / 权限 / 授权）；应用信息参考 admin 详情，但可编辑字段缩减为名称 / 描述 / Logo / 回调地址，密钥与「设为/取消应用管理员」不开放（FR-005，口径调整见 D12）。

**分阶段交付**：

1. **数据与授权基线**：迁移 `0006`、`ClientMember.IsAdmin`、admin 端「设为/取消应用管理员」端点与审计、`/me/apps` 返回 `isAppAdmin`。
2. **门户接口**：`middleware.AppAdmin` + `/me/apps/:id/*` 路由挂载（薄 wrapper 转调 admin handler，见 D2）、只读组织树 `/me/orgs/tree`、swagger 注解与 `openapi.json` 再生。
3. **门户前端**：`Apps.vue` 管理按钮、`views/app-admin/` 五个子页与路由（独立地址、可收藏，默认落地 `overview`）、403 局部化处理。
4. **收口**：AGENTS.md 同步（§2.2 已完成；§3/§4.1/§5.3/§6.7/§6.9 随代码落地）、`check-registry.sh` 四向一致、门禁全绿。

**实施状态（2026-10-22）**：以上 4 个阶段均已落地并通过 `make build`（含 `docs-check`）/ `make test` / `make lint` / `make check`，管理页五页（应用信息 + 成员 / 角色 / 权限 / 授权）中，后四页由 admin 端组件移植，应用信息页为门户自有实现（EP 局部引入、按路由分 chunk）。浏览器视觉回归见 T044 / T059 记录。

## Technical Context

**Language/Version**: Go 1.26.0（后端）；TypeScript 5.7.2 + Vue 3.5.13（前端）

**Primary Dependencies**: 既有 `gin v1.10.0` / `fx v1.23.0` / `gorm v1.25.12` / `go-redis v9.7.0` / `ulid v2.1.0` / `validator v10.23.0`；**后端零新增依赖**。前端 portal 新增 `element-plus 2.9.1`（已确认并按 §2.0 登记 AGENTS §2.2，仅 `views/app-admin/` 懒加载引入，见 D9）

**Storage**: MySQL 8.4 —— `migrations/0006_app_admin_role.up/down.sql` 为 `client_members` 增加 `is_admin TINYINT(1) NOT NULL DEFAULT 0`；Redis 不新增任何键（**权限判定不做缓存**，理由见 D4）

**Testing**: `stretchr/testify` + 既有集成脚手架（`internal/service/org_integration_test.go` 的 `migrations/*.up.sql` 升序执行）；portal `vitest`（`src/**/*.spec.ts`，jsdom + `@vue/test-utils`）

**Target Platform**: Linux server（Docker Compose）+ 浏览器（portal / admin）

**Project Type**: web-service（`:8081` 管理 API）+ 两个 web 前端

**Performance Goals**: 授权判定为单条 `clients LEFT JOIN client_members` 主键查询，p95 增量 < 2ms；成员/角色/权限接口行为与 admin 端完全一致（分页默认 20、上限 100；候选页 ≤50；批量 ≤500；全选 ≤5000）

**Constraints**: 分层 `api → service → store`（api 不直连 store）；`AppAdmin` 必须在 `Recover → Trace → RateLimit → Auth` 之后（规范 §3）；门户接口不得挂 `Admin()`（A2/FR-003）；新增错误码登记 §5.3、新增审计事件登记 §6.7；结构变更只走手写 SQL

**Scale/Scope**: 单应用管理员数量无上限（A3/A9）；新增 1 列、1 个中间件、17 个门户端点、1 个 admin 端点、1 个只读组织树端点、2 个审计事件、4 个门户子页

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 说明 |
|------|------|------|
| I. 协议层单一来源 | N/A | 不触碰 `/oauth2/*` 与 `/.well-known/*`，不改 scope 语义与令牌签发 |
| II. 端口与信任边界物理分离 | PASS | 仅新增 `:8081` 管理 API 端点；`:8080` 协议面零改动 |
| III. 分层与数据一致性 | PASS | 标记读写落 `store/app_auth_store.go`，判定落 `service/app_auth.go`，HTTP 只做参数与响应；授权变更用单条 `UPDATE`，不做「读-改-写」 |
| IV. 凭证不出服务端 | PASS | 门户侧不返回 `client_secret` 或任何密钥；`redirect_uris` / TTL / 启停仅在应用详情接口中作**只读展示**（不可经门户修改），无新 token |
| V. 结构变更只走手写 SQL 迁移 | PASS | `0006_app_admin_role.up/down.sql` 成对提交，`TINYINT(1)` 布尔列，时间列不加 `TIMESTAMP` |
| 技术与接口约束 | PASS（有条件） | 须同步 AGENTS.md §3/§4.1/§5.3/§6.7/§6.9；新错误码 `APP_MEMBER_NOT_FOUND`；审计新增 2 个事件类型；`scripts/check-registry.sh` 四向一致 |

## 关键设计决策与取舍

**D1 授权载体：`client_members.is_admin` 布尔列，而不是「内置管理员角色」。**
`client_role_members` 无「角色类型」列，`client_roles` 也无 `is_system`，用角色表达会让管理员身份随角色改名/删除而丢失，且无法表达「成员维度标记」（FR-001）。布尔列随成员行删除天然满足 FR-008（移出即失去、重新加入默认 0）。

**D2 门户接口复用 admin 端 handler，不复制业务逻辑。**
`admin.Handler` 的成员/角色/权限方法只做「解析参数 → 调 `AppAuthService` → `failAppAuth` → 审计」，鉴权全在中间件，因此可原样挂在 `/me/apps/:id` 分组下（`c.Param("id")` 一致）。这样 FR-006「两端校验规则与错误提示一致」与 FR-014「审计口径一致」由结构保证，而不是靠两边同步维护。
落地方式：在 `internal/api/me/app_admin.go` 写 15 个**薄 wrapper**（各 3 行 + 独立 swag 注解，内部直接转调 `admin.Handler` 的同名方法），另有 1 条摘要与 4 条应用信息端点为本文件自有实现（见 D12）。加 wrapper 的唯一目的是让 `/docs` 能列出门户路径并标注「应用管理员」鉴权——swag 的路径来自注解，直接复用 admin 方法会把文档路径写成 `/admin/clients/:id/...`，与实际路由不符。业务逻辑仍然只有一份。
*被否方案*：把 handler 抽到共享包 —— 属目录/分层重构（AGENTS 附加禁止项），本特性无必要；不做 wrapper 直接挂载 —— 文档缺门户端点且鉴权说明错误。

**D3 门户侧拒绝语义统一为 `403 FORBIDDEN`。**
应用不存在 / 非成员 / 成员但非管理员 / 应用已停用（FR-015），HTTP 层一律 403 + 同一文案；区分信息只写应用日志（`reason`）与审计。满足 FR-010「响应不泄露该应用是否存在」。前端不靠 404/403 区分原因，统一走「无权限」UI。
*代价*：排障需看日志；已在中间件内置 `logger.Warn("app admin denied", "reason", ...)`。

**D4 权限判定不缓存，每请求实时查库。**
`Auth` 只注入 `session.UserID`，`AppAdmin` 每次用 `(clientID, userID)` 查一行，因此取消管理员后**下一次请求即失效**（FR-011 / SC-005）。禁止为 `is_admin` 加 Redis 或进程内缓存——那会直接违反「0 个旧权限残留」。

**D5 门户分组只挂 `Auth + AppAdmin`，不挂 `Admin()`。**
平台管理员不因平台身份获得门户侧权限（A2 / FR-003 / US4-AC7）；平台能力仍在 `/api/v1/admin/*`，行为不变（FR-013）。两者身份重叠时按应用管理员处理（同一中间件放行）。

**D6 授予/取消只在 admin 端：`PUT /api/v1/admin/clients/:id/members/:uid/admin`，body `{"isAdmin": true|false}`，幂等。**
重复设置同一状态返回 200 且不产生重复记录（FR-002）；目标不是成员 → `404 APP_MEMBER_NOT_FOUND`（复用已存在但未被使用的 `service.ErrAppMemberNotFound`）；门户分组下**不注册**该路径，直接请求落入 `NoRoute` → 404（FR-007）。

**D7 审计：新增 2 个事件，其余沿用既有事件类型。**
`app_member_admin_granted` / `app_member_admin_revoked`，`detail` 记 `{clientId, userId, isAdmin:{before,after}}`（FR-014 / SC-006）。门户侧管理操作走同一 handler，天然写同一批事件（`app_member_added` 等），无需新口径。

**D8 门户成员页的部门筛选：新增只读 `GET /api/v1/me/orgs/tree`（仅 session）。**
成员筛选与「按部门加入」依赖组织树，而 `/admin/orgs/tree` 挂 `Admin()` 会对应用管理员 403；门户侧复用 `OrgService` 的读路径暴露只读端点，不开放任何组织写操作。这是保持 US3 与 admin 端体验一致的最小改动。

**D9 门户管理界面：引入 `element-plus@2.9.1`（与 admin 同版本，按路由懒加载），已确认（2026-10-22）。**
admin 的成员/角色/权限/授权四页 100% 依赖 EP（`el-table` 跨页多选、`el-tree-select`、`el-tree`、`el-tabs`、`el-pagination`、`ElMessageBox`）。用户原始诉求为「复制代码过去」，且 FR-006 要求两端操作与提示一致——复制 EP 组件是成本最低、漂移最小的路径。
AGENTS §2.0 的三步已走完：① 提案见本节；② 用户 2026-10-22 明确确认「引入 element-plus@2.9.1，不重写」；③ 已 `npm install --save-exact element-plus@2.9.1` 落地（`package-lock.json` 仅新增 23 个包、零既有版本被动升级），并登记到 `AGENTS.md` §2.2（含使用范围与短板）。
**使用范围约束**：仅 `web/portal/src/views/app-admin/` 及其子组件 `import`（含 `element-plus/dist/index.css`），由 Vite 切成独立 chunk；**不得**在 `portal/src/main.ts` 全局注册 EP，登录页与其余门户页面首屏不得加载 EP。
**已知短板**：EP 全局样式与 Tailwind v4 preflight 存在互相覆盖风险 → 改动 app-admin 样式后须回归登录页 / 账号中心 / 我的应用。
*被否方案*：用 portal 原生 `<table>` + `ConfirmDialog` + `useToast` 重写四页（需自研分页器、多选表格、树选择；`ClientPermissions.vue` 的父级防成环与 `ClientMembers.vue` 的跨页全选是回归高发点）。

**D10 门户 403 处理局部化。**
`web/portal/src/main.ts` 现在把任何 `FORBIDDEN` 整页跳 `/403`，管理页里某次越权操作会导致整页丢失上下文。app-admin 路由以 `meta.handleForbidden: 'local'` 声明例外：页面首次加载 403 → 页内「无权限 + 返回我的应用」（FR-016）；操作期 403 → toast 提示并刷新当前 tab 数据。

**D11 管理页是独立地址的 5 级子路由。**
`/account/apps/:clientId/manage/overview|members|roles|permissions|grants`，壳组件 `AppAdminLayout.vue` 负责 tab 导航与应用摘要（`GET /me/apps/:id`），保证可收藏、刷新停留在原应用原 tab（FR-016）。默认落地页为 `overview`（应用信息）。该区域用 `max-w-7xl` 覆盖 `PortalLayout` 默认的 `max-w-5xl`（宽表需要）。

**D12 门户应用信息页「参考 admin、可编辑字段缩减」（2026-10-22 口径调整）。**
门户新增 `views/app-admin/Overview.vue`：字段与 admin `ClientOverview.vue` 一致（含回调/登出地址、授权方式、scope、TTL、状态、成员数），但只把名称 / 描述 / Logo / 回调地址做成输入控件，其余用只读描述列表呈现。服务端白名单落在 `ClientService.UpdateBasicInfo`（清空 `PostLogoutURIs`/`AccessTokenTTL`/`RefreshTokenTTL`/`Status` 后复用 `Update`），门户 handler 的请求结构体也只有这 4 个字段——前端不发、后端不认，双保险。admin 端保持 8 字段口径不变。

## Project Structure

### Documentation (this feature)

```text
specs/006-app-admin-role/
├── plan.md                          # 本文件
├── data-model.md                    # 迁移、模型、DTO 变更
├── contracts/
│   └── portal-app-admin-api.md      # 门户 + admin 新增端点契约与错误码增量
├── checklists/requirements.md       # 已完成（规格质检）
└── tasks.md                         # 后续 /speckit.tasks 产出
```

### Source Code (repository root)

```text
migrations/
├── 0006_app_admin_role.up.sql       # ALTER client_members ADD is_admin TINYINT(1) NOT NULL DEFAULT 0
└── 0006_app_admin_role.down.sql

internal/
├── store/
│   ├── model/app_auth.go            # ClientMember 增加 IsAdmin bool
│   └── app_auth_store.go            # 新增 GetClientMembership(单条 join)；SetMemberAdmin(幂等 UPDATE)；
│                                    # ListClientsForMember 返回带 is_admin 的行；删除成员时 is_admin 随行消失
├── service/
│   ├── client.go                    # AppItem 增加 IsAppAdmin（来源为成员行的 is_admin）
│   └── app_auth.go                  # 新增 SetMemberAdmin(含审计)、AuthorizeAppAdmin(判定+停用校验)；复用既有 CRUD
├── api/
│   ├── middleware/auth.go           # 新增 AppAdmin(appAuth, logger)：Auth 之后判定，403 统一语义
│   ├── router.go                    # 新增 /me/apps/:id 分组（挂 meH 的门户 wrapper）、/me/orgs/tree、
│   │                                # /admin/clients/:id/members/:uid/admin；me.NewHandler 增加 appAuth + adminH 依赖
│   ├── me/app_admin.go              # GET /me/apps/:id（管理页摘要：name/logo/status/memberCount，不含回调/密钥）
│   │                                # + 应用信息：GET /me/apps/:id/detail、PATCH /me/apps/:id（4 字段白名单）、
│   │                                #   POST|DELETE /me/apps/:id/logo
│   │                                # + 15 个薄 wrapper（3 行 + swag 注解，转调 admin.Handler 同名方法）
│   ├── me/organizations.go          # GET /me/orgs/tree（只读）
│   ├── admin/app_auth.go            # SetMemberAdmin handler + 成员列表返回 isAdmin + failAppAuth 补 APP_MEMBER_NOT_FOUND
│   └── dto/dto.go                   # AppItem.isAppAdmin、AppMemberItem.isAdmin、新增 AppManageContext
└── store/model/audit_event.go       # 追加 app_member_admin_granted / app_member_admin_revoked

web/
├── portal/src/
│   ├── api/app.ts                   # 新增：/me/apps/:id/* 全部封装（复用 client.ts 的 request/解包/错误映射）
│   ├── api/types.ts                 # AppItem.isAppAdmin；AppMemberItem / AppRoleItem / AppPermissionItem / AppManageContext
│   ├── views/Apps.vue               # 「管理」按钮插在「打开」左侧（仅 isAppAdmin === true）
│   ├── views/app-admin/
│   │   ├── AppAdminLayout.vue       # 摘要 + 5 tab 导航 + <router-view>；无权/停用 → 页内提示
│   │   ├── Overview.vue             # 应用信息：参考 admin 详情，仅名称/描述/Logo/回调地址可改（D12）
│   │   ├── Members.vue              # 由 admin/client/ClientMembers.vue 移植（部门树改 /me/orgs/tree）
│   │   ├── Roles.vue                # 由 admin/client/ClientRoles.vue 移植
│   │   ├── Permissions.vue          # 由 admin/client/ClientPermissions.vue 移植（localStorage key 换 portal: 前缀）
│   │   └── Grants.vue               # 由 admin/client/ClientGrants.vue 移植
│   ├── router.ts                    # /account/apps/:clientId/manage/* 子路由 + meta.handleForbidden
│   └── main.ts                      # FORBIDDEN 全局跳转尊重 meta.handleForbidden
└── admin/src/
    ├── api/index.ts                 # setAppMemberAdmin(clientId, userId, isAdmin)
    └── views/client/ClientMembers.vue  # 新增「身份」列 + 设为/取消管理员（取消最后一名时二次确认，不阻止）
```

**Structure Decision**: 严格沿用既有分层与目录。后端只在既定文件内增函数（不新建包、不搬目录）；门户前端新增 `views/app-admin/` 子目录与 `api/app.ts`，与 `views/Sessions.vue`、`api/index.ts` 的既有风格一致。

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| 门户侧新增 17 个端点（与 admin 重复路径） | FR-003/A2 要求门户只认应用管理员，而 admin 分组挂 `Admin()`；路径前缀决定中间件栈，无法复用同一路径 | 在 `Admin()` 内放开「应用管理员」会让平台管理员与应用管理员语义混叠，且违反 A2 |
| 门户引入 `element-plus`（D9，已确认并通过 §2.0 三步登记） | 复制的四页重度依赖 EP；重写会引入跨页全选/树选择/防成环的漂移风险 | 不引库重写：工作量最大且 FR-006「两端一致」难以保证 |
| `GET /me/orgs/tree`（只读组织树对全体登录用户开放） | 应用管理员需要部门筛选与「按部门加入」，`/admin/orgs/tree` 对其 403 | 门户成员页砍掉部门筛选：与 admin 端体验不一致，且按部门批量加入是高频操作 |
