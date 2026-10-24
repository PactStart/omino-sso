# Contract: `scripts/e2e/examples.sh`（三语言示例端到端）

**用途**：一条命令验证「三个示例都能按接入文档跑通」，并证明跨示例单点登录、令牌轮换刷新、登出失效都成立。

---

## 调用方式

```bash
bash scripts/e2e/examples.sh              # 默认参数
bash scripts/e2e/examples.sh --keep       # 结束后不关闭示例进程（便于手工接着点）
bash scripts/e2e/examples.sh --self-test  # 只校验脚本自身（参数解析 / 断言框架 / 前置检查），不连 IdP
```

**前置条件**（不满足则 `exit 2` 并说明缺哪项）：
1. IdP 本地开发实例已在运行：`{E2E_ISSUER}/.well-known/openid-configuration` 可 200；
2. 管理员账号可用；
3. 三个示例端口（8000/8001/8002）未被占用；
4. 三语言工具链就绪：`go`、`mvn`、`python3`。

## 可覆盖变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `E2E_ISSUER` | `http://localhost:8080` | IdP 基地址（示例的 `SSO_ISSUER`） |
| `E2E_ADMIN_USER` / `E2E_ADMIN_PASS` | `admin` / `Admin@12345` | 注册客户端、创建专用账号 |
| `E2E_EXAMPLE_USER` / `E2E_EXAMPLE_PASS` | `examples` / `Examples@12345` | **专用示例账号**：不存在则由脚本创建、存在则复用。**不使用**演示账号 `zhangsan`（避免失败计数把演示账号锁死，FR-012） |
| `E2E_GO_PORT` / `E2E_JAVA_PORT` / `E2E_PY_PORT` | `8000` / `8001` / `8002` | 端口变化后会同步重新注册回调 |
| `E2E_WORK_DIR` | `.tmp/e2e-examples` | 产物目录（gitignored） |
| `E2E_STRICT_REFRESH` | `0` | 置 `1` 时额外用旧 refresh_token 重放并断言 `invalid_grant`（会吊销整族，故只在最后一次执行） |
| `E2E_NET_TIMEOUT` | `300` | A0 依赖下载（`mvn`/`pip`/`go mod`）的单步超时秒数；国内网络可调大，或用镜像源加速（见 README） |
| `E2E_CURL_TIMEOUT` | `10` | 单个 HTTP 断言调用的 `--max-time` 秒数 |

## 退出码

| 码 | 含义 |
|---|---|
| `0` | 全部断言通过 |
| `1` | 至少一项断言失败（已打印 `[FAIL] 名称（got=… want=…）`） |
| `2` | 前置条件不满足或被拒绝执行（IdP 不通、端口占用、工具链缺失、账号异常），此时**未开始断言** |

## 断言清单（必须逐项打印 PASS/FAIL）

