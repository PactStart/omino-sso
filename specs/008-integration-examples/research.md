# Phase 0 Research: 三语言接入示例

**Feature**: 008-integration-examples | **Date**: 2026-10-24

本文件只记录**需要决策的未知项**，不复述 `docs/integration-guide.md` 已有的协议说明（该文档是接入事实的唯一来源）。

---

## D1. `examples/go` 用独立 Go module

- **Decision**：`examples/go/go.mod`（`module github.com/omino/omino-sso/examples/go`，`go 1.23`），与根模块并存但不被其覆盖；依赖**锁精确版本**，由实现首步 `go get <module>@<version>` 解析后回写，不留浮动。
- **Rationale**：根 `go.mod` 受 §2.1 版本锁定约束。`go-oidc`、`x/oauth2` 若进根模块，会让 `go mod tidy` 重排依赖并可能被动升级已锁定版本（§2.0 明令禁止）。嵌套 module 天然被 `go test ./...`、`golangci-lint run ./...` 排除，既有门禁零改动。`tools/go.mod` 已是同一范式。
- **Alternatives considered**：① 放进根模块 —— 违反锁定策略，弃；② 用 build tag 隔离 —— 依赖仍会进 `go.mod`，弃；③ 只给 `go run` 单文件 —— 无法表达 module 依赖边界，弃。

## D2. Java 侧版本上调到 Spring Boot 3.5.3 + `release 21`

- **Decision**：`spring-boot-starter-parent` `3.5.3`，`<java.version>21</java.version>`（用本地 JDK 23 编译到 21 字节码）。
- **Rationale**：本地只有 JDK 23（`java_home -V` 仅一项）。指南中的 Boot `3.2.5` 面向 Java 17–21，Spring Framework 6.1 的 ASM 不支持 Java 23 字节码（class file 67），在 JDK 23 上跑官方示例有「Unsupported class file major version」风险。Boot 3.5 官方支持 Java 17–24；编译目标压在 21 可让 JDK 17+ 的用户照样跑。
- **Alternatives considered**：① 坚持 3.2.5 —— 需额外装 JDK 21，且与本地环境不符；② 用 Gradle —— 本地无 Gradle，Maven 已就绪；③ 不用 Spring 而手写 servlet —— 违反「协议实现用成熟库」。
- **Verification**：实现阶段以 `mvn -q -DskipTests package` 通过为准；失败则回退 `3.4.7`（同属 Java 23 支持区间）。

## D3. 三个示例的令牌一律只存服务端（偏离指南的 Go 示例）

- **Decision**：示例自己的会话里只放会话 id（cookie 名 **`sid_go` / `sid_java` / `sid_py`**，`HttpOnly`），access/refresh/id token 存在**服务端**存储；`/api/me` 从服务端取 token 解声明。
- **Rationale**：指南 §9.3 的 Go 示例用 `gorilla/sessions` 的 CookieStore 存 `id_token`/`access_token`。CookieStore 只签名不加密，令牌实际以明文（可解码）随 cookie 进入浏览器，直接违反宪法 IV「浏览器只允许持有 sid cookie、不得持有任何 token」。示例是被复制去生产的模板，不能留这种坑。
- **cookie 命名（critique E1）**：三个示例与 IdP 同处 `localhost`，HTTP cookie **不按端口隔离**，若也用 `sid` 会与 IdP 会话互相覆盖。故固定语言前缀，并在 e2e 里加「IdP 的 `sid` 未被改写」断言（A11）。
- **Alternatives considered**：① 照抄 CookieStore —— 违反宪法 IV；② CookieStore 改存加密（gorilla/securecookie 加密需额外密钥管理）—— 仍是「令牌在浏览器」，弃；③ 三语言统一用服务端 JSON 文件存会话 —— 引入落盘与并发清理负担，改用进程内 map（重启失效，演示足够）。

## D4. Python 依赖版本（3.14 可装）

