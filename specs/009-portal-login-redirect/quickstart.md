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

## 6. 自动门禁

```bash
make lint && make build && make test                # 单测（含新增的纯函数 / 配置 / cookie 用例）
make check                                          # 注册表四向一致（含新事件与新变量）
make build-web                                      # 两端前端类型检查 + 构建
cd web/portal && npm run test                        # redirect / Login 用例
```

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
