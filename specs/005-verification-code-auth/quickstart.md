# Quickstart: 多通道验证与账号安全

本文档给出本特性的端到端验证场景与运行命令。实现细节与完整任务见 `tasks.md`；接口细节见 [contracts/verification-api.md](./contracts/verification-api.md)；数据模型见 [data-model.md](./data-model.md)。

## 前置条件

- 后端：Go 1.26、MySQL 8.4、Redis 7.4（`docker-compose up -d mysql redis`）。
- 前端：portal 与 admin 两个 Vite 应用。
- 迁移：`make migrate`（应用 `0005_verification_auth`，含 `send_records` 表与 `users.phone` 唯一索引）。
- 配置（`.env`，未启用 mock 时必填真实供应商）：

```bash
# 短信
SMS_PROVIDER=mock                    # 或 chuanglan
SMS_CHUANGLAN_URL=https://...
SMS_CHUANGLAN_ACCOUNT=...
SMS_CHUANGLAN_PASSWORD=...
SMS_CHUANGLAN_TEMPLATE=...

# 邮箱
MAIL_PROVIDER=mock                   # 或 smtp
MAIL_SMTP_HOST=smtp.example.com
MAIL_SMTP_PORT=465
MAIL_SMTP_USERNAME=...
MAIL_SMTP_PASSWORD=...
MAIL_FROM=noreply@example.com
MAIL_TEMPLATE_PATH=./configs/verify-mail.tmpl

# 验证码与保留
VERIFY_CODE_TTL=300
VERIFY_RESEND_INTERVAL=60
VERIFY_MAX_ATTEMPTS=5
VERIFY_RECORD_RETENTION_DAYS=180
```

> 开启 `SMS_PROVIDER=mock` / `MAIL_PROVIDER=mock` 时，验证码固定为 `888888`，不调用真实供应商（FR-017）。

## 运行

```bash
make build            # 编译 bin/sso（含邮件模板复制）
./bin/sso             # 启动 IdP（:8080 协议面 + :8081 管理 API）

# 前端（两个终端）
cd web/portal && npm run dev
cd web/admin && npm run dev
```

## 验证场景（端到端）

### 1. 绑定手机号与邮箱（US1）
1. 用账号密码登录 portal，进入「账号中心」。
2. 输入手机号 → 点击「获取验证码」→ 提交 `888888`（mock 下）→ 显示已绑定且**脱敏**（如 `138****8000`）。
3. 邮箱同样流程绑定并脱敏展示。

### 2. 验证码登录（US2）
1. 退出登录，在登录页切换到「手机验证码登录」。
2. 输入已绑定手机号 → 获取验证码 → 提交 `888888` → 进入门户。
3. 邮箱验证码登录同理；账号密码登录改用邮箱/手机号 + 密码也能登录（正则识别）。

### 3. 找回密码（US3）
1. 登录页点击「忘记密码」→ 输入邮箱 → 获取验证码 → 设置新密码。
2. 重置成功后系统**不自动登录**，用新密码重新登录验证成功。

### 4. MFA 绑定与解绑（US4）
1. 账号中心「安全状态」→ 点击「绑定」→ 弹出二维码 → 用 TOTP 设备（如 Authy/Google Authenticator）扫码 → 填写 6 位动态码 → 绑定成功，状态显示「已启用」。
2. 重新登录（密码方式）需 TOTP 二步验证；验证码登录**豁免** TOTP。
3. 回到账号中心 → 「解绑」→ 通过短信或邮箱验证码 → 解绑成功。

### 5. 发送记录（US5）
1. 管理员登录 admin → 「发送记录」页 → 分页查看短信/邮箱记录，按渠道/场景/结果/时间筛选。

### 6. 模拟发送（US6）
1. 保持 `SMS_PROVIDER=mock` / `MAIL_PROVIDER=mock` → 任意验证码请求返回固定 `888888`，且不产生真实短信/邮件费用。
2. 切换为真实供应商后，验证码由真实渠道发送。

## 自动化校验

- 后端单测：`go test ./internal/service/... ./internal/store/...`（验证码签发/校验、错误计数、供应商 mock、清理任务）。
- 契约/文档：`make docs-check`（若涉及接口文档更新）。
