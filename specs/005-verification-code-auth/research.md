# Research: 多通道验证与账号安全（技术选型）

本文件沉淀 Phase 0 的技术决策，规格中的所有开放技术点在此落定。

## 决策 1 — 发送抽象：`NotificationProvider` 接口

- **Decision**: 在 service 层定义发送抽象，短信与邮箱共用同一接口族，供应商可插拔：
  - `SendSms(ctx, phone, code, scene) error`
  - `SendEmail(ctx, email, code, scene) error`
  分别提供短信实现（创蓝）与邮箱实现（SMTP），并各提供一个 `mock` 实现（模拟发送）。业务侧不感知具体供应商，仅依赖接口。
- **Rationale**: 与 004 的 `FileStore` 同构——把「供应商操作」与「业务逻辑（验证码签发/校验、记录、审计）」分离；用户明确要求「先支持创蓝，后续扩展其他供应商（阿里云等）」，抽象可让新供应商只加一个实现即可。`mock` 实现满足 FR-017（模拟发送），使开发/测试/演示环境零外部依赖。
- **Alternatives considered**:
  - 短信、邮箱各写死一套逻辑不抽象：后续换供应商需改动所有调用点，违反「可扩展」要求 → 拒绝。
  - 直接依赖云厂商「统一消息」SDK：额外引入重依赖且本期无需求 → 拒绝。

## 决策 2 — 短信供应商：创蓝走 HTTP API，不引入 SDK

- **Decision**: 创蓝短信通过其 HTTP 接口直连（`net/http` 客户端，自行做参数签名/编码），不引入第三方 Go SDK；接口地址、账号、密码、模板 ID 等由 `internal/config` 配置。本期仅实现「验证码」类短信（模板固定，参数为验证码与有效期）。
- **Rationale**: 创蓝未提供维护良好的官方 Go SDK，且验证码短信场景单一（单模板 + 短参数），HTTP 直连即可覆盖；避免引入无人维护的 SDK 与额外依赖，符合宪法「不过度设计」。
- **Alternatives considered**:
  - 引入社区创蓝 SDK：质量/维护不确定，且把供应商签名细节锁进第三方库 → 拒绝。

## 决策 3 — 邮箱发送：Go 标准库 `net/smtp` + 手写 MIME

- **Decision**: 邮箱验证码走 SMTP，用 Go 标准库 `net/smtp` 发送；邮件正文为 `multipart/alternative`（`text/plain` + `text/html`），HTML 模板由**模板文件**渲染（`html/template`），模板路径由配置指定。不引入 `gomail` 等第三方库。
- **Rationale**: 验证码邮件结构简单，标准库足以；模板文件满足 FR-012（模板走文件 + 配置读取 + Docker 镜像复制），并便于运维不重新编译即可微调文案。避免新增依赖。
- **Alternatives considered**:
  - `gomail`/`gopkg.in/mail`：API 更友好但引入额外依赖，且对验证码场景收益有限 → 拒绝。
  - 模板字符串硬编码进代码：无法满足「模板写文件 + 按配置读取」→ 拒绝。

## 决策 4 — 验证码存储：Redis 短时存储（TTL + 一次性 + 错误计数）

- **Decision**: 验证码存 Redis，key 形如 `verify:{scene}:{channel}:{target}`，值存 6 位数字，`TTL = 5 分钟`；校验通过即 `DEL`（一次性）；同一目标 60 秒内禁止重发（用独立 key 或 `SET NX` 记发送时间）；连续错误计数用 `INCR` + 过期对齐，超过 5 次立即失效。
- **Rationale**: 验证码是短生命周期临时凭证，天然适合 Redis 的 TTL 与原子操作，无需落库；一次性消费 + 错误计数 + 频率限制分别对应 FR-018/FR-019 与澄清 Q2。
- **Alternatives considered**:
  - 验证码落 MySQL：引入无必要的持久化与清理负担，且高并发下锁竞争 → 拒绝。
  - 仅依赖 Redis TTL 不做错误计数：无法满足「连续错误 5 次失效」→ 拒绝。

## 决策 5 — 登录账号识别：正则判定（手机号 / 邮箱 / 用户名）

- **Decision**: 登录入口对账号输入做类型识别：`^1\d{10}$`（中国大陆 11 位手机号，澄清 Q3）判定为手机号；含 `@` 判定为邮箱；其余按用户名处理。`UserService.Authenticate` 扩展为按标识符查询（用户名/邮箱/手机号各自走对应唯一索引）。
- **Rationale**: 用户明确要求「通过正则判断何种登录方式」；邮箱已有 `uk_users_email` 唯一索引，手机号本期新增 `uk_users_phone` 唯一索引，三者查询均可命中索引。
- **Alternatives considered**:
  - 前端区分账号类型传独立字段：与「正则判断」要求不符，且把判定逻辑外置易漂移 → 拒绝。

## 决策 6 — MFA 绑定/解绑：复用 `pquerna/otp`，secret 加密存储

