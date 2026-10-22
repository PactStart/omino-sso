# omino-sso 项目规范

> 本文件是项目的**唯一权威规范**，所有开发会话（人类或 AI）必须先读本文件再动手。
> 与本文件冲突的实现一律视为缺陷；确需变更规范时，先改本文件并写明理由，再改代码。
> 配套文档：`技术选型报告.md`（背景与决策依据，不作为实现约束）。

---

## 1. 项目概述

| 项 | 内容 |
|---|---|
| 项目名 | `omino-sso` |
| 一句话描述 | 面向企业内部的统一身份提供方（IdP），基于 OIDC/OAuth 2.1 为内部系统提供单点登录、单点登出与集中账号管理。 |
| 目标用户 | ① 企业 IT / 运维管理员（管理用户、组织、应用与审计）② 全体员工（登录、改密、绑定 MFA、查看授权记录）③ 接入方研发（按 OIDC 标准接入，无需了解内部实现） |

**核心功能（仅此三项，不多做）**

1. **统一认证**：账号密码 + TOTP MFA，签发标准 OIDC 令牌（authorization code + PKCE，仅 S256）。
2. **单点登录 / 单点登出**：一次登录贯通所有接入系统；登出即时失效服务端会话。
3. **身份与应用管理**：用户、组织架构、OAuth client 的增删改查，以及全量认证审计日志。

---

## 2. 技术栈（锁定版本）

版本一经锁定不得随意升级；升级需单独提交并说明回归验证范围。初始基线如下，实际以 `go.mod` / `package.json` 为准。

### 2.0 第三方依赖的引入原则（2026-10-22 新增）

**优先用成熟轮子，不要自己造。** 一个能力只要存在「周知、高 star、长期维护」的现成实现，就应当引入而不是手写；手写协议/编码类逻辑（MIME 报文、TLS 形态、SMTP 会话、边界拼装、字符编码等）是缺陷高发区，且回归成本远高于依赖成本。

**但引入前必须先征得确认**，流程固定为三步：

1. **提案**：说明要做的事、候选库（模块路径 + 拟锁版本 + star 量级 + 最近提交活跃度 + 许可证），以及"不引库自己写"的代价对比；
2. **确认**：取得用户明确同意后才可写入 `go.mod` / `package.json`；未经确认擅自引入视为缺陷（同"改规范需先改本文件"的约束强度）；
3. **登记**：确认后登记到 §2.1 / §2.2 并锁定**精确版本**（Go 侧 `go get <path>@<version>`，前端 `npm install --save-exact`），同时写清"用它的哪部分、哪部分不用"以及已知短板（例如超时/ctx 不支持）。

准入门槛（不满足需在提案中显式说明例外理由）：

- 许可证为 MIT / Apache-2.0 / BSD；**GPL / AGPL 一律拒绝**；
- 维护活跃：近 12 个月有提交或 issue 响应；star 量级与生态使用面匹配（此类库通常数千 star 起）；
- 不得因引入它而被迫升级 §2.1 / §2.2 已锁定的任何版本（`go mod tidy` 后对比 `go.mod` 差异，出现被动升级即视为不可接受，须改用 `tools/go.mod` 隔离或换方案）；
- 依赖增量可控：不引入 CGO、不引入大型框架、不引入与现有库职责重叠的实现（例如已有 `zitadel/oidc` 就不再引第二个 OIDC 库）。

反向约束仍然有效：**协议实现类**（OIDC/JOSE/密码哈希/TOTP）必须用 §2.1 指定的库，不得因为"另一个库更流行"而替换。

### 2.1 后端

| 组件 | 模块 | 锁定版本 | 用途 |
|---|---|---|---|
| 语言 | Go | `1.26.0` | `go.mod` 的 `go` 指令（2026-10-20 由 `1.24.1` 升级） |
| HTTP 框架 | `github.com/gin-gonic/gin` | `v1.10.0` | 仅管理 API（`:8081`） |
| DI | `go.uber.org/fx` | `v1.23.0` | 生命周期与装配 |
| ORM | `gorm.io/gorm` | `v1.25.12` | 管理类 CRUD |
| MySQL 驱动 | `gorm.io/driver/mysql` | `v1.5.7` | |
| MySQL 错误码 | `github.com/go-sql-driver/mysql` | `v1.7.0` | 组织写事务识别 `1213`（死锁）/`1205`（锁等待超时）后重试；2026-10-21 由间接依赖转为直接依赖，版本未变 |
| OIDC Provider | `github.com/zitadel/oidc/v3` | `v3.28.0` | 协议层（OP），**唯一实现来源** |
| JOSE | `github.com/go-jose/go-jose/v4` | `v4.0.4` | JWS / JWKS，禁止使用 v3 |
| 密码哈希 | `golang.org/x/crypto` | `v0.31.0` | `argon2.IDKey` |
| TOTP | `github.com/pquerna/otp` | `v1.4.0` | MFA |
| 验证码字体 | `golang.org/x/image` | `v0.20.0` | 仅用 `font/basicfont` + `math/fixed` 把图形验证码画成 PNG 位图（2026-10-22 新增；此前用 SVG `<text>` 明文，脚本可直接解码，等于没有防刷） |
| Redis | `github.com/redis/go-redis/v9` | `v9.7.0` | session / 限流 / 缓存 |
| ID | `github.com/oklog/ulid/v2` | `v2.1.0` | 主键 |
| 校验 | `github.com/go-playground/validator/v10` | `v10.23.0` | 请求校验 |
| 文档 UI 资源 | `github.com/swaggo/files` | `v1.0.1` | 内嵌 swagger-ui 静态资源（`/docs/*`），不依赖 CDN（§2.4） |
| 测试 | `github.com/stretchr/testify` | `v1.10.0` | |
| 日志 | `log/slog` | 标准库 | JSON handler |
| 对象存储 | `github.com/aliyun/aliyun-oss-go-sdk/oss` | `v2.2.9` | 图片直传（PostObject 预签名）；2026-10-21 新增（004-object-storage-upload），另带间接依赖 `golang.org/x/time v0.16.0` |
| 邮件发送 | `github.com/jordan-wright/email` | `v4.0.1-0.20210109023952-943e75fe5223+incompatible` | 验证码邮件：`Email` 组装 `multipart/alternative` 报文，`SendWithTLS`（465）/`SendWithStartTLS`（587）/`Send`（其它）负责投递；2026-10-22 新增，替代手拼 boundary + 自建 SMTP 会话。**该库的发送方法不带超时**（内部 `tls.Dial`），故邮件链路的超时依赖 TCP/服务器侧，`Send` 前只做 `ctx.Err()` 预检 |

### 2.2 前端

| 组件 | 模块 | 锁定版本 | 用途 |
|---|---|---|---|
| 框架 | `vue` | `3.5.13` | |
| 构建 | `vite` | `6.3.5` | |
| 语言 | `typescript` | `5.7.2` | `strict: true` |
| 路由 | `vue-router` | `4.5.0` | |
| 状态 | `pinia` | `2.3.0` | |
| 请求 | `axios` | `1.7.9` | `withCredentials: true` |
| 样式（portal） | `tailwindcss` | `4.1.0` | 登录页 / 门户 |
| 组件库（admin） | `element-plus` | `2.9.1` | 表格 / 树 / 表单 |
| 图表 | `echarts` + `vue-echarts` | `5.5.1` / `7.0.3` | 审计统计 |
| 二维码 | `qrcode` | `1.5.4` | 两步验证绑定二维码（浏览器 canvas 渲染 `otpauth://`，2026-10-22 新增） |

**前端质量工具（devDependencies，2026-10-22 新增）**

| 组件 | 模块 | 锁定版本 | 用途 |
|---|---|---|---|
| Linter | `eslint` | `10.13.0` | 扁平配置 `eslint.config.js`，两套前端各自 `npm run lint` |
| Vue 规则 | `eslint-plugin-vue` | `10.11.1` | `flat/essential` |
| TS 规则 | `typescript-eslint` | `8.72.0` | 类型感知关闭，仅语法/未使用变量等硬规则 |
| Vue+TS 预设 | `@vue/eslint-config-typescript` | `14.9.0` | 组合上述两套 |
| 全局变量 | `globals` | `17.13.0` | `browser` 全局声明 |
| 二维码类型 | `@types/qrcode` | `1.5.6` | `qrcode` 的 TS 类型 |
| 测试运行器 | `vitest` | `3.2.7` | portal 首屏用例（登录方式切换、图形验证码、验证码登录失败提示），接入 `make test`（2026-10-22 新增） |
| 组件测试 | `@vue/test-utils` | `2.5.1` | 挂载 `Login.vue` 等组件做交互断言（2026-10-22 新增） |
| DOM 环境 | `jsdom` | `25.0.1` | vitest 的 `environment: 'jsdom'`（2026-10-22 新增） |

> 上述前端依赖走 `devDependencies`/`dependencies` 的**精确版本**（`npm install --save-exact`），与 §2.1 的锁定策略一致；升级须单独提交。

### 2.3 基础设施

| 组件 | 版本 | 说明 |
|---|---|---|
| MySQL | `8.4` LTS | 字符集 `utf8mb4`，排序规则按 §6 规定 |
| Redis | `7.4` | 单实例 + AOF，不使用 Cluster |
| 阿里云 OSS | — | 图片对象存储（logo/头像直传）；provider 驱动配置，未配置则不启用 |
| Docker / Compose | `27.x` / `v2` | 本地与 MVP 部署 |

### 2.4 接口文档生成链（构建期依赖）

**不进入请求路径**，仅在 `make docs` / `make docs-check` 时使用；两个依赖均为 MIT 许可。

**独立模块约束**：这两个依赖放在 `tools/go.mod`（独立 module），**不得**加入根 `go.mod`。原因：实测把它们引入根模块会强制升级 `golang.org/x/crypto` `v0.31.0 → v0.36.0`、`x/net`、`x/sys`、`x/text`，直接违反 §2.1 的版本锁定。构建期工具与运行时依赖必须物理隔离，主模块 `go.mod` 因本特性**零改动**。

| 组件 | 模块 | 锁定版本 | 许可证 | 用途 |
|---|---|---|---|---|
| 注解解析 | `github.com/swaggo/swag` | `v1.16.6` | MIT | 从 handler 注释生成 Swagger 2.0（**该库不产出 OpenAPI 3**，见下） |
| 规范转换 | `github.com/getkin/kin-openapi` | `v0.135.0` | MIT | `openapi2conv.ToV3` 做 2.0→3.0 转换、`openapi3` 做漂移比对 |

