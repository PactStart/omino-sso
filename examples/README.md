# 接入示例（Go / Java / Python）

三份**行为一致**的接入示例：授权码 + PKCE（S256），令牌只存在服务端，浏览器只拿会话 cookie。
对外形状的权威定义见 [`specs/008-integration-examples/contracts/example-app-api.md`](../specs/008-integration-examples/contracts/example-app-api.md)；
本文件只讲「怎么跑起来」与「三份实现差在哪」。

> 示例不是产品代码。生产必改的三处在文末「仅演示声明」。

---

## 1. 端口与回调

| 语言 | 目录 | 默认端口 | 注册的回调 |
|---|---|---|---|
| Go | [`go/`](./go) | `8000` | `http://localhost:8000/callback` |
| Java | [`java/`](./java) | `8001` | `http://localhost:8001/callback` |
| Python | [`python/`](./python) | `8002` | `http://localhost:8002/callback` |

`APP_PORT` 可覆盖端口，但**回调必须与注册值逐字符一致**：换端口就要同步改客户端注册的 `redirectUris`，
否则令牌交换会返回 `invalid_grant`。示例不会自动换端口（端口占用直接失败退出）。

## 2. 环境变量

| 变量 | 必填 | 说明 |
|---|---|---|
| `SSO_ISSUER` | 是 | IdP 地址，如 `http://localhost:8080`（不带尾斜杠） |
| `SSO_CLIENT_ID` | 是 | 管理端注册的客户端 id |
| `SSO_CLIENT_SECRET` | 是 | 客户端密钥（只经 HTTP Basic 发送，不进请求体） |
| `SSO_REDIRECT_URI` | 是 | 回调地址，必须与注册值逐字符一致 |
| `SESSION_SECRET` | 是 | 长度 ≥ 32 字符；三语言都要求存在（用途见 §6.2） |
| `APP_PORT` | 否 | 覆盖监听端口（默认 8000 / 8001 / 8002） |
| `SSO_USERINFO_URL_OVERRIDE` | 否 | **仅调试**：覆盖 `userinfo` 地址，用于验证降级。启用时启动日志打印 `[start] debug=userinfo_override url=...`，**生产禁用**；它只能改 userinfo 地址，不能关闭验签或令牌校验 |

缺必填项时进程以非零码退出并打印缺失项名称；`SSO_ISSUER` 的发现文档取不到时启动失败并提示「连不上 issuer」。

## 3. 运行

先准备一个客户端（管理端界面或 `POST /api/v1/admin/clients`），`scopes` 必须含 `openid profile email offline_access`
（缺 `offline_access` 就拿不到 refresh_token）。

```bash
export SSO_ISSUER=http://localhost:8080 \
       SSO_CLIENT_ID=<clientId> \
       SSO_CLIENT_SECRET=<clientSecret> \
       SSO_REDIRECT_URI=http://localhost:8000/callback \
       SESSION_SECRET=$(openssl rand -base64 32)
```

**Go**（Go **1.26+**：`examples/go/go.mod` 的 `go` 指令为 `1.26.0`，由精确锁定的 `golang.org/x/oauth2 v0.37.0`（要求 1.26.0）与 `go-oidc v3.21.0`（要求 1.25.0）决定，见 §6.3）

```bash
cd examples/go
go run .
```

**Java**（JDK 17+，本地用 23 验证）

```bash
cd examples/java
mvn -B -DskipTests package
java -jar target/omino-sso-example-java-0.0.1-SNAPSHOT.jar
```

**Python**（CPython 3.11+，实测 3.12；示例目录内已带 `venv/`，被 gitignore）

```bash
cd examples/python
python3.12 -m venv venv && ./venv/bin/python -m pip install -r requirements.txt   # 首次
./venv/bin/python app.py
```

启动成功会打印一行 `[start] listen=:8000 issuer=... redirect=...`（**不含密钥**）。

## 4. 验证

```bash
curl -i http://localhost:8000/api/me            # 未登录：401 {"error":"unauthenticated",...}
```

