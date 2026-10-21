# API 契约：文件上传与对象存储（004-object-storage-upload）

所有端点均在 `:8081` 管理 API，路径前缀 `/api/v1`，统一响应 `{code, message, data, traceId}`（AGENTS.md §5）。鉴权由 session cookie（`sid`）承载，`admin` 端点另需 `middleware.Admin()`。

## 1. 签发上传凭证

`POST /api/v1/files/upload-credential`（需登录；`scene=app_logo` 需 admin）

**请求体**

```json
{ "scene": "user_avatar", "format": "png", "size": 245760 }
```

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `scene` | string | 是 | `app_logo` / `user_avatar`；`app_logo` 需 admin |
| `format` | string | 是 | `jpg` / `png` / `webp` |
| `size` | int | 是 | 字节数，`1 ≤ size ≤ 5*1024*1024` |

**响应 200**（`data`）

```json
{
  "key": "app_logo/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png",
  "uploadUrl": "https://bucket.oss-cn-hangzhou.aliyuncs.com",
  "accessKeyId": "STS.xxxx",
  "securityToken": "<STS security token>",
  "policy": "<base64 policy>",
  "signature": "<hmac-sha1>",
  "expiresAt": "2026-10-21T10:05:00Z"
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `key` | string | OSS 对象键（含场景前缀 + ULID + 扩展名） |
| `uploadUrl` | string | 直传目标端点 |
| `accessKeyId` | string | STS 临时凭证的 AccessKeyId（非 master 密钥） |
| `securityToken` | string | STS 临时凭证的 SecurityToken，客户端以表单字段 `x-oss-security-token` 提交 |
| `policy` | string | base64 编码的 PostObject policy（约束 key/type/size/过期） |
| `signature` | string | policy 的 HMAC-SHA1 签名（用 STS 临时密钥签名） |
| `expiresAt` | string | 凭证过期时间（RFC3339） |

**错误码**：`VALIDATION_FAILED`（字段/格式/大小不合法）、`FORBIDDEN`（非 admin 请求 `app_logo`）、`UNAUTHENTICATED`、`DEPENDENCY_UNAVAILABLE`（对象存储不可用）。

---

## 2. 确认上传并回写业务字段

直传成功后，客户端调用对应「确认」端点：服务端写入一条 `files` 记录并回写业务表字段（FR-017）。

### 2.1 员工自助头像

`POST /api/v1/me/avatar`（需登录）

**请求体**

```json
{ "key": "user_avatar/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png", "format": "png", "size": 245760 }
```

**响应 200**（`data`）

```json
{ "fileId": "01J9ZK9M1N3P5Q7R9S1T3V5W7X", "url": "https://cdn.example.com/avatar/01J9ZK9M1N3P5Q7R9S1T3V5W7X.png" }
```

效果：`users.avatar_url = url`（当前用户），`files` 新增 `scene=user_avatar` 记录。

### 2.2 管理员代管用户头像

`POST /api/v1/admin/users/:id/avatar`（admin）

请求体同上；效果：`users.avatar_url = url`（目标用户），`files` 新增 `scene=user_avatar, creator=当前管理员` 记录。

### 2.3 应用 Logo

`POST /api/v1/admin/clients/:id/logo`（admin）

请求体同上；效果：`clients.logo_url = url`（目标应用），`files` 新增 `scene=app_logo` 记录。

**错误码（三类确认端点通用）**：`VALIDATION_FAILED`、`UNAUTHENTICATED`、`FORBIDDEN`、`NOT_FOUND`（目标用户/应用不存在）、`INTERNAL_ERROR`、`DEPENDENCY_UNAVAILABLE`。

---

## 3. 文件记录分页列表

`GET /api/v1/admin/files`（admin）

**Query**：`page`（默认 1）、`pageSize`（默认 20，上限 100）、`scene`（可选）、`status`（可选）、`keyword`（可选，按 `url`/`object_key` 模糊）。

**响应 200**（`data` 为 `response.Page`，`list` 元素）

```json
{
  "id": "01J9ZK9M1N3P5Q7R9S1T3V5W7X",
  "provider": "aliyun.oss",
  "scene": "app_logo",
  "format": "png",
  "size": 245760,
  "url": "https://cdn.example.com/logo/portal.png",
  "status": "active",
  "creator": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
  "createdAt": "2026-10-21T10:04:00Z"
}
```

---

## 4. 更新文件记录状态

`PATCH /api/v1/admin/files/:id`（admin）

**请求体**

```json
{ "status": "disabled" }
```

**响应 200**（`data` 为更新后的文件记录，同上结构）。

- `status` 取值：`active` / `disabled` / `deleted`。
- 置 `disabled`：清空关联业务字段（`logo_url`/`avatar_url` 置 NULL）但**保留** OSS 对象，可恢复（`disabled→active` 时重新写回 URL）。
- 置 `deleted`：清空业务字段并**删除** OSS 对象（终态）。
- 展示回退由业务字段为空驱动（FR-007）；状态变更写入 `file_status_updated` 审计事件。

**错误码**：`VALIDATION_FAILED`（非法状态值）、`NOT_FOUND`、`UNAUTHENTICATED`、`FORBIDDEN`、`DEPENDENCY_UNAVAILABLE`。

---

## 契约约定

- 上传凭证与确认端点均返回统一信封；`data` 结构如上，不含任何密钥（master secret 不出服务端）。
- 审计：上传确认、状态更新写入审计事件（FR-010），事件类型登记 AGENTS.md §5.3（新增 `file_uploaded` / `file_status_updated`）。
- 新错误码（如有）须先登记 AGENTS.md §5.3 再使用；本契约优先复用既有错误码。
