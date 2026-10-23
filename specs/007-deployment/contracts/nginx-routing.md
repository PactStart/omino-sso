# 契约：四域 nginx 路由与反代

**Feature**: `007-deployment` | **覆盖需求**: FR-001 / FR-003 / FR-007 / FR-015 / FR-021

## 1. 总览

| 域名 | 公网 | server 块 | 主要 location | upstream |
|---|:--:|---|---|---|
| `portal.<主域>` | ✔ | `web/nginx/portal.conf` | `/api/` → 反代；其余 SPA fallback | `${API_UPSTREAM}` |
| `admin.<主域>` | ✔ | `web/nginx/admin.conf` | 同上（根路径托管，无 `/admin` 前缀） | `${API_UPSTREAM}` |
| `api.<主域>` | ✘（仅内网 DNS） | `web/nginx/api.conf` | `/api/v1/` → `sso:8081` | `sso:8081` |
| `sso.<主域>` | ✔ | `web/nginx/api.conf` 同文件的第二个 server 块（或独立 conf） | `/oauth2/`、`/.well-known/`、`/login`、`/logout`、`/logged-out`、`= /healthz` | `sso:8080` |

## 2. portal / admin（静态 + 同源反代）

必须满足：

1. **反代路径**：`location /api/ { proxy_pass ${API_UPSTREAM}; }` —— `proxy_pass` **不带 URI**，保证 `/api/v1/...` 原样转发（带 URI 会做前缀替换，易产生双斜杠）。
2. **透传头**：`Host`、`X-Real-IP`、`X-Forwarded-For`（追加而非覆盖）、`X-Forwarded-Proto`、`X-Forwarded-Host`。
3. **超时**：`proxy_connect_timeout 5s`、`proxy_read_timeout 30s`（与前端 axios 15s 超时错开，让前端先超时以便给出友好提示）。
4. **请求体上限**：`client_max_body_size 10m`（Logo/头像直传走 OSS 预签名，但确认接口可能带大 JSON）。
5. **SPA fallback**：`location / { try_files $uri $uri/ /index.html; }`。
6. **缓存**：`location ~* \.(js|css|woff2?|png|jpe?g|svg|ico)$ { expires 1y; add_header Cache-Control "public, immutable"; }`；`location = /index.html { add_header Cache-Control "no-store"; }`。
7. **gzip**：`gzip on` + `gzip_min_length 1024` + `gzip_proxied any`，`gzip_types` **只列** `text/css application/javascript application/json image/svg+xml`。`text/html` 由 nginx 恒定压缩（内建默认类型），把它写进 `gzip_types` 会触发 `duplicate MIME type "text/html"` 警告（`nginx -t` 判定为配置告警，T061 的自检会带出来），故实现刻意不列；请勿为"字面对齐旧口径"补回去。
8. **隐藏版本**：`server_tokens off`。
9. **不得**出现任何 CORS 相关头（`add_header Access-Control-*`）——本项目不跨源。
10. **探针**：`location = /healthz { access_log off; add_header Cache-Control "no-store"; return 200 "ok"; }`。portal 与 admin 都必须有，否则容器 healthcheck（打自身 `/healthz`）恒 404 → 永远 unhealthy。`/readyz` **必须显式 404**：`location = /readyz { access_log off; return 404; }`。
   *2026-10-24 修正*：原先写"不配置"，实现时被 `location / { try_files … /index.html; }` 兜成 **200 的 index.html**（实测），监控会把"能返回 HTML"当成"依赖已就绪"，违反 FR-015。前端确实不探依赖，但公网必须 404。

**反例（视为缺陷）**：`proxy_pass ${API_UPSTREAM}/api/v1/;`（改写了路径）、`add_header Access-Control-Allow-Origin *;`、`location /admin/`（admin 已根路径化）、缺 `location = /healthz`。

**启动期解析**：`proxy_pass` 用静态主机名时 nginx 在**启动时**解析；DNS 或后端未就绪会让容器直接退出并重启循环。一体化用 `depends_on: {sso: {condition: service_healthy}}`，test/prod 用 `restart: unless-stopped`，排障表登记"前端容器反复重启 → 查 upstream 解析"。

## 3. api 域

1. 只允许 `location /api/v1/ { proxy_pass http://sso:8081; }`，`/docs` 在 prod 由后端自行 404（无需 nginx 规则）。
2. **不配置** `ssl_certificate`（内网明文，见 spec 澄清项）。
3. `server_name api.<主域>;` 且该域名**只在 VPC 内 DNS 解析**；验收时以「公网 `curl` 连不通」为准（SC-007）。
   *实现口径（2026-10-24）*：`web/nginx/api.conf` 里写的是 `server_name _;`（独占入口 nginx 时可用）；**与其它站点共用同一 nginx** 时必须按该文件头部的"部署前必须替换"清单改成 `api.<主域>` / `sso.<主域>`，否则默认 server 归属不确定。
