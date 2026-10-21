# Phase 1 Data Model: 管理后台组织架构增删改

**Date**: 2026-10-21 | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

本特性**不新增表、不新增列、不新增索引**，只对既有 `organizations` 表做读写。下文"模型"指代码层的实体、请求/响应结构与不变量。

---

## 1. 既有实体：组织节点

表 `organizations`（`migrations/0001_init.up.sql`，规范 §6.2）：

| 列 | 类型 | 约束 | 本特性用法 |
|---|---|---|---|
| `id` | `CHAR(26)` | PK | 服务端用 ULID 生成；同时参与 `path` 编码 |
| `parent_id` | `CHAR(26)` | NULL, INDEX | `NULL` = 根级；移动即改此列 |
| `name` | `VARCHAR(64)` | NOT NULL | 去首尾空白后写入；同级唯一（区分大小写） |
| `path` | `VARCHAR(512)` | NOT NULL, INDEX | 服务端派生：`父 path + id + "/"`；根级 `/id/`；客户端不可写 |
| `sort` | `INT` | NOT NULL DEFAULT 0 | 同级升序；移动不改写 |
| `tenant_id` | `VARCHAR(32)` | NOT NULL DEFAULT `'default'` | 根级固定 `default`；子节点继承父节点；客户端不可指定 |
| `created_at` | `BIGINT` | NOT NULL | Unix 毫秒，创建时写入；无 `updated_at` 列（表未定义，本次不新增） |

**重要**：该表排序规则为 `utf8mb4_0900_ai_ci`（大小写不敏感）。名称唯一性必须用 `BINARY name = ?` 比较，否则 `Platform` 与 `platform` 会被误判为同名。

### 关联实体

| 实体 | 关系 | 本特性的使用方式 |
|---|---|---|
| 用户 `users` | `users.dept_id` → `organizations.id`（NOT NULL，逻辑外键） | 删除前置校验：`SELECT COUNT(*) WHERE dept_id = ?`；不因组织变更修改用户 |
| 审计事件 `audit_events` | 独立记录，无外键 | 写操作成功后记 `success`、业务拒绝记 `failure`；`detail` 为 JSON 文本 |

---

## 2. 派生属性与不变量

| 编号 | 不变量 | 保证方式 |
|---|---|---|
| INV-1 | 每个节点 `path = (parent.path 或 "/") + id + "/"` | 写入前由服务端计算；不接受客户端输入 |
| INV-2 | `path` 以 `/` 开头与结尾，且恰含 `depth+1` 个 `/` | 同上；深度由斜杠数推导 |
| INV-3 | 根级节点 `parent_id IS NULL`，`depth = 1`；深度上限 10 | 创建/移动时校验 |
| INV-4 | 同一租户、同一 `parent_id` 下 `name` 唯一（去空白、区分大小写） | 事务内 `BINARY` 等值查询 |
| INV-5 | `parent_id` 指向的节点必然存在（无孤儿）；`users.dept_id` 不得指向已删除或不存在节点 | 事务内先锁后校验；删除前拒绝非空子树；账号写入侧在短事务内锁定目标部门行并校验存在，与删除互斥 |
| INV-6 | 节点租户 = 父节点租户；根级租户 = `default` | 写入时按父链取值，请求值被忽略 |
| INV-7 | 子树内所有节点共享当前节点 `path` 前缀 | 移动用单条前缀替换 SQL 维护 |
| INV-8 | 不存在环（`path` 前缀关系与 `parent_id` 关系一致） | 移动时拒绝把节点挂到自身或后代下 |

**深度公式**：`depth(path) = strings.Count(path, "/") - 1`，`/ID/` → 1。

**子树高度**：`1 + max(depth(sub) - depth(node))`，`sub` 取 `path` 以 `node.path` 为前缀的全部节点（含自身）。

---

## 3. 代码层模型

### 3.1 请求模型（`internal/api/admin/orgs.go`）

```text
CreateOrgRequest
  parentId : string?   // 缺失或 null = 根级；非空必须存在
  name     : string    // 必填，去空白后 1–64 字符
  sort     : int?      // 可选，默认 0

UpdateOrgRequest（PATCH 局部更新）
  name     : string?      // 缺失 = 不变
  sort     : int?         // 缺失 = 不变
  parentId : 三态          // 缺失 = 不变；null = 移到根级；"ID" = 移到该节点下
```

`parentId` 的三态用自定义类型 `optionalString{Set bool; Value *string}` 表达：`UnmarshalJSON` 被调用即 `Set=true`，`null` 得到 `Value=nil`。普通 `*string` 无法区分"未提交"与"显式 null"。

