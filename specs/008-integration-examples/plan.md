# Implementation Plan: 三语言接入示例工程与本地端到端验证

**Branch**: `008-integration-examples` | **Date**: 2026-10-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/008-integration-examples/spec.md`

## Summary

在 `examples/` 下交付 Go / Java / Python 三个**独立可运行**的 OIDC 接入示例，每个工程暴露同一组四端点（`/login`、`/callback`、`/api/me`、`/logout`）与一页可点击演示界面，协议实现全部交给各自语言的成熟库（不写验签、不写状态机）。另交付 `scripts/e2e/examples.sh`：无头驱动、自行拉起/关闭三个示例，完成「三示例登录 → 跨示例免登录 → 刷新轮换 → 登出后全 401」的断言，幂等可重跑。**统一身份服务零改动**（不改协议层/管理 API/表结构/前端）。

关键取舍（详见 [research.md](./research.md)）：
1. `examples/go` 用**独立 Go module**（同 `tools/go.mod` 范式），根 `go.mod` 与 §2.1 锁定表零改动，示例依赖锁精确版本；
2. 三示例的令牌一律存**服务端会话**，不落浏览器 cookie——指南的 Go 示例把 token 塞进 `gorilla/sessions` CookieStore，那会把令牌明文（仅签名）送到浏览器，与宪法 IV 冲突；连带去掉 `gorilla/sessions`（D11）；
3. Java 侧 Spring Boot 由指南的 `3.2.5` 上调（本地仅 JDK 23，Boot 3.2 只支持到 Java 21），Python 侧钉住支持 3.14 的版本；
4. 会话 cookie 名固定 `sid_go`/`sid_java`/`sid_py`——三示例与 IdP 同处 `localhost` 且 cookie 不按端口隔离，重名会覆盖 IdP 的 `sid`（critique E1）；
5. `docs/integration-guide.md §9` 的三语言代码收敛为索引，`examples/` 是接入代码唯一权威（D12）；
6. 端到端用**专用示例账号**而非演示账号 `zhangsan`，避免登录失败计数把演示账号锁死（D13）。

## Technical Context

**Language/Version**: 主站 `Go 1.26.0`（**不改**）；示例：Go `1.23+`、Java `17+`（本地 JDK 23，按 `release 21` 编译）、Python `3.11+`（本地 3.14）

**Primary Dependencies**（全部锁精确版本，仅存在于 `examples/*` 清单）：
- Go 示例：`github.com/coreos/go-oidc/v3`、`golang.org/x/oauth2`、`html/template`（标准库）；**不需要** `gorilla/sessions`（D11）
- Java 示例：Spring Boot `3.5.3`（`starter-web`、`starter-oauth2-client`、`starter-thymeleaf`）
- Python 示例：`fastapi`、`authlib`（starlette 集成）、`uvicorn`、`httpx`、`itsdangerous`、`jinja2`
- 验证脚本：纯 `bash` + `curl` + `python3`（已有工具链，无新依赖）

**Storage**: 示例仅进程内会话（Java `HttpSession` / Python 服务端字典 + 签名 cookie / Go 内存 map + `sid` cookie）；IdP 侧复用既有 MySQL + Redis，仅通过管理 API 写入客户端注册

**Testing**: `scripts/e2e/examples.sh` 的无头断言（登录、SSO、刷新轮换、登出、失败可诊断）；不新增各语言的单元测试框架

**Target Platform**: 本地开发机（macOS / Linux），依赖 IdP 已按 README「本地开发」运行

**Project Type**: 多语言示例集合（3 个独立小工程）+ 1 个验证脚本 + 1 份说明

**Performance Goals**: 无吞吐目标（单用户演示）；约束是 **端到端一次跑完 ≤ 10 分钟**（SC-002）与 **单语言上手 ≤ 10 分钟**（SC-001）

**Constraints**:
- 统一身份**零改动**（FR-015）：不新增路由、不改 DTO、不动 `openapi.json`
- 根 `go.mod` / §2.1 锁定表**零改动**；示例依赖只存在于 `examples/*` 自有清单且**锁精确版本**（FR-023）
- 示例与脚本不得进入镜像构建上下文（需在 `.dockerignore` 排除 `examples/`）
- 三语言端点路径、环境变量名、返回字段名**完全一致**（SC-006）
- 令牌与客户端密钥**不出服务端**（宪法 IV）：浏览器只持有会话 id，且 cookie 名必须是 `sid_go`/`sid_java`/`sid_py`——**禁止** `sid`（与 IdP 同域冲突，FR-021）
- 端到端只使用专用示例账号，且登录失败即止（不清空、不重试，FR-012）
- `docs/integration-guide.md §9` 收敛为索引（FR-020）；协议事实（§1–§8、§10–§13）不动

**Scale/Scope**: 3 个工程（每个 ≤ 6 个文件）+ 1 个脚本（≤ 400 行）+ 1 份 `examples/README.md`；只面向本地演示

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 / 落实方式 |
|---|---|---|
| **I. 协议层单一来源** | PASS | 三条链路各用指南指定的成熟库（`go-oidc` / Spring Security OAuth2 Client / Authlib）；示例内**不出现**任何手写 JWT 验签、JWKS 缓存或授权状态机；不改 IdP 的 `zitadel/oidc` 适配 |
| **II. 端口与信任边界物理分离** | PASS | 示例是独立进程与独立端口（8000/8001/8002），与 IdP 的 `:8080`/`:8081` 无耦合；不向 `/oauth2/*` 挂任何中间件（不在本项目改动范围内） |
| **III. 分层与数据一致性** | PASS | 本特性**不写服务端代码**、不改表结构；客户端注册只经管理 API（`api → service → store` 既有链路）；无「先查后改」引入 |
| **IV. 凭证不出服务端** | PASS（有偏离需说明） | 浏览器只拿 `HttpOnly` 会话 cookie；access/refresh/id token 仅存服务端。**偏离点**：指南的 Go 示例把 token 存入 `gorilla/sessions` CookieStore（签名但不加密，令牌实际落到浏览器）——本实现改为服务端 session map，见 research.md D3 |
| **V. 结构变更只走手写 SQL 迁移** | PASS | 无 `migrations/` 变更；示例不建表、不写库 |
| **技术与接口约束（依赖登记）** | PASS（有边界） | §2.1 锁定表**零改动**：Go 示例依赖写在自己 `examples/go/go.mod`，不进根模块（`go test ./...` / `golangci-lint ./...` 自动排除嵌套 module）。依赖清单与理由见 research.md「依赖提案」 |
| **协作纪律（不扩写文档）** | PASS（显式例外） | 仅新增 `examples/README.md` 一份说明；理由已在 spec Assumptions 记录。**不**改根 README、不新增脚本以外的工具 |

**Gate 结论**：无阻塞违例。两处偏离（token 存储、Boot 版本）已在 research.md 写成决策 + 理由，不需 Complexity Tracking 表（表为空）。

## Project Structure

### Documentation (this feature)

```text
specs/008-integration-examples/
├── plan.md              # 本文件
├── research.md          # Phase 0：决策与依赖提案
├── data-model.md        # Phase 1：会话/注册/运行产物的字段与状态
├── quickstart.md        # Phase 1：本地验证剧本
├── contracts/           # Phase 1：对外接口契约
│   ├── example-app-api.md
│   └── e2e-examples-cli.md
└── tasks.md             # Phase 2（/speckit.tasks 产出，非本命令）
```

### Source Code (repository root)

```text
examples/
├── README.md                     # 唯一说明：环境变量、注册回调、运行与验证命令、报错对照
├── go/                           # 独立 module（不进根 go.mod）
│   ├── go.mod
│   ├── main.go                   # 路由装配 + 启动校验
│   ├── oidc.go                   # 发现/JWKS/验签/换令牌/刷新（库调用）
│   ├── session.go                # 服务端会话存储（token 不出服务端）
│   └── templates/index.html      # 演示首页（html/template）
├── java/
│   ├── pom.xml
│   └── src/main/
│       ├── java/com/example/demo/
│       │   ├── DemoApplication.java
│       │   ├── SecurityConfig.java   # PKCE、回调对齐、登出
│       │   └── AuthController.java   # /api/me、userinfo、首页
│       └── resources/
│           ├── application.yaml
│           └── templates/index.html  # 演示首页（Thymeleaf）
└── python/
    ├── requirements.txt
    ├── app.py                    # 路由 + Authlib 注册 + 刷新单飞
    ├── session_store.py          # 服务端会话（token 不出服务端）
    └── templates/index.html      # 演示首页（Jinja2）

scripts/
└── e2e/
    ├── run.sh                    # 既有：四域 HTTPS 端到端（不动）
    └── examples.sh               # 新增：三语言示例端到端（自行拉起/关闭）

# 需顺带修改的既有文件（均为最小改动）
#   .dockerignore              —— 排除 examples/，避免进入镜像构建上下文
#   docs/integration-guide.md  —— §9 三语言完整代码收敛为索引 + 差异说明（FR-020 的核心交付物，勿漏）
#   AGENTS.md §3               —— 目录树补 examples/ 与 scripts/e2e/examples.sh（受保护文件，提交需尾注）
```

**Structure Decision**: 采用「仓库根 `examples/` 平铺三个独立工程 + `scripts/e2e/` 放置验证脚本」。理由：
- `examples/go` 必须是独立 module，否则 `go-oidc`/`x/oauth2` 会进根 `go.mod`，直接违反 §2.1 锁定策略；
- 验证脚本放 `scripts/e2e/` 与既有 `run.sh` 同目录，沿用其「自行拉起与被测进程、`.tmp/` 放中间产物、失败逐项断言」的模式；
- 不提供 `make` 别名：一旦在 `specs/` 或 `docs/` 里写出 `make <target>`，`scripts/check-docs-commands.sh` 会要求该目标真实存在；本特性只需一条脚本命令，少一个登记点。
- 会话 cookie 名不是「变量改名」：Java 需设 `server.servlet.session.cookie.name: sid_java`（默认 `JSESSIONID`）、Python 需 `SessionMiddleware(app, session_cookie="sid_py")`（默认 `session`）、Go 直接 `SetCookie("sid_go", …)`；漏改即复现 critique E1 的事故，tasks 里要把这三行配置单独列出。
- `examples/python/app.py` 同时承载路由、Authlib 注册与调试用 `SSO_USERINFO_URL_OVERRIDE`（FR-025）；该覆盖只影响 userinfo 地址，不得成为关闭校验的开关。

## Complexity Tracking

无违例，本表不使用。

## Post-Design Constitution Re-check（Phase 1 之后）

设计产出（[data-model.md](./data-model.md) / [contracts/](./contracts/) / [quickstart.md](./quickstart.md)）复核结果：未引入服务端代码改动、未新增表、未改任何既有契约；G1–G5 与依赖边界结论**不变**。

可核锚点：`internal/api/docs/openapi.json` 的 sha256 已冻结在 [api-evolve/baseline/manifest.json](./api-evolve/baseline/manifest.json)（`7a5cb8d1…ede41`）。本特性交付时该文件必须与基线**逐字节一致**（FR-015 的可执行证明）。

## Critique 修订记录（2026-10-24）

双视角评审（[critique-20261024-2046.md](./critiques/critique-20261024-2046.md)）的 3 项必修与 8 项建议已全部并入规格、计划与契约：

| 来源 | 落点 |
|---|---|
| E1 会话 cookie 冲突 | spec FR-021；plan Constraints；[contracts/example-app-api.md](./contracts/example-app-api.md)「会话 cookie 命名」；[data-model.md](./data-model.md) §3；e2e 断言 A11 |
| E3 演示账号锁定 | spec FR-012 + Assumptions；research D13；contracts/e2e 变量表与 A2；quickstart 排障表 |
| P1/X1 双份权威 | spec FR-020 + Assumptions「文档收敛的边界」；research D12；plan Constraints |
| P2/P6/E6 指标可核 | spec SC-001/002/003；quickstart「首次会慢」与耗时要求 |
| P3 版本漂移 | spec FR-023 + SC-006；research 依赖版本策略；quickstart 完成判据 |
| P4/X2 演示安全边界 | spec FR-022；contracts 文末「仅演示声明」；quickstart 场景 A 期望 |
| P5 userinfo 降级 | spec FR-007 + SC-009 + Edge Cases；contracts `/api/me` 的 `degraded`；e2e A15 |
| E2 去掉 gorilla | research D11 + 依赖表；plan 技术上下文 |
| E4/E5/E8 脚本与日志健壮性 | contracts/e2e「行为约束」（超时、信号清理、0600）；contracts「日志约定」 |
| E7 构建冒烟 | contracts/e2e 断言 A0 与 `--self-test` |

复验轮（[critique-20261024-2056.md](./critiques/critique-20261024-2056.md)，0 个 🎯）也已并入：

| 来源 | 落点 |
|---|---|
| E11 降级可验证 | spec FR-025 + SC-009 收紧；contracts「调试与故障注入」（`SSO_USERINFO_URL_OVERRIDE`）；research D14；e2e A15（禁止 SKIP） |
| E12/E16 cookie 判据 | contracts/e2e A11 改为「门户仍 200 + 未写 `sid`」，新增 A16 前缀断言；spec SC-010；research D16 |
| E13 默认 cookie 名 | contracts cookie 表增「实现落点」列（`server.servlet.session.cookie.name`、`session_cookie=`）；plan 结构见下 |
| E14 账号创建 | contracts/e2e A2 改为「直接 POST，冲突即存在」；research D15 |
| E17 漏项 | plan 修改清单补 `docs/integration-guide.md`（FR-020 交付物） |
| P7 刷新可见 | spec FR-024；contracts `/api/me` 增 `accessExpiresIn`/`lastRefreshedAt` 与页面状态行；data-model §3 |
| P8 依赖下载慢 | contracts/e2e 增 `E2E_NET_TIMEOUT`/`E2E_CURL_TIMEOUT`；quickstart 排障补镜像方案 |
| E15 残留清理 | quickstart §5「清理（可选）」；contracts/e2e「残留是有意的」 |
