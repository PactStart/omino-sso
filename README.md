# omino-sso

企业内部的统一身份提供方（IdP），基于 OIDC / OAuth 2.1 为内部系统提供单点登录、单点登出与集中账号管理。

- 协议层：`github.com/zitadel/oidc/v3`，授权码 + PKCE（仅 S256）
- 浏览器侧只持有 `HttpOnly` 的 `sid` 会话 cookie，**前端代码不接触任何 token**
- 规范以 [`AGENTS.md`](./AGENTS.md) 为唯一权威；前端交互契约见 [`PRD.md`](./PRD.md)

## 功能

| 模块 | 能力 |
|---|---|
| 统一认证 | 账号密码登录、TOTP 二次验证、失败计数与锁定、图形验证码（连续失败 3 次后启用） |
| 单点登录 / 登出 | 一次登录贯通所有接入系统；登出即时失效服务端会话 |
| 账号中心（门户） | 个人信息、我的应用、登录设备（可下线其他设备）、修改密码 |
| 身份管理（后台） | 用户增改、组织架构树、OAuth 客户端注册与密钥轮换、全量认证审计日志 |

**非目标（本期不做）**：邮件/短信通知、SAML、SCIM、多租户计费、i18n、暗色主题、移动端管理后台。

## 技术栈

| 层 | 组件 |
|---|---|
| 后端 | Go 1.26.0、gin v1.10.0（管理 API）、uber/fx v1.23.0、gorm v1.25.12、zitadel/oidc/v3 v3.28.0、go-jose/v4、argon2id、redis/go-redis/v9 |
| 门户 | Vue 3.5.13 + Vite 6.3.5 + TS 5.7.2 + Pinia + vue-router + Tailwind CSS 4.1.0 |
| 后台 | 同上组件基线 + Element Plus 2.9.1 |
| 存储 | MySQL 8.4、Redis 7.4 |

## 端口与路径

| 服务 | 地址 | 说明 |
|---|---|---|
| IdP 协议层（`net/http`） | `http://localhost:8080` | `/oauth2/*`、`/.well-known/*`，RFC 原生报文 |
| 管理 API（Gin） | `http://localhost:8081` | `/api/v1/*`，统一响应 `{code,message,data,traceId}` |
| 门户（dev） | `http://localhost:5173` | 登录页与账号中心 |
| 管理后台（dev） | `http://localhost:5174` | 用户 / 应用 / 审计 |
| MySQL | `127.0.0.1:3307` | docker-compose 映射，避开本机 3306 |
| Redis | `127.0.0.1:6380` | docker-compose 映射 |

## 环境要求

- Go 1.26.0+
- Node.js 20+ / npm 10+
- Docker 27+ 与 Docker Compose v2（或自备 MySQL 8.4 + Redis 7.4）
- `openssl`（生成开发签名密钥）、`mysql` 客户端（手动迁移时需要）

## 快速开始

### 1. 启动依赖

```bash
docker compose up -d          # mysql(3307) + redis(6380)
docker compose ps             # 两者 healthy 后再继续
```

首次创建数据卷时，MySQL 会自动执行 `migrations/0001_init.up.sql`。

> 若数据库已存在但缺表，执行 `make migrate-up` 手动迁移（默认连 `127.0.0.1:3307`，可用 `DB_HOST/DB_PORT/...` 覆盖）。

### 2. 配置环境变量与签名密钥

```bash
cp .env.example .env          # 按需修改；变量含义见文件内注释
make keys                     # 生成 configs/dev-signing-key.pem
```

`.env` 中的必填项（缺失会启动失败）：`OIDC_ISSUER`、`OIDC_SIGNING_KEY_PATH`、`OIDC_CRYPTO_KEY`（32 字节）、`DB_USER`、`DB_PASSWORD`、`REDIS_ADDR`。

### 3. 启动后端

```bash
make dev                      # go run ./cmd/sso，同时监听 :8080 与 :8081
```

首次启动且 `users` 表为空时，会按 `ADMIN_BOOTSTRAP_*` 创建初始管理员。

### 4. 启动前端

```bash
# 门户
cd web/portal && npm install && npm run dev     # http://localhost:5173

# 管理后台（另开终端）
cd web/admin  && npm install && npm run dev     # http://localhost:5174
```

两端 dev server 均已把 `/api` 代理到 `:8081`，会话 cookie 为 host-only + `Path=/`，因此两个端口共享同一登录态。

### 5. 默认账号（来自 `.env.example`）

| 账号 | 密码 | 角色 | 说明 |
|---|---|---|---|
| `admin` | `Admin@12345` | 管理员 | 来自 `ADMIN_BOOTSTRAP_*`，登录后进入 `/admin/users` |
| `zhangsan` | `Demo@12345` | 普通用户 | 演示数据（仅 `APP_ENV!=prod`） |
| `lisi` | `Demo@12345` | 普通用户 | 演示数据，已绑定 TOTP（登录走二次验证） |

> 种子数据由 `internal/bootstrap/seed.go` 在 `users` 表为空时写入；`APP_ENV=prod` 只创建管理员。
> 演示账号的 TOTP 密钥会打印在启动日志的 `seed: demo users created` 一行中，仅供本地联调，生产请立即清理演示数据并修改初始密码。

