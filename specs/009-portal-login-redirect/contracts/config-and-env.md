# Contract: 配置项与启动校验

**Feature**: `009-portal-login-redirect`

所有变量由 `internal/config` 统一加载并在启动时校验；校验失败必须**非零退出**并指明变量名（宪法「技术与接口约束」）。新增/变更项必须同一次提交内同步：`internal/config/config.go` ↔ `.env.example` ↔ `AGENTS.md §9`（`scripts/check-registry.sh` 会双向比对，缺一处即 fail）。

## 1. 新增：`PORTAL_LOGIN_URL`

| 项 | 内容 |
|---|---|
| 名称 | `PORTAL_LOGIN_URL` |
| 必填 | `prod` 是；`test` / `dev` 否 |
| 默认 | 空（= 不启用门户跳转，回落内置登录页） |
| 示例 | `https://portal.example.com/login` |
| 用途 | 未登录 OIDC 授权的落点（门户登录页） |
| 归属文件 | `.env.example`（留空 + 注释）、`deploy/env/{test,prod}.env.example`、`docker-compose.yml` 的 `sso.environment` |

**格式校验**（非空时）：解析为 URL 且 `Scheme ∈ {http, https}`、`Host != ""`、无 `Fragment`。不合法 → 启动失败：`PORTAL_LOGIN_URL must be an absolute URL like https://portal.example.com/login`。

**协议校验**：`APP_ENV=prod` 或 `APP_ENV=test` → 必须 `https`；`dev` 允许 `http`（本地 vite 5173）。

**与前端变量的关系**：后端 `PORTAL_LOGIN_URL` 与前端 `VITE_PORTAL_LOGIN_URL`（admin 用）语义相同、**取值必须一致**，但各自独立注入（前端是构建期、后端是运行期）。部署文档需写明这条，避免「admin 跳 portal 域、IdP 跳另一处」的错配。

## 2. 变更：`COOKIE_SAME_SITE`

| 项 | 变更前 | 变更后 |
|---|---|---|
| 允许值 | 任意字符串；`Strict` 生效，其它值静默当 `Lax` | **白名单**：`Lax`（大小写不敏感）或空 |
| `Strict` | 允许 | **启动失败**：`COOKIE_SAME_SITE=Strict is not allowed: cross-site redirects would drop the session` |
| 其它值 | 静默当 `Lax` | 启动失败：`unsupported COOKIE_SAME_SITE: <value>`（仅允许 Lax） |
| 代码 | `pkg/cookie` 按配置分支 | 恒发 `SameSite=Lax`（不再分支） |

## 2.1 构建期必填：`VITE_SSO_BASE_URL`（门户产物）

后端开关一旦打开，门户就必须信任 IdP 域。该变量缺失时前端会**静默退化**（白名单只剩自身 + admin），因此与 `VITE_ADMIN_HOME` 同款处理：

| 位置 | 要求 |
|---|---|
| `web/Dockerfile` portal target | `test -n "$VITE_SSO_BASE_URL" || exit 1`（构建期失败） |
| `docker-compose.yml` portal `build.args` | 传入 `http://localhost:8080`（本地一体化） |
| `deploy` 构建命令 / `web/portal/.env*.example` | 三套环境各自注入 `https://sso.<主域>` |
| `scripts/check-build-env.sh` | 断言镜像内 `sso.<本环境>` ≥1 次、`sso.<外来环境>` 0 次 |

> 与后端 `PORTAL_LOGIN_URL` 的关系：两者都指向"门户登录页 / 统一身份域"这一对事实，**必须成对配置**；只配一边即为不兼容组合（见 `portal-login-redirect.md` §6 兼容矩阵）。

## 3. 校验矩阵（启动即失败）

| 场景 | `APP_ENV=dev` | `test` | `prod` |
|---|---|---|---|
| `PORTAL_LOGIN_URL` 为空 | 启动（内置页） | 启动（内置页） | **失败** |
| `https://portal.example.com/login` | 启动 | 启动 | 启动 |
| `http://localhost:5173/login` | 启动 | **失败**（https） | **失败**（https） |
| `portal.example.com/login` | **失败**（绝对地址） | 失败 | 失败 |
| `https://portal.example.com/login#x` | **失败**（fragment） | 失败 | 失败 |
| `COOKIE_SAME_SITE=Strict` | **失败** | **失败** | **失败** |
| `COOKIE_SAME_SITE=lax` | 启动（大小写不敏感） | 启动 | 启动 |
| `COOKIE_SAME_SITE=None` | **失败** | 失败 | 失败 |

## 4. 注册一致性（`make check`）

新增变量后下列四处必须同时出现，否则 `scripts/check-registry.sh` 报「未登记 / 疑似废弃」：

1. `internal/config/config.go`：`env("PORTAL_LOGIN_URL", "")`
2. `.env.example`：`PORTAL_LOGIN_URL=`（含注释说明「留空 = dev 回落内置页」）
3. `AGENTS.md §9`：表格新增一行（必填列写「是（prod）」）
4. `AGENTS.md §9` 的 `COOKIE_SAME_SITE` 说明改为「仅 `Lax`」

审计事件同理：`internal/store/model/audit_event.go` ↔ `AGENTS.md §6.7` 枚举行 ↔ `web/admin/src/utils/format.ts` 的 `EVENT_LABEL`（§6.7 是脚本比对项，`EVENT_LABEL` 是筛选下拉的来源）。

## 5. 明确不做

- 不引入「门户地址列表」或多门户路由（单值即可覆盖单租户部署）；
- 不新增 `PORTAL_BASE_URL` 之类的派生变量（避免二义性与第二个失效点）；
- 不在运行时热更新该配置（重启生效，与既有配置策略一致）。
