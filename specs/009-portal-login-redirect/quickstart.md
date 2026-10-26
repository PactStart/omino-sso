# Quickstart: 未登录授权跳门户的验证指南

**Feature**: `009-portal-login-redirect` | **Date**: 2026-10-25

本文件只列**可复现的验证动作与预期结果**，用于人工与 CI 复核；实现细节见 `tasks.md` 与代码。契约口径见 [contracts/](./contracts/)。

## 0. 前置

- 后端：`make deps && make keys && make build`；`.env` 里配好 MySQL / Redis / `OIDC_ISSUER=http://localhost:8080`。
- 前端：`cd web/portal && npm ci`；`make build-web` 供 e2e 用。
- 一个可用的接入方：用仓库自带 `examples/go`（三条命令即可跑起来，见 `examples/README.md`）或任意已注册 client。

## 1. 主链路：未登录 → 门户登录页 → 自动回到应用

```bash
# .env 追加（dev 允许 http）
PORTAL_LOGIN_URL=http://localhost:5173/login

make dev                       # 起 :8080 / :8081
cd web/portal && npm run dev   # 起 :5173
```

操作：清空浏览器 cookie → 访问 `http://localhost:8000/`（go 示例）→ 点「登录」。

**预期**：

1. 浏览器地址栏落在 `http://localhost:5173/login?next=http%3A%2F%2Flocalhost%3A8080%2Flogin%3Fid%3D…%26portal%3D1&app=<应用名>`；
2. 门户副标题显示「登录后返回 <应用名>」，**不出现** `/login` 之类路径；
3. 输入账号密码（含 MFA 账号则再输动态码）→ 页面自动跳回 `localhost:8080/login?id=…&portal=1`，随即 302 到示例，示例首页显示已登录用户；
4. 全过程只输入一次凭据，浏览器跳转次数 ≤ 2（门户 → IdP → SP）。

## 2. 反证：未配置时回落内置登录页

```bash
# .env 中留空
PORTAL_LOGIN_URL=
```

重启后端，重复场景 1。

**预期**：浏览器直接落在 `http://localhost:8080/login?id=…` 的内置 HTML 表单页，登录后正常回到示例（与改造前一致）。

## 3. loop guard：回跳后仍无会话

```bash
# 取一个真实待认证请求 id（从场景 1 的地址栏复制 id），并确保浏览器没有任何 sid
curl -si "http://localhost:8080/login?id=<REQ_ID>&portal=1" | head -20
```

**预期**：

- `HTTP/1.1 200`，**不是** 302；
- 响应体是内置表单（含 `<form`）且带降级说明（含「未能在跳转中延续」）；
- 后端日志出现一条 `Warn`：`portal roundtrip without session`（含 client_id / request_id，不含任何凭据）；
- 在同一 id 上重复请求仍为 200（不会来回弹跳）。

再验证「降级页仍能登录」：用该 id + 正确账号密码 `POST /login` → `302` → 跟随到示例回调 → 示例显示已登录。

## 4. 审计：SSO 快捷完成可追溯

```bash
# 场景 1 完成一次后
mysql -h127.0.0.1 -P3307 -uroot -pomino_dev_pwd omino_sso \
  -e "SELECT event_type,result,username,client_id,detail,created_at
      FROM audit_events WHERE event_type='sso_login_success' ORDER BY created_at DESC LIMIT 5\G"
```

**预期**：每完成一次授权恰好新增 1 条；`detail` 形如 `{"amr":["pwd"],"entry":"portal"}`；同一时间窗内的表单登录仍对应 `login_success`，两者不互相顶替。

管理后台：登录 admin → 审计页 → 事件类型下拉出现「SSO 快捷登录」→ 选中后可筛出该批事件。

## 5. 配置守护（启动失败反证）

各场景都应**非零退出**且日志点名变量：

```bash
COOKIE_SAME_SITE=Strict make dev            # → COOKIE_SAME_SITE=Strict is not allowed…
PORTAL_LOGIN_URL=portal.example.com/login make dev   # → must be an absolute URL…
APP_ENV=prod PORTAL_LOGIN_URL= make dev     # → PORTAL_LOGIN_URL is required in prod
APP_ENV=test PORTAL_LOGIN_URL=http://localhost:5173/login make dev  # → must use https in test
```