## 验证既有功能

```bash
# 健康检查
curl -s http://localhost:8081/api/v1/me

# 登录（成功时下发 sid cookie）
curl -s -c /tmp/c.txt http://localhost:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@12345","clientId":"portal"}'

# 带会话取当前用户
curl -s -b /tmp/c.txt http://localhost:8081/api/v1/me

# OIDC 元数据
curl -s http://localhost:8080/.well-known/openid-configuration
```

## 构建与产物

```bash
make build                    # 后端 -> bin/sso
make build-web                # 前端 -> web/dist/portal、web/dist/admin
```

单个前端的构建与本地预览：

```bash
cd web/portal
npm run build                 # 类型检查 + 打包到 dist/
npm start                     # vite preview，:5173（同样代理 /api）
```

`package.json` 脚本：`dev`（开发）、`build`（构建）、`start`（预览构建产物）、`typecheck`（仅类型检查）。

### 接口文档（OpenAPI 3.0.3）

文档由 handler 注解生成，产物 `internal/api/docs/openapi.json` 入库并随二进制内嵌：

```bash
make docs                     # swag(2.0) -> openapi3(转换) -> internal/api/docs/openapi.json
make docs-check               # 校验已提交产物与注解是否一致，不一致则非零退出
make docs-serve               # 生成后启动服务，回显文档地址
```

`make build` 会先执行 `docs-check`，因此文档腐化会直接让构建失败；改注解后必须重新 `make docs` 并与注解改动放在同一个提交里。

流水线里可直接粘贴：`make docs-check`。

文档入口：<http://127.0.0.1:8081/docs/>（仅 `APP_ENV != prod` 时提供，prod 下与不存在的路径表现完全一致）。

## 开发流程规范（Spec Kit）

新功能一律走规格驱动流程（SDD）：**先写清「做什么/为什么」，再定技术方案，最后才写代码**。
原则层约束见 `.specify/memory/constitution.md`，实现细节以 `AGENTS.md` 为唯一权威。

### 工具与环境

仓库已通过 `specify init --here --integration codebuddy` 完成初始化，产物为 `.specify/` 与
`.codebuddy/commands/speckit.*.md`（共 54 个命令：10 个核心命令 + 44 个扩展命令）。

| 项 | 说明 |
|---|---|
| CLI 版本 | `specify 1.2.2`（`uv tool install` 安装） |
| 重装/升级 | `uv tool install --python 3.12 --with "cryptography==44.0.2" specify-cli` |
| 为何锁定 cryptography | 50.x 无 macOS x86_64 预编译 wheel，会退化为源码编译并因缺少 Rust 失败 |
| 已安装扩展 | 10 个：`git` `gates` `api-evolve` `bug` `assess` `ears` `critique` `checkpoint` `doctor` `changelog`（`.specify/extensions.yml` 中 `auto_execute_hooks: true`） |
| 浏览扩展 | `specify extension search`；`specify integration status` 查看集成状态 |

### 分支约定

仓库已安装 `git` 扩展，`/speckit.specify` 会先执行**强制前置钩子** `speckit.git.feature`，
自动创建并切到与 spec 目录同名的分支（如 `001-mfa-self-binding`），因此**无需手动建分支**。
`before_clarify` / `before_plan` 还注册了可选的自动提交钩子（`speckit.git.commit`）。

若需要手动控制分支（例如要从最新 `main` 出发或复用已有分支）：

```bash
git switch main && git pull
git switch -c 001-mfa-self-binding   # 与后续 spec 目录编号一致
```

### 七步节奏

一条命令一步，**每步产物 review 通过后再敲下一条**，禁止连发。

| 步 | 命令 | 输入 | 产出 | 进入下一步的门槛 |
|---|---|---|---|---|
| 1 | `/speckit.specify` | 业务描述（做什么、为什么、不做什么） | `specs/NNN-xxx/spec.md` | 需求无歧义，且不含任何技术方案 |
| 2 | `/speckit.clarify` | 直接回车（由它提问） | spec.md 中的澄清条目 | 所有提问已回答 |
| 3 | `/speckit.plan` | 技术偏好，可省略 | `plan.md`、`research.md`、`data-model.md`、`contracts/` | 复用既有架构，无规范外依赖 |
| 4 | `/speckit.tasks` | 无需参数 | `tasks.md` | 任务粒度可独立完成 |
| 5 | `/speckit.analyze` | 无需参数 | 一致性报告 | 无与宪法/`AGENTS.md` 冲突的 Critical 项 |
| 6 | `/speckit.implement` | 无需参数 | 代码 + 勾选 `tasks.md` | 任务全部完成，`make test` 通过 |
| 7 | `/speckit.converge` | 无需参数 | 收敛报告 | 报告为 `Converged`；否则回到第 6 步循环 |

可选命令：`/speckit.checklist`（第 3 步后生成质量清单）、`/speckit.taskstoissues`（任务转 GitHub Issue）。

### 第 1 步怎么写（最容易写错）

