# Quickstart 验证指南：容器化多域名部署

**Feature**: `007-deployment` | **Date**: 2026-10-23

本文件只做**可执行验收**：每条给出命令与期望输出。实现细节见 [plan.md](./plan.md)，契约见 [contracts/](./contracts/)。

---

## 0. 前置

```bash
# 仓库根执行
make deps assets keys         # 依赖、邮件模板、开发签名密钥
cp .env.example .env          # 本地一体化不需要改 .env（compose 有默认值）
```

- 需要 Docker 27+/compose v2、Go 1.26、Node 22、MySQL 8.4/Redis 7.4（一体化由 compose 提供）。
- 四域名部署还需要：DNS A 记录 × 4、通配符证书 × 1（或 3 张单域名证书，`api` 域不需要）。

---

## 1. 本地一体化（P2，最快路径）

```bash
docker compose up -d --build
docker compose ps            # 期望：5 个服务 Up，sso/portal/admin 为 healthy
```

**浏览器验证**

| 步骤 | 期望 |
|---|---|
| 打开 `http://localhost:8088/login`，用初始化管理员登录 | 成功，cookie `sid` 的 Domain 为空（host-only，localhost 共享） |
| 打开 `http://localhost:8090/` | 直接进「用户管理」，**地址栏无 `/admin` 段** |
| 打开 `http://localhost:8090/clients` → 进入某应用 → 切到「成员」并刷新 | 正常渲染，不 404 |
| 浏览器网络面板看任意 `/api/v1/*` | URL 为 `localhost:8090/api/v1/...`（同源），**无 OPTIONS 预检** |
| 门户点「去后台」 | 落到 `http://localhost:8090/`（**不得**跳到 `admin.example.com` 或 `:5174`） |
| 后台清 cookie 后访问 `http://localhost:8090/users` | 跳 `http://localhost:8088/login?next=...`（**不得**跳 `:5173` / 生产域名）；登录后回到 `/users` |

**命令行验证**

```bash
docker compose exec sso wget -qO- http://127.0.0.1:8081/healthz      # {"status":"ok"}
docker compose exec sso wget -qO- http://127.0.0.1:8081/readyz       # {"status":"ready"}
docker compose stop redis && docker compose exec sso wget -qO- http://127.0.0.1:8081/readyz
# 期望：503 + {"status":"unready","failing":["redis"]}；随后 docker compose start redis

# 探针不受限流（把配额压到 1 后连打 10 次，必须 10 次全成功）
docker compose exec sso sh -c 'for i in $(seq 10); do wget -qO- http://127.0.0.1:8081/healthz >/dev/null && echo ok; done' | grep -c ok
# 期望：10（若出现 429，说明 RateLimit 没对探针短路）

# 容器 healthcheck 依赖 nginx 的 location = /healthz
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8090/healthz   # 200
```

**保留的开发方式（FR-020）**

```bash
make dev                      # :8080 + :8081
cd web/portal && npm run dev  # :5173（vite /api 代理 → :8081）
cd web/admin  && npm run dev  # :5174
```

---

## 2. 测试/生产（P1）

### 2.1 构建

```bash
# 三镜像一条命令（前端绝对地址由 DOMAIN 派生；等价的手工 docker build 见 DEPLOYMENT.md §5）
make images DOMAIN=example.com IMAGE_TAG=2026.10.23-1
```

**断言**：三镜像 tag 相同；`docker history` 中不含 `example.com`（域名只来自构建参数与 `API_UPSTREAM`）。

**串环境检查**（读镜像内产物，2 个参数一组环境）：

```bash
PORTAL_IMAGE=registry.example.com/omino-portal:2026.10.23-1 \
ADMIN_IMAGE=registry.example.com/omino-admin:2026.10.23-1 \
EXPECT_DOMAIN=example.com FOREIGN_DOMAIN=example-test.com \
  ./scripts/check-build-env.sh
```

### 2.2 迁移与启动

```bash
cp deploy/env/prod.env.example /etc/omino-sso/prod.env   # 填真值（600）
DB_HOST=... DB_PORT=3306 ... make migrate-up             # 手动执行，见 DEPLOYMENT.md §6
docker compose -f deploy/compose.prod.yml --env-file /etc/omino-sso/prod.env up -d
docker compose -f deploy/compose.prod.yml ps             # 三容器 healthy
```

**断言**：`docker compose -f deploy/compose.prod.yml config | grep -E '^\s+(mysql|redis):'` **无输出**（不启本地 DB/Redis）。
> 注意别写成 `grep -E 'mysql|redis'`：`DB_HOST=...mysql.rds...`、`REDIS_ADDR=...` 也会命中，那是误报。

---

## 3. 四域名验收（对照 SC）

> **占位符来源**（下面命令里的 `<主域>`、`<cid>`、`<uri>`、`<id>`）：`<主域>=example.com`；`<cid>` 取管理端「应用管理」列表里的 clientId；`<uri>` 用该应用登记的回调地址；`<id>` 取用户管理里的用户 ULID。
>
> **一条命令跑完探针/暴露面/深链/缓存/同源反代**（只读、无副作用，需在公网机器执行）：
>
> ```bash
> DOMAIN=example.com ./scripts/smoke-deploy.sh      # 自签证书环境加 INSECURE=1
> ```
>
> 下面的 curl 是同一批断言的逐条展开，排障时用。