浏览器打开示例首页 → 点「登录」→ 到统一身份登录 → 回到示例看到用户卡片（`sub` / 登录名 / 角色 / `amr`）→
点「登出」后再看 `/api/me` 必须重新登录（401）。

三个示例同时在线时，在同一个浏览器里登录其中一个，另外两个点「登录」**不应再出现登录页**（跨示例 SSO）。
三个页面的 `sub` 相同即判定 SSO 生效。

### 4.1 一条命令做自动验证

```bash
bash scripts/e2e/examples.sh              # 跑 A0–A16 并打印逐项 PASS/FAIL
bash scripts/e2e/examples.sh --keep       # 结束后保留三个示例进程，便于接着手工点
bash scripts/e2e/examples.sh --self-test  # 只校验脚本自身（不连 IdP、不起进程）
```

断言清单与行为约束见
[`specs/008-integration-examples/contracts/e2e-examples-cli.md`](../specs/008-integration-examples/contracts/e2e-examples-cli.md)。

| 项 | 说明 |
|---|---|
| 前置 | IdP 在跑、管理员账号可用、端口空闲（默认 `8000/8001/8002`，可用 `E2E_*_PORT` 改）、`go`/`mvn`/`python3` 就绪；**Python 依赖由脚本自行准备**（缺 `examples/python/venv` 时自动 `python3 -m venv` + `pip install -r requirements.txt`，受 `E2E_NET_TIMEOUT` 约束）；不满足则 `exit 2` 且**不开始**断言 |
| 退出码 | `0` 全绿 / `1` 有断言失败（已打印 got/want 并附日志尾部）/ `2` 前置不满足 |
| 产物 | `.tmp/e2e-examples/`：`report.txt`（逐项结果）、`clients.env`（`0600`，可 `source` 后手工起示例）、`go.log`/`java.log`/`python.log` |
| A9 的判据 | 脚本先回读三个客户端原 `accessTokenTtl`，临时改成 60 秒并重新登录，等令牌过期后访问 `/api/me`，以日志 `[refresh] ok rotated=1` 判定轮换（令牌本身不出服务端，脚本无法直接比对 refresh_token），跑完按**原值**恢复 |
| 超时 | 每步 HTTP 用 `E2E_CURL_TIMEOUT`（默认 10s）；A0 的依赖准备/构建用 `E2E_NET_TIMEOUT`（默认 300s）。机器上没有 coreutils 的 `timeout` 时，脚本退到 `gtimeout`，再没有就用 bash 计时 + `kill` 兜底，不会静默裸跑 |
| A11 的判据 | 用同一 `sid` 直接查 `{API}/api/v1/me` 是否仍 200，不依赖门户 dev server 是否在跑；同时断言示例响应里没有 `Set-Cookie: sid=` |
| 幂等 | 连跑两次都全绿（客户端按 `clientName` 复用、密钥复用缓存，已存在即通过）；2026-10-24 与 2026-10-25 各实测连续两轮 17/17 PASS |
| 已知限制 | `refresh_token` 不出服务端，`E2E_STRICT_REFRESH=1` 的「重放旧令牌应得 invalid_grant」在示例侧无法执行，脚本会明确打印这条限制而不是假装通过 |

### 4.2 实测用时（2026-10-25，macOS x86_64）

| 项 | 实测 |
|---|---|
| 示例进程启动 → 首次 `/api/me` **200** | **Go 7s / Java 8s / Python 10s**（热缓存；`report.txt` 的 `A4-TIMING` 行） |
| 进程启动 → 首次 `/api/me` 401（未登录基线） | Go 1s / Java 5s / Python 1s（`A3-TIMING` 行） |
| 一条命令整体（连续第二次起） | **103s**，`PASS=17 FAIL=0` |

口径：上表两轮均为**热缓存**（`venv/` 已装、Maven 依赖已下载），只代表「后续执行」；冷缓存首次会在 A0 多出 `mvn` 依赖下载与 `pip install -r requirements.txt` 的耗时（受 `E2E_NET_TIMEOUT` 约束）。
数据来源固定为 `.tmp/e2e-examples/report.txt` 的 `A3-TIMING` / `A4-TIMING` 两行，重跑即刷新。

