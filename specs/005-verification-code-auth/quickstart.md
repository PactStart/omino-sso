# Quickstart: 多通道验证与账号安全

本文档给出本特性的端到端验证场景与运行命令。实现细节与完整任务见 `tasks.md`；接口细节见 [contracts/verification-api.md](./contracts/verification-api.md)；数据模型见 [data-model.md](./data-model.md)。

## 前置条件

- 后端：Go 1.26、MySQL 8.4、Redis 7.4（`docker-compose up -d mysql redis`）。
- 前端：portal 与 admin 两个 Vite 应用。
- 迁移：`make migrate-up`（按文件名升序执行 `migrations/*.up.sql`，其中 `0005_verification_auth` 建 `send_records` 表并加 `users.phone` 唯一索引；回退用 `make migrate-down`）。
- 配置（`.env`，未启用 mock 时必填真实供应商）：

```bash
# 短信
SMS_PROVIDER=mock                    # 或 chuanglan
SMS_CHUANGLAN_URL=https://...
SMS_CHUANGLAN_ACCOUNT=...
SMS_CHUANGLAN_PASSWORD=...
SMS_CHUANGLAN_TEMPLATE_ID=1019888301 # 创蓝 V2 模板接口必填（缺它返回 code=130 templateId 不能为空）
SMS_CHUANGLAN_TEMPLATE=验证码：{s}   # 仅在未配 TEMPLATE_ID 时使用；与 TEMPLATE_ID 二选一

# 邮箱
MAIL_PROVIDER=mock                   # 或 smtp
MAIL_SMTP_HOST=smtp.example.com
MAIL_SMTP_PORT=465                   # 465=SMTPS（连上即 TLS）；587=明文+STARTTLS；其余按明文
MAIL_SMTP_USERNAME=...
MAIL_SMTP_PASSWORD=...
MAIL_FROM=noreply@example.com        # 信封发件人自动取 MAIL_SMTP_USERNAME（邮箱形态时），避免 553
MAIL_TEMPLATE_PATH=./configs/verify-mail.tmpl

# 验证码与保留
VERIFY_CODE_TTL=300
VERIFY_RESEND_INTERVAL=60
VERIFY_MAX_ATTEMPTS=5
# 供应商熔断：同一渠道连续失败达阈值即开闸，冷却期内直接返回 VERIFY_SEND_FAILED 且不调用供应商
# 阈值 0 关闭熔断；阈值 > 0 时冷却必须 > 0，否则启动即报配置错误
VERIFY_CIRCUIT_THRESHOLD=5
VERIFY_CIRCUIT_COOLDOWN=60
VERIFY_RECORD_RETENTION_DAYS=180
```

> 开启 `SMS_PROVIDER=mock` / `MAIL_PROVIDER=mock` 时，验证码固定为 `888888`，不调用真实供应商（FR-017）。

## 运行

```bash
make build            # 校验邮件模板存在（assets）+ 文档漂移检查（docs-check）+ 编译 bin/sso
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
4. 反例校验：用**未绑定**的邮箱/手机号取码 → 返回 `404 VERIFY_TARGET_NOT_FOUND`「该邮箱/手机号未绑定任何账号，请用密码登录后到账号中心绑定」，`send_records` 不产生记录；「忘记密码」用未绑定目标取码 → 仍返回成功但不发信（防枚举）。

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
- 业务集成测试：`internal/service/verification_integration_test.go`（绑定/验证码登录/验证码重置密码/MFA 绑定解绑）与 `internal/service/notification/integration_test.go`（验证码生命周期、熔断）**需同时设置** `TEST_MYSQL_DSN` 与 `TEST_REDIS_ADDR`，任一缺失即整组 `t.Skip`——因此 `make test` 本身不依赖外部基础设施，要覆盖这些用例需显式跑：

  ```bash
  TEST_MYSQL_DSN='root:omino_dev_pwd@tcp(127.0.0.1:3307)/omino_sso_test?charset=utf8mb4&parseTime=true' \
  TEST_REDIS_ADDR='127.0.0.1:6380' \
  go test ./internal/service/... -v
  ```

  > 这些用例会 `DROP` 并重建 `TEST_MYSQL_DSN` 指向的库，禁止指向开发库或生产库。
- 前端用例：`make test-web`（`web/portal` 的 `Login.spec.ts` 12 条：登录方式切换、图形验证码、换图时机、失败提示）。
- 契约/文档：`make docs-check`（若涉及接口文档更新）。
- 提交前全量门禁：`make build && make test && make lint`（`make lint` = golangci-lint + 两套前端 eslint）。
