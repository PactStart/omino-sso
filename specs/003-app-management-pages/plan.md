# Implementation Plan: 应用详情子页面与应用内授权体系

**Branch**: `003-app-management-pages` | **Date**: 2026-10-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/003-app-management-pages/spec.md`

## Summary

把管理后台"应用"的详情从抽屉改为独立子页面（左侧五菜单：应用详情 / 应用成员 / 应用角色 / 应用权限 / 授权管理），并为其提供完整的数据与接口支撑：应用成员制取代"所有启用应用对所有人可见"，新增应用内角色、权限点与两类绑定（角色↔权限、角色↔成员），最后让 `GET /oauth2/userinfo` 返回该用户在当前应用下的 `app_roles` 与 `permissions`。

技术路径：新增 6 张表（`client_members`、`client_roles`、`client_permissions`、`client_role_permissions`、`client_role_members`、`access_tokens`）与 1 个迁移对（`migrations/0003_app_auth.*`）；新增 `service.AppAuthService` 承载成员/角色/权限/绑定的全部规则（名称与编码校验、唯一性、权限树成环、级联解除、成员自动补齐），`store` 层只做 SQL；管理端新增 10 条 path / 17 个操作，全部注册在 `:8081` 的 `adminGroup`；`oidc` 侧仅在 `claims.go` 读取既有 service 取数（协议层零改动）。前端沿用 Element Plus，新增 5 个子视图与 1 个父级布局，不引入新依赖。

## Technical Context

**Language/Version**: Go `1.26.0`；Vue `3.5.13` + TypeScript `5.7.2`（`web/admin`）

**Primary Dependencies**:
- 既有：`gin v1.10.0`、`go.uber.org/fx v1.23.0`、`gorm v1.25.12` + `gorm.io/driver/mysql v1.5.7`、`oklog/ulid v2.1.0`、`go-playground/validator v10.23.0`、`zitadel/oidc v3.28.0`、Element Plus `2.9.1`
- **无版本升级、无新增库**。`github.com/go-sql-driver/mysql v1.7.0` 已在 002 登记为直接依赖（`AGENTS.md` §2.1），本次沿用

**Storage**: MySQL `8.4`，新增迁移 `migrations/0003_app_auth.up.sql` / `.down.sql`（6 张表，全部 `utf8mb4`；时间列为 `BIGINT` Unix 毫秒、以 `_at` 结尾；大小写敏感列显式 `COLLATE utf8mb4_0900_as_cs`）。Redis 不参与本特性。

**Testing**:
- `go test ./...` 既有基线
- 纯函数单测：角色名校验（`^[A-Za-z0-9]{1,32}$`）、权限编码校验（`^[A-Za-z0-9_.:-]{1,64}$`）、权限树成环判定、`app_roles`/`permissions` 并集去重
- 路由层：10 条新 path 无 `sid` cookie 时均 `401`；`r.Routes()` 精确断言；**`ops` 断言 19 → 36、`paths` 断言 16 → 26**（`router_test.go` 与 `docs/docs_test.go`）
- **`internal/oidc/claims_test.go`（新增）**：用假 `AppAuthService` 覆盖 userinfo 应用上下文的三种失效态——① 映射命中且有绑定（返回角色与权限）② `access_tokens` 无该 `tokenID`（映射缺失）③ 映射存在但用户已非成员——②③ MUST 返回空数组、不报错，且各打一条 `slog.Warn`（`E10` 的唯一可观测信号；消息固定为 `userinfo app context miss`，字段 `token_id`/`sub`/`client_id`）
- DB 行为集成测试（`TEST_MYSQL_DSN` 门控，新增 `app_auth_integration_test.go`）：批量添加幂等与计数、重复添加不重复插入、移除成员级联解除角色绑定、**移除成员后同一枚未过期令牌的 userinfo 立即返回空数组**、角色授权自动补成员、权限树删除保护、角色名/权限编码唯一冲突（区分大小写）、userinfo 快照的并集去重、门户可见性过滤、`GET /admin/clients` 的 `memberCount` 与 `hasMembers=false` 过滤、按部门整批加入的三类计数（新增/已存在/已停用）、`updatedAt` 版本冲突返回 `409`
- **既有测试的同步修订**（`E8`）：`ClientService.ListForUser` 由"全部启用应用"改为成员制后，检索所有断言"新用户可见 N 个应用"的用例，改为先建成员关系再断言；并新增一条"非成员看不到应用"的回归用例
- 文档一致性：`make docs` 重生成后 `TestDocsPathsMatchRouterRoutes`、`TestAnnotationErrorCodesAreRegistered`、`TestEmbeddedSpecStructure` 必须通过
- **性能验收**（SC-002 / SC-004）：按 [quickstart.md](./quickstart.md) §11 造 10,000 用户 / 500 成员 / 200 权限节点，成员列表、候选、`memberCount` 三个接口 ≤ 1s 且 `EXPLAIN` 无 `Using filesort`；权限树切树/列表 ≤ 1s
- 手工验收：[quickstart.md](./quickstart.md)（含浏览器路径与 curl 脚本）

**Target Platform**: Linux 服务端（容器）；管理后台为桌面浏览器 SPA

**Project Type**: 单体 Web 服务（Go 后端 + 两个 Vue 前端）；本特性触及 `:8081` 管理 API、`/oauth2/userinfo` 的 claims 填充与 `web/admin`

**Performance Goals**：应用有 500 名成员、候选池 10,000 用户时，成员列表与候选筛选 ≤ 1s（SC-002）；权限树 200 节点切换视图 ≤ 1s（SC-004）。

- 候选查询用 `NOT EXISTS` 反连接，排序键用 `u.username`（唯一索引，天然有序）而非 `created_at`（无索引，会触发文件排序）；`display_name` 的包含匹配保留，但 `pageSize ≤ 50` 且 `keyword` 长度 ≤ 64
- 成员/候选的 `all=true`（跨页全选）设上限 5,000：超过时返回 `400 VALIDATION_FAILED` 并提示"请先按部门或关键字缩小范围"，避免一次返回 600KB+ 的 ID 列表
- **增量**成员批量添加/移除的单次 `userIds` 上限 500；前端对"全选全部"自动按 500 分片顺序提交并汇总计数。授权接口 `PUT /roles/:rid/members` 属整体覆盖，**不做分片**（角色成员实际规模为数十到数百），上限 5,000 仅为防御边界

**Constraints**:
- 新端点只能注册在 `:8081` 的 `adminGroup`；`:8080` 的 `op.Provider` 路由与中间件栈零改动（宪法原则 II）
- 分层必须为 `api → service → store`；`internal/oidc` 只通过 `service` 取数，不写 SQL（宪法原则 III）
- 表结构只走手写 SQL 迁移，禁止 `AutoMigrate` 与代码内 DDL；时间列为 `BIGINT` 毫秒；大小写敏感列显式 `COLLATE utf8mb4_0900_as_cs`（宪法原则 V）
- 新错误码先登记 `AGENTS.md` §5.3、新审计事件类型先登记 §6.7，再落代码（宪法"技术与接口约束"）
- 用户信息接口为**纯新增字段**：既有 `sub/name/preferred_username/email/email_verified/picture` 与既有 `roles`（平台角色）语义不变
- 无软删除：成员、角色、权限、绑定的删除均为即时硬删除（spec Assumptions）
- 不做"按组织架构授权"，权限只经角色授予成员（spec FR-021 澄清结论）
- 回滚策略：**先回退二进制**——旧代码不读新表，成员/角色/权限配置完整保留；观察稳定 ≥1 个发布周期后，再决定是否执行 `0003_app_auth.down.sql`（逆序 `DROP TABLE`，属不可逆的数据删除）。回退二进制后 `ListForUser` 会恢复"全员可见"，属预期降级。**禁止**在回退二进制之前执行 down（会先丢配置）
- **`access_tokens` 写入失败即签发失败**：`CreateAccessToken` / `CreateAccessAndRefreshTokens` 中映射插入失败时 MUST 返回 error、不签发令牌，MUST NOT 静默降级为"userinfo 返回空数组"；理由：静默失败会让接入方误判"该用户无任何权限"，且只能靠外部反馈发现。签发路径本就读 `clients`，DB 依赖面不变
- **并发写用版本校验而非静默覆盖**：应用详情、角色、权限与两类授权写入 MUST 接受读取时的 `updatedAt`（可选参数，缺省=不校验，兼容脚本调用），与服务端当前值不一致时返回 `409 APP_RESOURCE_STALE`（spec FR-034、Edge Cases 的"该角色已被他人修改，请刷新"）

**Scale/Scope**: 10 条新 path / 17 个操作（总 26 path / 36 操作；`GET /admin/clients` 增加 `memberCount` 与 `hasMembers` 属纯新增字段、不增 path）、6 张新表、1 个新 service + 1 个新 handler 文件、1 个 `ClientService.Update`、1 处 `ListForUser` 语义变更、2 处 `claims.go` 取数、5 个新错误码、10 个新审计事件、5 个前端子视图 + 1 个布局 + 1 处列表页改造、1 个门控集成测试 + 1 个 `claims` 单测

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 / 条款 | 判定 | 依据 |
|---|---|---|
| I. 协议层单一来源 | **PASS** | 不新增 OIDC 实现；仅在 `op.Storage` 的 `SetUserinfoFromToken` 中读取业务数据（宪法允许"数据存取"）；不自研 userinfo 端点 |
| II. 端口与信任边界物理分离 | **PASS** | 17 个操作全在 `:8081` `adminGroup`；`:8080` 路由表与中间件栈零改动 |
| III. 分层与数据一致性 | **PASS** | 新规则集中在 `service.AppAuthService`；`store` 只写 SQL；批量添加/移除/授权变更全部在 `store.Transaction` 内，且以唯一索引 + `RowsAffected` 判定；不触碰 token/code 关键路径 |
| IV. 凭证不出服务端 | **PASS** | 成员/角色/权限无凭证；应用详情页对 `client_secret` 只读且不返回；审计 detail 仅写名称、编码、数量，唯一例外是地址类字段记录前后数组（非凭证） |
| V. 结构变更只走手写 SQL 迁移 | **PASS（有迁移）** | 新增 `0003_app_auth.up/down.sql` 成对提交；6 张表均为 `BIGINT` 毫秒时间列；`client_roles.name`、`client_permissions.code` 显式 `COLLATE utf8mb4_0900_as_cs` |
| `/api/v1/**` 统一响应 | **PASS** | 复用 `response.Success/Created/Fail`；批量移除返回 `data:{removed:n}`；移除无成员返回 `data:null` 语义由契约固定 |
| 错误码先登记后使用 | **PASS（需同步规范）** | 新增 5 个码，同批写入 `AGENTS.md` §5.3 与 `response/errors.go` |
| 审计事件类型先登记 | **PASS（需同步规范）** | 新增 10 个事件类型（应用更新 1、成员 2、角色 3、权限 3、授权 1），同批写入 §6.7 与 `model` 常量 |
| 接口文档一致性门禁 | **PASS（需重生成）** | 17 个操作补 swag 注解，`make docs` 重生成；`paths 16→26`、`ops 19→36` 断言同步 |
| 不重构既有结构 | **PASS（最小移动）** | `web/admin` 仅把 `Clients.vue` 的抽屉删除，新增同目录子视图文件；后端按 AGENTS.md §3 既定文件落位，不新建包 |
| 不新增规范外依赖 | **PASS** | 无新库、无版本升级；前端全部复用 Element Plus 既有组件 |

**Gate 结论**：无违规。设计阶段需显式记录的取舍是"新增 `access_tokens` 表以确定 userinfo 的应用上下文"与"授权写接口采用整体覆盖（PUT）而非增量 diff"（见 Complexity Tracking 与 [research.md](./research.md) R2、R7）。

### Post-Design Re-check（Phase 1 后复审）

设计产物（[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)）落地后逐条复核，结论**不变**，四点补充：

1. **原则 III 的边界**：`oidc.claims.go` 只调用 `AppAuthService.Snapshot(ctx, clientID, userID)`，不判断业务规则；"成员才可见/有角色必为成员"等不变量全部落在 service 的事务内。
2. **原则 IV 的细节**：`PATCH /admin/clients/:id` 不接受 `clientSecret` 字段；响应 DTO 不含 `client_secret_hash`；审计 detail 对应用更新一般只记字段名列表，**但地址类字段（`redirectUris` / `postLogoutUris`）必须记录变更前后的完整数组**。理由：这两项决定授权码会被送到哪个域，是 OAuth 里最敏感的可编辑值，"改过"对安全复盘没有价值，必须能看到改成了什么；它们本就存在于应用配置中，不属凭证。其余字段（名称、描述、Logo、TTL）仍只记字段名。
3. **原则 V 的落实**：`data-model.md` 给出 6 张表的完整 DDL 约束与索引；`down.sql` 逆序删除；集成测试按文件名升序执行 `migrations/` 下全部 `*.up.sql`（`0001`–`0003`）建库，禁止 `AutoMigrate`。
4. **文档门禁闭环**：`quickstart.md` 把 `make docs` / `make docs-check` 与三处计数断言（`router_test` 2 处、`docs_test` 1 处）列为验收步骤。

## 交付批次（Delivery Batches）

按价值分两批交付，每批自洽、可独立上线与回演（spec Assumptions 已固化该顺序）。

### 批次 1（M1）：子页面 + 详情 + 成员 —— 兑现门户可见性

- **后端**：`migrations/0003_app_auth.*` 一次提交全部 6 张表（迁移成对提交，避免同一特性拆两次 DDL 导致 down 口径分裂；本批只读写 `client_members`）、`GET /admin/clients/:id`（详情读取，子页面刷新直达）与 `PATCH /admin/clients/:id`（含 `updatedAt` 版本校验）、成员列表 / 候选 / 批量添加（支持 `userIds` 或 `deptId` 整批）/ 批量移除、`GET /admin/clients` 的 `memberCount` 与 `hasMembers` 过滤、`ClientService.ListForUser` 改为成员制、审计 `client_updated` / `app_member_added` / `app_member_removed`、错误码 `APP_RESOURCE_STALE`
- **前端**：`ClientDetail.vue` + `ClientOverview.vue` + `ClientMembers.vue`；`Clients.vue` 去掉抽屉、展示成员数并支持"仅看无成员"；门户"我的应用"空状态文案
- **验收**：SC-005、SC-007 达成；上线后一周按 SC-008 观察"员工看不到应有应用"工单，出现 ≥5 起即触发回滚评审（回退二进制即可，配置不丢）；`make docs` 计数断言按**本批实际注册量**提交（批次 2 再上调至 26 path / 36 操作）
- **上线前置条件（对应评审 X1）**：`ListForUser` 的语义切换与"成员数可见 + 按部门整批加入"必须在**同一次发布**内完成，且发布前用 `hasMembers=false` 列出全部 0 成员启用应用并完成补员（[quickstart.md](./quickstart.md) §10）。否则会出现"应用对所有员工消失、管理员却没有补救入口"的窗口。

### 批次 2（M2）：角色 + 权限 + 授权 + userinfo 字段 —— 兑现对外鉴权

- **后端**：角色 / 权限 CRUD（含树与成环校验）、两类覆盖式授权写入、`access_tokens` 写入与限频清理、`claims.go` 填充 `app_roles` / `permissions`；审计余下 6 类事件、错误码余下 4 个
- **前端**：`ClientRoles.vue`、`ClientPermissions.vue`、`ClientGrants.vue`（角色页副标题标注"仅在本应用生效，与平台角色无关"）
- **验收**：SC-001 完整闭环、SC-003（userinfo 与授权页 100% 一致）、SC-004 达成

> 两批共用一个版本校验码 `APP_RESOURCE_STALE`：批次 1 用于应用详情，批次 2 用于角色、权限与两类授权写入。

## Project Structure

### Documentation (this feature)

```text
specs/003-app-management-pages/
├── plan.md                          # 本文件
├── research.md                      # Phase 0：12 项技术决策与备选方案
├── data-model.md                    # Phase 1：6 张表、约束、不变量与错误映射
├── contracts/
│   ├── admin-app-auth-api.md        # Phase 1：17 个管理端操作与批量语义
│   └── userinfo-claims.md           # Phase 1：userinfo 新增字段与取值规则
├── quickstart.md                    # Phase 1：端到端验证（curl + 浏览器）
├── api-evolve/detected.json         # api-evolve 检测结果（已生成）
├── checklists/requirements.md       # /speckit.specify 产出
└── tasks.md                         # /speckit.tasks 生成，本命令不创建
```

### Source Code (repository root)

仅列本特性触及与新增的路径；未列出的保持不动。

```text
migrations/
├── 0003_app_auth.up.sql             # [新] 6 张表 + 索引 + 唯一约束
└── 0003_app_auth.down.sql           # [新] 逆序 DROP

internal/
├── api/
│   ├── router.go                    # [改] NewRouter 注入 appAuth；adminGroup 注册 10 条 path
│   ├── router_test.go               # [改] 新路由 401 断言；ops 19→36、paths 16→26
│   ├── docs/docs_test.go            # [改] ops 断言 19→36
│   ├── admin/
│   │   ├── clients.go               # [改] 新增 UpdateClient（PATCH，可编辑字段 + 校验）
│   │   ├── app_auth.go              # [新] 成员/候选/角色/权限/授权 handler
│   │   └── app_auth_test.go         # [新] 请求体解析（userIds 去重、空数组拒绝）单测
│   ├── dto/
│   │   └── dto.go                   # [改] AppMember/AppRole/AppPermission/Grant 相关 DTO
│   └── response/errors.go           # [改] 5 个新错误码（含 `APP_RESOURCE_STALE`）
├── service/
│   ├── app_auth.go                  # [新] AppAuthService：成员/角色/权限/绑定 + Snapshot
│   ├── app_auth_test.go             # [新] 校验器与并集去重单测
│   ├── app_auth_integration_test.go # [新] TEST_MYSQL_DSN 门控的 DB 行为测试
│   └── client.go                    # [改] Update（可编辑字段）、ListForUser 改为成员制
├── store/
│   ├── app_auth_store.go            # [新] 6 张表的读写（含批量与反连接查询）
│   ├── client_store.go              # [改] UpdateClientFields、ListClientsForMember
│   └── model/
│       ├── app_auth.go              # [新] ClientMember/ClientRole/ClientPermission/绑定/AccessToken
│       └── audit_event.go           # [改] 10 个事件常量
└── oidc/
    ├── storage.go                   # [改] Storage 增加 appAuth 字段（构造参数）；CreateAccessToken* 写 access_tokens 映射与限频清理
    ├── claims.go                    # [改] userinfo 填 app_roles/permissions（经 AppAuthService.Snapshot）；映射未命中打 Warn
    └── claims_test.go               # [新] 映射命中/映射缺失/非成员 三态用例

cmd/sso/
└── main.go                          # [改] fx 装配 AppAuthService；传入 NewRouter 与 oidc.NewStorage

web/admin/src/
├── router.ts                        # [改] /clients/:id/:tab 子路由
├── api/
│   ├── index.ts                     # [改] 17 个操作对应的客户端函数
│   └── types.ts                     # [改] 成员/角色/权限/授权类型
└── views/
    ├── Clients.vue                  # [改] 删除抽屉，详情改为 router.push；列表展示成员数 + "仅看无成员"筛选
    ├── client/
    │   ├── ClientDetail.vue         # [新] 左侧菜单 + 右侧内容 + 返回列表
    │   ├── ClientOverview.vue       # [新] 基本信息只读 + 可编辑字段表单 + 停用二次确认
    │   ├── ClientMembers.vue        # [新] 成员表 + 批量添加/移除 + 按部门整批加入 + 跨页全选
    │   ├── ClientRoles.vue          # [新] 角色 CRUD
    │   ├── ClientPermissions.vue    # [新] 权限树/列表双视图 + CRUD
    │   └── ClientGrants.vue         # [新] 角色↔权限/成员 授权
    └── Audit.vue                    # [改] EVENT_LABEL 增加 10 个应用事件

web/portal/src/views/Apps.vue         # [改] "我的应用"空状态文案与"联系管理员开通"引导

AGENTS.md                            # [改] §4 接口总览与字段约定（含 `roles` vs `app_roles`、`permissions` vs `scope`）、§5.3 错误码、§6 新表、§6.7 事件类型
internal/api/docs/openapi.json       # [改·生成物] make docs 重生成（26 path / 36 操作）
```

**Structure Decision**: 沿用既有单体与分层，不新建包。后端按 AGENTS.md §3 的既定落位：`api/admin/app_auth.go`、`service/app_auth.go`、`store/app_auth_store.go`、`store/model/app_auth.go`；前端在 `views/client/` 下新增子视图，属于 `views/` 的既有层级内新增目录，不改动其它页面结构。

## Complexity Tracking

| 取舍点 | 为什么需要 | 更简单的替代为何被否决 |
|---|---|---|
| 新增 `access_tokens` 表（`token_id → client_id`）以确定 userinfo 的应用上下文 | `op.Storage.SetUserinfoFromToken` 只拿到 `tokenID/subject/origin`，`origin` 是 `Origin` 请求头而非 clientID；`SetUserinfoFromRequest` 仅用于 id_token 生成，userinfo 端点不会调用。要按"令牌所属应用"返回角色权限，必须在签发时记录映射 | ① 把 clientID 编进 `jti`：依赖 token 形态（JWT/不透明）且把业务信息混入协议标识；② Redis 缓存映射：命中率受淘汰策略影响，与 SC-003"100% 一致"冲突；③ 解析/验签访问令牌：在 storage 内做协议校验，违反原则 I；④ 返回"全部应用合并"的角色：与 FR-028 直接冲突 |
| 授权写接口采用 `PUT` 整体覆盖（`permissionIds` / `userIds`）而非增量 diff | 前端是逐角色编辑的抽屉/面板，提交的是"最终态"，整体覆盖语义简单、幂等、与界面一致；并发覆盖可通过"读到的版本号"提示处理（spec Edge Cases） | 增量接口需要 `added/removed` 两组参数与两套幂等规则，前端还需自行 diff，测试面翻倍 |
| 成员移除后"级联解除角色绑定"放在同一事务 | 不级联会留下"有角色但非成员"的脏数据，直接违反 FR-011/FR-025 的不变量 | 后台补偿任务：新增定时组件且存在不一致窗口 |
| 唯一性改用数据库唯一索引（`client_id + name`、`client_id + code`）而非 002 的"全表加锁" | 本特性已引入迁移，具备加索引的条件；唯一索引把并发正确性交给数据库，避免全局串行化影响 17 个操作的吞吐 | 复用 002 的全表 `FOR UPDATE`：需要把 5 张表都纳入同一把锁，写吞吐与死锁面显著变差，且 002 的做法是"无迁移"条件下的妥协 |
| 权限树只存 `parent_id`，不存物化路径 | 权限点规模为百级、只按应用内查询，构建树与成环检测在 service 内做（向上走父链 + 步数上限），无需路径列与批量搬迁 | 复用组织的 `path`：权限需要移动操作的频率远低于组织，路径维护与其一致性成本不划算 |
| 候选成员查询用 `NOT EXISTS` 反连接而非先取成员 ID 列表 | 10,000 候选 × 500 成员的 `IN` 列表会超出合理参数规模；反连接可在库内走索引完成分页 | 应用层差集：需要把全部成员读进内存，随规模线性劣化 |

**后续工作（本次不做）**：① `access_tokens` 的独立清理任务（当前在签发入口按 `expires_at` 限频批量删除，见 research R12）；② 权限点的批量导入/导出；③ 应用级角色在 id_token 中的投影（当前仅 userinfo，见 research R6 的备选记录）；④ 用户物理删除路径——本系统"删除用户 = 置 `status=0`"，成员与角色绑定随之保留；若未来引入物理删除，删除路径 MUST 在同一事务内清理 `client_members` 与 `client_role_members`（spec FR-014）；⑤ userinfo 上下文未命中的指标化（当前仅有 `slog.Warn`，接入 Prometheus 后补 `userinfo_ctx_miss_total`）。
