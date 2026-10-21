# Phase 0 Research: 管理后台组织架构增删改

**Date**: 2026-10-21 | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

本特性不需要技术选型（技术栈已锁定），需要解决的是"在既有的 ID 型物化路径 + 大小写不敏感排序规则 + 无唯一索引"这套现状下，如何可靠地实现规格中的四类业务约束。以下 10 项决策均已落地到 [data-model.md](./data-model.md) 与 [contracts/admin-orgs-api.md](./contracts/admin-orgs-api.md)。

---

## R1. `path` 编码：沿用 ID 型物化路径

- **Decision**: 新节点路径 = 父节点路径 + 自身 ID + `/`；根节点 = `/` + 自身 ID + `/`。不改为"名称路径"，不接受客户端提交 `path`。
- **Rationale**: 种子数据（`internal/bootstrap/seed.go`）已按 ID 型写入（`/A/`、`/A/B/`），既有 `ListOrganizations`/`buildTree` 与该格式一致。ID 固定 26 字符，使深度可直接由斜杠数量推导，也让重命名完全不影响子树路径。
- **Alternatives considered**: ① 名称型路径（`/总公司/技术中心/`）：重命名需级联重写且中文长度使 512 上限提前触发；② 仅存 `parent_id` 递归查询：MySQL 8 递归 CTE 可行，但既有列与索引已按物化路径设计，改动面更大；③ 闭包表：需新建表，违反"不新增迁移"。

## R2. 并发串行化：事务内锁定 `organizations` 全表

- **Decision**: 每个写事务的第一步执行 `SELECT id, parent_id, path FROM organizations ORDER BY id FOR UPDATE`，再读、校验（存在性、唯一性、深度、成环、删除保护）、写入。该语句锁定整表并把组织写操作全局串行化。
- **Rationale**: ① 同级唯一性没有唯一索引兜底（`parent_id` 可为 NULL，MySQL 唯一索引对 NULL 不去重）；② 根级节点没有父行可锁，"只锁父行"无法阻止同名根节点并发创建；③ InnoDB 的间隙锁彼此兼容，两个事务可以同时通过存在性检查再双双插入；④ `organizations` 没有 `tenant_id` 索引（仅 `idx_orgs_parent`、`idx_orgs_path`），`WHERE tenant_id = ? FOR UPDATE` 会全表扫描并锁全表，因此**不宣称**"按租户隔离"——直接承认全局串行，避免文档与事实不符；⑤ 组织写是低频管理动作（每租户每日个位数到百级），行数 ≤ 500、持锁毫秒级，代价可接受；⑥ 一把锁让四类校验共享同一临界区，正确性不依赖隔离级别细节；读路径走 MVCC，不加锁、不受影响。
- **Alternatives considered**: 只锁目标行与父行（死锁风险 + 根级空窗）；`SELECT ... GET_LOCK()` 命名锁（新增机制、连接绑定、异常路径需显式释放）；唯一索引/生成列（需迁移）；新增 `idx_orgs_tenant` 后按租户过滤（同样需迁移，列入后续工作）。
- **Residual risk**: 所有租户的组织写互相阻塞；若未来单租户组织数超过数千或需要租户级隔离，需加索引 + 改为按父链加锁 + 唯一索引，作为独立特性评估。

## R3. 同级名称唯一性：显式 `BINARY` 比较

- **Decision**: 唯一性查询为 `WHERE tenant_id = ? AND parent_id <=> ? AND BINARY name = ?`（更新时追加 `AND id <> ?`），`parent_id <=> ?` 为 NULL 安全等值，用于根级（`parent_id IS NULL`）。
- **Rationale**: `organizations` 建表使用 `utf8mb4_0900_ai_ci`（大小写与重音均不敏感），直接 `name = ?` 会把 `Platform` 与 `platform` 判为同名，与 spec"区分大小写"冲突。`BINARY` 转换是逐字节比较，无需改列排序规则、无需迁移。
- **Alternatives considered**: `COLLATE utf8mb4_0900_as_cs`（同样可行，但依赖该 collation 在 8.4 中存在且写法更易被误改）；把列改为 `as_cs`（需迁移，且影响既有查询）；应用层拉全表比较（500 行也可行，但把判断放到 Go 里会让 SQL 与业务逻辑分散）。

## R4. 深度与 10 级上限：由 `path` 段数推导

- **Decision**: `depth(path) = strings.Count(path, "/") - 1`（`/ID/` → 1）。创建时校验 `depth(parent)+1 ≤ 10`；移动时校验 `depth(newParent) + 子树高度 ≤ 10`，子树高度由"子树内最大深度 − 当前节点深度 + 1"得出。上限以常量 `maxOrgDepth = 10` 表达，前端按同一数值禁用入口。
- **Rationale**: 路径即层级事实，无需额外列；`path` 采用定长 ID 编码，深度计算 O(1) 且不可能与 `parent_id` 链不一致（不一致由写入路径保证）。10 级时最长路径约 271 字符，仍在 512 列宽内。
- **Alternatives considered**: 递归 CTE 数层数（每次写多一次查询）；新增 `depth` 列（需迁移）；只靠 `path` 长度 ≤ 512 兜底（阈值约 18 级，提示不友好，且与 spec 的 10 级冲突）。