正例：`APP_ENV=test PORTAL_LOGIN_URL=https://portal.example-test.com/login` 应正常启动；`COOKIE_SAME_SITE=lax` 应正常启动。

**2026-10-26 实测（T034，逐条真实进程启动，`bin/sso` + 独立端口）**

| 场景 | 结果 |
|---|---|
| `COOKIE_SAME_SITE=Strict` | exit=1，`COOKIE_SAME_SITE=Strict is not allowed: cross-site redirects would drop the session (use Lax)` |
| `PORTAL_LOGIN_URL=portal.example.com/login` | exit=1，`PORTAL_LOGIN_URL must be an absolute URL like https://portal.example.com/login` |
| `APP_ENV=prod PORTAL_LOGIN_URL=` | exit=1，`PORTAL_LOGIN_URL is required in prod` |
| `APP_ENV=test PORTAL_LOGIN_URL=http://…` | exit=1，`PORTAL_LOGIN_URL must use https in test` |
| `PORTAL_LOGIN_URL=https://portal.example.com/login#frag` | exit=1，同上「absolute URL」分支（fragment 不被接受） |
| 正例 3 条（dev 空地址 / test https / `COOKIE_SAME_SITE=lax`） | 均 `healthz=200` 正常启动 |
| **SC-007 回退**：dev 清空地址后未登录授权 | `/oauth2/authorize` → `302 /login?id=…`（**不跳门户**）→ 该页直接渲染内置 `<form>`，可完成登录 |

> 反例的证据形态固定为：**非零退出 + 日志点名变量**；`docs/deployment.md` §11 的排障表按同一口径引用这些文案。

## 6. 自动门禁

```bash
make lint && make build && make test                # 单测（含新增的纯函数 / 配置 / cookie 用例）
make check                                          # 注册表四向一致（含新事件与新变量）
make build-web                                      # 两端前端类型检查 + 构建
cd web/portal && npm run test                        # redirect / Login 用例
```

**发布前人工门禁：OpenID 一致性套件（AGENTS §8.5）**

```bash
# 前提：被测 IdP 以「清空 PORTAL_LOGIN_URL」的形态启动（内置登录页供套件自动完成登录）
docker run --rm -p 8080:8080 \
  -e CONFORMANCE_SERVER=http://host.docker.internal:8080 \
  openidentity/conformance-suite
# 然后在 UI 新建 basic OP + config OP 计划，全部用例通过才可发布
```

> **2026-10-26 状态（T032）：未执行。** 该镜本次尝试无法获取——`docker pull openidentity/conformance-suite`
> 返回 `pull access denied / repository does not exist`（`:latest`、`:release-v5.0.0`、`:v4.1.5` 均如此），
> `registry.gitlab.com/openid/conformance-suite:latest` 亦无 `latest` 标签。因此本特性（改变了未登录
> `authorize` 第一跳的落点）**尚未取得一致性套件证据**，发布前必须在能访问套件分发源的机器上补做，并把
> 命令、profile、用例通过数、执行日期补写在此处。
> **口径**：必须用「清空 `PORTAL_LOGIN_URL`」的形态跑，因为套件的自动化浏览器期望授权端点直接给出登录表单；
> **禁止**为了套件给代码加按环境/开关分流的逻辑（FR-002：行为不得按环境名开关）。

## 7. 四域端到端（需 docker）

```bash
make e2e    # scripts/e2e/run.sh：四域 HTTPS
```

**预期**：既有断言全绿 + 新增两条——① `/oauth2/authorize` 302 到 `https://portal.lvh.me:<port>/login`（`next` 指回 sso 域登录入口）；② 带 `portal=1` 且无会话时展示内置页而非再次跳门户。

## 8. 接入示例端到端

```bash
# 被测 IdP 已按上述配置启动（PORTAL_LOGIN_URL=http://localhost:5173/login）
bash scripts/e2e/examples.sh            # A0–A19
bash scripts/e2e/examples.sh --self-test  # 仅校验脚本自身
```

