# Implementation Plan: 应用访问按组织架构授权

**Branch**: `010-app-access-org-grant` | **Date**: 2026-11-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/010-app-access-org-grant/spec.md`

## Summary

在 `clients` 与 `organizations` 之间新增一条「组织授权」关系（应用 × 部门 + 是否含子部门），把门户可见性从「只认 `client_members`」扩展为「手动成员 ∪ 组织授权覆盖」：判定按用户**当前** `dept_id` 与部门物化路径前缀实时计算，因此调岗、入离职、部门迁移自动生效，不需要同步任务也不需要人工补员。管理侧在 admin 与门户应用管理员两端提供同一套「组织授权」能力（列表含覆盖人数与撤销影响人数、新增、撤销、含子部门开关），撤销只解除授权、不触碰手动成员与角色绑定；新增/调整/撤销各写一条审计事件；`organizations` 删除前增加「被组织授权引用」的前置校验。OIDC 协议层（`:8080`）零改动，`app_roles` / `permissions` 仍按成员身份计算。

**定位（critique P3/X1，2026-11-01 用户确认方案 A）**：本特性是**门户可见性与入口收敛**，不是访问控制。现状 `internal/oidc/claims.go:55` 的 `IsClientMember` 只用于 userinfo 计字段、`internal/oidc/storage.go:76` 只校验账号 `status`，授权端点从不校验成员关系；`RemoveMembers`（`internal/store/app_auth_store.go`）也不吊销会话与令牌。因此：撤销组织授权**不会**让已登录用户掉线，也**不会**阻止其直接访问接入方；离职/越权处置仍以停用账号为准。页面文案与部署文档必须写明该边界（spec FR-029）。真正的 authorize 拦截 + 令牌吊销属独立特性，本期不做、也不预留开关。

**上线影响面（critique E12）**：未配置任何组织授权时，门户可见性与现状**逐字节一致**（新表为空 ⇒ `EXISTS` 恒假）。全部改动对既有 path 都是纯增量：6 个新 operation + 成员/候选列表新增字段；唯一语义变化发生在管理员**主动**配置授权之后。故本次不需要灰度或开关，按常规版本发布即可。

## Technical Context

**Language/Version**: Go `1.26.0`（`go.mod`）；前端 Vue `3.5.13` + TypeScript `5.7.2`（`strict`）

**Primary Dependencies**: `gin v1.10.0`、`go.uber.org/fx`、`gorm v1.25.12`、`zitadel/oidc/v3 v3.28.0`（本特性**不改**协议层）、`redis/go-redis/v9`；前端 `element-plus 2.9.1`（admin 与 portal 均已引入）、`vue-router 4.5.0`。**本特性不新增任何第三方依赖**

**Storage**: MySQL `8.4`，新增手写迁移 `migrations/0007_client_org_grant.{up,down}.sql`；时间列 `BIGINT` 毫秒；不使用 `AutoMigrate`

**Testing**: `make test`（Go 单测 + 前端 vitest）、`make test-integration`（MySQL 真库，`package service_test`）、`make lint`、`make docs-check`

**Target Platform**: Linux 容器；`:8081` Gin 管理 API + 两个静态前端（portal / admin）

**Project Type**: web-service + 双 Vue 前端（`web/admin`、`web/portal`）

**Performance Goals**: SC-005 —— 单部门子树含 2000 名在职员工时，保存一次授权 ≤ 3s（p95），组织授权列表（含覆盖人数）≤ 2s；`GET /api/v1/me/apps` 单用户判定维持单条 SQL，不引入按用户的额外查询

**Constraints**: `/api/v1/**` 必须走统一响应封装；`/oauth2/*`、`/.well-known/*` 报文不得改动；应用管理员判定沿用「实时查库、禁止缓存」；不改既有成员/角色/权限语义与 003 的不变量（有角色必是成员）

**Scale/Scope**: 单租户默认部署；组织树深度 ≤ 10；单应用组织授权 ≤ 500 条；在职用户量级 1e3–1e4；新增 3 个操作 × 2 端，文档 operation 数 78 → 84

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 说明 |
|---|---|---|
| I. 协议层单一来源 | PASS | 不改 `op.Storage`、不改 `claims.go` 的 `IsClientMember` 计算路径，`app_roles`/`permissions` 语义保持（spec FR-014）；不引入新协议库；spec FR-029 明确**不**在 authorize 上加业务拦截，避免把协议层当授权层用 |
| II. 端口与信任边界物理分离 | PASS | 新增端点全部落在 `:8081` Gin；`:8080` 无任何改动，`/oauth2/*` 不挂统一响应/限流 |
| III. 分层与数据一致性 | PASS | 新增代码遵循 `api → service → store`；service 不持有 `db`；写操作经 `internal/store/tx.go` 的 tx-in-context；撤销「读取影响人数 → 撤销」不依赖读结果做写入判定（撤销本身按 `(client_id, org_id)` 原子删除并用 `RowsAffected` 判存在性） |
| IV. 凭证不出服务端 | PASS | 本特性不涉及密钥/令牌；组织授权只存 `client_id`/`org_id`/操作者 ID |
| V. 结构变更只走手写 SQL 迁移 | PASS | `0007_client_org_grant.up/down.sql` 成对提交；`*_at` 为 `BIGINT`；`client_id` 显式 `COLLATE utf8mb4_0900_as_cs` |
| 技术与接口约束 | PASS | 统一响应体；新增错误码先登记 AGENTS §5.3 再使用；新增环境变量：无 |
| 开发工作流与质量门禁 | PASS（有强制动作） | 必须重跑 `make docs` 并把 `internal/api/router_test.go` 的 operation 断言 78 → 84；新增路由同步登记 AGENTS §4.1（`scripts/check-registry.sh` 会逐条比对）；新增审计事件登记 §6.7；新表登记 §6.12；同步 §3 目录树（`check-artifact-refs.sh` 会校验） |

**Gate 结论**：通过，无违规项，`Complexity Tracking` 无需填写。

## Project Structure

### Documentation (this feature)

```text
specs/010-app-access-org-grant/
├── plan.md              # 本文件
├── research.md          # Phase 0：11 个决策点（R1–R11）
├── data-model.md        # Phase 1：新表、派生计算、校验规则、迁移
├── quickstart.md        # Phase 1：可执行的验证剧本
├── contracts/
│   ├── admin-org-grants.md    # admin 端 3 个端点 + 成员列表响应变更
│   └── portal-org-grants.md   # 门户应用管理员端 3 个端点（同一实现）
├── checklists/
│   └── requirements.md  # 已生成（spec 阶段）
└── tasks.md             # 由 /speckit.tasks 生成，本命令不产出
```

### Source Code (repository root)

```text
migrations/
└── 0007_client_org_grant.{up,down}.sql          # 新增：client_org_grants 表

internal/
├── store/
│   ├── model/
│   │   └── client_org_grant.go                  # 新增：ClientOrgGrant + OrgGrantMatchSQL 判定片段
│   ├── org_grant_store.go                       # 新增：CRUD / 覆盖人数 / 独占人数 / visibleTotal / 部门引用校验
│   ├── app_auth_store.go                        # 改：ListClientsForMember 增加 EXISTS 分支；成员来源列；候选标记
│   └── organization_store.go                    # 改：HasOrgGrants（供删除保护）
├── service/
│   ├── app_auth.go                              # 改：新增 OrgGrant 服务方法（上限、幂等、审计）；ListForUser 口径
│   ├── org.go                                   # 改：Delete 增加 ErrOrgHasGrants
│   └── app_auth_test.go / *_integration_test.go  # 改：新增用例
├── api/
│   ├── dto/                                     # 改：OrgGrantItem / OrgGrantRequest / 成员行来源
│   ├── admin/
│   │   ├── app_auth.go                          # 改：ListOrgGrants / AddOrgGrants / RemoveOrgGrants（两端共用的唯一实现）
│   │   ├── clients.go                           # 改：成员列表响应带 visibleTotal
│   │   └── orgs.go                              # 改：ORG_HAS_GRANTS 映射
│   ├── me/app_admin.go                          # 改：3 个薄 wrapper（@x-auth-level appAdmin）
│   ├── response/errors.go                       # 改：ORG_HAS_GRANTS / APP_ORG_GRANT_LIMIT
│   ├── router.go                                # 改：两端各 +2 path / +3 operation
│   ├── router_test.go                           # 改：operation 断言 78 → 84
│   └── docs/openapi.json                        # 改：make docs 重新生成

web/admin/src/
├── api/{index.ts,types.ts}                      # 改：orgGrants 三个调用 + 类型；下线 addAppMembersByDept
└── views/client/ClientMembers.vue               # 改：组织授权区 + 来源列 + 组织树加载失败提示

web/portal/src/
├── api/{index.ts,types.ts}                      # 改：同上
└── views/app-admin/Members.vue                  # 改：与 admin 同构（006 决策：复制实现，不抽公共组件）

scripts/e2e/examples.sh                           # 改：新增"两端同操作返回结构一致"断言（E16）

AGENTS.md                                        # 改：§3 目录树、§4.1 路由、§5.3 错误码、§6.7 事件、§6.12 新表
```

**Structure Decision**: 沿用现有单仓库分层（`internal/{api,service,store,oidc}` + `web/{admin,portal}`），不新增目录层级、不引入新包。组织授权的 HTTP 实现放在 `internal/api/admin/app_auth.go`——它是 003 建立的「admin 与门户共用同一套应用内授权实现」的既有落点，portal 侧只加转发 wrapper，避免两端行为漂移（006 的既定做法）。

## Phase 0: Outline & Research

输入（spec 未预设、必须在 plan 落地）：① 组织授权的存储形态；② 「含子部门」的匹配方式与路径快照问题；③ 覆盖人数与可见性如何共用同一口径；④ 撤销影响人数从哪来（是否新增预检端点）；⑤ 成员来源如何计算且不产生 N+1；⑥ 既有「按部门批量加入」的兼容处置；⑦ 部门删除保护的落点；⑧ 审计事件划分；⑨ 文档与注册表需要同步的位置；⑩ 前端两端的复用方式。

输出：[research.md](./research.md)（R1–R12，均已给出 Decision / Rationale / Alternatives considered，无遗留 NEEDS CLARIFICATION；R12 为 critique 复评后的定位与事务约束收敛）。

## Phase 1: Design & Contracts

- [data-model.md](./data-model.md)：`client_org_grants` 表结构、索引、迁移 up/down、5 个派生计算的口径、校验规则与状态语义。
- [contracts/admin-org-grants.md](./contracts/admin-org-grants.md)：`GET/POST /api/v1/admin/clients/:id/org-grants`、`POST .../org-grants/remove`，以及成员列表响应新增字段。
- [contracts/portal-org-grants.md](./contracts/portal-org-grants.md)：`/api/v1/me/apps/:id/org-grants` 三端点（同一 service 实现、不同鉴权）。
- [quickstart.md](./quickstart.md)：从迁移到两端验证的可执行剧本。

**Post-Design Constitution Re-check**：设计未引入新的协议层改动、未新增依赖、未跨层调用、未绕过迁移体系；`/me/apps` 的判定仍为单条 SQL（无缓存）。结论：PASS，与 Phase 0 前一致。

## 实施要点（critique 结论落地）

- **E2 / E4 · 写入一律进事务并锁目标部门**：`POST .../org-grants` 在 `store.Transaction` 内先 `LockOrganization(orgID)`（不存在 → `ErrAppDeptNotFound` → 400，整批回滚），再读取既有 `include_sub`、再判上限（`COUNT` 与插入同一事务），避免"校验后部门被删"的悬空 `org_id` 与并发超限。与 002 对 `users.dept_id` 的临界区写法保持一致。
- **E3 · 三态计数不依赖 `ON DUPLICATE KEY`**：先读后分流为 insert / update（仅 `include_sub` 真变化）/ no-op，`updated_at` 只在真更新时刷新；否则 `RowsAffected` 恒为 2，`unchanged` 永远为 0。
- **E5 · 撤销影响人数在删除前、同事务内计算**：`impactUsers` = 命中待撤销授权 ∧ 非手动成员 ∧ 未被其它保留授权命中的去重人数；计数失败则整次撤销失败（不允许"删了但不知道影响多少人"）。
- **E1 · 口径等价性作为门禁测试**：固定两条 integration 用例——① `/me/apps` 的可见集合 == 授权列表覆盖人数去重后的并集；② `include_sub=0` 时子孙部门人员不计入。测试名固化，防止实现期被简化。
- **E8 · 性能留证**：对覆盖人数聚合与可见性 EXISTS 各做一次 `EXPLAIN` 并记录 fixtures 计时（2000 人子树 / 20 条授权），结果写进 PR；确认 `LIKE CONCAT(列,'%')` 无法使用 `organizations.path` 索引，若实测超 SC-005，升级路径为「Go 侧解析 `dept.path` 得祖先 ID 集合 → `g.org_id IN (...)`」，对外契约不变。
- **E9 · 可见总人数只首屏算**：`visibleTotal` 仅在成员列表 `page=1` 返回数字，其余页返回 `null`，前端沿用首屏值；不新增端点。
- **E7 · 门户鉴权挂载**：三条 `/me/apps/:id/org-grants*` 必须注册在 `appAdminGroup`，并在 `router_test.go` 断言非管理员请求返回 `403 FORBIDDEN`。
- **E14 · 观测**：写操作成功路径打一条 `slog.Info`（`clientId`/`orgId`/`includeSub`/`covered`/`traceId`），写入与失败均已有审计事件；不新增指标系统。
- **E16 · 两端一致性**：`scripts/e2e/examples.sh` 增一次"同一操作在 admin 与 portal 两端返回结构一致"的断言（约 20 行 shell），弥补"复制实现"的漂移风险。
- **E11 · quickstart 数据自洽**：补齐 `dev3`（研发部本级），使"覆盖 3 人"与"含/不含子部门"两条断言可执行。
- **E13 · 回滚顺序**：先回滚镜像、再执行 `0007.down`（`DROP TABLE` 会丢失全部组织授权配置，需重新配置）；旧代码不引用新表，提前 `down` 也不会崩。

## Operational Notes

- **首次上线无需配置项**：本特性不新增任何环境变量；`config` 加载与启动校验不变。
- **迁移**：`0007_client_org_grant.up.sql` 为纯新增表，无数据回填，可在业务运行期执行（`CREATE TABLE` 期间不锁既有表）。
- **观察指标（上线后一周）**：`app_org_granted` / `app_org_grant_revoked` 条数、`/me/apps` 的 p95 与慢查询、成员页 `visibleTotal` 缺失（返回 `null` 以外的异常）次数。

## Complexity Tracking

无宪法违例，无需豁免说明。以下属于「有意识的不做」，记录备查：

| 决策 | 选择的简单方案 | 被否的更"优雅"方案及原因 |
|---|---|---|
| 可见性计算 | 请求时按物化路径前缀实时判定（无物化成员行、无同步任务） | 物化「组织授权成员」到 `client_members`：需要触发点覆盖入职/调岗/停用/部门迁移，漏一处就会安静地错权限，且撤销语义与手动成员纠缠 |
| 两端 UI | portal 复制 admin 的成员页改造（006 既定做法） | 抽公共组件包：跨两个独立 Vite 工程，构建与版本耦合成本高于一次性重复 |
| 是否做真拦截 | **不做**：定位为门户可见性（spec FR-029），`/oauth2/authorize` 零改动 | 在 authorize 校验「成员 ∨ 组织授权」并吊销令牌：触及宪法 I 的边界（协议层只做存取、不做业务状态流转），且需定义 access/refresh 的吊销范围，风险与工作量远高于本期目标；如需，另立专项 |
| 跨应用复用授权 | **不做**：逐应用配置 | "批量应用到多个应用"/"复制到其他应用"：需要新的批量入口与失败语义（部分应用无权限、部分应用已停用），本期不值得；接口 `grants[]` 已为其留好形态 |
| 撤销后通知 | **不做**：仅审计与列表可见 | 站内信/邮件通知受影响员工：产品决策未定（critique Q1），且需接入既有 `notification` 通道与模板，属独立特性 |
