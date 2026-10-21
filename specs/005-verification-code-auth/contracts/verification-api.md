# 验证码与账号安全 API 契约

本契约定义「多通道验证与账号安全」特性的对外接口（`:8081` 管理 API 面）。所有端点遵循项目统一响应格式与鉴权约定，具体错误码登记 AGENTS.md §5.3。

## 统一约定

- **响应格式**：成功 `{"code":"OK","message":"success","data":{...},"traceId":"..."}`；失败 `{"code":"<ERR>","message":"...","traceId":"..."}`（`/oauth2/*` 协议面除外，本特性全部落在 `:8081`）。
- **鉴权**：
  - 未登录可调：`/auth/code`、`/auth/login`、`/auth/password/reset`（需图形验证码防刷）。
  - 需会话（`sid` cookie）：`/me/*`。
  - 需管理员会话：`/admin/*`。
- **脱敏约定**：手机号与邮箱在响应中一律脱敏（澄清 Q4），脱敏在服务端完成，前端不做二次处理。

## 1. 发送验证码

`POST /api/v1/auth/code`

**用途**：按场景向手机号或邮箱发送 6 位验证码。mock 开启时验证码固定 `888888`，不真实发送。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `scene` | string | 是 | 枚举：`bind_phone` / `bind_email` / `login` / `find_password` / `mfa_unbind` |
| `channel` | string | 是 | `sms` / `email`（与 scene 匹配） |
| `target` | string | 是 | 手机号（11 位）或邮箱 |
| `captchaId` | string | 否 | 未登录场景（登录/找回）图形验证码 id |
| `captchaCode` | string | 否 | 未登录场景图形验证码值 |

**响应 200**

```json
{ "code": "OK", "message": "success", "data": { "expiresIn": 300, "resendAfter": 60 }, "traceId": "..." }
```

**错误**：`VALIDATION_FAILED`、`AUTH_CAPTCHA_INVALID`、`RATE_LIMITED`（60s 内重发）、`VERIFY_SEND_FAILED`（供应商失败）。

**防枚举约定**：`login` / `find_password` 场景对「未注册或未绑定目标联系方式」返回**统一成功响应**但不真实发送（不泄露账号存在性）；`bind_phone` / `bind_email` 场景对「已被其他账号占用」的目标返回 `VERIFY_TARGET_IN_USE`。

## 2. 登录（扩展多方式）

`POST /api/v1/auth/login`

**用途**：在既有账号密码登录基础上扩展——账号标识支持用户名/邮箱/手机号（正则识别），并新增验证码登录。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `loginType` | string | 是 | `password` / `email_code` / `sms_code` |
| `identifier` | string | 是 | 用户名 / 邮箱 / 手机号（`password` 时也可沿用 `username` 字段） |
| `password` | string | 否 | `loginType=password` 时必填 |
| `code` | string | 否 | `loginType=email_code/sms_code` 时必填 |
| `captchaId` / `captchaCode` | string | 否 | 未登录防刷 |
| `rememberMe` | bool | 否 | 会话时长（7 天 / 8 小时） |

**响应**：同既有 `/auth/login`（`mfaRequired` 为 true 时返回 `mfaTicket` 进入二步；验证码登录因已通过渠道验证，**不触发** TOTP 二步，澄清 Q6）。

**错误**：`AUTH_INVALID_CREDENTIALS`、`AUTH_CODE_INVALID`、`AUTH_ACCOUNT_DISABLED`、`AUTH_ACCOUNT_LOCKED`、`RATE_LIMITED`。

## 3. 找回密码

`POST /api/v1/auth/password/reset`

**用途**：未登录用户通过邮箱/手机验证码重置密码。成功后**不自动登录**（澄清 Q1），须用新密码重新登录。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `channel` | string | 是 | `sms` / `email` |
| `target` | string | 是 | 手机号或邮箱 |
| `code` | string | 是 | 6 位验证码 |
| `newPassword` | string | 是 | 8–128 |

**响应 200**：`{ "code":"OK", "message":"success", "data":{} }`

**错误**：`VALIDATION_FAILED`、`AUTH_CODE_INVALID`、`RATE_LIMITED`。

## 4. 绑定 / 修改手机号

`POST /api/v1/me/phone`

**鉴权**：session。

**请求体**：`{ "phone": "13800138000", "code": "123456" }`

**响应 200**：`{ "code":"OK", "message":"success", "data": { "phone": "138****8000" } }`（脱敏）

**错误**：`VALIDATION_FAILED`、`AUTH_CODE_INVALID`、`VERIFY_TARGET_IN_USE`（手机号已被他人占用）。

## 5. 绑定 / 修改邮箱

`POST /api/v1/me/email`

**鉴权**：session。

**请求体**：`{ "email": "zhangsan@example.com", "code": "123456" }`

**响应 200**：`{ "code":"OK", "message":"success", "data": { "email": "z***@example.com" } }`（脱敏）

**错误**：`VALIDATION_FAILED`、`AUTH_CODE_INVALID`、`VERIFY_TARGET_IN_USE`。

## 6. MFA 绑定（两步）

### 6.1 启动绑定

`POST /api/v1/me/mfa/bind`

**鉴权**：session。已绑定返回 `MFA_ALREADY_BOUND`。

**响应 200**：`{ "code":"OK", "message":"success", "data": { "otpauthUrl": "otpauth://totp/...", "mfaSetupTicket": "mfs_..." } }`

### 6.2 确认绑定

`POST /api/v1/me/mfa/bind/confirm`

**请求体**：`{ "mfaSetupTicket": "mfs_...", "code": "123456" }`

**响应 200**：`{ "code":"OK", "message":"success", "data": { "bound": true } }`

**错误**：`VALIDATION_FAILED`、`AUTH_MFA_INVALID`（动态码错误）、`AUTH_MFA_TICKET_EXPIRED`。

## 7. MFA 解绑

`POST /api/v1/me/mfa/unbind`

**鉴权**：session。未绑定返回 `MFA_NOT_BOUND`。

**请求体**：`{ "channel": "sms" | "email", "code": "123456" }`（通过短信或邮箱验证码验证）

**响应 200**：`{ "code":"OK", "message":"success", "data": { "bound": false } }`

**错误**：`AUTH_CODE_INVALID`、`MFA_NOT_BOUND`、`RATE_LIMITED`。

## 8. 发送记录列表

`GET /api/v1/admin/send-records`

**鉴权**：admin。

**查询参数**：`page`、`pageSize`、`channel`（sms/email）、`scene`、`result`（success/failed）、`startTime`、`endTime`。

**响应 200**：`{ "code":"OK", "message":"success", "data": { "items": [ { "id": "...", "channel": "sms", "scene": "login", "target": "138****8000", "result": "success", "provider": "chuanglan", "createdAt": 1780000000000 } ], "total": 42 } }`

## 审计

以下动作写审计事件（登记 AGENTS.md §6.7）：`verification_code_sent`、`phone_bound`、`email_bound`、`mfa_bound`、`mfa_unbound`、`password_reset`；验证码登录复用 `login_success`/`login_failure`。