**文档 UI 的归属（与生成链不同）**：swagger-ui 的静态资源由 `github.com/swaggo/files` `v1.0.1` 提供，它随二进制发布、在 `/docs/*` 上被浏览器直接请求，属**请求路径**依赖，因此登记在 §2.1 并加入根 `go.mod`（它只要求 `x/net` `v0.7.0`，不触发升级）。**不使用** `gin-swagger`：文档页 HTML 由 `internal/api/docs` 自写（约 20 行），以确保只引用同源内嵌资源、且 OpenAPI 地址固定为 `./openapi.json`。

**锁定理由（不得随意改动）**

- `kin-openapi` 暂仍停在 `v0.135.0`：它当初被锁定的原因是 `v0.136.0` 起要求 `go >= 1.25`，而项目当时为 `go 1.24.1`。Go 已于 2026-10-20 升到 `1.26.0`，该天花板不再成立；但升级 kin-openapi 需单独提交并完整回归文档生成链（`make docs` / `make docs-check` 的转换与漂移比对），故本次不动版本号。`v0.135.0` 在 Go 1.26 下可正常构建。
- `swaggo/swag` 仅产出 Swagger 2.0（其 OpenAPI 3 支持自 2019 年起一直是未合入的提案），因此**转换步骤不可省略**，禁止为了"少一个依赖"而直接交付 2.0。
- swag CLI 作为 `tools/go.mod` 的 require 引入，命令行一律在 `tools/` 目录内执行 `go run github.com/swaggo/swag/cmd/swag`，**禁止写 `@version`**（会绕过 `tools/go.sum` 锁定并要求联网）。

---

## 3. 目录结构

```
omino-sso/
├── AGENTS.md                          # 本文件
├── 技术选型报告.md
├── go.mod
├── Makefile
├── docker-compose.yml                 # mysql + redis + sso + portal + admin
├── Dockerfile                         # 2026-10-22 新增：服务端多阶段镜像，必须 COPY configs/verify-mail.tmpl
├── .dockerignore                      # 2026-10-22 新增：构建上下文裁剪（排除 web/、specs/、.env）
├── .env.example                       # 环境变量模板（§9）
├── .tmp/                              # 文档生成中间产物（gitignore，不入库）
├── scripts/                           # 交付前自检（§7.4，2026-10-22 新增）
│   ├── check-registry.sh              # 路由 / 错误码 / 审计事件 / 环境变量 四向一致性
│   ├── check-docs-commands.sh         # 文档中 `make <target>` 的可执行性
│   └── ci-local.sh                    # 临时 worktree 复刻干净检出跑全套门禁
├── tools/                             # 构建期工具，独立 module（禁止被业务包 import）
│   ├── go.mod                         # 独立依赖图，隔离到这份，根 go.mod 不受影响
│   └── openapi3/
│       ├── main.go                    # Swagger 2.0 → OpenAPI 3.0.3 转换 + -check 漂移比对
│       └── main_test.go
├── cmd/
│   └── sso/
│       └── main.go                    # fx.New 入口，仅装配，无业务
├── internal/
│   ├── config/
│   │   ├── config.go                  # Config 结构体 + 加载/校验
│   │   └── config_test.go
│   ├── oidc/
│   │   ├── module.go                  # fx.Module("oidc")
│   │   ├── provider.go                # newProvider: op.NewProvider 包装（变长参数）
│   │   ├── storage.go                 # op.Storage 完整实现（唯一适配点）
│   │   ├── login.go                   # 登录闭环：AuthRequest 回写 sub/amr/auth_time
│   │   ├── claims.go                  # SetUserinfoFromScopes / GetPrivateClaimsFromScopes
│   │   ├── keys.go                    # 签名密钥加载与 kid 轮换
│   │   └── server.go                  # 独立 http.Server(:8080)，挂 op.Provider
│   ├── api/
│   │   ├── module.go                  # fx.Module("api")
│   │   ├── router.go                  # Gin 路由注册 + 中间件顺序
│   │   ├── middleware/
│   │   │   ├── auth.go                # 从 cookie sid 解析会话
│   │   │   ├── ratelimit.go           # Redis 令牌桶
│   │   │   ├── trace.go               # traceId 生成与透传
│   │   │   └── recover.go
│   │   ├── response/
│   │   │   ├── response.go            # 统一响应封装（§5）
│   │   │   └── errors.go              # 错误码枚举与 HTTP 映射
│   │   ├── docs/                      # 接口文档（非 prod 可达；§4.0 豁免统一响应）
│   │   │   ├── docs.go                # //go:embed 产物 + 运行时注入环境/版本 + gin handler
│   │   │   ├── openapi.json           # 生成物，入库（OpenAPI 3.0.3）
│   │   │   └── docs_test.go
│   │   ├── auth/                      # /api/v1/auth/*
│   │   │   ├── login.go               # 密码 / 验证码登录、失败计数、锁定
│   │   │   ├── code.go                # 005：验证码发送（未登录强制图形码；已登录目标由服务端解析）
│   │   │   ├── captcha.go             # 005：图形验证码签发（GET /auth/captcha）
│   │   │   ├── password.go            # 005：验证码找回密码
│   │   │   ├── mfa.go                 # TOTP 校验、绑定/确认
│   │   │   └── logout.go
│   │   ├── me/                        # /api/v1/me/*
│   │   │   ├── profile.go
│   │   │   ├── password.go
│   │   │   ├── contact.go             # 005：绑定/修改手机号与邮箱（POST /me/phone、/me/email）
│   │   │   ├── mfa.go                 # 005：MFA 绑定 / 确认 / 解绑（/me/mfa/*）
│   │   │   ├── apps.go
│   │   │   └── sessions.go
│   │   └── admin/                     # /api/v1/admin/*
│   │       ├── users.go
│   │       ├── orgs.go
│   │       ├── clients.go
│   │       ├── audit.go
│   │       └── send_records.go        # 005：发送记录分页查询
│   ├── service/                       # 业务规则层（不依赖 gin/gorm）
│   │   ├── user.go                    # 含标识符识别 / 联系方式绑定 / 验证码重置密码
│   │   ├── mfa.go                     # 005：MFA 绑定与解绑
│   │   ├── cleanup.go                 # 005：内置定时清理（发送记录 / 审计日志）
│   │   ├── captcha.go                 # 005：图形验证码（Redis 短时 + 一次性消费 + PNG 位图，响应不含明文码）
│   │   ├── captcha_test.go            # 005：图形验证码单测（假 Redis 实现，无需外部依赖）
│   │   ├── client.go
│   │   ├── session.go
│   │   ├── audit.go                   # 含 notification.AuditSink 实现
│   │   └── notification/              # 005：验证码与发送通道抽象
│   │       ├── provider.go            # Provider 接口 / 场景渠道枚举 / 脱敏
│   │       ├── chuanglan.go           # 创蓝短信（HTTP，3s 超时）
│   │       ├── smtp.go                # SMTP 邮件（模板文件 + multipart/alternative）
│   │       ├── mock.go                # 模拟发送（固定 888888）
│   │       └── service.go             # 签发 / 校验 / 发送记录 / 审计
│   ├── store/
│   │   ├── module.go                  # fx.Module("store")，Provide 接口绑定
│   │   ├── db.go                      # gorm.Open + 连接池 + 健康检查
│   │   ├── tx.go                      # tx-in-context（§7 约束 3）
│   │   ├── model/
│   │   │   ├── user.go
│   │   │   ├── organization.go
│   │   │   ├── client.go
│   │   │   ├── auth_code.go
│   │   │   ├── refresh_token.go
│   │   │   ├── session.go
│   │   │   ├── consent.go
│   │   │   ├── send_record.go         # 005
│   │   │   └── audit_event.go
│   │   ├── user_store.go
│   │   ├── send_record_store.go       # 005：发送记录 CRUD + 分批清理
│   │   ├── client_store.go
│   │   ├── code_store.go              # 原子 UPDATE 消费授权码
│   │   ├── token_store.go             # 刷新轮换 + family 吊销
│   │   ├── session_store.go           # Redis
│   │   └── audit_store.go
│   └── pkg/
│       ├── crypto/
│       │   ├── password.go            # argon2id 编码/校验（常量时间）
│       │   ├── random.go              # 256bit 随机串
│       │   └── hash.go                # SHA-256 -> []byte(32)
│       ├── id/
│       │   └── ulid.go
│       ├── clock/
│       │   └── clock.go               # 接口化，便于测试
│       └── log/
│           └── log.go
├── configs/
│   ├── dev-signing-key.pem            # 开发用签名私钥（不入仓库）
│   └── verify-mail.tmpl               # 005：验证码邮件模板（html/template）
├── migrations/
│   ├── 0001_init.up.sql
│   ├── 0001_init.down.sql
│   ├── 0005_verification_auth.up.sql  # 005：send_records + uk_users_phone
│   └── README.md                      # 手写 SQL，禁止 AutoMigrate
└── web/
    ├── portal/                        # 登录页 + 账号中心（Tailwind）
    │   ├── eslint.config.js
    │   ├── vitest.config.ts           # 2026-10-22 新增：jsdom + @ 别名，`npm run test`
    │   ├── index.html
    │   ├── vite.config.ts
    │   ├── src/main.ts
    │   ├── src/router.ts
    │   ├── src/api/client.ts
    │   ├── src/views/Login.vue        # 账号密码 / 验证码登录 + 图形验证码 + MFA 二步
    │   ├── src/views/Login.spec.ts    # 2026-10-22 新增：登录方式切换 / 图形码 / 失败提示用例
    │   ├── src/views/Password.vue     # 修改密码：原密码与验证码两种方式（§4.1 的 me/password*）
    │   ├── src/views/Account.vue      # 账号中心：联系方式、MFA、会话
    │   ├── src/views/Apps.vue
    │   └── src/styles/tokens.css      # 品牌变量（主色 / 圆角 / 字体）
    └── admin/                         # 管理后台（Element Plus）
        ├── eslint.config.js
        ├── index.html
        ├── vite.config.ts
        ├── src/main.ts
        ├── src/router.ts
        ├── src/layout/AppLayout.vue
        ├── src/views/Users.vue        # 左组织树 + 右用户表
        ├── src/views/Clients.vue
        ├── src/views/AuditLogs.vue
        ├── src/views/SendRecords.vue  # 005 发送记录
        └── src/views/Settings.vue
```