### 3.2 响应模型（`internal/api/dto/dto.go`）

```text
OrgDetail
  id        : string      // ULID
  parentId  : string|null // 根级为 null
  name      : string
  path      : string      // 服务端派生，与组织树响应同源
  sort      : int
  tenantId  : string      // 只读，服务端判定
  userCount : int64       // 创建时为 0；更新时为该节点及其子部门汇总
  createdAt : string      // RFC3339（UTC）
```

创建返回 `201` + `OrgDetail`；修改返回 `200` + `OrgDetail`；删除返回 `200` + `data: null`。既有组织树响应 `OrgNode{id,name,parentId,path,userCount,children[]}` **保持逐字段不变**。

### 3.3 服务层输入（`internal/service/org.go`）

```text
CreateOrgInput
  ParentID *string
  Name     string
  Sort     *int
  // 无租户字段：接口不接收租户，创建时按父链派生（根级 default），见 §2 INV-6

UpdateOrgInput
  Name        *string
  Sort        *int
  ParentID    *string    // 仅在 ParentIDSet 为 true 时生效
  ParentIDSet bool
```

---

## 4. 写操作序列

### 4.1 创建

```text
BEGIN
  SELECT id, parent_id, path FROM organizations ORDER BY id FOR UPDATE   -- 锁全表（无 tenant 索引）；全局串行
  if parentID != nil:
      读父节点（锁内）；不存在 -> ORG_INVALID_PARENT
      tenant = 父.tenant（为空则回落 default）
  else:
      tenant = "default"
  name = trim(name)；校验非空/长度/控制字符            -- 不合法 -> VALIDATION_FAILED
  if depth(parent)+1 > 10 -> VALIDATION_FAILED("层级不能超过 10 级")
  if 存在同级同名（BINARY） -> ORG_NAME_ALREADY_EXISTS
  path = parentPath + newID + "/"（根级 "/" + newID + "/"）
  INSERT organizations
COMMIT
```

### 4.2 修改（重命名 / 排序 / 移动）

```text
BEGIN
  SELECT id, parent_id, path FROM organizations ORDER BY id FOR UPDATE   -- 锁全表；全局串行
  从锁定结果中取当前节点；不存在 -> NOT_FOUND
  若 ParentIDSet:
      新父 = ParentID == nil ? 根级 : 重读父节点（不存在 -> ORG_INVALID_PARENT）
      if 新父 path 以 当前 path 为前缀 -> ORG_INVALID_PARENT   -- 自身/后代/成环
      if 新父.tenant != 当前.tenant -> ORG_INVALID_PARENT      -- 仅存量脏数据可能触发
      if depth(新父)+子树高度 > 10 -> VALIDATION_FAILED("层级不能超过 10 级")
  若 Name 提交:
      trim + 校验；同级同名（排除自身） -> ORG_NAME_ALREADY_EXISTS
  若 ParentIDSet:
      UPDATE organizations SET path = CONCAT(新前缀, SUBSTRING(path, len(旧前缀)+1))
        WHERE path LIKE '旧前缀%'                              -- 含自身，一条 SQL
      UPDATE organizations SET parent_id = 新父ID WHERE id = 当前ID
  若 Name/Sort 提交:
      UPDATE organizations SET name/sort = ? WHERE id = 当前ID
COMMIT
```

顺序说明：先搬子树再改自身 `parent_id`，两者都在同一事务内；`sort` 不在移动时被改写（spec 决策）。响应中的 `userCount` 由一次子树聚合查询得出（`users JOIN organizations`，按路径前缀统计），不逐节点统计，也不引入 N+1。

**空请求体语义**：提交 `{}`（或 `name`/`sort` 均为 `null`、`parentId` 未出现）时，视为"无变更"——不执行任何 `UPDATE`、不写审计，直接返回当前节点（200），保证幂等（契约 §2）。

### 4.3 删除

```text
BEGIN
  SELECT id, parent_id, path FROM organizations ORDER BY id FOR UPDATE   -- 锁全表；全局串行
  从锁定结果中取当前节点；不存在 -> NOT_FOUND
  if 存在 parent_id = 当前ID 的节点 -> ORG_HAS_CHILDREN
  if COUNT(users WHERE dept_id = 当前ID) > 0 -> ORG_HAS_USERS
  DELETE FROM organizations WHERE id = 当前ID
COMMIT
```

