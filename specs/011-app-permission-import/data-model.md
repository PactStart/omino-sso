# Data Model: 应用权限点批量导入（011）

**Date**: 2026-11-03 | **Plan**: [plan.md](./plan.md) | **Research**: [research.md](./research.md)

本特性**不新增数据库表、不改既有表结构**（无需迁移）。下面定义的"模型"分三类：① **传输模型**（导入文档 / 导出文档，仅存在于请求与响应中）；② **派生模型**（校验问题清单、三态计数、导入计划，均为一次性产物，不持久化）；③ **既有实体**（`client_permissions`，仅消费不修改）。

---

## 1. 导入文档（传输模型，客户端 → 服务端）

```json
{
  "version": 1,
  "mode": "merge",
  "permissions": [
    {
      "code": "order",
      "name": "订单",
      "description": "订单域",
      "children": [
        { "code": "order.write", "name": "创建订单", "sort": 10 }
      ]
    }
  ]
}
```

| 字段 | 类型 | 必填 | 规则 | 失败时的 `issue.code` |
|---|---|---|---|---|
| 文档根 | object | 是 | 必须是 JSON 对象；空字符串/空白/`null`/数组均拒绝 | `empty` / `bad_type` |
| `version` | number | 否 | 仅 `1`；缺省视为 `1`；其它值拒绝并说明"仅支持版本 1" | `bad_version` |
| `mode` | string | 否 | 仅 `"merge"`；缺省视为 `"merge"`；`"replace"` 及其它值拒绝并说明"暂不支持全量对齐" | `bad_mode` |
| `permissions` | array | 是 | 非空；元素必须是对象 | `missing_field` / `bad_type` |
| `permissions[].code` | string | 是 | 复用既有规则 `^[A-Za-z0-9_.:-]{1,64}$`（`internal/service/app_auth.go:68` 的 `permCodePattern`）；先 `TrimSpace`；**区分大小写** | `missing_field` / `invalid_code` / `too_long` |
| `permissions[].name` | string | 是 | 非空；长度 **≤ 64 字节**（与既有 `CreatePermission` 一致，中文约 21 字；既有行为，不在本特性放宽，FR-010） | `missing_field` / `too_long` |
| `permissions[].description` | string | 否 | ≤ 255 字节；空串等价于未提供 | `too_long` |
| `permissions[].sort` | number | 否 | 整数（`int` 范围）；缺省时按同级数组下标自动分配 `0..n-1`（FR-008） | `bad_type` |
| `permissions[].children` | array | 否 | 递归同构；空数组等价于未提供 | `bad_type` |
| 其它字段（任意层级） | — | 否 | **一律拒绝**并指出字段名与位置（FR-006；`id`/`parentId`/`permissionList` 等） | `unknown_field` |

**错误分层的执行方式**：一条 `issue` 不阻断其它节点的检查（FR-005 要求一次暴露多处）。例外：根不是对象、`permissions` 不是数组时，后续检查无法进行，只报这一条；某节点 `code` 缺失或非法时，仍继续检查该节点的 `name`/`description`，但**跳过**对它的跨层查重登记（其子节点继续检查，父级路径按"未知父"处理）。

**未知字段的判定口径**：允许集合在每一层都是 `{code, name, description, sort, children}`（根层为 `{version, mode, permissions}`）。判定用 `json.Decoder` 逐层解到 `map[string]json.RawMessage` 再比对键集合，而不是靠 struct 反序列化静默丢弃——静默丢弃会让"用户写了 `parentId` 却以为生效"成为真实陷阱（spec 边界情形）。

---

## 2. 导出文档（传输模型，服务端 → 客户端）

```json
{
  "version": 1,
  "mode": "merge",
  "importable": { "ok": true, "reasons": [] },
  "permissions": [
    { "code": "order", "name": "订单", "children": [{ "code": "order.write", "name": "创建订单" }] }
  ]
}
```

| 字段 | 规则 |
|---|---|
| `version` / `mode` | 恒为 `1` / `"merge"`（FR-019，保证原样回导） |
| `importable` | `{ok, reasons[]}`：这份文件能否回导。`reasons ∈ {empty(权限点为空), too_many(>1000 条), too_large(格式化序列化 >512KB), too_deep(深度 >100)}`，可同时出现多个；**仅用于前端提示，不写入下载文件**（见 research R9） |
| `permissions[]` | 含 `code`、`name`、可选 `description`、可选 `children`；**不含** `id`/`parentId`/`sort`/`childCount`/`roleCount`/时间字段 |
| 排序 | 每层按 `(sort, created_at)` 升序，与 `GET .../permissions` 同口径（但导出**不复用**该查询的 `LIMIT 1000`，见下） |
| 空应用 | `permissions: []`，`importable={ok:false, reasons:["empty"]}`（导出成功，但文件不可回导——回导会被 `empty` 拒绝） |