- **Decision**：`fastapi>=0.115,<1`、`authlib>=1.4,<2`、`uvicorn>=0.34,<1`（不带 `[standard]`）、`httpx>=0.28,<1`、`itsdangerous>=2.2,<3`、`jinja2>=3.1,<4`，不写死指南中的小版本。
- **Rationale**：本地 Python 3.14.7。已核实 `pydantic-core 2.50.1` 提供 cp314 轮子（31 个），因此 FastAPI 栈在 3.14 上无需编译即可安装；指南里的 `fastapi==0.115.6` 早于 3.14，硬钉会解析失败。`uvicorn` 不带 `[standard]` 是为了避免 `uvloop`/`httptools` 的编译依赖。
- **Alternatives considered**：① 退回 Starlette 裸用 —— 可用但失去指南的 FastAPI 对照价值；② 用 `uv` 管理环境 —— 增加新工具依赖，`venv + pip` 已够。

## D5. Java 回调路径对齐 `/callback`

- **Decision**：`redirect-uri: "{baseUrl}/callback"` + `oauth2Login().redirectionEndpoint(baseUri("/callback"))`，两处同时改。
- **Rationale**：SC-006 要求三语言路由一致；指南 §9.4 已明确「想统一必须两处同时改，否则 code 打到 404」。这是示例内配置选择，不改变 IdP 的逐字符匹配规则。
- **Alternatives considered**：保留框架默认 `/login/oauth2/code/omino` —— 三语言路由不一致，横向对照价值打折。

## D6. 端到端脚本的客户端注册幂等策略

- **Decision**：按 `clientName` 用 `GET /api/v1/admin/clients?keyword=` 查找 → 命中则 PATCH `redirectUris` 并复用；未命中则创建。密钥缓存到 `.tmp/e2e-examples/clients.env`（gitignored）：缓存命中且客户端仍存在则直接用，否则调用 `POST /api/v1/admin/clients/{id}/secret` 轮换取新值并回写缓存。
- **Rationale**：`clientId` 由服务端生成（ULID 后 16 位），请求体不接受自定义 id；`clientSecret` 只在创建/轮换时返回一次；管理 API 没有 DELETE。三者叠加决定「查询复用 + 缓存密钥 + 按需轮换」是唯一稳定做法，满足 FR-012 的幂等要求且不依赖手工清理。
- **Alternatives considered**：① 每次运行都新建客户端 —— 库内残留会无限增长；② 每次运行都轮换密钥 —— 会让手工运行中保存的旧密钥失效，破坏「跑完 e2e 还能手动点示例」的体验。

## D7. 刷新单飞的实现基线

- **Decision**：三语言各用一把进程内互斥锁（Java `synchronized`/`ReentrantLock`、Python `asyncio.Lock`、Go `sync.Mutex`），锁内比对「当前令牌是否已被别人换过」，已换则直接返回，不再重复刷新。
- **Rationale**：IdP 的 refresh 是轮换 + 重放即吊销整族（指南 §7）。示例必须示范最小正确实现，否则复制者必然踩「并发刷新把用户踢下线」。
- **Alternatives considered**：不做单飞只在文档里写警告 —— 等于给错误示范；用分布式锁 —— 示例无 Redis，过度设计。

## D8. 演示界面用各语言模板，不引进前端构建链

- **Decision**：Go `html/template`、Java Thymeleaf、Python Jinja2，页面为单文件 + 内联 `<style>`。
- **Rationale**：FR-018 要求「可点击演示界面」，同时禁止引入与该语言无关的构建链。模板方案零额外工具链，三份页面结构可逐字对照。
- **Alternatives considered**：独立 SPA —— 需 Node/打包器，把「按说明跑起来」的前置条件翻倍；纯 JSON —— 已被澄清否决。

## D9. 端口与命令入口

- **Decision**：`8000`（Go）/ `8001`（Java）/ `8002`（Python），均可用 `APP_PORT` 覆盖；验证入口只有 `bash scripts/e2e/examples.sh`，**不提供** `make` 别名。
- **Rationale**：跨示例 SSO 断言要求三者同时在线（FR-017）；`scripts/check-docs-commands.sh` 会校验文档里出现的每个 `make <target>` 是否真实存在，不引入别名就少一个必须同步的登记点。
- **Alternatives considered**：全部用 8000 逐个启动 —— 无法验证 SC-003 的 3/3；加 `make` 别名 —— 收益仅是少敲几字符，代价是文档/门禁同步面变大。