```bash
# SC-001 探针
curl -s -o /dev/null -w '%{http_code}\n' https://sso.example.com/healthz          # 200
curl -s -o /dev/null -w '%{http_code}\n' https://sso.example.com/readyz           # 404

# SC-001 issuer
curl -s https://sso.example.com/.well-known/openid-configuration | grep -o '"issuer":"[^"]*"'
# 期望：issuer 与 https://sso.example.com 完全一致（无尾斜杠）

# SC-007 api 域公网不可达（反证项，必须在公网机器执行）
curl -m 5 -sS http://api.example.com/api/v1/me ; echo "exit=$?"   # 期望：连不通（exit 非 0）

# FR-021 深链与缓存
curl -sI https://admin.example.com/clients/abc/members | head -1  # 200
curl -sI https://admin.example.com/index.html | grep -i cache-control   # no-store
curl -sI https://admin.example.com/assets/$(curl -s https://admin.example.com/ | grep -o 'assets/[^"]*\.js' | head -1) | grep -i cache-control
# 期望：public ... immutable
```

**浏览器侧（SC-002 / SC-007 / FR-009）**

1. 门户登录 → 打开 `https://admin.example.com/users` → **无需二次登录**，地址栏无 `/admin`。
2. 打开 `https://admin.example.com/clients/<id>/members` → 刷新 → 仍在该页。
3. 清 cookie 后直接打开上述深链 → 跳 `https://portal.example.com/login?next=...` → 登录 → **回到同一深链**。
4. 网络面板：所有 `/api/v1/*` 请求同源、无 OPTIONS；cookie 的 Domain 为 `.example.com`、`Secure`、`HttpOnly`、`SameSite=Lax`。
5. 主动登出 → 落 `https://portal.example.com/login?loggedOut=1`（显示"已退出"，地址栏参数被清掉）→ 再访问 admin 要求重新登录。
6. 会话失效语义（分三类，见 [data-model.md §4.1](./data-model.md)）：
   - 登出、凭原密码改密 → 旧 `sid` 必须 401；改密响应头 `Set-Cookie` 必须带 `Domain=.example.com`；
   - **验证码重置**（`POST /api/v1/me/password/reset`）→ 当前 `sid` **仍有效**（`/me` 200），另一台设备的 `sid` 401（回归保护，防止把 reset 误并进统一清 cookie）；
   - 会话过期 → 401 但不清 cookie。

**协议面（不回退）**

```bash
curl -s -o /dev/null -w '%{http_code}\n' 'https://sso.example.com/oauth2/authorize?client_id=<cid>&response_type=code&scope=openid&redirect_uri=<uri>&code_challenge=<S256>&code_challenge_method=S256'
# 期望：302 到 https://sso.example.com/login?id=... （未登录）
```

---

## 4. 可信代理（FR-014 / SC-004）

```bash
# 直连伪造 XFF：不应被采信
curl -s -H 'X-Forwarded-For: 1.2.3.4' https://api.example.com/api/v1/me -o /dev/null
# 查审计/日志中的 IP：应为真实客户端 IP 或代理 IP，绝不出现 1.2.3.4

# 经反代正常访问后核对审计
# 期望：审计 audit_events.ip 为真实客户端 IP；未配 TRUSTED_PROXIES 时为反代 IP
```

**断言**：`TRUSTED_PROXIES` 为空时，伪造的 `X-Forwarded-For` 完全不生效；配置为反代网段后，真实 IP 生效。

**多跳反证（必做，5 分钟）**：把 `TRUSTED_PROXIES` 中**前端容器网段**临时去掉，重启 `sso`，从门户页面做一次登录，再看审计日志：
- 期望（错误示范）：`audit_events.ip` 变成前端容器 IP（`172.x`）——说明网段漏配；
- 加回后应为真实客户端 IP。

> 这一条比"配对了就通过"更重要：漏配时系统**不会报错**，只会静默记录错误的 IP。

---

## 5. 门禁

```bash
make test          # go test + portal vitest
make lint          # golangci-lint + 两个前端 eslint
make build         # 含 docs-check（探针不得进入 openapi）
make build-web     # 两个前端产物
make check         # check-registry（含 TRUSTED_PROXIES 三向）+ 文档命令可执行
```

---

## 6. 常见失败对照

| 现象 | 先查 |
|---|---|
| 门户登录成功、进后台又要登录 | `COOKIE_DOMAIN` 是否 `.example.com`（host-only 必挂） |
| 所有 `/api/` 请求 502 | 前端容器 `API_UPSTREAM` 是否可达（`getent hosts api.example.com` / `sso:8081`） |
| 后台根路径 404 | admin 镜像是否 `base='/'`、路由是否已去前缀、nginx 是否有 SPA fallback |
| 公网能打开 `api` 域 | DNS/安全组漏了内网限制（安全事故，见 contracts/nginx-routing.md §3） |
| 审计 IP 全是容器网段 | `TRUSTED_PROXIES` 未配或与反代网段不符 |
| 浏览器报 CORS | 构建误传了 `VITE_API_BASE_URL` 绝对地址（应为 `/api/v1`） |
