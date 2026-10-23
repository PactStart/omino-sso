# Phase 0 Research: 容器化多域名部署

**Feature**: `007-deployment` | **Date**: 2026-10-23

本文件固化 [plan.md](./plan.md) 中 D1–D9 的调研结论。每条按「Decision / Rationale / Alternatives considered」记录，避免后续实现时重新争论。

---

## R1 探针的实现位置与响应形态（对应 D1）

**Decision**：`GET /healthz`（存活）与 `GET /readyz`（就绪）在**两个 server 上各自注册**，路径都在根（不在 `/api/v1` 下）；`/healthz` 返回 `200 {"status":"ok"}`，`/readyz` 成功 `200 {"status":"ready"}`、失败 `503 {"status":"unready","failing":["mysql"]}`。均不套 `{code,message,data,traceId}`，不加 swag 注解，不进 `openapi.json`。

**Rationale**：

- 统一响应是 `/api/v1/**` 对外契约（宪法「技术与接口约束」），探针是运维面，套壳会让 `docker healthcheck` / LB 需要解析业务信封，且把运维路径混进 API 文档。
- 不加注解 → `make docs-check` 零漂移，避免为了两个探针重新生成并评审 1500 行 `openapi.json`。
- `scripts/check-registry.sh` 只比对 `/api/v1*` 路由，因此探针不会强制进 §4.1 总览；登记放在 `AGENTS.md §4.0`（人工项），保证"文档里能找到"。
- `:8080` 用 `http.ServeMux` 直接挂（与 `/login`、`/logout` 同级），`:8081` 在 `gin.Engine` 上 `r.GET`；两侧各自实现，符合宪法 II（两端口不共享中间件栈）。

**Alternatives considered**：

- ① 挂到 `/api/v1/healthz`：会被统一响应与限流中间件覆盖，LB 探活还要吃限流配额。
- ② 只做 `:8081` 一个探针：`:8080` 挂了但 `:8081` 活着时容器仍报健康，OIDC 全站不可用却探测不到。
- ③ 引入 `github.com/heptiolabs/healthcheck` 等库：多一个依赖换 20 行代码，且其响应格式不可控（违反 §2.0 的准入必要性判断）。

---

## R2 可信代理判定算法（对应 D2）

**Decision**：新增 `internal/pkg/clientip`，签名 `FromRequest(r *http.Request, trusted []netip.Prefix) string`：

1. 解析 `RemoteAddr` 的 IP；若 `trusted` 为空或不包含该 IP → 直接返回该 IP（**忽略 XFF**）。
2. 否则解析 `X-Forwarded-For`，**从右向左**取第一个不在 `trusted` 内的地址返回；全部命中则回退到最左值。

`internal/api` 侧另设 `engine.SetTrustedProxies(list)`——`list` 为空时传 `nil`（Gin 关闭信任链，`ClientIP()` 退化为 `RemoteAddr`）。`:8080` 的 `login.go:clientIP()` 删除自有实现，改调 `clientip.FromRequest`。

**Rationale**：

- 现状 `login.go:clientIP()` **无条件信任 XFF**，任意客户端可伪造审计 IP；这是既有缺陷，与 FR-014 同源，顺手收敛到一套实现，避免两套口径漂移（Gin 用 `SetTrustedProxies`，协议层用自己的函数，必须有同一个判定语义）。
- 从右向左取是 RFC 7239 / 主流实现的稳妥做法：左侧可被客户端伪造，右侧由最近的代理追加。
- 空值即"不信任任何代理"是安全默认；本地 `go run` 直连时行为不变。

**Alternatives considered**：

- ① 只配 Gin 的 `SetTrustedProxies`：协议层仍裸信 XFF，审计 IP 仍可伪造（不满足 FR-014）。
- ② 引入 `github.com/tomasen/realip`：逻辑等价，但多一个依赖，且不支持"从右向左跳过可信段"的可配置语义（§2.0 准入不划算）。
- ③ `TRUSTED_PROXIES` 用固定常量 `127.0.0.1,::1`：云环境反代在 VPC 网段，无法覆盖。

---

## R3 nginx 反代目标的注入方式（对应 D5）

**Decision**：`API_UPSTREAM` 通过官方 nginx 镜像的模板机制注入——`web/nginx/templates/*.conf.template`（含 `${API_UPSTREAM}`）在容器启动时由 `envsubst` 渲染到 `/etc/nginx/conf.d/`。浏览器侧 `VITE_API_BASE_URL` 恒为 `/api/v1`。

**Rationale**：

- 镜像要对 test/prod/一体化三套环境复用，域名不能进镜像（否则三套镜像 + 三个 tag，回滚复杂）。
- nginx 官方镜像已内置 `20-envsubst-on-templates.sh`，不引入新工具、不写 entrypoint。
- 反代只改一段 upstream，`proxy_pass ${API_UPSTREAM};` 配合 `proxy_set_header Host $host`、`X-Forwarded-For/Proto` 透传；`proxy_pass` 不带 URI 时保留原始路径（`/api/v1/...` 原样转发），因此 backend 的 `/api/v1` 前缀无需改写。