**`too_large` 的字节口径（必须与下载文件一致）**：判定用 `json.MarshalIndent({version, mode, permissions}, "", "  ")` 的长度，即前端 `JSON.stringify(_, null, 2)` 产出的文件字节数。**不得使用 compact 序列化**：3 层嵌套下 compact 比实际文件小 20%–30%，据此判定会产出"服务端说可回导、下载后 413"的假承诺。该口径由 `TestImportableMatchesDownloadBytes` 固定（见 research R10）。

**读取口径**：导出必须用 `ListClientPermissionsForExport`（无 `LIMIT`、无 `child_count`/`role_count` 子查询）。复用 `ListClientPermissions` 会让 >1000 条的应用**静默导出残缺文件**，而 US4/SC-004 承诺的是"100% 一致"。

**下载文件的构成**（前端）：`{version, mode, permissions}`（去掉 `importable`），`JSON.stringify(_, null, 2)`，文件名 `permissions-<clientId>-<yyyyMMdd>.json`。`importable` 不能进文件——否则回导会被 FR-006 的未知字段校验拒绝。

---

## 3. 校验问题条目（派生，不持久化）

```json
{
  "path": "permissions[2].children[0].code",
  "field": "code",
  "code": "invalid_code",
  "message": "编码含非法字符，只允许字母、数字与 _.:-",
  "line": null
}
```

| 字段 | 说明 |
|---|---|
| `path` | JSON 路径式位置，`permissions[i]` + 每层 `.children[j]`，末段为字段名；文档级问题为 `""` 或 `"permissions"` |
| `field` | `""`（文档级）/ `version` / `mode` / `permissions` / `code` / `name` / `description` / `sort` / `children` |
| `code` | 机器可读分类，见下表 |
| `message` | 面向管理员的中文说明，含修复动作（"改为…"/"删除该字段"/"裁剪后重试"） |
| `line` | 仅语法错误时有值（由 `json.SyntaxError.Offset` 换算行号）；其余为 `null` |

`code` 枚举（**闭合集合**，新增需同步 spec 与本表）：

| `code` | 触发 | 归属错误码（确认导入时） |
|---|---|---|
| `syntax_error` | 不是合法 JSON | `APP_PERMISSION_IMPORT_INVALID` |
| `empty` | 空内容 / `permissions` 为空数组 | `APP_PERMISSION_IMPORT_INVALID` |
| `bad_type` | 类型不符（根非对象、`permissions` 非数组、`sort` 非整数等） | `APP_PERMISSION_IMPORT_INVALID` |
| `unknown_field` | 出现白名单外字段 | `APP_PERMISSION_IMPORT_INVALID` |
| `missing_field` | 缺 `code` / `name` / `permissions` | `APP_PERMISSION_IMPORT_INVALID` |
| `bad_version` | `version` ≠ 1 | `APP_PERMISSION_IMPORT_INVALID` |
| `bad_mode` | `mode` ≠ `"merge"` | `APP_PERMISSION_IMPORT_INVALID` |
| `invalid_code` | 编码不匹配 `permCodePattern` | `APP_PERMISSION_IMPORT_INVALID` |
| `too_long` | `code`/`name`/`description` 超长 | `APP_PERMISSION_IMPORT_INVALID` |
| `dup_code` | 同一文档内跨层重复（同时指出两处及以上 `path`） | `APP_PERMISSION_IMPORT_INVALID` |
| `too_deep` | 层级超过 100（与既有 `maxPermissionDepth` 一致；指出超限节点路径与实际深度） | `APP_PERMISSION_IMPORT_INVALID` |
| `too_many` | 总条数 > 1000（`message` 含实际条数与上限） | `APP_PERMISSION_IMPORT_TOO_LARGE` |

`hint` 类（**不阻断**、也不计入失败）：不放在 `issues` 里，而是独立字段 `warnings[]`，同样有 `path`/`code`/`message`：

| `code` | 触发 |
|---|---|
| `prefix_hint` | 子节点 `code` 不以父节点 `code` 为前缀（FR-011，仅提示） |
| `case_hint` | 两个 `code` 忽略大小写后相同（如 `order.read` 与 `Order.Read`），视为**不同**权限点，仅提示疑似重复 |

`issues` 最多返回 20 条（`maxImportIssues`），另给 `issueTotal`（实际总数）。`warnings` 同样截断 20 条并给 `warningTotal`。

---

## 4. 三态计数（派生）

以 `code` 为键（应用内唯一，`uk_client_permissions_code`，`utf8mb4_0900_as_cs`）：

| 分类 | 判定 |
|---|---|
| **新增** `added` | `code` 不在库内 |
| **更新** `updated` | `code` 在库内，且 `name` / `description` / `sort` / **父级** 任一不同 |
| **无变化** `unchanged` | `code` 在库内，且上述四项全同 |
| **上级将变更** `moved` | `updated` 的子集：父级不同（计入 `updated`，**不重复计入**总数） |