## D10. 登出链路的落点

- **Decision**：示例 `/logout` 清本地会话后 302 到 `{issuer}/logout?next=/logged-out`；不调用 `end_session`。
- **Rationale**：指南 §8 明确 `end_session` 只吊销该 client 的 refresh_token、不清 `sid`，真正清 IdP 会话的是 `/logout`。`post_logout_redirect_uri` 不可注册，故不在示例中尝试回跳。
- **Alternatives considered**：`end_session` —— 用户会以为登出失败（回到示例仍是登录态），弃。

## D11. 不引入 `gorilla/sessions`（critique E2）

- **Decision**：Go 示例只用 `net/http` 的 `SetCookie`/`ReadCookie` + 进程内 `map` 管理会话与 `pending`。
- **Rationale**：令牌已改为服务端存储（D3），`gorilla/sessions` 剩下的能力（签名、序列化）对「只存一个随机 id」没有价值；少一个第三方依赖，也让三语言实现逐行可比。
- **Alternatives considered**：保留它做 `pending` 存储 —— 多一层抽象但无收益；改用 `securecookie` —— 同样多余。

## D12. 文档收敛为索引（critique P1）

- **Decision**：`docs/integration-guide.md §9` 的三语言完整代码改为「指向 `examples/{go,java,python}` 的索引 + 与文档既有约定的差异说明（Boot 版本、回调路径对齐、cookie 前缀、服务端会话）」，`examples/` 成为接入代码的唯一权威。
- **Rationale**：两份可复制代码必然漂移，而本项目已为此建过 `check-registry` / `check-docs-commands` 两道门禁；与其再加第三道比对门禁，不如消掉重复源。协议与端点事实（§1–§8、§10–§13）仍留在文档，不因收敛而减少。
- **Alternatives considered**：保留双份 + 机械比对 —— 比对只能保证「代码片段相同」，无法保证「叙述与代码一致」，且维护成本翻倍。

## D13. 端到端使用专用账号（critique E3）

- **Decision**：脚本用 `examples` / `Examples@12345`（`E2E_EXAMPLE_USER`/`E2E_EXAMPLE_PASS` 可覆盖），不存在则经管理 API 创建、存在则复用；对同一账号登录失败**立即中止、不重试**。
- **Rationale**：服务端有失败计数与锁定（5 次）。脚本要登录 3 次以上（A4–A6、A8），一旦凭据配置错就可能把演示账号 `zhangsan` 锁死，反过来破坏 dev 门户——这与 FR-012「不得破坏既有演示数据」直接冲突。专用账号把风险隔离，且 DB 残留已获用户许可。
- **Alternatives considered**：继续复用 `zhangsan` + 「失败即停」—— 仍可能因为一次拼写错而锁号；每轮创建新账号 —— 库里噪音增长。

---

## D14. 降级路径用「调试覆盖变量」注入（critique E11）

- **Decision**：示例支持 `SSO_USERINFO_URL_OVERRIDE`（默认空），只覆盖 `userinfo` 地址；e2e 的 A15 把它指向 `http://127.0.0.1:1/userinfo` 后重启一个示例来完成降级断言。启用时启动日志打印 `debug=userinfo_override`。
- **Rationale**：`userinfo` 地址来自发现文档，若不开口子，A15 只能永远 `[SKIP]`，SC-009 就成了纸面指标。用覆盖变量比「停掉 IdP」精准（不影响其他断言），也避免为测试加进程内开关。
- **Alternatives considered**：① 停 IdP 再验 —— 会把所有断言一起打死；② 只做单元测试 —— 本特性没有单测框架；③ 允许 A15 永久 SKIP —— 等于不验证。
- **边界**：该变量只能改 userinfo 地址，不得成为关闭验签/令牌校验的开关（契约已写明）。

## D15. 专用账号的创建是「直接 POST，冲突即存在」（critique E14）

