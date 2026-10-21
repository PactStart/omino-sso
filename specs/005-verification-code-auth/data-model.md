# Data Model: 多通道验证与账号安全

本文档定义本特性的数据模型（新增表 + 既有表约束更新），对齐 AGENTS.md §6 与宪法 V（手写 SQL 迁移、`BIGINT` 时间、ULID 主键）。

## 新增表：`send_records`（发送记录）

每完成一次短信或邮箱验证码发送（无论成功/失败）写入一条记录（FR-013）。接收方存**脱敏后**的字符串（澄清 Q4：服务端返回脱敏数据）。

| 列 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | `CHAR(26)` | PK | ULID 主键 |
| `channel` | `VARCHAR(16)` | NOT NULL | 渠道枚举：`sms` / `email` |
| `scene` | `VARCHAR(32)` | NOT NULL | 场景枚举（见下） |
| `target` | `VARCHAR(128)` | NOT NULL | 脱敏后的接收方（如 `138****8888`、`z***@example.com`） |
| `result` | `VARCHAR(16)` | NOT NULL | 结果枚举：`success` / `failed` |
| `provider` | `VARCHAR(32)` | NOT NULL | 供应商：`chuanglan` / `smtp` / `mock` |
| `error` | `VARCHAR(255)` | NULL | 失败原因（成功时为 NULL） |
| `created_at` | `BIGINT` | NOT NULL | Unix 毫秒（发送时间，清理依据） |

- 字符集 `utf8mb4`；枚举为小写字符串。
- 索引：`(channel, created_at)`、`(scene, created_at)`、`(result, created_at)` 支持管理端按渠道/场景/结果分页筛选（FR-014）。
- **不使用软删除**（宪法 III：token/code/session 类禁软删除；本表为审计型流水，由定时任务物理清理，见数据保留）。

### 枚举

| 枚举 | 取值 | 说明 |
|---|---|---|
| `channel` | `sms`、`email` | 发送渠道（FR-013） |
| `scene` | `bind_phone`、`bind_email`、`login`、`find_password`、`mfa_unbind` | 验证码用途场景（绑定/登录/找回/解绑） |
| `result` | `success`、`failed` | 发送结果 |
| `provider` | `chuanglan`、`smtp`、`mock` | 实际执行发送的供应商/实现 |

> `scene` 的取值以实施阶段最终 API 契约为准，`contracts/verification-api.md` 中定义；此处为数据模型预定义。

### 数据保留

- 发送记录与审计日志均保留 6 个月（澄清 Q5），由内置定时任务每日分批物理删除超过保留期的行（research.md 决策 8）。
- 保留期阈值由配置 `VERIFY_RECORD_RETENTION_DAYS`（默认 180）控制。

## 既有表变更：`users`

### 唯一索引（新增）

| 表 | 变更 | 说明 |
|---|---|---|
| `users` | 新增 `UNIQUE KEY uk_users_phone (phone)` | 手机号全局唯一（FR-004）；`phone` 列已存在（`0001_init`），本期仅补唯一索引 |

- `email` 唯一索引 `uk_users_email` 已在 `0001_init` 存在；`phone` 唯一索引本期新增。
- `phone`/`email` 均为 NULL 可空，MySQL 唯一索引允许多个 NULL，存量未绑定账号不受影响。
- **迁移前置**：建唯一索引前须清理存量重复手机号（如有），否则迁移失败（research.md 遗留项）。

### 既有字段复用（无结构变更）

| 列 | 说明 |
|---|---|
| `email` / `phone` | 联系方式字段已存在，绑定/修改时写入，解绑/清空时置 NULL |
| `mfa_enabled` / `mfa_secret` / `mfa_required` | MFA 字段已存在（`0001_init`），绑定写 secret + 置 enabled，解绑清空 secret + 置 disabled |

> 无需为 `users` 新增列：联系方式与 MFA 字段均已存在，本特性只补 `phone` 唯一索引与相关读写逻辑。

## 审计事件类型（新增，登记 AGENTS.md §6.7）

| 事件类型 | 说明 |
|---|---|
| `verification_code_sent` | 验证码发送（含渠道/场景/结果） |
| `phone_bound` | 手机号绑定/修改 |
| `email_bound` | 邮箱绑定/修改 |
| `mfa_bound` | MFA 绑定 |
| `mfa_unbound` | MFA 解绑 |
| `password_reset` | 找回/重置密码 |

> 验证码登录成功/失败复用既有 `login_success` / `login_failure`；MFA 校验复用既有 `mfa_success` / `mfa_failure`。

## 关系

- `send_records` 与 `users` 无强外键：接收方以脱敏字符串存储（不可反查真实联系方式），仅作审计流水。发送时若目标对应已知用户，可选择性在审计事件中记录 `user_id`，但 `send_records` 本身不存 `user_id`。
- 验证码（Redis）与 `send_records`（MySQL）分属两套存储：前者管「一次性凭证生命周期」，后者管「发送历史审计」，二者通过场景与目标在业务上对应，不建显式关联。

## 迁移

- `migrations/0005_verification_auth.up.sql`：
  - `CREATE TABLE send_records (...)`；
  - `ALTER TABLE users ADD UNIQUE KEY uk_users_phone (phone)`（前置清理重复数据）。
- `migrations/0005_verification_auth.down.sql`：
  - `DROP TABLE send_records`；
  - `ALTER TABLE users DROP KEY uk_users_phone`。
- 手写 SQL，禁止 `AutoMigrate`（宪法 V）。
