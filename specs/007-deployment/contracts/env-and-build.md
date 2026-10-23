# 契约：环境变量与构建参数

**Feature**: `007-deployment` | **覆盖需求**: FR-005 / FR-008 / FR-010 / FR-011 / FR-014 / FR-022

## 1. 运行时环境变量（后端）

### 1.1 新增

| 变量 | 必填 | 默认 | 示例 | 说明 |
|---|:--:|---|---|---|
| `TRUSTED_PROXIES` | 否 | 空 | `10.0.0.0/8,172.16.0.0/12` | 逗号分隔 CIDR 或单 IP；**仅**这些来源的 `X-Forwarded-For` 被采信 |

校验：每一项必须能被 `netip.ParsePrefix` / `netip.ParseAddr` 解析；非法项启动即失败并打印具体值（不允许静默忽略）。`prod` 下不强制必填（单机直连属合法形态），但 `DEPLOYMENT.md` 验收清单要求显式确认。

**必须列全所有反代网段**：生产链路是 `浏览器 → 入口 LB → portal/admin nginx → api 入口 → sso:8081`，XFF 至少两跳。若只写入口 LB 网段而漏掉前端容器网段，"从右向左取第一个非可信地址"会取到**前端容器 IP**——审计 IP 全变成 `172.x`，且看起来像已经配好了。quickstart §4 有对应的反证步骤。

### 1.2 语义变更

| 变量 | 变更 | 影响 |
|---|---|---|
| `COOKIE_DOMAIN` | 必须显式为主域（`.example.com`），**禁止** host-only；`prod` 下**启动校验**：非空且以 `.` 开头（不含 `://`、不含尾斜杠），否则进程非零退出 | 留空会导致「门户登录成功、进后台又要登录」（FR-006）。`AGENTS.md §9` 的示例从 `sso.example.com` 改为 `.example.com` |
| `OIDC_ISSUER` | 恒等于 `https://sso.<主域>` | `api`/`portal`/`admin` 域不得作为 issuer |

### 1.3 明确不新增

| 变量 | 结论 | 理由 |
|---|---|---|
| `CORS_ALLOWED_ORIGINS` | **不引入** | 前端全部同源，跨源只发生在服务端（FR-007） |
| `PORTAL_HOME` / `ADMIN_HOME`（后端侧） | 不引入 | 前端地址由镜像构建参数决定，后端不需要知道 |
| `API_UPSTREAM`（后端侧） | 不引入 | 只存在于 nginx 模板与编排中 |

### 1.4 登记门禁

`TRUSTED_PROXIES` 必须同时出现在三处，否则 `make check` 失败：

1. `internal/config/config.go`（`env("TRUSTED_PROXIES", "")` 或等价读取）；
2. `.env.example`（`TRUSTED_PROXIES=`）；
3. `AGENTS.md §9` 环境变量表（`| `TRUSTED_PROXIES` | 否 | — | ... |`）。

## 2. 前端构建参数

见 [data-model.md §3](../data-model.md)。约束：

- `VITE_API_BASE_URL` **不传**，取默认 `/api/v1`；误传绝对地址会退回跨源形态（本项目无 CORS，直接全部请求失败）。
- URL 型参数必须绝对地址；`VITE_ADMIN_HOME` 的 pathname 必须为 `/`（域名根，不带 `/admin/`）。

## 3. 镜像与标签

| 镜像 | Dockerfile | target | 构建上下文 |
|---|---|---|---|
| `omino-sso` | `Dockerfile` | — | 仓库根 |
| `omino-portal` | `web/Dockerfile` | `portal` | `./web` |
| `omino-admin` | `web/Dockerfile` | `admin` | `./web` |

- 标签：`<版本或日期>-<序号>`（如 `2026.10.23-1`）；**三镜像 tag 必须一致**。
- 同一 tag 可用于 test 与 prod（配置差异在 env / 构建参数，不在 tag）。
- 前端镜像内含 nginx 与静态产物；**不含**任何域名（域名来自 `API_UPSTREAM` 与 `VITE_*` 构建参数）。

## 4. 编排契约

| 文件 | 服务 | DB/Redis | env 来源 |
|---|---|---|---|
| `docker-compose.yml` | mysql, redis, sso, portal, admin | 容器内 | compose 内联（dev 默认值） |
| `deploy/compose.test.yml` | sso, portal, admin | 外部云 | `env_file: /etc/omino-sso/test.env` |
| `deploy/compose.prod.yml` | sso, portal, admin | 外部云 | `env_file: /etc/omino-sso/prod.env` |