只写**用户视角的行为 + 边界**，出现库名、表名、缓存、类名即视为写错——那属于第 3 步。

```text
/speckit.specify 允许员工在账号中心自助绑定 TOTP：未绑定用户可生成密钥并展示二维码、
输入 6 位验证码确认后启用；已绑定用户可解绑，解绑需再次输入登录密码；
管理员可将指定用户标记为强制 MFA，被强制且未绑定的用户登录成功后先被引导到绑定页。

边界：不改变现有 /oauth2/* 协议流程与 /api/v1/** 统一响应结构；
不实现短信/邮件/备用码；前端交互契约参考 PRD.md 对应页面章节。
```

### 产物与提交节奏

```
specs/001-mfa-self-binding/
├── spec.md          # 需求（人话，无技术方案）
├── plan.md          # 技术方案
├── research.md      # 备选方案与取舍
├── data-model.md    # 数据模型增量
├── contracts/       # 接口契约（对齐 AGENTS.md §4）
├── quickstart.md    # 验证步骤
└── tasks.md         # 任务清单（implement 时逐项勾选）
```

一步一提交，便于单独回滚与评审：

```bash
git add specs/001-mfa-self-binding/spec.md && git commit -m "spec: MFA 自助绑定需求"
git add specs/001-mfa-self-binding/         && git commit -m "plan: MFA 自助绑定技术方案"
# implement 之后按模块提交，提交前跑 make test / make build-web
# 改了 handler 注解：先 make docs，再把 openapi.json 与注解改动放进同一个提交（make build 会执行 docs-check 拦截漂移）
```

### 三条铁律

1. **一次只开一个 feature 目录**，走完 `converge` 再开 `002-xxx`；并行会让 plan/analyze 互相污染。
2. **spec.md 出现技术名词即返工**：它只回答「用户看到什么、什么情况算成功或失败」。
3. **每一步都要人工拍板**：`implement` 前没看 `plan.md`，等于把架构决策权交给模型。

### 文档分工

| 文档 | 角色 | 何时被读 |
|---|---|---|
| `.specify/memory/constitution.md` | 不可协商的原则（5 条） | 每次 `plan` / `analyze` |
| `AGENTS.md` | 实现细节唯一权威（版本、字段、错误码、非目标） | 写 spec/plan 的输入 |
| `PRD.md` | 前端交互契约（页面、路由、接口） | 前端类特性的 spec 输入 |
| `specs/NNN-xxx/` | 本次变更的过程记录 | 逐阶段产出 |

### 命令不可用时的兜底

若当前 AI 会话未注册 `/speckit.*`，可直接要求按命令文件执行，产物与流程一致：

```text
按 .codebuddy/commands/speckit.specify.md 执行，参数是「<业务描述>」
```

## 生产部署要点

1. 反向代理把协议层与管理 API 置于同一域名下（否则 `sid` cookie 无法共享）：
   - `/oauth2/*`、`/.well-known/*`、`/login`、`/logout` → `:8080`
   - `/api/*` → `:8081`
   - `/` → `web/dist/portal`，`/admin/*` → `web/dist/admin`
2. `APP_ENV=prod` 时强制校验：`OIDC_ISSUER` 必须 `https://`、`COOKIE_SECURE=true`。
3. 管理后台产物默认使用 `base: '/'`，挂到 `/admin/` 子路径时需要在 `vite.config.ts` 调整 `base` 并同步 `createWebHistory`。
4. 数据库与 Redis 密码、`OIDC_CRYPTO_KEY`、签名私钥须通过密钥管理下发，不要提交进仓库。

## 目录速览

```
cmd/sso            # fx 装配入口
internal/oidc      # OIDC Provider、Storage 适配、登录闭环、签名密钥
internal/api       # Gin 路由、中间件、auth/me/admin 处理器
internal/service   # 业务规则层
internal/store     # gorm 模型与各 store 实现（Redis 会话）
internal/pkg       # crypto / id / clock / log
migrations/        # 手写 SQL（禁止 AutoMigrate）
web/portal         # 门户（Tailwind）
web/admin          # 管理后台（Element Plus）
.specify/          # Spec Kit 配置：宪法（memory/）、模板、脚本
specs/             # 每个功能的规格/计划/任务（由 /speckit.specify 生成）
.codebuddy/        # AI 斜杠命令（speckit.*）
```

## 常见问题

| 现象 | 处理 |
|---|---|
| 启动报 `missing required env` | `.env` 未创建或缺必填项，参考 `.env.example` |
| 启动报 `OIDC_CRYPTO_KEY must be a 32-byte key` | 使用 32 位原始字符或 base64(32B) |
| 登录后立刻跳回登录页 | cookie 未被浏览器接受：检查 `COOKIE_SECURE`（http 下须为 false）与是否同域访问 |
| 管理后台 403 | 当前账号不含 `admin` 角色 |
| 端口占用 | `HTTP_IDP_ADDR`/`HTTP_API_ADDR` 可改；前端端口在各自 `vite.config.ts` |
| MySQL 3306 冲突 | docker-compose 已映射到 3307，可用 `MYSQL_HOST_PORT` 覆盖 |