### 4.4 账号归属写入（配套修正，非本特性端点）

```text
CreateUser:
  参数校验（密码强度等）→ argon2 哈希           -- 都在事务外，避免持锁期间做 CPU 密集工作
  BEGIN
    SELECT id FROM organizations WHERE id = ? FOR UPDATE   -- 锁目标部门，与组织删除互斥
    不存在 -> ErrDeptNotFound
    INSERT users
  COMMIT

UpdateUser（仅当提交了 deptId）:
  BEGIN
    SELECT id FROM organizations WHERE id = ? FOR UPDATE
    不存在 -> ErrDeptNotFound（对外 400 VALIDATION_FAILED）
    UPDATE users SET dept_id = ?
  COMMIT
```

`UpdateUser` 过去不校验 `deptId` 存在性，本次一并修正（行为变更，见 spec 边界用例与 contracts §6.6）。

---

## 5. 校验规则汇总

| 规则 | 判据 | 失败码 |
|---|---|---|
| 名称必填 | 去首尾空白后长度 ≥ 1 | `VALIDATION_FAILED` |
| 名称长度 | 去空白后 rune 数 ≤ 64（与 `VARCHAR(64)` 一致） | `VALIDATION_FAILED` |
| 名称字符 | 不含 Unicode 控制字符 | `VALIDATION_FAILED` |
| 名称排序 | 入库前 trim；比较用 `BINARY` | — |
| 同级唯一 | 同租户 + 同 `parent_id`（NULL 安全等值）+ `BINARY name` 相同，排除自身 | `ORG_NAME_ALREADY_EXISTS` |
| 父节点存在 | 父 ID 非空时必须能读到 | `ORG_INVALID_PARENT` |
| 父节点合法性 | 不能是自身或自身后代 | `ORG_INVALID_PARENT` |
| 深度上限 | 创建/移动后整棵子树深度 ≤ 10 | `VALIDATION_FAILED`（message 固定为"层级不能超过 10 级"） |
| 路径长度断言 | 10 级上限下 `path` 最长约 271 字符，低于 512 列宽；仅作内部断言 | 断言失败 → `INTERNAL_ERROR`（非用户输入校验） |
| 删除前置 | 无子节点且无直属账号 | `ORG_HAS_CHILDREN` / `ORG_HAS_USERS` |
| 目标存在 | 修改/删除的 `:id` 必须存在 | `NOT_FOUND` |
| 账号部门存在 | 创建用户、修改用户部门时目标部门必须存在（本次修正） | `VALIDATION_FAILED`（`ErrDeptNotFound`） |
| 排序值 | `sort` 为整数，缺省 0；MUST 落在 `int32` 范围（或收敛为 `0..1_000_000`），避免 MySQL `INT` 溢出导致 500 | `VALIDATION_FAILED` |

**租户规则**：请求不定义租户字段；若 JSON 中携带 `tenantId`，绑定会忽略它（结构体无该字段），服务端始终按父链取值。根级固定 `default`。

---

## 6. 错误映射（service → API）

| service 哨兵错误 | HTTP | code | 典型触发 |
|---|---|---|---|
| `ErrOrgNotFound` | 404 | `NOT_FOUND` | `:id` 不存在或已被删除 |
| `ErrOrgInvalidName` | 400 | `VALIDATION_FAILED` | 空/全空白/超长/含控制字符 |
| `ErrOrgDepthExceeded` | 400 | `VALIDATION_FAILED` | 创建或移动后超过 10 级 |
| `ErrOrgNameExists` | 409 | `ORG_NAME_ALREADY_EXISTS` | 同级同名 |
| `ErrOrgInvalidParent` | 400 | `ORG_INVALID_PARENT` | 父不存在 / 自身 / 后代 / 跨租户 |
| `ErrOrgHasChildren` | 409 | `ORG_HAS_CHILDREN` | 删除非叶子 |
| `ErrOrgHasUsers` | 409 | `ORG_HAS_USERS` | 删除仍有账号的部门 |
| `ErrDeptNotFound`（既有用户端点） | 400 | `VALIDATION_FAILED` | 创建用户或修改用户部门时目标部门不存在（本次修正：过去静默写入） |
| 其他 | 500 | `INTERNAL_ERROR` | 数据库/未知错误（日志记完整错误，响应不含细节） |

HTTP 与 code 的定义必须与 `internal/api/response/errors.go` 及 `AGENTS.md` §5.3 三处一致；新增码在同一提交内登记。

---

## 7. 审计模型