## 5. 日志

固定三类行，便于定位；**都不含令牌**：

```text
[start]   listen=:8000 issuer=http://localhost:8080 redirect=http://localhost:8000/callback
[login]   sub=01J... amr=[pwd] elapsed=118ms
[refresh] ok rotated=1 elapsed=42ms          # 或 [refresh] fail reason=invalid_grant → reauth
```

Java 的日志带 Spring Boot 的前缀（时间戳 / 进程号 / 线程），关键字与其余两语言一致。

## 6. 三语言对照

### 6.1 一致的部分

| 项 | 三份实现 |
|---|---|
| 端点 | `GET /`、`GET /login`、`GET /callback`、`GET /api/me`、`GET /logout`（不额外暴露业务路由） |
| 会话 cookie | `sid_go` / `sid_java` / `sid_py`，`HttpOnly`、`SameSite=Lax`、`Path=/`、host-only；**禁止**叫 `sid` |
| `/api/me` 字段 | `sub` `username` `name` `email` `tenant` `deptId` `roles` `amr` `appRoles` `permissions` `degraded` `accessExpiresIn` `lastRefreshedAt`（空值为 `""` / `[]` / `null`，不省键） |
| 令牌位置 | 只在服务端（进程内 map 或 HttpSession）；页面、URL、浏览器存储都没有令牌 |
| 刷新 | 进程内互斥锁单飞，锁内复查「是否已被并发请求换过」；轮换后的新 refresh_token 覆盖旧值 |
| 降级 | `userinfo` 不可达时 `/api/me` 仍 `200`，`degraded=["userinfo"]`、`appRoles`/`permissions` 为 `[]` |
| 登出 | 清本地会话 + 清 `sid_<lang>`，302 到 `{issuer}/logout?next=/logged-out`（不用 `end_session`，它不清 `sid`） |

### 6.2 实现内部差异（对外形状相同）

| 项 | Go | Java | Python |
|---|---|---|---|
| 框架 | `net/http` + `go-oidc` + `x/oauth2` | Spring Boot + `spring-security-oauth2-client` | FastAPI + Authlib |
| 模板 | `html/template` | Thymeleaf | Jinja2 |
| 令牌存储 | 进程内 map | 服务端 `HttpSession` | 进程内 map |
| 刷新锁 | `sync.Mutex` | `synchronized` | `asyncio.Lock` |
| `pending` 状态 | 服务端 map | `HttpSession`（Spring 的授权请求仓库） | 服务端 map |
| `SESSION_SECRET` 用途 | 必填校验（会话是服务端 map，cookie 只存随机 id） | 必填校验（`HttpSession` 的 id 本身即服务端随机值） | 用它给 `sid_py` 签名（`itsdangerous`），防篡改 |

两个**已知且刻意**的行为差异：

1. **Java 的 `/login` 是两跳**：`302 → /oauth2/authorization/omino → 302 → IdP`。Spring 要求授权请求由框架发起（它负责 state/nonce/PKCE），所以示例不能自己拼 authorize URL。浏览器最终落点与其余两语言一致。
2. **Java 的回调错误码粒度更粗**：Spring 在回调时先消费本地 `pending`，因此「state 不匹配」与「没有进行中的授权请求」都表现为 `400 {"error":"no_pending_authorization","stage":"callback"}`；Go/Python 能区分出 `state_mismatch`。状态码与 `stage` 仍符合契约。

另：Python 没有用 Starlette 的 `SessionMiddleware(session_cookie="sid_py")`——它把会话数据放在**浏览器 cookie** 里，令牌会跟着出服务端。改为服务端会话 + 自签名的 `sid_py`，cookie 名与属性仍符合契约。

### 6.3 已验证版本矩阵（2026-10-24，macOS x86_64）

