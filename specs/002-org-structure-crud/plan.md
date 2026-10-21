# Implementation Plan: 管理后台组织架构增删改

**Branch**: `002-org-structure-crud` | **Date**: 2026-10-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/002-org-structure-crud/spec.md`

## Summary

为管理 API 增加组织节点的创建、修改、删除能力（`POST /api/v1/admin/orgs`、`PATCH /api/v1/admin/orgs/:id`、`DELETE /api/v1/admin/orgs/:id`），并在管理后台"用户管理"页的组织树面板提供五个入口：新增根部门、新增子部门、重命名、移动到…、删除。既有 `GET /api/v1/admin/orgs/tree` 的报文结构保持不变。

技术路径：新增 `service.OrgService` 承载全部业务规则（名称规范化与唯一性、10 级深度、成环检测、移动时的子树路径级联、删除前置校验），store 层只做 SQL；写操作统一走 `store.Transaction` 的 tx-in-context，并在事务开头对 `organizations` 加一次全表 `SELECT ... FOR UPDATE`（该表无 `tenant_id` 索引，实际效果是**全局串行化**组织写操作），从而使"唯一性、深度、成环、删除保护"四类约束无需依赖数据库唯一索引即可成立（不新增迁移）。账号归属写入侧（创建用户、改用户部门）对目标部门行加锁，与部门删除共用同一临界区。前端复用 Element Plus 现有组件，不引入新依赖。

## Technical Context

**Language/Version**: Go `1.26.0`（`go.mod`）；前端 Vue `3.5.13` + TypeScript `5.7.2`（`web/admin`）

**Primary Dependencies**:
- 既有：`gin v1.10.0`（管理 API）、`go.uber.org/fx v1.23.0`（装配）、`gorm.io/gorm v1.25.12` + `gorm.io/driver/mysql v1.5.7`、`oklog/ulid v2.1.0`（主键）、`go-playground/validator v10.23.0`（请求绑定）、Element Plus `2.9.1`（admin 前端）
- **无版本升级、无新增库**：`github.com/go-sql-driver/mysql v1.7.0` 原为间接依赖（由 `gorm.io/driver/mysql` 带入），本次因需识别 `1213`/`1205` 转为**直接依赖**，版本不变，已登记 `AGENTS.md` §2.1。并发控制只用 MySQL 行锁；不引入 `sqlmock` / `testcontainers` 之类测试依赖（会违反 §2.1 版本锁定表与"不新增规范外依赖"）

**Storage**: MySQL `8.4`，复用既有表 `organizations`（`migrations/0001_init.up.sql`）。**无迁移、无 AutoMigrate、无新索引**。注意该表排序规则为 `utf8mb4_0900_ai_ci`（大小写不敏感），而 spec 要求同级名称区分大小写，因此唯一性查询必须显式用 `BINARY name = ?` 比较，不能依赖列默认排序规则。Redis 不参与本特性。

**Testing**:
- `go test ./...` 现有基线
- 新增纯函数单测：名称规范化/校验、深度计算、路径拼接与前缀判定（成环与子树判定）、`optionalString` 的 JSON 三态语义（缺失 / `null` / 值）
- 路由层：三个新路由在未携带 `sid` cookie 时返回 `401 UNAUTHENTICATED` 且报文符合统一响应；`r.Routes()` 断言 4 条组织路由（`GET /admin/orgs/tree`、`POST /admin/orgs`、`PATCH /admin/orgs/:id`、`DELETE /admin/orgs/:id`）的 method 与 path 精确匹配，固化"静态段与参数段共存不冲突"（不触碰 DB，因为 Auth 中间件在无 cookie 时先返回 401）
- **既有 ops 断言同步**：`internal/api/router_test.go:104` 与 `internal/api/docs/docs_test.go:269-270` 各有一处硬编码 `ops != 16`。新增 3 个端点后操作数由 **16 → 19**，两处断言必须同步改为 19，否则 `make test` / `make build` 必红
- 文档一致性：`make docs` 重新生成 `openapi.json` 后（14 条 path → **16** 条、16 个操作 → **19** 个；3 个新端点只新增 2 条 path，即 `/admin/orgs` 与 `/admin/orgs/{id}`），`TestDocsPathsMatchRouterRoutes`（AC-1）、`TestAnnotationErrorCodesAreRegistered`（新错误码必须已登记）、`TestEmbeddedSpecStructure`（每个 op 必须有 summary/tags/500/`security: cookieAuth`）必须通过
- **DB 行为集成测试（新增，零新依赖）**：`internal/service/org_integration_test.go`，由 `TEST_MYSQL_DSN` 门控（为空则 `t.Skip`，默认 `make test` 不依赖 MySQL）。覆盖 9 类核心行为（对应 13 个测试函数，`-run 'TestOrg'` 命中 12 个，另 1 个为 `TestUpdateUserRejectsMissingDept`）：同级同名、大小写敏感、成环、10 级成功 / 第 11 级拒绝（含移动使子树超限）、移动级联与用户归属不变、删除两类保护、并发同名创建、并发"删部门 + 建用户"、**空表并发创建（验证 1213 重试后一方成功、一方 409）**。**建库建表方式**：测试用不含库名的 DSN 连接执行 `CREATE DATABASE IF NOT EXISTS`，再执行 `migrations/0001_init.up.sql`；每个用例前 `TRUNCATE organizations / users / audit_events`（迁移只有 `CREATE TABLE IF NOT EXISTS`、无 DROP，不清理会残留上次数据）；**禁止 `AutoMigrate`**（`migrations/README.md` 禁令）
- 手工验证：[quickstart.md](./quickstart.md) 保留为端到端验收脚本（含浏览器路径），与集成测试互补

**Target Platform**: Linux 服务端（容器）；管理后台为浏览器端 SPA

**Project Type**: 单体 Web 服务（Go 后端 + 两个 Vue 前端），本特性触及 `:8081` 管理 API 与 `web/admin`

**Performance Goals**: 组织规模 500 节点、单节点 50 直接子部门时，树查询与单次写操作均 ≤ 1s（SC-004）。写操作全局串行：事务内锁定 `organizations` 全部行（≤ 500）与目标部门的账号行，单次持锁毫秒级；读路径走 MVCC 不加锁，不受影响

**Constraints**:
- 只能在 `:8081` 的 Gin 上新增路由，`:8080` 的 `op.Provider` 零改动（宪法原则 II）
- 分层必须为 `api → service → store`，handler 不得直接写 SQL（宪法原则 III、AGENTS.md §3）；**读路径同样经 service**：组织树读取走 `OrgService.Tree`，删除前取审计用名称走 `OrgService.Name`，`internal/api/admin/` 内不得出现 `h.st.*` 调用
- 不改表结构、不加索引、不引入唯一约束（宪法原则 V；spec 澄清"不新增迁移"）
- 所有新错误码先登记 `AGENTS.md` §5.3，新审计事件类型先登记 §6.7（宪法"技术与接口约束"）
- 租户不可由客户端指定：根级固定 `default`，子节点继承父节点（spec Clarifications 2026-10-21）
- 修改接口的 `parentId` 必须区分"未提交"与"显式 null"（前者不变、后者移到根级），普通 `*string` 无法表达
- 层级路径长度（512）由 10 级上限自然满足（最长约 271 字符），实现中以断言校验，不新增面向用户的错误分支
- 回滚策略：回退二进制即可；`organizations` 结构未变，新增数据对旧版本只读兼容，无需数据修复
- **对既有端点的行为修正**：`PATCH /api/v1/admin/users/:id` 的 `deptId` 过去不校验存在性、静默写入；本次为消除 `dept_id` 悬空竞态，改为在短事务内锁定目标部门并校验存在（不存在 → `VALIDATION_FAILED`），属有意的行为修正，需同步到 `AGENTS.md` §4.1 该行说明
- **死锁兜底**：组织写事务在 service 层对 MySQL `1213`（死锁）与 `1205`（锁等待超时）**重试一次**，覆盖"空表并发创建时 supremum 间隙锁互相兼容导致的插入死锁"；重试对调用方不可见，重试后仍失败才返回 500

**Scale/Scope**: 3 个新端点、1 个新 service、1 个新 handler 文件（并把 `OrgTree` 从 `users.go` 迁入同包新文件）、装配链 4 处改动（`NewRouter` 签名、`admin.NewHandler`/`Handler`、`cmd/sso/main.go`、`newTestRouter`）、`organization_store.go` 扩展 9 个方法、`user.go` 短事务加锁 + dept 校验、4 个新错误码、3 个新审计事件类型、1 个前端页面改造 + 1 个审计标签表 + 3 个 API 客户端函数、1 个 DSN 门控集成测试、2 处 `ops` 断言由 16 改为 19

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 / 条款 | 判定 | 依据 |
|---|---|---|
| I. 协议层单一来源 | **PASS** | 不触碰 `internal/oidc/*`、不新增 OIDC 相关代码 |
| II. 端口与信任边界物理分离 | **PASS** | 三个路由全部注册在 `internal/api/router.go` 的 `adminGroup`（`:8081`）；`:8080` 的 `net/http` mux 零改动 |
| III. 分层与数据一致性 | **PASS** | `admin.Handler` 只做绑定/映射/审计；业务规则在 `service.OrgService`；SQL 在 `store`。写操作统一 `store.Transaction`，子树路径用单条 `UPDATE ... CONCAT/SUBSTRING` 完成，不做逐行"读-改-写"；不涉及 token/code/session 关键路径 |
| IV. 凭证不出服务端 | **PASS** | 组织数据无凭证；审计 detail 只写名称/父节点等非敏感字段；响应与日志不含 token/密码 |
| V. 结构变更只走手写 SQL 迁移 | **PASS（空操作）** | `organizations` 表字段与索引已满足需求（`parent_id`/`path`/`sort`/`tenant_id`），`migrations/` 无新增文件；唯一性用事务内校验而非唯一索引 |
| `/api/v1/**` 统一响应 | **PASS** | 复用 `response.Success/Created/Fail`；删除返回 `data:null`；`traceId` 由既有中间件注入 |
| 错误码先登记后使用 | **PASS（需同步规范）** | 新增 4 个码，随本特性同批写入 `AGENTS.md` §5.3 与 `response/errors.go` |
| 审计事件类型先登记 | **PASS（需同步规范）** | 新增 `org_created` / `org_updated` / `org_deleted`，同批写入 §6.7 与 `model` 常量 |
| 接口文档一致性门禁 | **PASS（需重生成）** | 三个端点补 swag 注解并 `make docs` 重生成 `openapi.json`；`make build` 依赖 `docs-check`，漂移即失败 |
| 不重构既有结构 | **PASS（最小移动）** | 仅把已属于组织域的 `OrgNode`/`buildTree`/`OrgTree` 从 `admin/users.go` 移入同包 `admin/orgs.go`（AGENTS.md §3 已规划该文件），不改包边界、不改行为 |
| 不新增规范外依赖 | **PASS** | 无版本升级、无新增库；`go-sql-driver/mysql` 由间接依赖转为直接依赖（`v1.7.0` 不变），已登记 §2.1 |

**Gate 结论**：无违规，无需要豁免的复杂度。设计阶段需要显式记录的取舍是"组织写操作全局串行化的行锁"与"移动只通过选择上级而非拖拽"（见 Complexity Tracking 与 [research.md](./research.md) R2）。

### Post-Design Re-check（Phase 1 后复审）

设计产物（[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/admin-orgs-api.md](./contracts/admin-orgs-api.md)、[quickstart.md](./quickstart.md)）落地后逐条复核，结论**不变**，四点补充：

1. **原则 III 强化**：`contracts/admin-orgs-api.md` 明确"锁 → 读 → 校验 → 写"全部在同一事务内，且删除/创建/移动共用同一把 `organizations` 全表行锁（全局串行），避免"校验通过后被并发删除"的窗口；账号归属写入侧对目标部门行加锁，与删除互斥，消除 `dept_id` 悬空；`store` 层新增方法均接收 `ctx` 并走 `s.DB(ctx)`。
2. **原则 V 无触发**：`data-model.md` 确认所需字段全部已存在；`path` 采用与种子数据一致的 ID 型物化路径（`/26位ID/26位ID/`），不改变列宽与索引。
3. **文档门禁闭环**：`quickstart.md` 把 `make docs` / `make docs-check` 与 `TestDocsPathsMatchRouterRoutes` 列为验收步骤，确保新增端点不会与内嵌 `openapi.json` 脱节。
4. **审计可见性闭环**：`web/admin/src/utils/format.ts` 的 `EVENT_LABEL` 与 Go 侧事件常量必须同批更新，否则审计页无法展示/筛选组织事件（FR-014）。

## Project Structure

### Documentation (this feature)

```text
specs/002-org-structure-crud/
├── plan.md                          # 本文件
├── research.md                      # Phase 0：12 项技术决策与备选方案
├── data-model.md                    # Phase 1：实体、约束、不变量、状态与错误映射
├── contracts/
│   └── admin-orgs-api.md            # Phase 1：三个端点的报文契约与并发语义
├── quickstart.md                    # Phase 1：端到端验证步骤（curl + 浏览器）
├── checklists/
│   └── requirements.md              # /speckit.specify + /speckit.clarify 产出
└── tasks.md                         # Phase 2 输出（/speckit.tasks 生成，本命令不创建）
```

### Source Code (repository root)

仅列本特性触及与新增的路径；未列出的保持不动。

```text
internal/
├── api/
│   ├── router.go                    # [改] NewRouter 签名加 orgs；admin.NewHandler 调用；adminGroup 注册 3 条写路由
│   ├── router_test.go               # [改] newTestRouter 构造 OrgService；4 条组织路由断言；ops 16 → 19
│   ├── docs/
│   │   └── docs_test.go             # [改] TestEmbeddedSpecStructure 的 ops 断言 16 → 19
│   ├── admin/
│   │   ├── orgs.go                  # [新] OrgNode/OrgTree（自 users.go 迁入）+ Create/Update/Delete + optionalString
│   │   ├── orgs_test.go             # [新] 请求三态语义（optionalString：缺失 / null / 值）单测
│   │   └── users.go                 # [改] Handler 结构体/NewHandler 增加 orgs 字段；移出 OrgNode/buildTree/OrgTree（行为不变）
│   ├── dto/
│   │   └── dto.go                   # [改] 新增 dto.OrgDetail + FromOrganization
│   └── response/
│       └── errors.go                # [改] 新增 4 个错误码
├── service/
│   ├── org.go                       # [新] OrgService：Create/Update/Delete + Tree/Name（只读，供 handler 使用）+ 纯函数校验/路径工具
│   ├── org_test.go                  # [新] 名称、深度、路径、成环、子树高度单测
│   ├── org_integration_test.go      # [新] TEST_MYSQL_DSN 门控的 DB 行为集成测试（13 用例，`-run 'TestOrg'` 命中 12 个）
│   ├── audit.go                     # [改] Event.Fields：结构化变更摘要序列化为 detail 列（FR-014）；新增 List 读方法（T040）
│   └── user.go                      # [改] CreateUser/UpdateUser 改 deptId 时：短事务内锁部门 + 校验存在；新增 List 读方法（T040）
└── store/
    ├── organization_store.go        # [改] 全表锁/按名查/子节点/账号计数/插入/更新/子树路径/删除/子树人数
    └── model/
        ├── organization.go          # [改] 增加 path 深度与路径拼接辅助（纯函数）
        └── audit_event.go          # [改] 新增 3 个事件常量

cmd/sso/
└── main.go                          # [改] NewRouter 调用传入 OrgService（:122）

web/admin/src/
├── api/
│   ├── index.ts                     # [改] createOrg / updateOrg / deleteOrg
│   └── types.ts                     # [改] CreateOrgPayload / UpdateOrgPayload / OrgDetail
├── utils/
│   └── format.ts                    # [改] EVENT_LABEL 增加 org_created / org_updated / org_deleted
└── views/
    ├── Audit.vue                    # [改] detail 为 JSON 文本：解析后按键值/代码块渲染，修复按字符迭代（FR-014）
    └── Users.vue                    # [改] 五个入口 + 弹窗/确认 + 移动候选过滤 + 深度由 path 推导 + 树折叠与过滤

AGENTS.md                            # [改] §4.1 总览、§4.6 字段行、§5.3 错误码、§6.7 事件类型
internal/api/docs/openapi.json       # [改·生成物] make docs 重生成（16 条 path / 19 个操作）
```

**Structure Decision**: 沿用既有单体结构（Go 单模块 + `web/admin` 独立前端工程）。组织域代码按 AGENTS.md §3 的既定规划落在 `internal/api/admin/orgs.go`、`internal/service/org.go`、`internal/store/organization_store.go`，不新建包、不改目录层级。

## Complexity Tracking

| 取舍点 | 为什么需要 | 更简单的替代为何被否决 |
|---|---|---|
| 写操作在事务开头锁定 `organizations` **全表**（`SELECT ... FOR UPDATE`），实际效果是组织写全局串行 | 同级唯一性、10 级深度、成环、删除保护四类约束都没有数据库唯一索引兜底；集中一把锁后可把"读-校验-写"放进同一临界区，彻底消除并发窗口。该表无 `tenant_id` 索引，`WHERE tenant_id = ? FOR UPDATE` 会全表扫描并锁全表，因此不再宣称"跨租户不阻塞" | ① 只锁父行：根级节点无父行可锁，同名根节点并发创建可双写；② 依赖 RR 间隙锁：间隙锁彼此兼容，两个事务仍可同时通过存在性检查；③ `GET_LOCK` 命名锁：新增一套并发机制与连接绑定语义，团队无先例；④ 加唯一索引/生成列或 `idx_orgs_tenant`：需迁移，违反本次"不新增迁移"的澄清 |
| 移动通过"选择新上级"弹窗实现，不做树拖拽 | 拖拽需要处理放置目标、深度提示与误操作回滚，交互与测试成本高；选择弹窗能完整表达 `parentId` 三态（未提交/根级/指定节点） | 纯 API 无界面会让管理员仍需研发代劳（评审 P1）；拖拽实现则与"不做拖拽排序"的澄清冲突 |
| 不引入 `testcontainers` / `sqlmock`，改用 `TEST_MYSQL_DSN` 门控的集成测试 | 两个测试库都属规范外新依赖，需先改 AGENTS.md §2 并评估版本连锁；现有 gorm 驱动已能完成建表与断言 | 只靠 quickstart 手工验证会让唯一性/级联/成环/删除保护失去回归保护（评审 E10）；默认 `make test` 仍不依赖 MySQL |
| 修复 `dept_id` 竞态时顺带给 `UpdateUser` 补部门存在性校验 | `UpdateUser` 现状是把 `deptId` 直接塞进 `UPDATE`，不加校验时锁也挡不住写入不存在的部门；不补校验则本次修复无法成立 | 改用"删除时锁定该部门用户区间"（依赖 RR 间隙锁语义、隐蔽且不改 user API）；或维持现状并在 `users.dept_id` 上加真实外键（需迁移） |

**后续工作（本次不做）**：① 若未来需要真正的租户级写隔离，新增 `idx_orgs_tenant` 迁移并把锁语句改为按租户过滤；② 组织写操作指标 `org_write_total{op,code}`；③ 审计事件类型字典由接口下发，消除 Go 常量与前端 `EVENT_LABEL` 的双事实源。