- **Decision**：`POST /api/v1/admin/users`（`username`/`displayName`/`password` 必填，`email` 可选），把 `ErrUsernameExists` 当成功处理；随后登录一次确认密码可用，失败即 `exit 2`。
- **Rationale**：先查后建会多一次分页查询且仍有竞态；直接创建 + 冲突即存在是最短的幂等写法。密码需满足强度策略（`Examples@12345` 达标），新建用户状态为 active 且不强制改密，因此无头登录可行（已核 `service/user.go` 的 `ErrAccountDisabled`/`ErrAccountLocked` 分支）。
- **Alternatives considered**：① 先 `GET /admin/users?keyword=` 再决定 —— 多一次调用且仍可能撞竞态；② 复用演示账号 —— 已因锁号风险否决（D13）。

## D16. cookie 前缀用「正向断言」守住（critique E12/E16）

- **Decision**：A11 改为「门户 `/api/v1/me` 仍 200 + 示例未写 `sid`」，另加 **A16**「三个 `Set-Cookie` 名恰为 `sid_go`/`sid_java`/`sid_py` 且互不相同」。
- **Rationale**：用 sid 字节比对既可能因 IdP 正常刷新会话而误报，也可能因为三个示例都错叫 `session` 而漏报；「结果（门户仍可用）+ 原因（cookie 名正确）」两侧都盯住才是稳的判据。
- **Alternatives considered**：只保留字节比对 —— 误报/漏报都占；只保留前缀断言 —— 测不出「是否真的踩坏了门户」。

---

## 依赖提案（按 AGENTS §2.0 的三步走：提案 → 确认 → 登记）

**边界声明**：以下依赖**全部只存在于 `examples/*` 自有清单**，不进根 `go.mod`、不进 §2.1 锁定表、不进任何镜像；因此不改变服务端的运行时依赖图。根 `go.mod` 零改动是本特性的硬约束（FR-016）。

**版本策略（critique E9）**：全部**锁精确版本**，清单里不留 `>=` / 区间。实现首步解析出具体版本后回写清单，并同步 `examples/README.md` 的「已验证版本矩阵」（语言版本 + 依赖版本 + 验证日期）。

| 语言 | 依赖 | 版本 | 许可证 | 用途 / 不用它的代价 |
|---|---|---|---|---|
| Go | `github.com/coreos/go-oidc/v3` | 精确版本，实现首步 `go get` 解析后回写 `go.mod` | Apache-2.0 | 发现、JWKS 缓存、id_token 验签。自己写等于重造协议层，宪法 I 禁止 |
| Go | `golang.org/x/oauth2` | 同上（由 `go-oidc` 与示例共同决定） | BSD-3-Clause | 授权 URL、换令牌、刷新。手写表单与 Basic 认证易错 |
| Java | `spring-boot-starter-parent` | `3.5.3`（精确） | Apache-2.0 | 版本基线（见 D2）；其 starters 由 parent 统一管理版本 |
| Java | `spring-boot-starter-oauth2-client` | 随 parent | Apache-2.0 | 授权码 + PKCE + 验签全内置 |
| Java | `spring-boot-starter-thymeleaf` | 随 parent | Apache-2.0 | 演示首页（D8） |
| Python | `fastapi` / `authlib` / `uvicorn` / `httpx` / `itsdangerous` / `jinja2` | 精确版本（`==`），实现首步 `pip freeze` 回写 `requirements.txt` | MIT / BSD-3-Clause | 框架、协议、ASGI 服务、HTTP 调用、会话签名、模板 |
| — | ~~`gorilla/sessions`~~ | **已移除**（D11） | — | 令牌改服务端存储后不再需要 |

许可证全部为 MIT / Apache-2.0 / BSD，无 GPL/AGPL；无 CGO 依赖（Go 侧纯 Go，Python 侧 `pydantic-core` 用预编译轮子）。

## 未决项

无（critique E9/E10 已定：锁精确版本；演示保持 `localhost`）。实现阶段的验证动作（已写进 quickstart/断言 A0）：
1. `mvn -q -DskipTests package` 在 JDK 23 下通过（否则回退 Boot `3.4.7`）；
2. `python3 -m venv && pip install -r requirements.txt` 在 3.14 下通过；
3. `bash scripts/e2e/examples.sh` 连续两次全绿。
