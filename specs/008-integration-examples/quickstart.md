# Quickstart: 验证三语言接入示例

**目的**：用最短路径证明本特性可用。协议细节看 [`contracts/example-app-api.md`](./contracts/example-app-api.md)，脚本行为看 [`contracts/e2e-examples-cli.md`](./contracts/e2e-examples-cli.md)，此处不重复。

> **首次运行会慢**：Java 需要下载 Maven 依赖、Python 需要装 wheel，冷缓存下首次到全绿可能接近 10 分钟（SC-002 的首次口径）；第二次起应在 3 分钟内。

---

## 0. 前置（一次性）

1. 本地 IdP 按仓库 README「本地开发」跑起来：`make deps` → 准备 `.env` → `make dev`（`:8080` 协议层、`:8081` 管理 API）。
2. 确认元数据可访问：`curl -s http://localhost:8080/.well-known/openid-configuration | head -c 200`。
3. 确认演示环境用 `localhost`（本特性刻意不引入 `*.lvh.me` 子域，cookie 冲突靠命名前缀解决，见契约）。

## 1. 场景 A：手工跑通单个示例（以 Go 为例）

**目的**：验证「按说明填 5 个环境变量就能登录」这一最低门槛（SC-001）。

```bash
# 1) 用管理员会话注册一个客户端（回调端口必须与示例一致）
curl -s -c /tmp/admin.cookie -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@12345"}' \
  http://localhost:8081/api/v1/auth/login >/dev/null

curl -s -b /tmp/admin.cookie -H 'Content-Type: application/json' \
  -d '{"clientName":"示例-Go","redirectUris":["http://localhost:8000/callback"],
       "grantTypes":["authorization_code","refresh_token"],
       "scopes":["openid","profile","email","offline_access"],"isPublic":false}' \
  http://localhost:8081/api/v1/admin/clients
# 记下响应里的 clientId 与 clientSecret（secret 只出现这一次）

# 2) 启动示例
cd examples/go
export SSO_ISSUER=http://localhost:8080 \
       SSO_CLIENT_ID=<clientId> SSO_CLIENT_SECRET=<clientSecret> \
       SSO_REDIRECT_URI=http://localhost:8000/callback \
       SESSION_SECRET=$(openssl rand -base64 32)
go run .
```

**期望**：
- 控制台打印 `[start]` 一行：监听地址、`SSO_ISSUER`、回调地址，**不打印密钥**；
- 浏览器打开 `http://localhost:8000/` → 点「登录」→ 跳 `http://localhost:8080/login?id=...` → 登录成功回到示例 → 页面显示用户卡片（`sub`、登录名、角色、`amr`）；
- 直接访问 `http://localhost:8000/api/me` 未登录时应为 `401`；
- 点「登出」后再打开受保护页面，必须重新登录；
- 页脚可见「仅本地演示」与生产必改三处的提示；
- 登录后页面有一行**会话状态**（访问令牌剩余秒数 / 上次刷新时间），登出后随会话消失——这是人工确认「刷新确实发生过」的依据。

Java / Python 同一剧本，差别仅端口与启动命令（`8001` / `8002`；见 `examples/README.md`）。

> 手工跑完想省事：先执行一次场景 C，`.tmp/e2e-examples/clients.env` 里已有三套凭据，`source` 后直接起示例即可（该文件含密钥，权限 0600，勿外传）。

## 2. 场景 B：三示例同时在线并验证跨示例免登录（SC-003）

1. 三个示例都按场景 A 注册（回调分别为 `:8000`、`:8001`、`:8002` 的 `/callback`）并启动；
2. 浏览器完成其中一个示例的登录；
3. 打开第二个示例首页点「登录」。

**期望**：第二个示例**不再出现登录页**，直接回到自己的页面且 `/api/me` 的 `sub` 与第一个完全相同；第三个同理。三个页面上的 `sub` 一致即判定 SSO 生效（3/3 组合）。

**同时要确认**：登录示例时，同一浏览器里门户（`localhost:5173`）的登录态**不受影响**——这是会话 cookie 前缀（`sid_go`/`sid_java`/`sid_py`）生效的反证。

## 3. 场景 C：一条命令跑完自动验证（SC-002 / SC-007）

```bash
bash scripts/e2e/examples.sh
```