| 语言 | 运行时 | 依赖（精确版本） |
|---|---|---|
| Go | go 1.26.0 | `github.com/coreos/go-oidc/v3 v3.21.0`、`golang.org/x/oauth2 v0.37.0` |
| Java | OpenJDK 23.0.2（编译目标 21）、Maven 3.9.9 | `spring-boot-starter-parent 3.5.3`（web / oauth2-client / thymeleaf 随 parent） |
| Python | CPython 3.12.14 | 见 [`python/requirements.txt`](./python/requirements.txt)（22 项全部 `==`，关键项 `fastapi==0.115.6`、`authlib==1.4.1`、`cryptography==44.0.0`） |

验证方式：`go build`、`mvn -B -DskipTests package`、`python -c "import app"` 均通过；三者各完成一次真实登录、
一次跨示例 SSO（两个 `sub` 一致）、一次 `userinfo` 降级（`SSO_USERINFO_URL_OVERRIDE` 指向 `http://127.0.0.1:1/...`）。

> Python 侧刻意把 `cryptography` 钉在 `44.0.0`：更新版本在 macOS x86_64 无 wheel，会退化成本地 Rust 编译（示例的前置条件里没有 `cargo`）。
> 同理 Python 示例当前在 **3.12** 上验证（`venv/` 即 3.12.14）；`fastapi 0.115.6` 早于 3.14，硬钉会导致 3.14 装不上。

## 7. 排障

| 现象 | 症结 | 处理 |
|---|---|---|
| 启动即报「连不上 issuer」 | IdP 没跑或 `SSO_ISSUER` 端口写错 | `curl $SSO_ISSUER/.well-known/openid-configuration` |
| 回调 `invalid_grant` | `SSO_REDIRECT_URI` 与注册值不一致（多/少斜杠、端口不同） | 改环境变量或 PATCH `redirectUris`，二者逐字符对齐 |
| 换令牌 `invalid_client` | 用了轮换前的旧密钥，或把密钥塞进请求体 | 重新取密钥；示例只允许 HTTP Basic |
| 登录示例后门户掉线 | 示例 cookie 名与 IdP 的 `sid` 冲突 | 检查是否写成了 `sid`；必须是 `sid_<lang>` |
| 页面显示「应用内角色暂不可用」 | `userinfo` 不可达，示例已降级（预期行为） | 确认 IdP；恢复后刷新即恢复，无需重登 |
| 第二个示例仍要求登录 | 换了浏览器隐私窗口，或回调未注册 | 用同一浏览器窗口，三示例各自回调都要注册 |
| Java `Port 8001 was already in use` | 上一次的示例进程没退 | `lsof -i :8001` 清掉 |
| Python 装依赖卡住 | uv/pip 在等锁或解析回溯 | 用 `python3.12 -m venv` + `-r requirements.txt`；必要时设 `PIP_INDEX_URL` 走镜像 |

## 8. 清理（可选）

跑完不想留残留时（**不清理也不影响下次运行**）：

| 残留 | 位置 | 清理方式 |
|---|---|---|
| 示例客户端 ×3 | IdP 的客户端表 | 管理 API 没有删除接口，按 `client_name` 为 `示例-%` 手工执行 SQL（连同其密钥记录） |
| 专用账号 `examples` | IdP 的用户表 | 管理端置为**停用**即可（保留审计链），不要物理删除 |
| 本地凭据缓存 | `.tmp/e2e-examples/clients.env` | `rm -rf .tmp/e2e-examples` |

建议用**专用账号**（如 `examples` / `Examples@12345`）做验证而不是演示账号：IdP 有失败计数与锁定，
连续输错会把演示账号锁 15 分钟，反过来影响本地开发。

## 9. 仅演示声明

三份示例都**只能用于本地演示**，上生产前必须改三处：

1. **登出改为 `POST` + CSRF 防护**（现在是 `GET /logout`，便于地址栏验证，可被跨站触发）；
2. **启用 HTTPS 并给会话 cookie 加 `Secure`**（现在跑在 http 上，`Secure=false`）；
3. **会话移出进程内存**（现在重启即登出；生产要用外部会话存储，多实例才能共享登录态）。