4. 反代到 `sso:8081` 时同样透传 `X-Forwarded-*`，否则后端看到的客户端 IP 是前端容器 IP。
5. **边界说明**：`sso` 容器同时监听 `8080/8081`，容器网络内任何服务都能直连 `:8081`；`api` 域只是"谁被允许作为入口"的**唯一命名入口**，实际隔离由内网 DNS + 安全组/网络 ACL 强制。不得以"域名存在与否"作为安全验收依据。

## 4. sso 域

1. 反代 `/oauth2/`、`/.well-known/`、`/login`、`/logout`、`/logged-out` → `http://sso:8080`。
2. **`location = /healthz`** 放行到 `:8080`；**不配置** `/readyz` location（公网必须 404，FR-015）。
3. 不得把 `/api/v1/*` 转发到 `:8081`（spec FR-002/FR-003：api 与 sso 职责分离）。
4. TLS 终止在此（或上游 LB）；`proxy_set_header X-Forwarded-Proto $scheme` 必须设置，否则 OIDC issuer 校验与回跳协议会错。

## 5. 上游注入契约

- 模板：`web/nginx/templates/<domain>.conf.template`，变量为 `${API_UPSTREAM}`。
- 渲染：nginx 官方镜像的 `20-envsubst-on-templates.sh`，输出到 `/etc/nginx/conf.d/<domain>.conf`。
- 取值：
  - 一体化 compose：`API_UPSTREAM=http://sso:8081`
  - test/prod：`API_UPSTREAM=http://api.<主域>`（内网 LB 亦可）
- **禁止**把具体域名写进镜像内的静态 conf（否则镜像不可跨环境复用）。
- **禁止裸 `envsubst`**（`envsubst < tpl > conf`）：它会把 `$host`、`$uri`、`$remote_addr` 一起替换成空串，配置"能起来但头全错"。只使用官方镜像的 `/etc/nginx/templates/*.template` 机制（其脚本只替换已定义的环境变量）。
- **单一模板源（2026-10-24 实现口径）**：`web/nginx/templates/{portal,admin}.conf.template` 是唯一内容载体，`web/nginx/{portal,admin}.conf` 是指向它的符号链接（§1 的"server 块文件"以链接形式存在，避免两份内容漂移）；`web/Dockerfile` 只 COPY `templates/`。`web/nginx/api.conf` 无变量，保持静态且**不进前端镜像**（部署在入口 nginx / 宿主机 nginx）。
- **必须删除官方 `default.conf`（2026-10-24 实现口径）**：`nginx:1.27-alpine` 自带 `/etc/nginx/conf.d/default.conf`（`server_name localhost`），它是 80 端口的默认 server；不清掉时渲染出的 `<domain>.conf` 永远不接流量 → 全站 404。两个 target 的运行阶段都必须 `RUN rm -f /etc/nginx/conf.d/default.conf`。
- **`X-Forwarded-Proto` 取值（2026-10-24 实现口径）**：portal/admin 在 LB 之后终止 TLS，容器内 `$scheme` 恒为 `http`，故用 `map $http_x_forwarded_proto $<domain>_forwarded_proto { default $http_x_forwarded_proto; "" $scheme; }`（LB 有值则沿用，缺失退回 `$scheme`）；sso 域在本机终止 TLS，直接用 `$scheme`。

## 6. 验收断言（可脚本化）

| # | 断言 | 期望 |
|---|---|---|
| 1 | `curl -I https://portal.<主域>/users`（深链） | 200，`text/html`，`Cache-Control: no-store` |
| 2 | `curl -I https://portal.<主域>/assets/<hash>.js` | 200，`Cache-Control` 含 `immutable` |
| 3 | `curl -s -o /dev/null -w '%{http_code}' https://portal.<主域>/api/v1/me` | 401（同源反代生效，未带 cookie） |
| 4 | 公网 `curl http://api.<主域>/api/v1/me` | 连接失败（DNS 不解析/超时） |
| 5 | `curl -s -o /dev/null -w '%{http_code}' https://sso.<主域>/readyz` | 404 |
| 6 | `curl -s -o /dev/null -w '%{http_code}' https://sso.<主域>/healthz` | 200 |
| 7 | 浏览器网络面板任意 `/api/v1/*` | 无 `OPTIONS` 预检；URL 为当前域名相对路径 |