**预期**：A0–A17 全绿（A4–A6 走 SSO 快捷完成，`LOGIN_SAW_IDP_FORM=0`）；A18/A19 全绿；把 `E2E_PORTAL_LOGIN_URL` 改成别的地址再跑 → 退出码 `2` 且提示为「前置不满足」。

**2026-10-26 实测（T038，SC-006 的 120% 口径）**

| 版本 | 断言数 | 单次执行耗时（热缓存，同一台机器） |
|---|---|---|
| 改造前 `43ac6cf`（008 完成态，独立 worktree） | 18 | **156s** |
| 当前 HEAD（009 + Phase 8 断言） | 20 | **176s**（= 112.8%，≤120% ✓） |

测量条件：`go build`/`mvn package`/python venv 均已就绪（双方各先跑一次预热），被测 IdP 为 `APP_ENV=dev` +
`PORTAL_LOGIN_URL=http://localhost:5173/login`，示例端口 8000–8002，未并行跑其他 e2e。HEAD 侧额外的
~20s 中包含 A18/A19 与 T035 的「审计 10 连击」（10 次额外授权），即 009 本身引入的增量更小。

## 9. 首次上线顺序与回滚

**首次上线必须按序**（反序会造成"登录成功却进不去任何系统"）：

1. 先发 **portal** 产物（已注入 `VITE_SSO_BASE_URL`，白名单含 IdP 域）；
2. 在 `deploy/env/<env>.env` 填入 `PORTAL_LOGIN_URL`（prod 缺失会让新副本 CrashLoop）；
3. 再滚动**后端**镜像。

验证顺序生效：发布后按场景 1 走一遍完整链路；若第 3 步之前就打开后端跳转，用户会停在门户首页（对应 `contracts/portal-login-redirect.md` §6 的禁止组合）。

**回滚**：

- dev/test：清空 `PORTAL_LOGIN_URL` → 重启 → 场景 2 成立即视为回滚成功。
- prod：该配置必填，回滚以"回到上一版本"执行，且**保留**该变量（清空会导致启动失败）。

## 10. 上线后观测（SC-009）

三条信号都可从既有数据源得出，无需新增埋点或接口：

```sql
-- ① 主路径占比（按日）：应为绝大多数
SELECT DATE(FROM_UNIXTIME(created_at/1000)) d, event_type, COUNT(*)
FROM audit_events
WHERE event_type IN ('sso_login_success','login_success')
GROUP BY d, event_type ORDER BY d DESC;

-- ③ 高频被送门户的账号（异常的直观信号）
SELECT username, COUNT(*) c FROM audit_events
WHERE event_type='login_success' AND created_at > UNIX_TIMESTAMP(DATE_SUB(NOW(), INTERVAL 1 DAY))*1000
GROUP BY username HAVING c >= 3 ORDER BY c DESC;
```

- ② `/login` 的"请求已失效"占比（目标 < 1%）：从协议层日志/网关 access log 统计 `/login` 响应分布；超过阈值优先检查 `PORTAL_LOGIN_URL` 与请求有效期。

判读：① 偏低 → 门户地址未生效或被降级；② 偏高 → 有效期太短或配置错配；③ 异常升高 → 标记丢失/回跳失败。触发其中任一项异常时，按场景 5 逐项核对配置，必要时回滚（§9）。

**2026-10-26 实测（T036，本地 `omino_sso` 库）**

- ① 可直接算出：`2026-10-25` 为 `login_success=136` / `sso_login_success=129`（约 1:1——一次门户往返 = 1 条门户密码登录 + 1 条凭会话完成授权，符合预期）；
- ③ 可直接算出（近 24h，`HAVING c>=3` 命中 `examples=127 / zhangsan=34 / admin=29`，均为脚本与联调账号，阈值与账号白名单的选择属运营口径）；
- ② **不落库**：`/login` 的“请求已失效”响应只体现在网关/协议层访问日志，库内无法统计。取数方式：对 nginx access log 按 `$request ~ "GET /login"` 聚合并统计渲染过期页的那部分（该页为 200 + 固定文案，可用响应体特征或给该分支加独立 access log 标记）。因此 ② 的可用性取决于部署是否保留 sso 域访问日志，**建议在 nginx 侧确认 `/login` 未被纳入采样丢弃**。