**分层规则**：`api → service → store`，禁止跨层调用（api 不得直接用 store）；`oidc` 只能通过 `service`/`store` 访问数据，不得直接写 SQL。

---

## 4. API 接口定义

### 4.0 端口与路径划分

| 服务 | 地址 | 路径前缀 | 响应格式 |
|---|---|---|---|
| IdP（`net/http`） | `:8080` | `/oauth2/*`、`/.well-known/*`、`/login`、`/logout` | **RFC 原生格式**，禁止套统一响应 |
| 管理 API（Gin） | `:8081` | `/api/v1/*` | 统一响应（§5） |
| 接口文档（Gin，仅非 prod） | `:8081` | `/docs/*` | **原始 HTML / JSON / 静态字节**，禁止套统一响应 |

> `/oauth2/*` 与 `/.well-known/*` 的报文由 `zitadel/oidc/v3` 生成，**不得包装、不得改写字段名**。

**`/docs/*` 的三条硬性约定**

1. **豁免统一响应**：文档页与 `openapi.json` 必须返回浏览器与 OpenAPI 工具链可直接消费的原始内容；套 `{code,message,data,traceId}` 会使 swagger-ui 与任何规范消费方无法解析。属与 `/oauth2/*` 同类的"非业务面"豁免。
2. **仅非 prod 注册**：`APP_ENV=prod` 时该路由**不得注册**，请求须落入 `NoRoute` 分支，响应与任意不存在路径完全一致（不得返回 403 或"已禁用"提示）。环境判定只在 `internal/api/router.go` 一处。
3. **不挂限流**：`/docs/*` 必须跳过全局限流中间件，使文档页不依赖 Redis 可用性（运维故障时正是最需要查文档的时刻）。

### 4.1 接口总览

| 方法 | 路径 | 说明 | 鉴权 |
|---|---|---|---|
| GET | `/oauth2/authorize` | 授权端点 | session |
| POST | `/oauth2/token` | 令牌端点（code / refresh_token） | client |
| GET | `/oauth2/userinfo` | 用户信息 | Bearer |
| GET | `/oauth2/jwks` | 公钥集 | 无 |
| POST | `/oauth2/revoke` | 撤销令牌 | client |
| POST | `/oauth2/introspect` | 令牌自省 | client |
| GET | `/oauth2/end_session` | 登出 | session |
| GET | `/.well-known/openid-configuration` | 元数据 | 无 |
| POST | `/api/v1/auth/login` | 账号登录（密码 / 邮箱验证码 / 手机验证码） | 无 |
| POST | `/api/v1/auth/mfa/verify` | MFA 二次校验 | mfaTicket |
| GET | `/api/v1/auth/captcha` | 获取图形验证码（未登录验证码登录 / 找回密码的前置） | 无 |
| POST | `/api/v1/auth/code` | 发送短信/邮箱验证码 | 无（bind 类场景需 session） |
| POST | `/api/v1/auth/password/reset` | 找回（重置）密码 | 无 |
| POST | `/api/v1/auth/logout` | 登出 | session |
| GET | `/api/v1/me` | 当前用户 | session |
| GET | `/api/v1/me/apps` | 我可访问的应用 | session |
| POST | `/api/v1/me/password` | 修改密码 | session |
| POST | `/api/v1/me/password/reset` | 验证码重置密码（吊销其他设备会话） | session |
| POST | `/api/v1/me/phone` | 绑定/修改手机号 | session |
| POST | `/api/v1/me/email` | 绑定/修改邮箱 | session |
| POST | `/api/v1/me/mfa/bind` | 启动 MFA 绑定（返回 otpauth URL） | session |
| POST | `/api/v1/me/mfa/bind/confirm` | 确认 MFA 绑定 | session |
| POST | `/api/v1/me/mfa/unbind` | 验证码解绑 MFA | session |
| GET | `/api/v1/me/sessions` | 登录设备列表 | session |
| DELETE | `/api/v1/me/sessions/:sid` | 撤销会话 | session |
| GET | `/api/v1/admin/users` | 用户列表 | admin |
| POST | `/api/v1/admin/users` | 创建用户 | admin |
| PATCH | `/api/v1/admin/users/:id` | 更新用户 | admin |
| GET | `/api/v1/admin/orgs/tree` | 组织树 | admin |
| POST | `/api/v1/admin/orgs` | 创建组织节点 | admin |
| PATCH | `/api/v1/admin/orgs/:id` | 更新组织节点（改名 / 调序 / 移动） | admin |
| DELETE | `/api/v1/admin/orgs/:id` | 删除组织节点 | admin |
| GET | `/api/v1/admin/clients` | 应用列表 | admin |
| POST | `/api/v1/admin/clients` | 注册应用 | admin |
| POST | `/api/v1/admin/clients/:id/secret` | 轮换密钥 | admin |
| GET | `/api/v1/admin/audit-logs` | 审计日志 | admin |
| GET | `/api/v1/admin/send-records` | 短信/邮箱发送记录 | admin |
| POST | `/api/v1/me/avatar` | 更新本人头像（004） | session |
| DELETE | `/api/v1/me/avatar` | 清除本人头像（004） | session |
| POST | `/api/v1/files/upload-credential` | 获取 OSS 直传凭证（004） | session |
| POST | `/api/v1/files/confirm` | 确认上传并登记文件（004） | session |
| POST | `/api/v1/admin/clients/:id/logo` | 设置应用 Logo（004） | admin |
| DELETE | `/api/v1/admin/clients/:id/logo` | 清除应用 Logo（004） | admin |
| POST | `/api/v1/admin/users/:id/avatar` | 设置用户头像（004） | admin |
| DELETE | `/api/v1/admin/users/:id/avatar` | 清除用户头像（004） | admin |
| GET | `/api/v1/admin/files` | 文件列表（004） | admin |
| PATCH | `/api/v1/admin/files/:id` | 更新文件状态（004） | admin |

> **`PATCH /api/v1/admin/users/:id` 的行为修正**：提交 `deptId` 时，服务端在短事务内锁定目标部门并校验其存在，不存在返回 `400 VALIDATION_FAILED`（过去为静默写入，会与部门删除竞态产生悬空 `dept_id`）。`POST /api/v1/admin/users` 的 `deptId` 校验方式同步调整为同一临界区，对外行为不变。
>
> **组织写操作的并发语义**：三个组织写端点在同一事务内先对 `organizations` 全表加一次 `SELECT ... FOR UPDATE`（该表无 `tenant_id` 索引，实际为全局串行化），再读、校验、写；账号归属写入侧对目标部门行加锁。因此同级唯一性、10 级深度、成环、删除保护不依赖数据库唯一索引即可成立。
>
> **图形验证码（2026-10-22）**：未登录调用 `/api/v1/auth/code` 的 `login`、`find_password` 场景与 `/api/v1/auth/login` 的 `email_code`、`sms_code` 类型时，必须携带 `captchaId` + `captchaCode`（先调 `GET /api/v1/auth/captcha`，2 分钟有效、一次有效），校验失败返回 `400 AUTH_CAPTCHA_INVALID`。已登录会话一律不校验图形码。图片为 PNG 位图 data URL，**禁止**以 SVG/文本节点等可被脚本直接解码的形式下发；因图形码一次性消费，前端在发送成功或提交失败后必须立即换图。

> **`login` 场景未绑定目标改为显式报错（2026-10-22 行为修正）**：过去 `/api/v1/auth/code` 在 `login` 场景遇到"该手机号/邮箱不属于任何账号"时返回与成功完全一致的响应但不发信（防枚举），结果用户只看到"验证码已发送"却永远收不到，且 `send_records` 也无记录，无法自诊断。现改为返回 `404 VERIFY_TARGET_NOT_FOUND`，代价是该场景可被用于探测注册状态；判定为可接受——登录页本身就要以"账号是否存在"回显错误提示，静默成功只增加支持成本。**`find_password` 场景保持静默成功**（找回密码流程不暴露注册状态），两处差异不得被"顺手统一"抹掉。
>
> **联系方式出服务端的口径（2026-10-22）**：`GET /api/v1/me` 与各绑定接口的响应只返回脱敏值（`138****8000` / `z***@example.com`），前端不得二次脱敏；`/api/v1/auth/code` 在已登录的 `mfa_unbind`、`find_password` 场景忽略请求体 `target`，由服务端按会话解析本人已绑定的联系方式，浏览器不持有完整值。

---

### 4.2 `POST /oauth2/token`（核心接口 1）

`Content-Type: application/x-www-form-urlencoded`，**不接受 JSON**。
客户端认证：`Authorization: Basic base64(client_id:client_secret)`，或表单内 `client_id` + `client_secret`。

**请求字段（grant_type=authorization_code）**

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `grant_type` | string | 是 | 固定 `authorization_code` |
| `code` | string | 是 | 授权码明文，服务端按 SHA-256 比对 |
| `redirect_uri` | string | 是 | 必须与授权请求中的值**完全一致** |
| `client_id` | string | 是 | 大小写敏感精确匹配 |
| `code_verifier` | string | 是 | 43–128 字符，`[A-Za-z0-9-._~]`，S256 校验 |
| `client_secret` | string | 否 | 与 Basic 二选一 |

**请求字段（grant_type=refresh_token）**

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `grant_type` | string | 是 | 固定 `refresh_token` |
| `refresh_token` | string | 是 | 旧 token；使用后立即失效并签发新 token |
| `client_id` | string | 是 | |
| `scope` | string | 否 | 只能是原 scope 的子集 |

**响应 200**

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IjIwMjYtMDEifQ...",
  "token_type": "Bearer",
  "expires_in": 900,
  "refresh_token": "rt_9f2b8c1e4d7a...",
  "scope": "openid profile email",
  "id_token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IjIwMjYtMDEifQ..."
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `access_token` | string | JWT，RS256，含 `iss/aud/exp/iat/sub/scope/tenant` |
| `token_type` | string | 固定 `Bearer` |
| `expires_in` | number | 秒，默认 900 |
| `refresh_token` | string | 不透明串，仅 `authorization_code` 与首次 `refresh_token` 返回 |
| `scope` | string | 空格分隔，实际授予的 scope |
| `id_token` | string | 仅当 scope 含 `openid` 时返回；含 `nonce`、`auth_time` |