**期望**：
- 先打印 `A0` 构建冒烟，再逐项打印 `A1`–`A16` 的 `PASS`（清单见 [contracts/e2e-examples-cli.md](./contracts/e2e-examples-cli.md)）；其中 **A15 会把 `userinfo` 指到不可达地址以验证降级，必须真实执行、不得 `[SKIP]`**；**A16 断言三个 `Set-Cookie` 名恰为 `sid_go`/`sid_java`/`sid_py`**；
- `report.txt` 里记录每个示例「启动 → 首次 200」的耗时（SC-001 的量化依据）；
- 最后一行给出汇总与退出码 `0`；
- **紧接着再执行一次仍然全绿**（幂等，SC-007）；
- 产物在 `.tmp/e2e-examples/`：`clients.env` 可直接 `source` 用于手工起示例。

**失败时**：退出码非 `0`，输出会指出「哪个示例 / 哪项断言 / 实际值」，并附该示例 `[start]/[login]/[refresh]` 日志行尾部。常见原因见下表。

## 4. 排障对照

| 现象 | 症结 | 处理 |
|---|---|---|
| 示例启动即报「连不上 issuer」 | IdP 没跑，或 `SSO_ISSUER` 端口写错 | 先 `curl` 元数据端点 |
| 授权后回调报 `invalid_grant` | `SSO_REDIRECT_URI` 与注册值不一致（多/少斜杠、端口不同） | 用 PATCH 改 `redirectUris` 或改环境变量，二者逐字符对齐 |
| 换令牌 `401 invalid_client` | 用的是旧的轮换前密钥，或把 secret 放进了请求体 | 重新取密钥；示例只允许 HTTP Basic |
| **登录示例后门户掉线 / A11 失败** | 示例会话 cookie 名与 IdP 的 `sid` 冲突（cookie 不按端口隔离） | 检查 cookie 名是否为 `sid_<lang>`；这是契约硬约束 |
| 登录后 `/api/me` 返回 `401` | 会话没建立（`SESSION_SECRET` 变化会导致重启后旧会话失效） | 重新点「登录」 |
| 页面显示「应用内角色暂不可用」 | `userinfo` 不可达，示例已降级（这是预期行为，`degraded=["userinfo"]`） | 确认 IdP/网络；修好后刷新即恢复，无需重登 |
| 第二个示例仍要求登录 | 端口/回调不同导致是不同客户端；或浏览器换了隐私窗口 | 用同一浏览器窗口、确认三示例各自回调已注册 |
| `A9` 刷新断言失败 | 客户端未申请 `offline_access`，或重复使用旧 refresh_token | 注册时带上 `offline_access`；让示例走单飞刷新 |
| 脚本 `exit 2` 且提示端口占用 | 上一次的示例进程没退（或手工起过） | `lsof -i :8000,8001,8002` 清掉后重跑 |
| 脚本 `exit 2` 且提示账号登录失败 | 专用示例账号密码不符或被绑了 MFA（脚本不重试，避免锁号） | 校正 `E2E_EXAMPLE_USER`/`E2E_EXAMPLE_PASS`，或在管理端重置该账号密码 / 解绑 MFA |
| `A0` 卡在依赖下载（Maven/PyPI 慢） | 外网源不可达或很慢 | 配置镜像：Python 用 `PIP_INDEX_URL`，Maven 用 `~/.m2/settings.xml` 的 mirror；再调大 `E2E_NET_TIMEOUT`（默认 300s） |
| `A15` 失败：明明 IdP 正常却出现降级 | 上一次调试留下的 `SSO_USERINFO_URL_OVERRIDE` 忘了清 | 该变量只应存在于 A15 的临时环境；检查 shell 里是否还 export 着 |

## 5. 清理（可选）

跑完不想留残留时（**不清理也不影响下次运行**）：

| 残留 | 位置 | 清理方式 |
|---|---|---|
| 示例客户端 ×3 | IdP 库的客户端表 | 管理 API 没有删除接口，按 `client_name` 为 `示例-%` 找到后手工执行 SQL（连同其密钥记录） |
| 专用账号 `examples` | IdP 库的用户表 | 管理端把状态置为**停用**即可（保留审计链），不要物理删除 |
| 本地凭据缓存 | `.tmp/e2e-examples/clients.env` | `rm -rf .tmp/e2e-examples` |

> 刻意不做「自动清理」：管理 API 没有删除接口，且这些残留已被用户明确允许（DB 改动无需还原）；自动删反而会误伤手工正在用的实例。

## 6. 完成判据

- 场景 A 三语言各走一遍 → 看到自己的用户信息，且门户登录态未被影响；
- 场景 B 三个 `sub` 一致（3/3）；
- 场景 C 连续两次 `exit 0`，`A0`–`A16` 全 `PASS`（无 `[SKIP]`）；
- `examples/README.md` 的「已验证版本矩阵」与实际依赖清单一致；
- 全程未出现令牌或密钥出现在浏览器可见处（页面、URL、浏览器存储）。