**Alternatives considered**：

- ① 构建期 `--build-arg API_UPSTREAM` 写死进 conf：同一镜像不能跨环境复用，与 FR-010「按环境构建」的初衷相悖（前端产物需要按环境，反代配置不需要）。
- ② 挂载 conf 文件：编排里挂载点变多，且 compose/k8s 迁移时要重做。
- ③ 用 `proxy_pass http://api.example.com/api/v1/`（带 URI 重写）：一旦带 URI，nginx 会替换匹配段，`/api/v1` 前缀处理容易出双斜杠，实测坑多；不带 URI 最稳。

---

## R4 前端镜像构建（对应 D6）

**Decision**：`web/Dockerfile` 单文件双 target（`portal` / `admin`），构建阶段 `node:22-alpine` + `npm ci` + `npm run build`，运行阶段 `nginx:1.27-alpine`。构建参数：portal 传 `VITE_ADMIN_HOME`/`VITE_PORTAL_HOME`/`VITE_PORTAL_LOGIN_URL`/`VITE_SITE_NAME`；admin 传后三项。`VITE_API_BASE_URL` 不传（默认 `/api/v1`）。

**Rationale**：

- Node 未列入 AGENTS §2.2 锁定表，选择当前 LTS 的 22 线即可；不引入 `pnpm`/`turbo` 等新工具。
- 双 target 共享依赖安装层，`docker build --target portal|admin` 语义清晰，与 DEPLOYMENT.md §5 已写好的命令一致。
- 运行阶段用 nginx 官方镜像（非自建 alpine+nginx），减少维护面；`index.html` 不缓存、`assets/*` 长缓存的策略写在 conf 里（FR-021）。

**Alternatives considered**：

- ① 前端产物塞进后端镜像由 Go 做静态托管：与"前端 nginx 反代 + 独立域名"拓扑冲突（同源反代需要 nginx），且会让后端镜像随前端发布滚动。
- ② 用 `vite preview` 当运行时：非生产级服务器，且不支持反代/缓存头。
- ③ 运行时 `config.js` 注入：spec Non-Goals 明确排除。

---

## R5 跨域回跳 `next` 的形态（对应 D4）

**Decision**：admin 跳门户登录时传**绝对地址**：`https://portal.<主域>/login?next=<encodeURIComponent(https://admin.<主域>/users)>`；门户侧白名单 = `[window.location.origin, new URL(ADMIN_HOME).origin]`，命中用 `window.location.assign()` 整页跳转。相对 `next`（以 `/` 开头）保持现有行为。

**Rationale**：

- 现状传相对路径，门户 `router.replace('/users')` 会在门户域名内找路由 → 落到 404，FR-009「回到 admin 域原路径」实际不成立；这是本特性必须修的现存缺陷。
- 用 `ADMIN_HOME` 的 origin 做白名单，不新增环境变量，也不依赖后端下发；`ADMIN_HOME` 本身就是"后台地址"的唯一来源。
- 整页跳转（`location.assign`）而非 SPA 路由跳转：跨源必须整页，且与现有 `goAdminHome()` 的实现一致。

**Alternatives considered**：

- ① 增加 `?next=/users&from=admin` 之类的语义参数：门户仍要维护"哪些前缀属于后台"的映射，比 origin 校验更脆。
- ② 后端下发回跳白名单（新接口/新环境变量）：为一次跳转引入配置项与端点，成本失衡。
- ③ 不做回跳，登录后统一进门户首页：不满足 FR-009 与 SC-002。

---

## R6 一体化 compose 的形态（对应 D7）

**Decision**：`docker-compose.yml` 在既有 mysql/redis/sso 之外补 `portal`（8088→80）与 `admin`（8090→80）；前端容器反代 `API_UPSTREAM=http://sso:8081`；`COOKIE_DOMAIN` 留空、`COOKIE_SECURE=false`、`APP_ENV=dev`。

**Rationale**：

- 四端口共用 `localhost`，cookie 不区分端口 → 天然共享会话，不需要 hosts、不需要 `.localhost` 域、不需要 CORS。
- 与线上唯一差异是 `API_UPSTREAM` 的值，前端产物与 nginx 逻辑完全一致，本地验证过的行为在生产可复现。
- 保留 `make dev` / `npm run dev` 路径（FR-020）：dev 用 vite 代理，不需要 compose。

**Alternatives considered**：

- ① 本地也搭 hosts（`portal.localhost` 等）：需要改 hosts 文件与证书，收益仅是"更像线上"。
- ② 本地用 nginx 统一入口 + 路径前缀（`localhost/admin`）：与 FR-004 去前缀冲突。
- ③ 本地前端直连 `localhost:8081`：跨源，需 CORS，与"不引入 CORS"冲突。