## R5. 移动（换父）实现：单条前缀替换更新子树

- **Decision**: 移动时在同一事务内执行
  `UPDATE organizations SET path = CONCAT(?, SUBSTRING(path, ?)) WHERE path LIKE CONCAT(?, '%')`，
  参数依次为"新前缀"、"旧前缀字符数 + 1"、"旧前缀"；随后单独更新该节点自身的 `parent_id`（`sort` 不变）。
- **Rationale**: 一条 SQL 完成整棵子树的路径搬迁，满足"不做逐行读-改-写"；ULID 字符集为 Crockford Base32（`0-9A-Z`），不含 `%` 与 `_`，`LIKE` 前缀不存在通配符转义问题。
- **Alternatives considered**: 逐行 `UPDATE`（N 次往返，且中途失败需回滚）；递归 CTE 更新（MySQL 不允许在 CTE 中直接更新同表）；把 `path` 改成相对深度（会破坏既有读取）。

## R6. 成环检测：前缀判定 + 同锁内二次读取

- **Decision**: 锁内读取目标父节点与当前节点后，若 `parentPath` 以 `nodePath` 为前缀（含相等，即自身），返回 `ORG_INVALID_PARENT`。因所有写操作共用 R2 的组织表锁，判定时读到的路径必然是最新已提交值。
- **Rationale**: 物化路径让"B 是否在 A 的子树内"退化为字符串前缀判断，一次比较即可；配合 R2 的组织表锁，不需要重试或版本号。
- **Alternatives considered**: 沿 `parent_id` 向上遍历（O(深度) 次查询且需处理脏数据环）；深度优先搜索子树（需加载子树全部行，虽可行但多余）。

## R7. 删除保护：先子节点、后账号，物理删除

- **Decision**: 锁内先 `SELECT 1 FROM organizations WHERE parent_id = ? LIMIT 1`，有则 `ORG_HAS_CHILDREN`；再 `SELECT COUNT(*) FROM users WHERE dept_id = ?`，有则 `ORG_HAS_USERS`；两者皆无才 `DELETE`。
- **Rationale**: 与 spec 的两类可区分原因一一对应；`users.dept_id` 为 NOT NULL 逻辑外键，物理删除父部门会产生悬空引用，必须在删除前拦截。`organizations` 无软删除列，本次不新增。
- **Alternatives considered**: 级联删除子树（会删掉用户归属，破坏审计可追溯）；软删除（需迁移 + 所有读取路径加过滤）；把用户自动迁移到父部门（隐式数据变更，超出"最保守"原则）。

## R8. 事务边界与分层

- **Decision**: `service.OrgService` 负责开启 `store.Transaction`，内部顺序固定为"锁 → 读 → 校验 → 写 → （必要时）返回重读结果"；`store` 方法只做单条 SQL；`admin.Handler` 只做绑定、错误映射与审计投递。
- **Rationale**: 与既有 `UserService.CreateUser`（校验 + 写入）和 `store.Transaction` 的用法一致；审计是异步 fire-and-forget（`Auditor.Emit`），不进入事务，避免审计写入失败回滚业务。
- **Alternatives considered**: 在 handler 里开事务（跨层，违反宪法 III）；在 store 里做业务校验（store 无法区分错误语义）。

## R9. 错误码、审计与文档同步

- **Decision**: 新增 `ORG_NAME_ALREADY_EXISTS`(409)、`ORG_HAS_CHILDREN`(409)、`ORG_HAS_USERS`(409)、`ORG_INVALID_PARENT`(400)；深度超限与名称非法复用 `VALIDATION_FAILED`(400)；目标节点不存在复用 `NOT_FOUND`(404)。新增审计事件 `org_created` / `org_updated` / `org_deleted`，成功记 `success`，业务拒绝记 `failure`（`Detail` 写原因），401/403/400 参数错误不写审计。同批更新 `AGENTS.md` §4.1、§4.6、§5.3、§6.7 并 `make docs` 重生成 `openapi.json`。
- **Rationale**: 四个码对应四类可自助纠正的拒绝原因（SC-005）；项目已有 `TestAnnotationErrorCodesAreRegistered` 与 `TestDocsPathsMatchRouterRoutes` 两道自动化门禁，不同步就会失败，因此"规范同步"不是可选项。审计事件类型存在两处事实源（Go 常量 + 前端 `EVENT_LABEL`），必须同批更新，否则审计页显示原始英文键且无法筛选项。
- **Alternatives considered**: 单一 `ORG_CONFLICT` 码（前端无法给出精确提示，验收测试只能断言 message）；把失败也全部写审计（参数错误噪声大，且 401 无操作者）；只写成功审计（丢失"为什么删不掉"的追溯）。