**错误响应（RFC 6749 §5.2，无统一包装）**

```json
{
  "error": "invalid_grant",
  "error_description": "authorization code is expired or already used"
}
```

| HTTP | `error` | 触发条件 |
|---|---|---|
| 400 | `invalid_request` | 缺参、参数重复、Content-Type 非 form |
| 400 | `invalid_grant` | code 过期/已用/redirect_uri 不匹配/code_verifier 校验失败/refresh 重放 |
| 400 | `unsupported_grant_type` | grant_type 非白名单 |
| 400 | `invalid_scope` | scope 越权 |
| 401 | `invalid_client` | 客户端认证失败（`WWW-Authenticate: Basic`） |

---

### 4.3 `POST /api/v1/auth/login`（核心接口 2）

**请求体**

```json
{
  "username": "zhangsan",
  "password": "S3cure!pass",
  "captchaId": "01J9ZK2Q3W5X7Y9A1B3C5D7E9F",
  "captchaCode": "8f3k",
  "clientId": "portal",
  "rememberMe": false
}
```

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `loginType` | string | 否 | `password`（默认）/ `email_code` / `sms_code` |
| `identifier` | string | 否 | 用户名 / 邮箱 / 手机号，服务端按正则识别；`password` 方式下与 `username` 二选一 |
| `username` | string | 否 | 兼容旧调用；`identifier` 为空时使用 |
| `password` | string | 否 | `loginType=password` 时必填，8–128 |
| `code` | string | 否 | `loginType=email_code/sms_code` 时必填，6 位 |
| `captchaId` | string | 否 | 连续失败 ≥ 3 次后必填 |
| `captchaCode` | string | 否 | 4 位，不区分大小写 |
| `clientId` | string | 是 | 默认 `portal` |
| `rememberMe` | bool | 否 | true 时会话 7 天，否则 8 小时 |

> **多方式登录语义**：`password` 方式按 `^1\d{10}$` 判手机号、含 `@` 判邮箱、其余判用户名，各自命中唯一索引后走既有密码校验与账号锁定策略；`email_code` / `sms_code` 方式校验一次性验证码（scene=`login`），成功后**不触发** TOTP 二步（渠道验证已证明归属），`amr` 记 `email_otp` / `sms_otp`。

**响应 200（需二次 MFA）**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "mfaRequired": true,
    "mfaTicket": "mft_5c1e9a7b3d2f",
    "expiresIn": 300
  },
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

**响应 200（登录完成）**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "mfaRequired": false,
    "mfaTicket": null,
    "expiresIn": 0,
    "redirectTo": "/account",
    "user": {
      "id": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
      "username": "zhangsan",
      "displayName": "张三"
    }
  },
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

> 成功后由服务端下发 `Set-Cookie: sid=<opaque>; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=...`。**响应体任何时候都不包含 access_token / refresh_token。**

**错误码**

| HTTP | code | 场景 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 字段缺失或格式错误 |
| 401 | `AUTH_INVALID_CREDENTIALS` | 用户名或密码错误（不区分是哪个错） |
| 400 | `AUTH_CAPTCHA_INVALID` | 验证码错误/过期 |
| 423 | `AUTH_ACCOUNT_LOCKED` | 连续失败 5 次锁定 15 分钟 |
| 403 | `AUTH_ACCOUNT_DISABLED` | 账号被禁用 |
| 429 | `RATE_LIMITED` | 超出限流 |

---

### 4.4 `POST /api/v1/admin/users`（核心接口 3）

**请求体**（`Authorization` 由 session cookie 承载，需 admin 角色）

```json
{
  "username": "lisi",
  "displayName": "李四",
  "email": "lisi@example.com",
  "phone": "13800000000",
  "password": "Init!pass2026",
  "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
  "tenantId": "default",
  "roleIds": ["user"],
  "mfaRequired": false,
  "sendNotify": false
}
```

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `username` | string | 是 | 3–64，`^[a-zA-Z][a-zA-Z0-9._-]{2,63}$`，全局唯一（大小写敏感） |
| `displayName` | string | 是 | 1–64 |
| `email` | string | 否 | 合法邮箱，全局唯一 |
| `phone` | string | 否 | `^1[3-9]\d{9}$` |
| `password` | string | 是 | 8–128，至少含字母与数字 |
| `deptId` | string | 是 | 必须存在 |
| `tenantId` | string | 否 | 默认 `default` |
| `roleIds` | string[] | 否 | 必须是已有角色，默认 `["user"]` |
| `mfaRequired` | bool | 否 | 是否强制 MFA，默认 false |
| `sendNotify` | bool | 否 | 本阶段恒为 false（不发通知，见 §7 非目标） |