---

## R7 admin 去前缀的兼容处理（对应 D3）

**Decision**：不做 `/admin/*` → `/*` 的重定向，直接删除旧路径；`/:pathMatch(.*)*` 仍是 404 页。

**Rationale**：后台未上线，无存量书签；保留重定向会让"两套路由"长期存在，与 FR-004「MUST NOT 再出现 `/admin/*` 前缀」直接冲突。反向代理/nginx 也不需要为旧路径写规则。

**Alternatives considered**：

- ① 301 重定向到去掉前缀的路径：看似平滑，但新增一条永久路由，且 `admin.<主域>/admin/users` 会短暂出现在浏览器历史里，与"根路径化"的目标相悖。
- ② 同时保留两套路由：维护成本翻倍，菜单高亮与 tab 计算都要双份判断。

---

## R8 探针的暴露面（对应 D8）

**Decision**：`sso` 域 nginx 只放行 `location = /healthz`；`/readyz` 不配置 location（落到 SPA 兜底或 404，对外表现为 404）。`api` 域整体不对外网解析；容器 healthcheck 打 `127.0.0.1:8080/healthz` 与 `127.0.0.1:8081/healthz`。

**Rationale**：`/readyz` 会暴露 MySQL/Redis 连通性与失败项，属"给攻击者指路"的信息；外部 LB 只需要"进程是否活着"。spec 澄清项已按此定稿（FR-015）。

**Alternatives considered**：

- ① 两个都公开：便于外部做完整就绪探活，但泄露依赖状态。
- ② 两个都不公开：外部 LB 只能 TCP 探活，无法区分"端口在听"与"进程正常"。

---

## R9 不引入 CORS（对应 D9）

**Decision**：后端不实现 CORS 中间件，不新增 `CORS_ALLOWED_ORIGINS`；前端所有请求同源相对路径。

**Rationale**：浏览器视角只有 `portal` / `admin` 两个源，XHR 全部发给自己域名的 `/api/`，由 nginx 转给 `api` 域。CORS 是纯增量成本（预检、精确 Origin、`Allow-Credentials`、cookie 域），收益为零。

**Alternatives considered**：

- ① 前端直连 `api` 域 + CORS：方案 A，被否（2026-10-23 决策）。
- ② 只对 `OPTIONS` 放行、不校验 Origin：等于把管理 API 开放给任意站点，安全性更差。

---

## R10 就绪检查复用 `store.Ping`（对应 FR-015）

**Decision**：`/readyz` 调既有 `(*store.Store).Ping(ctx)`（内含 MySQL `PingContext` + Redis `PING`），超时 1s；失败项以数组返回但不含内部地址/错误原文。

**Rationale**：`store.Ping` 是 `op.Storage.Health` 用的廉价探针（不加锁、不开事务），语义正好；避免为探针新增 SQL 或连接池逻辑。不回传错误原文，避免泄露 DSN 片段。

**Alternatives considered**：

- ① 直接 `db.Exec("SELECT 1")`：多一次查询规划，且需要 `store` 暴露 `RawDB`（内部接口，不该给 api 层用）。
- ② 分开两个端点（`/readyz/mysql`、`/readyz/redis`）：容器 healthcheck 只关心聚合结果。

---

## R11 `TRUSTED_PROXIES` 的登记与配置校验

**Decision**：`TRUSTED_PROXIES` 为逗号分隔的 CIDR/单 IP 列表（如 `10.0.0.0/8,172.16.0.0/12`），默认空；非法项**启动失败**并指出具体值。`APP_ENV=prod` 时**不强制必填**（单机部署可能直连），但 `DEPLOYMENT.md` 与验收清单要求显式确认。

**Rationale**：`scripts/check-registry.sh` 会强制三处一致（`config.go` / `.env.example` / `AGENTS.md §9`），所以"登记"不是可选项；合法性校验放启动期，避免运行期静默忽略导致审计 IP 全错。不做 prod 强制，是因为强制会让"反代与容器同机、用 Unix socket/127.0.0.1"的合法部署无法启动。

**Alternatives considered**：

- ① prod 强制必填：破坏本地/单机合法形态。
- ② 非法项降级为忽略：审计 IP 会静默错误，排查成本远高于启动失败。

---

## R12 前端构建基础镜像版本

**Decision**：`node:22-alpine`（构建）、`nginx:1.27-alpine`（运行）。不写入 AGENTS §2.2 依赖表（Node/nginx 是**部署工具链**，不是前端 npm 依赖），但写入 `DEPLOYMENT.md` §5 的构建前置说明。

**Rationale**：Node 不在锁定表内，选当前 LTS；nginx 1.27 为稳定主线，`envsubst` 脚本内置于官方镜像。两者都只影响镜像构建，不改变本地开发。

**Alternatives considered**：`node:20-alpine`（已进入维护末期）、`nginx:stable-alpine`（版本随上游漂移，构建不可复现）——均不如显式版本可控。