## R10. 前端实现：Element Plus 原生能力，零新依赖

- **Decision**: 在 `Users.vue` 组织树面板提供五个入口：顶部"新增根部门"（空树/未选中时同样可用）、节点操作区"新增子部门 / 重命名 / 移动到… / 删除"。新增与重命名共用 `el-dialog`（名称 + 排序）；移动用 `el-tree-select` 选择新上级（含"根级"），提交 `PATCH {parentId}`；删除用 `ElMessageBox.confirm`。成功后 `loadTree()` 并保持/清理选中态；失败用既有 `errorMessage(e)` 展示。深度由 `OrgNode.path` 的斜杠数推导（**不硬编码 10**，避免与后端常量形成第二处事实源）；达 10 级时禁用"新增子部门"；"移动到…"候选排除自身、后代与"新父深度 + 子树高度 > 10"的目标并给出置灰原因；收到 `ORG_HAS_USERS` 时自动按该部门筛选右侧列表（提示含账号数量）；节点数 > 50 时显示关键字过滤，树默认展开 3 层且搜索命中路径自动展开；操作按钮补 `title`/`aria-label`。
- **Rationale**: 页面已用 `el-tree` + `el-dialog` + `ElMessageBox` 组合，新增交互不引入组件或依赖；`errorCode(e)` 可直接把 `ORG_HAS_USERS` 映射为"部门下仍有账号"的提示；"移动到…"复用已支持的 `PATCH parentId` 三态语义，无需新增端点。
- **Alternatives considered**: 纯 API 不提供移动入口（管理员仍需研发代劳，评审 P1 否决）；树拖拽移动（与"不做拖拽排序"澄清冲突）；独立"组织管理"页面（需新路由/菜单，超出最小入口）；在表格区放入口（与组织树语义不符）。

## R11. 账号归属写入与部门删除互斥

- **Decision**: `UserService.CreateUser` 与 `UpdateUser`（`deptId` 变更时）在**短事务**内执行 `SELECT id FROM organizations WHERE id = ? FOR UPDATE` 锁定目标部门行并校验存在，随后写入；参数校验与 argon2 哈希放在事务外。`UpdateUser` 由此新增 `deptId` 存在性校验（过去完全不校验，直接写入）。
- **Rationale**: 现有 `CreateUser` 没有事务（校验 → 哈希 → 插入），在 autocommit 下 `FOR UPDATE` 会立即释放，等于没锁；argon2 约 50–100ms，放进事务会长时间持锁阻塞组织删除，因此必须把 CPU 密集步骤移到事务外、只让"锁 + 校验 + 写入"进事务。互斥后：删除方先成功则账号方重读失败并返回 `ErrDeptNotFound`；账号方先成功则删除方统计到账号并返回 `ORG_HAS_USERS`。代价是一次主键索引查找 + 一个短事务。
- **Alternatives considered**: 删除时对 `users` 按 `dept_id` 做锁定读（`idx_users_dept` 存在，RR 下可阻塞同部门插入，但语义隐蔽且依赖隔离级别）；删除后二次校验（覆盖不了后续插入）；给 `users.dept_id` 加真正外键（表结构与"不新增迁移"限制）；只加锁不校验（挡不住写入不存在的部门）。
- **行为变更**: `PATCH /api/v1/admin/users/:id` 携带不存在 `deptId` 时由"静默写入"改为 `400 VALIDATION_FAILED`，需同步 `AGENTS.md` §4.1 该行说明。

## R12. 空表并发创建的死锁兜底

- **Decision**: `OrgService` 的组织写事务在遇到 MySQL `1213`（死锁）或 `1205`（锁等待超时）时**重试一次**；重试对调用方不可见，仍失败才返回 500。
- **Rationale**: `organizations` 为空时（首次初始化或测试库），`SELECT … FOR UPDATE` 全表扫描只取到 supremum 间隙锁；间隙锁彼此兼容，两个事务都能通过同名检查，随后插入意向锁互相冲突 → 死锁。生产环境有种子数据（`seed.go` 在 `CountUsers==0` 时写入 3 个节点）时不会触发，但集成测试从空库起步会命中。重试一次后，后到者会在新的锁定读中看到已提交行，得到确定结果（`ORG_NAME_ALREADY_EXISTS`）。
- **Alternatives considered**: 插入前用 `GET_LOCK` 命名锁（新增机制，且空表场景本可重试解决）；预置种子节点（测试库不跑 seed）；不处理（集成测试偶发失败，且管理员并发创建首个部门时返回 500）。