- **Decision**: 绑定流程用 `totp.Generate` 生成 secret，加密后存 `users.mfa_secret`（复用既有 `DecryptMFASecret` 对称加解密）；返回 `otpauth://` URI 供前端渲染二维码；用户扫码后填 6 位动态码，`totp.Validate` 通过后置 `mfa_enabled=true`。解绑走短信/邮箱验证码验证后清空 secret 并置 `mfa_enabled=false`。绑定/解绑分别写 `mfa_bound`/`mfa_unbound` 审计。
- **Rationale**: `users` 表已具备 `mfa_enabled/mfa_secret/mfa_required` 字段（0001_init），且登录侧 `VerifyMFA` 已用 `pquerna/otp` + `DecryptMFASecret`，绑定/解绑完全复用既有设施，仅补「生成 secret + 二维码 + 验证绑定」与「验证码解绑」两端。
- **Alternatives considered**:
  - 自研 TOTP：违反「协议/凭证类缺陷成本最高，不自研」→ 拒绝。

## 决策 7 — 模拟发送：mock provider + 固定验证码「6 个 8」

- **Decision**: 配置 `SMS_MOCK` / `MAIL_MOCK`（或统一 `VERIFY_MOCK`）为 true 时，`NotificationProvider` 走 `mock` 实现：不调用真实供应商，验证码固定为 `888888`，仍照常写发送记录（`provider=mock`、`result=success`）。
- **Rationale**: 满足 FR-017 与 SC-004；mock 实现与真实实现同接口，业务逻辑零分叉，避免「测试路径」与「生产路径」漂移。
- **Alternatives considered**:
  - 在业务代码里 `if mock` 特判：会让验证码生成逻辑与发送逻辑耦合，易在真伪之间泄漏 → 拒绝。

## 决策 8 — 过期清理：内置 goroutine + ticker，不引入定时框架

- **Decision**: 发送记录与审计日志的过期清理由服务端**内置**定时任务完成（Go `time.Ticker`，`fx` 生命周期启动/停止），每日执行一次，删除超过 6 个月的数据；删除采用分批（如每批 500 条）循环，避免长事务/长锁，并记录结构化清理日志（删除行数、耗时、错误）。
- **Rationale**: 用户明确「系统启动时开启一个任务清理，不引入 xxl-job 之类定时任务框架，避免增加复杂性」（澄清 Q5）；企业内部单实例 IdP 无需分布式调度，内置 ticker 足够，符合宪法「不过度设计」。
- **Alternatives considered**:
  - 引入 xxl-job/分布式调度：运维与依赖成本高，对单实例无收益 → 拒绝。
  - 写入时懒清理：时序不可控，低峰无请求时数据不清理 → 作为补充，主方案仍为定时清理。

## 决策 9 — 发送记录写入：同步落库，保证审计完整性

- **Decision**: 验证码发送成功/失败后，发送记录（`send_records`）在**同一请求内同步落库**（供应商调用返回后立即写库），不引入异步队列。
- **Rationale**: 验证码发送本身已等待供应商网络响应（数百 ms），发送记录同步写入（数 ms）对接口延迟影响可忽略；同步落库严格满足 SC-005「每次发送 100% 产生记录」，避免进程崩溃/队列丢弃导致的审计缺失。
- **Alternatives considered**:
  - 异步队列（goroutine/channel）：能略微降低接口延迟，但进程崩溃会丢记录，破坏 SC-005「100% 记录」→ 拒绝。

## 决策 10 — 配置模型（环境变量）

- **Decision**: 新增配置项（`internal/config` 统一加载并校验）：
  - 短信：`SMS_PROVIDER`（`chuanglan`/`mock`）、`SMS_CHUANGLAN_URL`、`SMS_CHUANGLAN_ACCOUNT`、`SMS_CHUANGLAN_PASSWORD`、`SMS_CHUANGLAN_TEMPLATE`。
  - 邮箱：`MAIL_PROVIDER`（`smtp`/`mock`）、`MAIL_SMTP_HOST`、`MAIL_SMTP_PORT`、`MAIL_SMTP_USERNAME`、`MAIL_SMTP_PASSWORD`、`MAIL_FROM`、`MAIL_TEMPLATE_PATH`。
  - 验证码：`VERIFY_CODE_TTL`（默认 300s）、`VERIFY_RESEND_INTERVAL`（默认 60s）、`VERIFY_MAX_ATTEMPTS`（默认 5）。
  - 清理：`VERIFY_RECORD_RETENTION_DAYS`（默认 180）。
  按 provider 校验必填项，缺失启动失败（宪法「环境变量统一加载并校验」）。
- **Rationale**: 供应商不同所需配置不同，统一由 `validate()` 按 provider 校验，与 004 OSS 配置模型一致。

## 决策 11 — 供应商调用：超时、熔断与限流

- **Decision**: 短信（创蓝 HTTP）与邮箱（SMTP）调用设置明确超时（默认 3s）；发送失败**不自动重试**（用户可手动重新获取验证码）；对供应商的调用频率复用既有 Redis 令牌桶限流，防止触发供应商封禁；连续失败进入短时熔断，熔断期内请求直接返回失败提示。
- **Rationale**: 外部供应商不可控，超时防止慢响应拖垮验证码签发接口；不自动重试避免重复计费与重复短信；限流与熔断保护供应商配额与系统稳定性，使失败可预期、可观测。
- **Alternatives considered**:
  - 无超时/无熔断：供应商慢响应或宕机会雪崩式拖垮本服务 → 拒绝。

## 遗留（Non-blocking，交 tasks/实现）

- 创蓝短信的请求签名/编码细节在实现阶段落地并补单测。
- 邮件 HTML 模板的默认文案与样式由实现阶段产出（模板文件随镜像打包）。
- 手机号唯一索引 `uk_users_phone` 需在迁移中先清理存量重复数据（如有）再建唯一索引，否则迁移失败——实施阶段核对存量数据。