- test/prod **不得**声明 `mysql`/`redis` service，也不得 `depends_on` 它们。
- 三容器都必须有 `healthcheck`；`restart: unless-stopped` 且 `depends_on: {sso: {condition: service_healthy}}`（前端 nginx 启动期解析 upstream，后端没就绪会起不来）。
- `sso` 的 healthcheck 打 `http://127.0.0.1:8081/healthz`（业务面，见 [health-probes.md §4](./health-probes.md)）；`portal`/`admin` 打自身 `http://127.0.0.1/healthz`。
- `sso` 只暴露内网端口；对外由反代终止 TLS。`8080/8081` 禁止 `ports:` 到 `0.0.0.0`。

**一体化 compose 的 `build.args`（漏了本地演示必坏）**

| 服务 | build.args |
|---|---|
| `portal` | `VITE_ADMIN_HOME=http://localhost:8090/`、`VITE_PORTAL_HOME=http://localhost:8088/account/profile`、`VITE_PORTAL_LOGIN_URL=http://localhost:8088/login`、`VITE_SITE_NAME=统一身份认证` |
| `admin` | `VITE_PORTAL_HOME=http://localhost:8088/account/profile`、`VITE_PORTAL_LOGIN_URL=http://localhost:8088/login`、`VITE_SITE_NAME=统一身份认证` |

不传则产物里是 dev 缺省值（`:5174` / `:5173`），「去后台」与「会话失效回跳」都会跳到空端口。

**编排插值变量（2026-10-24 实现口径，只给 compose 用，不进容器）**

| 变量 | 用途 | 缺省 |
|---|---|---|
| `IMAGE_TAG` | 三镜像同一 tag；缺失时 `compose config` 直接报错 | 无（必填） |
| `API_UPSTREAM` | 前端 nginx 反代目标（一体化 `http://sso:8081`，test/prod `http://api.<主域>`） | 无（必填） |
| `FRONTEND_BIND_ADDR` / `PORTAL_HOST_PORT` / `ADMIN_UI_HOST_PORT` | 门户与后台的公网监听 | `0.0.0.0` / `8088` / `8090` |
| `SSO_IDP_BIND_ADDR` / `SSO_IDP_HOST_PORT` | `:8080` OIDC 端口回落 | `127.0.0.1` / `8080` |
| `SSO_API_BIND_ADDR` / `SSO_API_HOST_PORT` | `:8081` 管理 API 端口回落 | `127.0.0.1` / `8081` |

> `IMAGE_TAG` 与 `API_UPSTREAM` 用 `:?` 强制必填：给默认值只会在配错时静默指向不存在的地址，容器表现为反复重启，比直接报错难排查。
> 注意 `ADMIN_UI_HOST_PORT`（后台**页面**，8090）与 `.env.example` 里的 `ADMIN_HOST_PORT`（管理 **API**，8081）含义不同，别混用。

## 5. 环境模板（`deploy/env/*.example`）

必须包含且仅包含占位符（不得出现真实密钥）：

| 分组 | 变量 |
|---|---|
| 基础 | `APP_ENV`、`LOG_LEVEL` |
| OIDC | `OIDC_ISSUER`、`OIDC_SIGNING_KEY_PATH`、`OIDC_SIGNING_KEY_ID`、`OIDC_CRYPTO_KEY` |
| 会话 | `COOKIE_DOMAIN`、`COOKIE_SECURE`、`COOKIE_SAME_SITE` |
| 数据 | `DB_*`、`DB_MAX_*`、`REDIS_*` |
| 反代 | `TRUSTED_PROXIES` |
| 通道 | `SMS_*`、`MAIL_*` |
| 存储 | `OSS_*` |

## 6. 验收断言

| # | 断言 | 期望 |
|---|---|---|
| 1 | `make check` | 环境变量三向一致，0 漂移 |
| 2 | `grep -c 'TRUSTED_PROXIES' internal/config/config.go .env.example AGENTS.md` | 三处均 ≥ 1 |
| 3 | `docker run --rm -e TRUSTED_PROXIES=bad ... omino-sso` | 启动失败，日志含 `TRUSTED_PROXIES` 与非法值 |
| 4 | `grep -rn 'example.com' web/nginx/` | 0 命中（nginx 配置不含环境域名）。**不要**用 `docker history` 判断：`VITE_*` 是构建期变量，其值会作为 `--build-arg` 出现在历史里，这是设计如此 |
| 5 | `docker compose -f deploy/compose.prod.yml config \| grep -E '^\s+(mysql\|redis):'` | 无匹配（不启本地 DB/Redis）。注意别写成 `grep -E 'mysql\|redis'`：`DB_HOST=...mysql.rds...`、`REDIS_ADDR=...` 也会命中 |
| 6 | `make image-check DOMAIN=<主域> FOREIGN_DOMAIN=<另一环境主域> IMAGE_TAG=<tag>` | `nginx -t` 通过、镜像内无 `default.conf`、产物不串环境 |
| 7 | `docker run --rm <portal 镜像> nginx -t` | 通过（运行阶段已注入回环默认 `API_UPSTREAM`，故无需额外 env 即可自检） |