| ID | 断言 | 期望 |
|---|---|---|
| A0 | **构建冒烟** | `go build ./...`、`mvn -q -DskipTests package`、`pip install -r requirements.txt` + `python -c "import app"` 全部成功；失败即 `exit 2` 并打印具体命令与最后一屏错误 |
| A1 | 三示例客户端注册可复用/可创建 | 三个 `clientId` 非空，回调与端口一一对应，`clientName` **逐字**匹配 |
| A2 | 专用账号可用 | 直接 `POST /api/v1/admin/users`（`username`/`displayName`/`password` 必填）创建，返回「已存在」视为通过（无竞态）；随后登录一次即成功。失败立即 `exit 2`，不重试 |
| A3 | 三个示例进程都起来 | 三个端口的 `/api/me` 均 `401`（未登录基线） |
| A4 | Go 示例登录闭环 | 驱动 `/login` → IdP 登录 → `/callback` 后 `/api/me` = `200` |
| A5 | Java 示例登录闭环 | 同上 |
| A6 | Python 示例登录闭环 | 同上 |
| A7 | 三示例拿到同一用户 | 三次 `/api/me` 的 `sub` 完全一致 |
| A8 | 跨示例单点登录 | 已有 IdP 会话时，另一示例的 `/login` 不再出现登录页（`Location` 不含 `/login?id=`） |
| A9 | 刷新轮换（每示例各一次） | 返回新 `access_token`，`refresh_token` 与旧值不同 |
| A10 | 刷新后身份不变 | 刷新后 `/api/me` 的 `sub` 与 A7 一致 |
| A11 | **示例未破坏统一身份会话** | 两个可观测事实：① 示例登录/登出前后，门户（`localhost:5173`）的 `/api/v1/me` 均 `200`；② 期间示例响应里没有任何 `Set-Cookie: sid=…`。（不用 sid 字节比对：IdP 可能正常刷新会话，字节比对会误报） |
| A12 | 登出清会话 | 示例 `/logout` 的 302 指向 `{issuer}/logout?next=/logged-out`，且 `sid_<lang>` 被清除 |
| A13 | 登出后全失效 | 三示例 `/api/me` 全部 `401` |
| A14 | 无密钥外泄 | 三示例的 `/` 与 `/api/me` 响应体中不含 `clientSecret`、`refresh_token`、`id_token` 明文 |
| A15 | 降级不放大故障 | 以 `SSO_USERINFO_URL_OVERRIDE=http://127.0.0.1:1/userinfo` 重启一个示例，`/api/me` 仍 `200` 且 `degraded=["userinfo"]`，`appRoles`/`permissions` 为 `[]` |
| A16 | **cookie 前缀正确** | 三个示例登录时抓 `Set-Cookie`，名字恰为 `sid_go`/`sid_java`/`sid_py`，三者互不相同且都不是 `sid` |

> A9 默认只断言「新 refresh_token ≠ 旧值」；`E2E_STRICT_REFRESH=1` 时才做重放断言（会吊销整族，必须放在最后）。
> A15 用示例自带的调试覆盖入口（FR-025）实现，**必须真实执行**：降级路径不允许以 `[SKIP]` 代替通过（SC-009）。除 A15 外，任何断言都不得跳过。

## 产物

```text
.tmp/e2e-examples/
├── clients.env      # 0600；三个示例的 clientId/secret/redirect（可 source 后手工起示例）
├── report.txt       # A0–A16 的 PASS/FAIL/实测值 + 各示例「启动→首次 200」耗时
├── go.log / java.log / python.log
└── jar-*.txt / token-*.json   # 中间产物，每次运行前清空
```

## 行为约束

- **幂等**：连跑两次都必须全绿；允许库里留下客户端注册与专用账号（用户已明确 DB 改动无需还原），但不得因残留数据失败（FR-012）；
- **不碰既有演示数据**：不修改内置 `portal` 客户端回调、不修改演示账号密码与状态；
- **登录失败即止**：对同一账号的登录失败禁止自动重试（失败计数会累加，重试可能锁死账号）；仅网络类瞬时错误允许一次幂等重试；
- **超时兜底**：每个网络调用带 `--max-time`（默认 10s），整体有 `timeout` 上限，避免 `curl` 挂住导致进程残留；
- **进程管理**：脚本负责拉起三个示例，并在退出（正常/失败/`INT`/`TERM`）时关闭；`--keep` 才保留；
- **端口占用**：报错附「如何清理」一行（`lsof -i :8000,8001,8002`）；
- **失败可定位**：失败时打印对应示例日志中 `[start]/[login]/[refresh]` 三类行的最后 20 行（FR-013）；
- **不静默跳过**：任一断言无法执行即记为 FAIL，不得用 `[SKIP]` 充数（A15 也必须真实执行，见上文）；
- **残留是有意的**：客户端注册、专用账号、`clients.env` 会留在本地（DB 改动无需还原），清理属可选手工动作，见 `examples/README.md`「清理（可选）」。