| 操作 | `event_type` | `result` | `detail`（JSON） |
|---|---|---|---|
| 创建成功 | `org_created` | `success` | `{"orgId","name","parentId"}` |
| 修改成功 | `org_updated` | `success` | `{"orgId","changed":["name","sort","parentId"]}` |
| 删除成功 | `org_deleted` | `success` | `{"orgId","name"}` |
| 业务拒绝 | 对应操作的 `event_type` | `failure` | `{"message":"层级不能超过 10 级"}` 等可读原因 |

**审计划分以条件为准，不以 HTTP 状态码为准**（`400` 同时承载业务拒绝与输入格式错误，二者处理相反）：

- 写 `failure`：`ORG_NAME_ALREADY_EXISTS`、`ORG_HAS_CHILDREN`、`ORG_HAS_USERS`、`NOT_FOUND`、`ORG_INVALID_PARENT`、**层级超过 10 级**（同为 400 `VALIDATION_FAILED`，但属业务拒绝）。
- 不写审计：JSON 绑定失败、名称空/全空白/超长/含控制字符（输入格式类 `VALIDATION_FAILED`）、401（未认证）、403（非管理员）——只记应用日志。

`user_id` / `username` 取当前管理员；`ip` / `user_agent` 取请求；`trace_id` 由既有 trace 中间件注入。

前端展示：审计页的标签与筛选下拉由 `web/admin/src/utils/format.ts` 的 `EVENT_LABEL` 常量生成。三种组织事件必须同批加入该常量（"创建组织 / 更新组织 / 删除组织"），否则审计页会显示原始英文键且无法按事件类型筛选，FR-014 的"可筛选"将落空。

---

## 8. 并发语义

所有组织写事务在开始时执行 `SELECT id, parent_id, path FROM organizations ORDER BY id FOR UPDATE`。`organizations` **没有 `tenant_id` 索引**（仅 `idx_orgs_parent`、`idx_orgs_path`），该语句会全表扫描并锁定整表，因此**组织写操作全局串行**：不同租户的组织写也会互相等待。当前单租户部署下影响仅为组织写之间的短暂排队（≤ 500 行，毫秒级）；读路径走 MVCC，不加锁、不受影响。若未来需要租户级隔离，须新增 `idx_orgs_tenant` 迁移并改为按租户过滤（本次不做）。

| 场景 | 结果 |
|---|---|
| 同一父节点下并发创建同名 | 串行化，第二个事务在锁后读到已提交行 → `ORG_NAME_ALREADY_EXISTS` |
| **空表**上并发创建（首次初始化 / 测试库） | 无行可锁，只取到 supremum 间隙锁，而间隙锁彼此兼容 → 两个事务都通过同名检查，插入时互等形成死锁（1213）。service 层对组织写事务**重试一次**后：一方成功、另一方在重试中读到已提交行 → `ORG_NAME_ALREADY_EXISTS`；重试仍失败才返回 500。生产环境有种子数据（`seed.go` 在 `CountUsers==0` 时写入 3 个节点），正常不会触发 |
| 两个管理员互设对方为父（成环） | 串行化，后执行者前缀判定命中 → `ORG_INVALID_PARENT` |
| 删除父节点与创建子节点并发 | 串行化，创建方在锁后重读父节点失败 → `ORG_INVALID_PARENT`；或删除方先执行并成功，创建失败 |
| 删除节点与修改该节点并发 | 串行化，后执行者重读失败 → `NOT_FOUND` |
| 删除部门与创建账号 / 调整账号部门并发 | 账号写入侧在**短事务**内对目标部门行执行 `SELECT id FROM organizations WHERE id = ? FOR UPDATE`（参数校验与 argon2 哈希都在事务外，避免持锁期间做 CPU 密集工作）。删除方先持锁：账号方等待后读到部门已删 → `ErrDeptNotFound`；账号方先持锁：删除方等待后统计到账号 → `ORG_HAS_USERS`。最终不会出现悬空 `dept_id` |
| 两个事务写不同租户 | **互相阻塞**（全局串行化，见上）；不承诺租户级隔离 |

所有事务的锁顺序一致（先按 `ORDER BY id` 锁定组织行集；账号写入侧随后锁单个部门行），加锁顺序本身不产生死锁。唯一的例外是"空表并发创建"的 supremum 间隙锁死锁，由 service 层对 `1213`（死锁）/`1205`（锁等待超时）做**一次**重试兜底；命中重试时业务结果仍确定（一方成功、一方 409），不得把重试暴露给调用方。