**响应 201**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "id": "01J9ZK5B9C1D3E5F7G9H1J3K5M",
    "username": "lisi",
    "displayName": "李四",
    "email": "lisi@example.com",
    "phone": "13800000000",
    "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
    "tenantId": "default",
    "status": "active",
    "mfaEnabled": false,
    "mfaRequired": false,
    "roles": ["user"],
    "createdAt": "2026-10-20T10:12:33Z",
    "updatedAt": "2026-10-20T10:12:33Z"
  },
  "traceId": "01J9ZK5BA7B9C1D3E5F7G9H1J3"
}
```

**错误码**：`VALIDATION_FAILED`(400)、`USERNAME_ALREADY_EXISTS`(409)、`EMAIL_ALREADY_EXISTS`(409)、`DEPT_NOT_FOUND`(400)、`FORBIDDEN`(403)。

---

### 4.5 `GET /api/v1/me`（核心接口 4）

**请求**：无 body；cookie `sid` 必需。

**响应 200**

```json
{
  "code": "OK",
  "message": "success",
  "data": {
    "id": "01J9ZJ8Q2M4N6P8R0T2V4X6Z8A",
    "username": "zhangsan",
    "displayName": "张三",
    "email": "zhangsan@example.com",
    "phone": "13800000001",
    "avatarUrl": "https://cdn.example.com/avatar/zs.png",
    "deptId": "01J9ZJ7A1B2C3D4E5F6G7H8J9K",
    "deptPath": ["总公司", "技术中心", "平台组"],
    "tenantId": "default",
    "roles": ["user", "admin"],
    "mfaEnabled": true,
    "mfaRequired": false,
    "lastLoginAt": "2026-10-20T09:00:00Z",
    "createdAt": "2026-01-05T02:11:00Z"
  },
  "traceId": "01J9ZK6C1D3E5F7G9H1J3K5M7P"
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `email` | string \| null | **脱敏返回**（`z***@example.com`），未绑定为 `null` |
| `phone` | string \| null | **脱敏返回**（`138****8000`），未绑定为 `null` |
| `deptPath` | string[] | 从根到当前部门的名称路径 |
| `mfaEnabled` | bool | 用户是否已绑定 TOTP |
| `mfaRequired` | bool | 是否被管理员强制 MFA |
| `lastLoginAt` | string(RFC3339) | 可能为 `null` |

---

### 4.6 其余接口约定（字段从略，实现时必须与此一致）

| 路径 | 关键请求字段 | 关键响应字段 |
|---|---|---|
| `POST /api/v1/auth/mfa/verify` | `mfaTicket`, `code`(6 位) | `redirectTo`, `user{id,username,displayName}` |
| `POST /api/v1/me/password` | `oldPassword`, `newPassword` | `data: null` |
| `GET /api/v1/me/apps` | — | `[{clientId,name,description,logoUrl,homeUrl,lastAccessAt}]` |
| `GET /api/v1/me/sessions` | — | `[{sid,ip,userAgent,createdAt,lastSeenAt,current}]` |
| `GET /api/v1/admin/users` | `keyword`,`deptId`,`status`,`page`(默认1),`pageSize`(默认20,最大100) | `{list:[...],total,page,pageSize}` |
| `GET /api/v1/admin/orgs/tree` | — | `[{id,name,parentId,path,userCount,children[]}]` |
| `POST /api/v1/admin/orgs` | `parentId`(可空=根级), `name`(1–64，同级唯一), `sort`(可选，默认0) | `201` + `{id,parentId,name,path,sort,tenantId,userCount,createdAt}` |
| `PATCH /api/v1/admin/orgs/:id` | `name` / `sort`（缺失=不变）、`parentId`（三态：缺失=不变 / `null`=移到根级 / 值=移到该节点下）；移动整棵子树，`sort` 不被改写 | `{id,parentId,name,path,sort,tenantId,userCount,createdAt}`，`userCount` 为子树汇总 |
| `DELETE /api/v1/admin/orgs/:id` | — | `data: null` |
| `GET /api/v1/admin/clients` | `keyword`,`page`,`pageSize`,`hasMembers`(`true`/`false`) | `{list:[{clientId,clientName,isPublic,redirectUris,grantTypes,scopes,createdAt,memberCount}],total,page,pageSize}` |
| `POST /api/v1/admin/clients` | `clientName`,`redirectUris[]`,`grantTypes[]`,`scopes[]`,`isPublic` | `{clientId,clientSecret}`（secret **仅此一次返回**，不落日志） |
| `POST /api/v1/admin/clients/:id/secret` | `gracePeriodSeconds`(默认0) | `{clientId,clientSecret,expiresAt}` |
| `GET /api/v1/admin/clients/:id` | — | `{clientId,clientName,description,logoUrl,redirectUris,postLogoutUris,grantTypes,scopes,isPublic,accessTokenTtl,refreshTokenTtl,status,memberCount,createdAt,updatedAt}`；**不含密钥** |
| `PATCH /api/v1/admin/clients/:id` | 8 个可编辑字段（缺失=不变）+ `updatedAt`(可选版本) | 同 `GET /api/v1/admin/clients/:id`；版本不符 `409 APP_RESOURCE_STALE` |
| `GET /api/v1/admin/clients/:id/members` | `keyword`,`deptId`,`page`,`pageSize`(默认20,最大200),`all` | `{list:[{userId,username,displayName,deptId,deptName,status,joinedAt,roleCount}],total,page,pageSize}`；`all=true` 返回 `{userIds[]}`，上限 5000 |
| `POST /api/v1/admin/clients/:id/members` | `userIds[]`(1–500) **或** `deptId`+`includeSubDepts` | `{added,skipped}` 或 `{added,existing,disabled}` |
| `POST /api/v1/admin/clients/:id/members/remove` | `userIds[]`(1–500) | `{removed,roleBindingsRemoved,notMember}` |
| `GET /api/v1/admin/clients/:id/members/candidates` | `keyword`,`deptId`,`page`,`pageSize`(最大50),`all` | `{list:[{userId,username,displayName,deptId,deptName}],total}` |
| `GET/POST /api/v1/admin/clients/:id/roles` | `name`(`^[A-Za-z0-9]{1,32}$`)、`description`(≤100) | `{list:[{id,clientId,name,description,permissionCount,memberCount,createdAt,updatedAt}],total}` / `201` |
| `GET/PATCH/DELETE /api/v1/admin/clients/:id/roles/:rid` | `name`/`description`/`updatedAt` | `{id,clientId,name,description,permissionIds[],memberIds[],createdAt,updatedAt}` |
| `PUT /api/v1/admin/clients/:id/roles/:rid/permissions` | `permissionIds[]`(≤1000)、`updatedAt` | `{permissionIds[],added,removed}` |
| `PUT /api/v1/admin/clients/:id/roles/:rid/members` | `userIds[]`(≤5000)、`updatedAt` | `{memberIds[],added,removed,autoJoinedMembers}` |
| `GET/POST /api/v1/admin/clients/:id/permissions` | `code`(`^[A-Za-z0-9_.:-]{1,64}$`)、`name`(1–64)、`parentId`、`sort` | `{list:[{id,parentId,code,name,description,sort,childCount,roleCount,createdAt,updatedAt}],total}` / `201` |
| `PATCH/DELETE /api/v1/admin/clients/:id/permissions/:pid` | `code`/`name`/`description`/`parentId`/`sort`/`updatedAt` | 同上；含子权限删除 `409 APP_PERMISSION_HAS_CHILDREN` |
| `GET /api/v1/admin/audit-logs` | `userId`,`clientId`,`eventType`,`from`,`to`,`result`,`page`,`pageSize` | `{list:[{id,eventType,userId,username,clientId,ip,userAgent,result,detail,createdAt}],total}` |

**分页参数统一**：`page` 从 1 开始，`pageSize` 最大 100，超出则 `VALIDATION_FAILED`。

---

## 5. 统一响应格式

适用于 `/api/v1/**`。`/oauth2/**` 与 `/.well-known/**` **不适用**（RFC 原生格式）。

### 5.1 成功模板

```json
{
  "code": "OK",
  "message": "success",
  "data": {},
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `code` | string | 是 | 成功恒为 `"OK"` |
| `message` | string | 是 | 成功恒为 `"success"` |
| `data` | object \| array \| null | 是 | 载荷；无返回内容时为 `null`，**不得省略该字段** |
| `traceId` | string | 是 | ULID，同时写入响应头 `X-Trace-Id` 与日志 |

- HTTP 状态码：创建返回 `201`，其余成功 `200`，删除成功 `200`（返回 `data: null`）。
- 分页载荷统一放在 `data` 内：`{ "list": [], "total": 0, "page": 1, "pageSize": 20 }`。

### 5.2 错误模板

```json
{
  "code": "AUTH_INVALID_CREDENTIALS",
  "message": "用户名或密码错误",
  "data": null,
  "traceId": "01J9ZK3A7B5C7D9E1F3G5H7J9K"
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `code` | string | 大写下划线错误码，见下表 |
| `message` | string | 面向人的中文提示，**不得泄露内部细节/堆栈/SQL** |
| `data` | null | 错误时恒为 `null` |
| `traceId` | string | 与日志、审计可关联 |

### 5.3 错误码表

| HTTP | code | 含义 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 参数校验失败 |
| 401 | `UNAUTHENTICATED` | 未登录或会话失效 |
| 401 | `AUTH_INVALID_CREDENTIALS` | 账号密码错误 |
| 400 | `AUTH_CAPTCHA_INVALID` | 验证码错误 |
| 400 | `AUTH_MFA_INVALID` | MFA 校验码错误 |
| 400 | `AUTH_MFA_TICKET_EXPIRED` | MFA 票据过期 |
| 423 | `AUTH_ACCOUNT_LOCKED` | 账号锁定 |
| 403 | `AUTH_ACCOUNT_DISABLED` | 账号禁用 |
| 403 | `FORBIDDEN` | 无权限（非 admin 访问管理接口） |
| 404 | `NOT_FOUND` | 资源不存在 |
| 409 | `USERNAME_ALREADY_EXISTS` | 用户名冲突 |
| 409 | `EMAIL_ALREADY_EXISTS` | 邮箱冲突 |
| 409 | `CLIENT_ALREADY_EXISTS` | client_id 冲突 |
| 400 | `WEAK_PASSWORD` | 密码不满足策略 |
| 409 | `ORG_NAME_ALREADY_EXISTS` | 同租户 + 同父节点下已存在同名部门（区分大小写） |
| 409 | `ORG_HAS_CHILDREN` | 部门下仍有子部门，不允许删除 |
| 409 | `ORG_HAS_USERS` | 部门下仍有账号归属，不允许删除 |
| 400 | `ORG_INVALID_PARENT` | 上级部门不存在 / 为自身或自身后代 / 跨租户 |
| 409 | `APP_ROLE_NAME_EXISTS` | 同一应用内已存在同名角色（区分大小写） |
| 409 | `APP_PERMISSION_CODE_EXISTS` | 同一应用内已存在同名权限编码（区分大小写） |
| 409 | `APP_PERMISSION_HAS_CHILDREN` | 权限下仍有子权限，不允许删除 |
| 400 | `APP_PERMISSION_INVALID_PARENT` | 上级权限不存在 / 为自身或自身后代 / 跨应用 |
| 409 | `APP_RESOURCE_STALE` | 写请求携带的 `updatedAt` 与服务端不一致（并发编辑） |
| 400 | `AUTH_CODE_INVALID` | 短信/邮箱验证码错误、过期、已使用或错误次数超限 |
| 404 | `VERIFY_TARGET_NOT_FOUND` | `login` 场景请求验证码时，该手机号/邮箱未绑定任何账号；文案须含「请用密码登录后到账号中心绑定」（`find_password` 仍静默成功） |
| 409 | `VERIFY_TARGET_IN_USE` | 手机号/邮箱已被其他账号占用 |
| 502 | `VERIFY_SEND_FAILED` | 验证码供应商发送失败或通道未配置 |
| 409 | `MFA_ALREADY_BOUND` | 当前账号已绑定 MFA |
| 400 | `MFA_NOT_BOUND` | 当前账号尚未绑定 MFA |
| 429 | `RATE_LIMITED` | 触发限流 |
| 500 | `INTERNAL_ERROR` | 未预期错误（日志记录完整堆栈，响应不含） |
| 503 | `DEPENDENCY_UNAVAILABLE` | MySQL/Redis 不可用 |

新增错误码必须先登记到本表；禁止在 handler 内直接拼字符串作为 `code`。

---

## 6. 数据模型

数据库 `omino_sso`，字符集 `utf8mb4`。
**时间统一用 `BIGINT` 存 Unix 毫秒**（列名以 `_at` 结尾），禁用 `TIMESTAMP` 与 `DATETIME`。

### 6.1 `users`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | ULID |
| `username` | VARCHAR(64) | NOT NULL, UNIQUE, `COLLATE utf8mb4_0900_as_cs` | 大小写敏感 |
| `display_name` | VARCHAR(64) | NOT NULL | |
| `email` | VARCHAR(128) | NULL, UNIQUE | |
| `phone` | VARCHAR(20) | NULL | |
| `avatar_url` | VARCHAR(512) | NULL | |
| `password_hash` | VARCHAR(255) | NOT NULL | argon2id 编码串 |
| `dept_id` | CHAR(26) | NOT NULL, INDEX | FK → `organizations.id`（逻辑外键） |
| `tenant_id` | VARCHAR(32) | NOT NULL DEFAULT `'default'`, INDEX | |
| `roles` | JSON | NOT NULL | 字符串数组，如 `["user","admin"]` |
| `status` | TINYINT | NOT NULL DEFAULT 1 | 1=active 0=disabled 2=locked |
| `mfa_enabled` | TINYINT(1) | NOT NULL DEFAULT 0 | |
| `mfa_required` | TINYINT(1) | NOT NULL DEFAULT 0 | |
| `mfa_secret` | VARBINARY(255) | NULL | AES-GCM 加密后存储 |
| `failed_attempts` | INT | NOT NULL DEFAULT 0 | |
| `locked_until` | BIGINT | NULL | |
| `last_login_at` | BIGINT | NULL | |
| `password_updated_at` | BIGINT | NOT NULL | 改密后吊销全部会话 |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

> 本表**不使用软删除**。删除用户 = 置 `status=0`，数据保留以维护审计完整性。

### 6.2 `organizations`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | |
| `parent_id` | CHAR(26) | NULL, INDEX | 根为 NULL |
| `name` | VARCHAR(64) | NOT NULL | |
| `path` | VARCHAR(512) | NOT NULL, INDEX | 如 `/root/tech/platform/`，用于子树查询 |
| `sort` | INT | NOT NULL DEFAULT 0 | |
| `tenant_id` | VARCHAR(32) | NOT NULL DEFAULT `'default'` | |
| `created_at` | BIGINT | NOT NULL | |

### 6.3 `clients`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `client_id` | VARCHAR(64) | PK, `COLLATE utf8mb4_0900_as_cs` | 大小写敏感 |
| `client_name` | VARCHAR(128) | NOT NULL | |
| `description` | VARCHAR(255) | NULL | 应用描述，供"我的应用"展示 |
| `logo_url` | VARCHAR(512) | NULL | 应用图标地址，供"我的应用"展示 |
| `client_secret_hash` | BINARY(32) | NULL | SHA-256；`is_public=1` 时为空 |
| `is_public` | TINYINT(1) | NOT NULL DEFAULT 0 | 1=仅 PKCE |
| `redirect_uris` | JSON | NOT NULL | 字符串数组，**精确匹配** |
| `post_logout_uris` | JSON | NOT NULL | 白名单 |
| `grant_types` | JSON | NOT NULL | `["authorization_code","refresh_token"]` |
| `scopes` | JSON | NOT NULL | `["openid","profile","email","offline_access"]`；**签发 refresh_token 需客户端申请 `offline_access`** |
| `access_token_ttl` | INT | NOT NULL DEFAULT 900 | 秒 |
| `refresh_token_ttl` | INT | NOT NULL DEFAULT 2592000 | 秒，30 天 |
| `tenant_id` | VARCHAR(32) | NOT NULL DEFAULT `'default'` | |
| `status` | TINYINT | NOT NULL DEFAULT 1 | |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

### 6.4 `auth_codes`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `code_hash` | BINARY(32) | PK | SHA-256(授权码明文) |
| `client_id` | VARCHAR(64) | NOT NULL, INDEX | |
| `user_id` | CHAR(26) | NOT NULL | |
| `redirect_uri` | VARCHAR(512) | NOT NULL | 兑换时精确比对 |
| `scope` | VARCHAR(512) | NOT NULL | 空格分隔 |
| `nonce` | VARCHAR(128) | NULL | 原样回传至 id_token |
| `code_challenge` | VARCHAR(128) | NOT NULL | |
| `code_challenge_method` | VARCHAR(8) | NOT NULL, CHECK = `'S256'` | 只允许 S256 |
| `auth_time` | BIGINT | NOT NULL | |
| `amr` | JSON | NOT NULL | `["pwd","otp"]` |
| `expires_at` | BIGINT | NOT NULL, INDEX | 签发 + 60s |
| `used` | TINYINT(1) | NOT NULL DEFAULT 0 | 原子 UPDATE 更新 |
| `created_at` | BIGINT | NOT NULL | |

> 消费必须使用 `UPDATE auth_codes SET used=1 WHERE code_hash=? AND used=0 AND expires_at>?`，以 `RowsAffected==1` 判定成功。**禁止先 SELECT 再 UPDATE。**
> 表保留 24 小时后由定时任务清理；**禁止软删除**。

### 6.5 `refresh_tokens`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | |
| `token_hash` | BINARY(32) | NOT NULL, UNIQUE | SHA-256(明文) |
| `family_id` | CHAR(26) | NOT NULL, INDEX | 同一授权链共享；重放时按族吊销 |
| `user_id` | CHAR(26) | NOT NULL, INDEX | |
| `client_id` | VARCHAR(64) | NOT NULL | |
| `scope` | VARCHAR(512) | NOT NULL | |
| `amr` | JSON | NOT NULL | |
| `auth_time` | BIGINT | NOT NULL | |
| `expires_at` | BIGINT | NOT NULL, INDEX | |
| `revoked` | TINYINT(1) | NOT NULL DEFAULT 0 | 更新时必须用 `map` 或 `Select` |
| `revoked_at` | BIGINT | NULL | |
| `rotated_from` | CHAR(26) | NULL | 上一代 token id |
| `created_at` | BIGINT | NOT NULL | |

### 6.6 `sessions`

主存 Redis（`sess:{sid}`，TTL 与会话一致），MySQL 仅保留审计副本。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `sid` | VARCHAR(64) | PK | 256bit 随机串的十六进制 |
| `user_id` | CHAR(26) | NOT NULL, INDEX | |
| `tenant_id` | VARCHAR(32) | NOT NULL | |
| `amr` | JSON | NOT NULL | 本会话使用的认证方式 |
| `auth_time` | BIGINT | NOT NULL | |
| `ip` | VARCHAR(45) | NOT NULL | |
| `user_agent` | VARCHAR(512) | NULL | |
| `created_at` | BIGINT | NOT NULL | |
| `last_seen_at` | BIGINT | NOT NULL | |
| `expires_at` | BIGINT | NOT NULL | |
| `revoked` | TINYINT(1) | NOT NULL DEFAULT 0 | |

### 6.7 `audit_events`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | |
| `event_type` | VARCHAR(64) | NOT NULL, INDEX | 见下枚举 |
| `user_id` | CHAR(26) | NULL, INDEX | 匿名事件为空 |
| `username` | VARCHAR(64) | NULL | 冗余存储，便于用户删除后追溯 |
| `client_id` | VARCHAR(64) | NULL, INDEX | |
| `ip` | VARCHAR(45) | NOT NULL | |
| `user_agent` | VARCHAR(512) | NULL | |
| `result` | VARCHAR(16) | NOT NULL | `success` / `failure` |
| `detail` | JSON | NULL | 失败原因、撤销范围等，**禁止写入密码/token 明文** |
| `trace_id` | VARCHAR(32) | NULL | |
| `created_at` | BIGINT | NOT NULL, INDEX | |

`event_type` 枚举（新增需登记）：`login_success`、`login_failure`、`mfa_success`、`mfa_failure`、`logout`、`password_changed`、`token_issued`、`token_revoked`、`code_reused`、`refresh_reused`、`user_created`、`user_updated`、`client_created`、`client_secret_rotated`、`org_created`、`org_updated`、`org_deleted`、`client_updated`、`app_member_added`、`app_member_removed`、`app_role_created`、`app_role_updated`、`app_role_deleted`、`app_permission_created`、`app_permission_updated`、`app_permission_deleted`、`app_grant_updated`、`file_uploaded`、`file_status_updated`、`verification_code_sent`、`phone_bound`、`email_bound`、`mfa_bound`、`mfa_unbound`、`password_reset`。

> `verification_code_sent` 的 `detail` 记 `{channel,scene,target,provider}`（`target` 已脱敏）；验证码登录成功/失败复用 `login_success`/`login_failure`，MFA 二步复用 `mfa_success`/`mfa_failure`。

`detail` 详略规则（003 起）：一般只记字段名列表与数量；**地址类字段（`redirectUris` / `postLogoutUris`）记变更前后的完整数组**；删除角色/权限记被解除绑定的 ID 快照（超过 20 项记数量 + 前 20 项）。

> 组织写操作的审计划分以条件为准而非 HTTP 状态码：同级重名、存在子部门、存在账号、目标不存在、非法上级、层级超过 10 级都写 `failure`；JSON 绑定失败、名称/排序值等输入格式错误、401、403 只在应用日志留痕。

### 6.8 `consents`

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `user_id` | CHAR(26) | PK(1/2) | |
| `client_id` | VARCHAR(64) | PK(2/2) | |
| `scopes` | JSON | NOT NULL | 已同意 scope |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

> 所有建表语句写在 `migrations/0001_init.up.sql`，**禁止使用 `gorm.AutoMigrate`**。

### 6.9 应用内授权（003，`migrations/0003_app_auth.up.sql`）

| 表 | 关键列 | 约束 |
|---|---|---|
| `client_members` | `client_id`(64, as_cs)、`user_id`(26)、`added_by`(26)、`joined_at` | PK(client_id,user_id)；KEY idx_client_members_user(user_id,client_id) |
| `client_roles` | `id`(26)、`client_id`(64, as_cs)、`name`(32, as_cs)、`description`(100)、`created_at`、`updated_at` | PK(id)；UNIQUE uk_client_roles_name(client_id,name)；KEY idx_client_roles_client(client_id,created_at) |
| `client_permissions` | `id`(26)、`client_id`(64, as_cs)、`parent_id`(26)、`code`(64, as_cs)、`name`(64)、`description`(255)、`sort` | PK(id)；UNIQUE uk_client_permissions_code(client_id,code)；KEY idx_client_permissions_parent(client_id,parent_id,sort) |
| `client_role_permissions` | `role_id`(26)、`permission_id`(26)、`client_id` | PK(role_id,permission_id)；KEY idx_role_permissions_perm(permission_id) |
| `client_role_members` | `role_id`(26)、`user_id`(26)、`client_id` | PK(role_id,user_id)；KEY idx_role_members_user(user_id) |
| `access_tokens` | `token_id`(128, as_cs)、`sub`(26)、`client_id`(64)、`expires_at` | PK(token_id)；KEY idx_access_tokens_exp(expires_at) |

> 不变量：角色绑定的成员必然是 `client_members` 成员（授权写入时自动补成员）；删除成员级联解除 `client_role_members`；`access_tokens` 写入失败即签发失败，不降级。

### 6.10 `files`（004，`migrations/0004_files.up.sql`）

上传文件元数据（对象本体在 OSS）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | ULID |
| `provider` | VARCHAR(32) | NOT NULL | 恒为 `aliyun_oss` |
| `scene` | VARCHAR(32) | NOT NULL, INDEX | `app_logo` / `user_avatar` |
| `target_id` | VARCHAR(64) | NOT NULL, INDEX | app_logo=client_id；user_avatar=user_id |
| `format` | VARCHAR(16) | NOT NULL | `jpg` / `png` / `webp` |
| `size` | BIGINT | NOT NULL | 字节数 |
| `url` | VARCHAR(512) | NOT NULL | 公开访问地址 |
| `object_key` | VARCHAR(512) | NOT NULL | OSS 对象键 |
| `status` | VARCHAR(16) | NOT NULL, INDEX | `active` / `disabled` / `deleted` |
| `creator` | CHAR(26) | NOT NULL | 上传者 user_id |
| `created_at` | BIGINT | NOT NULL | |
| `updated_at` | BIGINT | NOT NULL | |

索引：`idx_files_scene_created(scene, created_at)`、`idx_files_status(status)`、`idx_files_target(scene, target_id)`。

### 6.11 `send_records`（005，`migrations/0005_verification_auth.up.sql`）

短信/邮箱验证码发送流水（审计型，不使用软删除，按保留期物理清理）。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `id` | CHAR(26) | PK | ULID |
| `channel` | VARCHAR(16) | NOT NULL, INDEX | `sms` / `email` |
| `scene` | VARCHAR(32) | NOT NULL, INDEX | `bind_phone` / `bind_email` / `login` / `find_password` / `mfa_unbind` |
| `target` | VARCHAR(128) | NOT NULL | **脱敏后**的接收方 |
| `result` | VARCHAR(16) | NOT NULL, INDEX | `success` / `failed` |
| `provider` | VARCHAR(32) | NOT NULL | `chuanglan` / `smtp` / `mock` |
| `error` | VARCHAR(255) | NULL | 失败原因 |
| `created_at` | BIGINT | NOT NULL, INDEX | 清理依据 |

索引：`idx_send_records_created(created_at)`、`idx_send_records_channel_created(channel, created_at)`、`idx_send_records_scene_created(scene, created_at)`、`idx_send_records_result_created(result, created_at)`。

> 同一迁移为 `users` 新增 `UNIQUE KEY uk_users_phone (phone)`（`uk_users_email` 已在 0001 存在）：手机号全局唯一，唯一索引允许多个 NULL，存量未绑定账号不受影响。发送记录与审计日志保留 180 天（`VERIFY_RECORD_RETENTION_DAYS`），由进程内每日定时任务分批物理删除。

---

## 7. 架构约束

### 7.1 必须遵守的 5 条

**约束 1：协议层只允许来自 `zitadel/oidc/v3`。**
禁止手写 OIDC 状态机、授权码签发流程、JWT 签发与验签（统一用 `go-jose/v4`），禁止引入第二个 OIDC 实现库。对 `op.Storage` 的适配只能做"数据存取 + 业务校验"，不得在其中实现协议状态流转。

**约束 2：IdP 与管理 API 物理分离为两个 `http.Server`。**
`:8080` 只挂 `op.Provider`（`net/http`），`:8081` 只挂 Gin。`/oauth2/*` 上禁止挂 CORS、业务鉴权、统一响应包装、全局限流；两个端口各自独立的中间件栈与超时配置。

**约束 3：数据访问必须事务安全，关键路径禁止"读-改-写"。**
`/oauth2/token` 链路通过 `internal/store/tx.go` 的 tx-in-context 传递事务；授权码消费、refresh 轮换、令牌吊销一律使用单条原子 `UPDATE` 并以 `RowsAffected` 判定结果；所有 Storage 方法必须传入 `ctx`（`db.WithContext(ctx)`）。

**约束 4：凭证永不进入浏览器可读区域。**
前端不得使用 `localStorage`/`sessionStorage`/内存变量持有 access_token、refresh_token 或任何 client_secret；浏览器只持有 `HttpOnly + Secure + SameSite=Lax` 的 `sid` cookie。响应体与日志中禁止出现 token 明文、密码、`client_secret`（新增 client 时的 secret 仅单次返回，且需在日志层做屏蔽）。

**约束 5：结构变更只走手写 SQL 迁移。**
`migrations/` 下按 `NNNN_name.up.sql` / `.down.sql` 成对提交；禁止 `AutoMigrate`、禁止在代码中 `CREATE TABLE`；token / code / session 类表禁止软删除；新增大小写敏感列必须显式声明 `COLLATE utf8mb4_0900_as_cs` 或使用 `BINARY(32)` 哈希。

### 7.2 非目标（本阶段明确不做）

> 以下条目**在没有明确指令前一律不实现**，包括不预留接口、不写 TODO、不建空表、不加配置项。

**非目标 1：不做 SAML 2.0 与 CAS 协议。**
不实现 SAML IdP/SP、不集成 `crewjam/saml`、不实现 CAS ticket 校验。需要接入 SAML/CAS 老系统时，通过外部协议网关或 `oauth2-proxy` 转换，本服务只讲 OIDC。

**非目标 2：不做账号自动开通与外部目录写入。**
不做 SCIM 2.0、不向 LDAP/AD 反向写回、不做 HR 系统同步、不发邮件/短信通知（`sendNotify` 恒为 false）、不做开放注册与邀请链接。账号只能由管理员创建。

**非目标 3：不做高可用与多副本部署。**
不写 K8s manifest / Helm / Operator、不做多实例共享缓存、不实现 Redis 版 `AuthRequestStorage` 之外的集群方案、不做读写分离与分库分表。MVP 明确单副本运行，`docker-compose` 为唯一部署形态。

**非目标 4：不做密码找回、短信 MFA 与第三方社交登录之外的认证方式。**
不实现"忘记密码"邮路/短信链路、不做短信验证码、不做邮件 OTP、不做 WebAuthn/Passkey、不做扫码登录的轮询接口、不做人脸/指纹。MFA 仅 TOTP，找回密码仅由管理员重置。企业微信/钉钉/飞书属于**后续阶段**，本阶段不实现。

**非目标 5：不做多租户 SaaS 化、计费与主题引擎。**
不做租户自助注册、不做套餐/计费/配额、不做租户级自定义域名与独立登录页、不做可视化主题编辑器、不做多语言 i18n（UI 恒为简体中文）、不做开放 API 的开发者门户与 SDK 发布。

**附加禁止项（AI 协作特别约定）**

- 不新增本文件未列出的第三方依赖；确需新增时先更新 §2 并说明理由。
- 不重构既有目录结构与分层（§3）。
- 不"顺手优化"与本任务无关的代码，不批量格式化。
- 不生成 `README`、演示脚本、示例数据种子文件，除非任务明确要求。
- 遇到规范未覆盖的情况：选择改动最小、最保守的实现，并在回复中显式说明该决策点。

### 7.3 过程文档与验证轻量化（001-api-docs 复盘）

- **不复述既有规范**：`spec` 不重复 `AGENTS.md` 已锁定的版本 / 端口 / 错误码；`plan` 不复述架构。重复内容信息量为零，只会让文档翻倍。
- **账本交给 git**：`tasks.md` 只留一句结论 + 勾选，不逐条回写「实测偏离」；`git log` / `git blame` 才是会被追溯的地方。
- **验证用测试表达**：一条命令 / 一个测试能证明的结论（如「逐端点错误码已登记」）写成一个测试，不写进 checklist。
- **扩展按事件触发**：`api-evolve` 等扩展只在有契约变更时产出内容，零变更特性不跑空仪式（空 diff 视为无效）。

### 7.4 任务完成定义与交付前自检（005-verification-code-auth 复盘）

005 共 76 条任务，其中 **27 条（36%）来自 6 轮 `/speckit.converge` 返工**：12 条是「代码改了、AGENTS/契约没同步」，6 条是功能/测试压根没交付，1 条是文档教用户执行不存在的 `make` 目标。这些都是**确定性可检查项**，不应靠事后人工审计发现。

**任务完成定义（DoD）：四件套缺一不可**

1. **代码**：行为符合 `spec.md` 与 `contracts/`，不留 TODO / 空实现；
2. **测试**：同一次提交内包含对应单测或门控集成用例（宪法「开发工作流与质量门禁」）；
3. **登记**：新增或变更的路由、错误码、审计事件、环境变量、表结构、依赖版本，在同一次提交内写入本文件 §2 / §4.1·§4.6 / §5.3 / §6.7 / §9；
4. **门禁**：`make build && make test && make lint && make check` 全绿。

**交付前自检（可脚本化，不依赖人工）**

| 命令 | 覆盖 |
|---|---|
| `make build` | 编译 + `docs-check`（OpenAPI 漂移）+ 运行时资源存在性 |
| `make test` | Go 单测（集成用例默认 `t.Skip`）+ portal vitest |
| `make lint` | golangci-lint + 两套前端 eslint（缺 `node_modules` 时自动安装） |
| `make check` | 注册表一致性（路由/错误码/审计事件/环境变量 四向比对）+ 文档命令可执行性 |
| `make ci-local` | 临时 `git worktree` 复刻干净检出跑上述全套，专治「本地绿、CI 红」 |

**两条硬约束**

- **行为取舍必须落纸**：实现期临时决定的取舍（如 005 把 `login` 场景改成 `404`、而 `find_password` 保持静默）必须在同一提交内写入 `spec.md` Edge Cases + `contracts/` + 本节相关条款，不得留到 converge 阶段再补。
- **文档里的命令必须可执行**：`specs/**`、`AGENTS.md` 中出现的 `` `make <target>` `` 由 `make check` 逐条用 `make -n` 验证；确需引用历史废弃目标时，在该行加 `docs-commands:ignore` 注释。

---

## 8. 启动命令

### 8.1 本地依赖

```bash
# 启动 MySQL 8.4 + Redis 7.4
docker compose up -d mysql redis

# 初始化数据库（幂等，重复执行安全）
mysql -h 127.0.0.1 -P 3306 -u root -p omino_sso < migrations/0001_init.up.sql
```

### 8.2 后端

```bash
cp .env.example .env          # 首次执行，按需修改
go mod download
go run ./cmd/sso              # 同时启动 :8080(IdP) 与 :8081(API)
```

等价于 `make dev`。单端口调试可分别设置 `HTTP_IDP_ADDR` / `HTTP_API_ADDR`。

### 8.3 前端

```bash
cd web/portal && npm ci && npm run dev     # http://localhost:5173
cd web/admin  && npm ci && npm run dev     # http://localhost:5174
```

### 8.4 构建与验证

```bash
make build                    # 产出 bin/sso
make test                     # 单元测试（集成用例默认 t.Skip）
make lint                     # golangci-lint + eslint
make check                    # 注册表一致性 + 文档命令可执行性
make ci-local                 # 临时 worktree 复跑全套（模拟干净检出）
make build-web                # 两个前端产物 -> web/dist
```

> **改动自测纪律（强制）**：任何改动在提交前必须主动执行 `make test` 与 `make lint`，不得依赖 pre-commit 兜底。新增/修改业务逻辑、端点或数据访问层时，必须同步新增或更新对应单元测试。新增/删除路由或改动 DTO 字段后，必须执行 `make docs` 重新生成 `openapi.json`，并全局搜索、同步更新依赖 operation 数 / 字段结构的硬编码测试断言（如 `internal/api/docs/docs_test.go`、`internal/api/router_test.go`）。改动前端后执行 `npm run typecheck`（`web/admin` 与 `web/portal`）。

### 8.5 合规验收（门禁，不可跳过）

```bash
docker run --rm -p 8080:8080 \
  -e CONFORMANCE_SERVER=http://host.docker.internal:8080 \
  openidentity/conformance-suite
# 手工在 UI 选择 basic OP + config OP profile，全部用例通过方可发布
```

### 8.6 部署（MVP）

```bash
docker compose up -d --build   # mysql + redis + sso + nginx(静态托管两套前端)
```

---

## 9. 环境变量清单

所有变量由 `internal/config` 统一加载并在启动时校验；缺失必填项**直接退出**（非零码），不得静默使用默认值上线。

| 变量 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `APP_ENV` | 是 | `dev` | `dev` / `test` / `prod`；`prod` 下强制校验 cookie 与 issuer 的 https |
| `LOG_LEVEL` | 否 | `info` | `debug` / `info` / `warn` / `error` |
| `HTTP_IDP_ADDR` | 否 | `:8080` | 协议层监听地址 |
| `HTTP_API_ADDR` | 否 | `:8081` | 管理 API 监听地址 |
| `HTTP_READ_TIMEOUT` | 否 | `15s` | |
| `HTTP_WRITE_TIMEOUT` | 否 | `15s` | |
| `OIDC_ISSUER` | 是 | — | 例 `https://sso.example.com`，必须与外部访问地址完全一致 |
| `OIDC_SIGNING_KEY_PATH` | 是 | — | RS256 私钥 PEM 路径（`0600`，不入仓库） |
| `OIDC_SIGNING_KEY_ID` | 是 | — | `kid`，例 `2026-10` |
| `OIDC_CRYPTO_KEY` | 是 | — | 32 字节随机串（base64），用于库内部加密 |
| `OIDC_ACCESS_TOKEN_TTL` | 否 | `900` | 秒 |
| `OIDC_REFRESH_TOKEN_TTL` | 否 | `2592000` | 秒 |
| `OIDC_AUTH_CODE_TTL` | 否 | `60` | 秒，上限 60 |
| `SESSION_TTL` | 否 | `28800` | 秒，8 小时 |
| `SESSION_TTL_REMEMBER` | 否 | `604800` | 秒，7 天 |
| `COOKIE_DOMAIN` | 是 | — | 例 `sso.example.com` |
| `COOKIE_SECURE` | 否 | `true` | 本地 http 调试可设 false |
| `COOKIE_SAME_SITE` | 否 | `Lax` | `Lax` / `Strict` |
| `DB_HOST` | 是 | `127.0.0.1` | |
| `DB_PORT` | 否 | `3306` | |
| `DB_USER` | 是 | — | |
| `DB_PASSWORD` | 是 | — | |
| `DB_NAME` | 否 | `omino_sso` | |
| `DB_MAX_OPEN_CONNS` | 否 | `25` | |
| `DB_MAX_IDLE_CONNS` | 否 | `10` | |
| `DB_CONN_MAX_LIFETIME` | 否 | `30m` | |
| `REDIS_ADDR` | 是 | `127.0.0.1:6379` | |
| `REDIS_PASSWORD` | 否 | 空 | |
| `REDIS_DB` | 否 | `0` | |
| `ARGON2_MEMORY_KIB` | 否 | `65536` | 64MB |
| `ARGON2_ITERATIONS` | 否 | `3` | |
| `ARGON2_PARALLELISM` | 否 | `2` | |
| `LOGIN_MAX_FAILURES` | 否 | `5` | 达到后锁定 |
| `LOGIN_LOCK_DURATION` | 否 | `900` | 秒 |
| `RATE_LIMIT_LOGIN_PER_MIN` | 否 | `10` | 按 `username+IP` |
| `RATE_LIMIT_API_PER_MIN` | 否 | `300` | 按 `sid` 或 IP |
| `AUDIT_ASYNC_BUFFER` | 否 | `1024` | 审计异步队列长度 |
| `OSS_PROVIDER` | 否 | 空 | 对象存储供应商；空=不启用上传，`aliyun_oss`=阿里云 OSS |
| `OSS_ENDPOINT` | 条件 | — | provider 为 aliyun_oss 时必填，例 `oss-cn-shenzhen.aliyuncs.com` |
| `OSS_BUCKET` | 条件 | — | 同上 |
| `OSS_ACCESS_KEY` | 条件 | — | 调用 STS AssumeRole 的 RAM 用户 AccessKeyId |
| `OSS_SECRET_KEY` | 条件 | — | 对应 AccessKeySecret，服务端专用、不出服务端 |
| `OSS_ROLE_ARN` | 条件 | — | 被扮演角色 ARN（`acs:ram::<accountId>:role/ramossrole`），provider 为 aliyun_oss 时必填 |
| `OSS_ROLE_SESSION_NAME` | 否 | `omino-sso` | AssumeRole 会话名 |
| `OSS_STS_ENDPOINT` | 否 | `sts.aliyuncs.com` | STS 端点 |
| `OSS_STS_DURATION_SECONDS` | 否 | `3600` | AssumeRole 临时凭证有效期（秒，900–43200） |
| `OSS_BASE_PATH` | 否 | 空 | 对象键前缀，例 `sso` |
| `OSS_CUSTOM_DOMAIN` | 否 | `https://<bucket>.<endpoint>` | 公开访问域名 |
| `OSS_CREDENTIAL_TTL` | 否 | `300` | 直传凭证有效期（秒） |
| `SMS_PROVIDER` | 否 | `mock` | `mock` / `chuanglan` / 空（关闭）；`prod` 下禁止 `mock` |
| `SMS_CHUANGLAN_URL` | 条件 | — | provider=chuanglan 时必填，创蓝 HTTP 接口地址 |
| `SMS_CHUANGLAN_ACCOUNT` | 条件 | — | 同上 |
| `SMS_CHUANGLAN_PASSWORD` | 条件 | — | 同上，服务端专用 |
| `SMS_CHUANGLAN_TEMPLATE_ID` | 条件 | — | 模板报备 ID（`v2/tpl/send` 必备，缺它返回 `130 templateId 不能为空`）；与 `SMS_CHUANGLAN_TEMPLATE` 二选一 |
| `SMS_CHUANGLAN_TEMPLATE` | 条件 | — | 内容模板文案，仅未配 `TEMPLATE_ID` 时使用，`{code}` / `{s}` 替换为验证码 |

> **模板变量 key 硬编码为 `param1`（2026-10-22 定稿，不做成环境变量）**：创蓝 V2 的 `templateParamJson` 按**位置**命名变量（`param1`、`param2`…），与模板文案里写 `{code}` 还是 `${s}` 无关。它一旦配错，网关只回 `code=130 所有手机号或参数均不合法`，不指向具体字段，排查成本远高于"多一个可配项"带来的灵活性；本项目模板固定为单变量验证码短信，故在 `internal/service/notification/chuanglan.go` 中以常量 `chuanglanParam` 硬编码，并有用例 `TestChuanglanSend_ParamKeyHardcoded` 锁定。

| `MAIL_PROVIDER` | 否 | `mock` | `mock` / `smtp` / 空（关闭）；`prod` 下禁止 `mock` |
| `MAIL_SMTP_HOST` | 条件 | — | provider=smtp 时必填 |
| `MAIL_SMTP_PORT` | 否 | `587` | `465` 走 SMTPS（连接后立即 TLS 握手），其它端口走明文 + STARTTLS |
| `MAIL_SMTP_USERNAME` | 否 | 空 | 非空时 `MAIL_SMTP_PASSWORD` 必填 |
| `MAIL_SMTP_PASSWORD` | 条件 | — | |
| `MAIL_FROM` | 条件 | — | 发件人（From 头），形如 `omino-sso <no-reply@example.com>`；**信封发件人**（MAIL FROM）在 `MAIL_SMTP_USERNAME` 为邮箱形态时自动取该账号，规避 126/163 的 `553 Mail from must equal authorized user` |
| `MAIL_TEMPLATE_PATH` | 否 | `./configs/verify-mail.tmpl` | `html/template` 模板文件 |
| `VERIFY_CODE_TTL` | 否 | `300` | 秒，验证码有效期上限 30 分钟 |
| `VERIFY_RESEND_INTERVAL` | 否 | `60` | 秒，必须小于 `VERIFY_CODE_TTL` |
| `VERIFY_MAX_ATTEMPTS` | 否 | `5` | 单个验证码最大连续错误次数 |
| `VERIFY_RECORD_RETENTION_DAYS` | 否 | `180` | 发送记录与审计日志保留天数 |
| `VERIFY_CIRCUIT_THRESHOLD` | 否 | `5` | 供应商熔断阈值：同一渠道连续失败达该值即开闸，`0` 关闭熔断（2026-10-22 新增，research 决策 11） |
| `VERIFY_CIRCUIT_COOLDOWN` | 否 | `60` | 熔断冷却秒数（开闸期间请求直接 `VERIFY_SEND_FAILED` 且不调用供应商），阈值 > 0 时必须 > 0 |
| `ADMIN_BOOTSTRAP_USERNAME` | 是（首次） | — | 初始化管理员，仅在 `users` 为空时生效 |
| `ADMIN_BOOTSTRAP_PASSWORD` | 是（首次） | — | 初始化后必须立即修改，并在生产环境删除该变量 |

`.env.example` 必须与本表保持同步；新增变量时同步更新两处。

> **`REDIS_KEY_PREFIX` 于 2026-10-22 移除（行为修正）**：该变量此前登记在本表中，但代码从未消费它——`internal/store/redis.go` 建客户端时只传 `Addr` / `Password` / `DB`，`config.RedisConfig.KeyPrefix` 无任何引用方，因此把它写进 `.env` 也不改变实际键名，属"看似生效、实则无效"的配置，会误导运维以为多个环境可以共用同一个 Redis DB。现删除该字段与相关文档，不加兼容分支（旧值留在环境里也不会被读取）。键命名一律由各模块自己的固定前缀承担（`sess:` / `captcha:` / `verify:code:` / `verify:attempt:` / `verify:resend:` / `mfa:setup:` / `rl:`），环境隔离只依赖 `REDIS_DB` 或独立实例。

---

## 附：一致性检查清单（提交前自检）

- [ ] 改动的接口字段与 §4 完全一致（含 `null` 与可选性）
- [ ] `/api/v1/*` 的响应符合 §5；`/oauth2/*` 未被包装
- [ ] 新增表/字段已写入 `migrations/`，无 `AutoMigrate`
- [ ] 时间字段为 `BIGINT` 毫秒；敏感列大小写敏感或 `BINARY(32)`
- [ ] 关键路径使用原子 `UPDATE` + `RowsAffected`，未出现"先查后改"
- [ ] 未引入 §7.2 非目标中的任何能力
- [ ] 未新增未登记的错误码与 `event_type`
- [ ] 日志与响应中无 token / 密码 / client_secret
- [ ] 改动已伴随/更新单元测试，且已主动执行 `make test` / `make lint`（未依赖 pre-commit 兜底）
- [ ] 新增/删除路由或改动 DTO 后已执行 `make docs`，并同步更新 operation 数等硬编码测试断言
- [ ] 新增带鉴权的端点前已界定访问者与可写范围（防越权）；前端筛选/搜索控件与后端实际过滤字段一致
- [ ] 新增第三方依赖前已按 §2.0 提案并取得确认，引入后已登记到 §2.1 / §2.2 并锁定精确版本，`go mod tidy` 未造成既有依赖被动升级
