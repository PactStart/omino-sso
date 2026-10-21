# Data Model: 图片上传与对象存储

本文档定义本特性的数据模型（新增表 + 既有业务字段更新），对齐 AGENTS.md §6 与宪法 V（手写 SQL 迁移、`BIGINT` 时间、ULID 主键）。

## 新增表：`files`（上传文件记录）

每完成一次上传（FR-012）写入一条记录。供应商对象本身由 OSS 持有，本表只存元数据与公开地址。

| 列 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | `VARCHAR(26)` | PK | ULID 主键（`internal/pkg/id`） |
| `provider` | `VARCHAR(32)` | NOT NULL | 供应商，本期恒为 `aliyun_oss` |
| `scene` | `VARCHAR(32)` | NOT NULL | 场景枚举：`app_logo` / `user_avatar` |
| `target_id` | `VARCHAR(64)` | NOT NULL | 关联业务对象：`app_logo`=client_id；`user_avatar`=user_id |
| `format` | `VARCHAR(16)` | NOT NULL | 图片格式：`jpg` / `png` / `webp` |
| `size` | `BIGINT` | NOT NULL | 文件字节数 |
| `url` | `VARCHAR(512)` | NOT NULL | 公开可访问地址（FR-006） |
| `object_key` | `VARCHAR(512)` | NOT NULL | OSS 对象键（删除/替换时定位对象） |
| `status` | `VARCHAR(16)` | NOT NULL | 状态枚举：`active` / `disabled` / `deleted` |
| `creator` | `VARCHAR(26)` | NOT NULL | 上传者用户 ID |
| `created_at` | `BIGINT` | NOT NULL | Unix 毫秒（列名 `_at` 结尾，宪法 V） |
| `updated_at` | `BIGINT` | NOT NULL | Unix 毫秒 |

- 字符集 `utf8mb4`；枚举为小写字符串，不区分大小写敏感（无需 `_as_cs` 排序规则）。
- 索引：`(scene, created_at)` 支持按场景分页；`status` 普通索引（管理列表筛选）。
- **不使用软删除**（宪法 III：token/code/session 类禁软删除；本表作为审计型元数据，删除状态由 `status` 表达）。

### 枚举

| 枚举 | 取值 | 说明 |
|---|---|---|
| `scene` | `app_logo`、`user_avatar` | 字符串枚举（FR-013） |
| `status` | `active`（生效）、`disabled`（禁用）、`deleted`（已删除） | 字符串枚举（FR-013） |

### 状态转移

```
上传成功  → active
active    → disabled（管理员禁用，FR-015）
active    → deleted （管理员删除，FR-015）
disabled  → active   （管理员恢复）
disabled  → deleted
deleted   （终态）
```

**状态对业务字段与对象的影响（U1 澄清）**：

| 状态 | 业务字段（logo_url/avatar_url） | OSS 对象 | 可恢复 |
|------|-------------------------------|---------|--------|
| `active` | 指向 URL | 存在 | — |
| `disabled` | **清空（置 NULL）** | **保留** | 可（`disabled→active` 时重新写回 URL） |
| `deleted` | **清空（置 NULL）** | **删除** | 否（终态） |

- 展示回退统一由「业务字段为空/无法加载」驱动（FR-007），展示方无需反查 `files.status`。
- 替换/清除头像或 logo 时，被替换/清除的旧记录置 `deleted`，并删除对应 OSS 对象（FR-008）。

## 既有业务字段（更新，无结构变更）

| 表 | 列 | 类型 | 说明 |
|---|---|---|---|
| `clients` | `logo_url` | `VARCHAR(512) NULL` | 应用 logo；上传确认后写入公开地址（已存在，迁移 `0002_clients_meta`） |
| `users` | `avatar_url` | `VARCHAR(512) NULL` | 用户头像；上传确认后写入公开地址（已存在，迁移 `0001_init`） |

> 无需为业务表新增列：`logo_url` / `avatar_url` 均已存在，上传确认时 `UPDATE` 该列即可。

## 关系

- `files.creator` → `users.id`（逻辑外键，不建物理 FK，遵循现有模型惯例）。
- `files` 与业务实体的关联通过「场景 + 业务字段回写」表达：`scene=app_logo` 回写 `clients.logo_url`；`scene=user_avatar` 回写 `users.avatar_url`。二者不存显式外键（历史存量外部 URL 无对应 `files` 记录）。

## 迁移

- `migrations/0004_files.up.sql`：`CREATE TABLE files (...)`。
- `migrations/0004_files.down.sql`：`DROP TABLE files`。
- 手写 SQL，禁止 `AutoMigrate`（宪法 V）。