恒等式：`added + updated + unchanged = 文档总条数`（预览与确认导入必须同口径，并发改动除外）。`moved ≤ updated`。

**父级比较在 `code` 空间进行**：库内行的 `parent_id` 是内部 ID，跨环境搬运时必然不同；因此 diff 时把库内 `parent_id` 映射成父 `code`（根为 `""`），与输入中该节点的父 `code` 比较。这是"导出→回导"能闭环的前提（内部 ID 不参与任何比较或输出，FR-007）。

**`sort` 的比较口径**：输入省略 `sort` 时按同级数组下标取值（`0..n-1`）。因此"库内 `sort` 曾被手工设置过、本次省略"会被判为**更新**（改 `sort`）。这是 FR-008 的直接后果，不是缺陷；同一份文档第二次导入即为 `unchanged`（SC-003 不受影响）。

---

## 5. 导入计划（派生，写入前的中间结构）

```text
importPlan
├── Insert []*model.ClientPermission   # 已分配 ULID、parent 已解析为最终 ID、sort 已定、created_at=updated_at=now
├── Update []{ ID string; Fields map[string]any; Moved bool }
├── Unchanged int
├── Moved int
├── MaxDepth int
└── CodeToID map[string]string          # 既有 code → 既有 ID；新插入的 code → 新 ULID（供子级解析父）
```

**构建顺序（拓扑分层）**：按输入树做 BFS，每处理一层时该层所有节点的父级 ID 都已确定——父在输入树中（本层之前的层，已插入并分配 ID）或父在库内（`CodeToID` 已含）。根级节点 `parent_id = NULL`。

**环的安全性**：输入是一棵树（`code` 全局唯一 + 单一父 + `children` 收敛），所有写入的 `parent_id` 都取自这棵树或"库内既有且不在本次输入中"的行；父先子后写，任何中间态都不成环，无需写入后二次环检测（research R5）。

**只改输入中出现过的节点**：未出现在本次输入中的既有节点，其 `parent_id` 一律不改动——即便它们的父节点被移动（spec 边界情形"移动节点时其原有子节点"）。实现上即"只对 `Update` 里的行发 `Updates`"。

---

## 6. 既有实体（消费方，不改结构）

`client_permissions`（`migrations/0003_app_auth.up.sql:32-45`，登记于 AGENTS §6.9）：

| 列 | 类型 | 本特性的使用方式 |
|---|---|---|
| `id` | `CHAR(26)` | 新记录由 `oklog/ulid/v2` 生成；仅内部使用，不出现在任何文档中 |
| `client_id` | `VARCHAR(64)` `as_cs` | 导入范围；由路径参数决定 |
| `parent_id` | `CHAR(26) NULL` | 由输入树结构推导；根级为 `NULL` |
| `code` | `VARCHAR(64)` `as_cs` | **幂等键**；`uk_client_permissions_code(client_id, code)` 兜底并发 |
| `name` / `description` / `sort` | `VARCHAR(64)` / `VARCHAR(255)` / `INT` | 由输入逐字段覆盖（仅在变化时写） |
| `created_at` / `updated_at` | `BIGINT`（毫秒） | 新增时同值；更新时只刷 `updated_at` |

**不变量（沿用 003，本特性不得破坏）**：

1. 同一应用内 `code` 唯一（区分大小写）——导入靠内存 diff 保证，唯一键兜底。
2. 角色↔权限绑定（`client_role_permissions`）不受导入影响（FR-017）：导入只 `INSERT`/`UPDATE client_permissions`，**不触碰** `client_role_permissions`、`client_members`、`client_role_members`。
3. 不删除：`DELETE` 语句不出现在导入路径（FR-015）。

**状态机**：无。权限点没有 status 字段，导入也不引入。

---

## 7. 写入语义汇总（供契约与测试对齐）

| 场景 | 结果 |
|---|---|
| 同一份文档连导两次 | 第二次 `added=0, updated=0, unchanged=N`，且写操作数为 0（SC-003） |
| 库内节点被移到其原有后代之下 | 按输入整体重写该节点及其祖先的 `parent_id`，不成环（US2-AC8） |
| 输入中的节点是根级、库内是子级 | `parent_id` 改为 `NULL`，计入 `updated`（并在 `moved` 里） |
| 输入中未出现的既有节点 | 完全不动（包括其 `parent_id` 与 `sort`） |
| 同名不同大小写的新增 | 插入成功；预览给 `case_hint` 警告（不阻断） |
| 写入途中应用被删除 | 事务回滚，0 条残留（spec 边界"导入过程中应用被删除"） |
| 并发两个导入 | 行锁串行化；后到者基于前者结果重算 diff，不产生重复；外部路径撞唯一键 → `409 APP_RESOURCE_STALE` |
