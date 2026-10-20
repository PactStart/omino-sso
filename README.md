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
| 后端 | Go 1.24、gin v1.10.0（管理 API）、uber/fx v1.23.0、gorm v1.25.12、zitadel/oidc/v3 v3.28.0、go-jose/v4、argon2id、redis/go-redis/v9 |
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

- Go 1.24+
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
