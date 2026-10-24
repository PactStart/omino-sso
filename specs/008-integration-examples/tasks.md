---

description: "Task list for 008-integration-examples"
---

# Tasks: 三语言接入示例工程与本地端到端验证

**Input**: Design documents from `specs/008-integration-examples/`

**Prerequisites**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**Tests**: 本项目不接受「测试可选」。本特性的被测对象是**三个演示工程**，它们不在主站 `make test` 的覆盖范围内，因此测试形态固定为两类，**与实现同提交**：

1. **构建/启动冒烟**（T012/T015/T018，以及 e2e 的 A0）：证明三份实现能编译、能启动、缺配置会失败；
2. **端到端断言 A0–A16**（T020–T022）：证明登录、SSO、刷新轮换、登出、降级、cookie 前缀、无密钥外泄全部成立。

任何实现类任务在勾选前必须通过其对应的冒烟或断言项，否则视为未完成。

**Organization**: 按用户故事分组；US1 的三门语言可并行；US2 依赖 US1；US3 依赖三份实现齐备。

## 任务完成定义（DoD，来自 AGENTS.md §7.4）

1. **代码**：行为符合 spec 与 contracts，无 TODO / 空实现；
2. **测试**：同提交内通过该任务列出的冒烟或断言项；
3. **登记**：新增目录/脚本写入 `AGENTS.md §3`（T028）；示例依赖只落在各自清单，**不进**根 `go.mod` 与 §2.1 表；
4. **门禁**：`make build && make test && make lint && make check` 全绿（T029），且 `internal/api/docs/openapi.json` 与 api-evolve 基线逐字节一致。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属用户故事（US1 / US2 / US3）
- 每条任务带确切文件路径

---

## Phase 1: Setup（共享准备）

**Purpose**: 划定示例与主站的物理边界，先立说明与脚本骨架

- [x] T001 [P] 在 `.dockerignore` 追加 `examples/`，确认镜像构建上下文不含示例
- [x] T002 [P] 创建 `examples/README.md` 骨架：6 个环境变量表（含调试项 `SSO_USERINFO_URL_OVERRIDE` 及「仅调试、生产禁用」标注）、三示例端口与回调（8000/8001/8002 → `/callback`）、运行命令、验证命令、`[start]/[login]/[refresh]` 日志说明、仅演示声明（生产必改三处）、版本矩阵占位、清理（可选）章节
- [x] T003 [P] 建立脚本骨架：`scripts/e2e/examples.sh`（占位阶段以退出码 2 拒绝执行，避免被误读为"验证已通过"）与 `scripts/e2e/lib/admin.sh`（只声明约定，不实现）——已随任务书落地，供 T004/T019 替换

---

## Phase 2: Foundational（阻塞所有用户故事）

**Purpose**: 三示例共用的「凭据准备」与「最小可启动骨架」；没有它们任何示例都跑不起来

- [x] T004 在 `scripts/e2e/lib/admin.sh` 实现管理员侧辅助：管理员登录（`POST {API}/api/v1/auth/login` + cookie jar）；客户端 upsert（`GET /api/v1/admin/clients?keyword=` 后**按 `clientName` 逐字比对**，缺失则 POST、命中则 PATCH `redirectUris`）；密钥获取（命中 `.tmp/e2e-examples/clients.env` 则复用，否则 `POST /api/v1/admin/clients/{id}/secret` 轮换并回写）；写文件 `umask 077` + `chmod 600`
- [x] T005 在 `scripts/e2e/lib/admin.sh` 实现专用账号 upsert：`POST /api/v1/admin/users`（`username`/`displayName`/`password` 必填、`email` 可选），**把「已存在」当成功**；随后登录一次确认，失败即 `exit 2` 且不重试（避免触发失败计数锁定）
- [x] T006 [P] 创建 `examples/go`（`go.mod`：独立 module、`go 1.23`、依赖锁精确版本；`main.go` 骨架：读 6 个环境变量、缺项打印名称后非零退出、发现文档探测失败即退出、端口占用不换端口、打印 `[start]`）
- [x] T007 [P] 创建 `examples/java`（`pom.xml`：Spring Boot `3.5.3`、`<java.version>21</java.version>`、web/oauth2-client/thymeleaf；`DemoApplication.java`、`application.yaml` 骨架）
- [x] T008 [P] 创建 `examples/python`（`requirements.txt` 精确 `==` 版本；`app.py` 骨架：uvicorn 启动、读 6 个环境变量并校验、打印 `[start]`）

**Checkpoint**: 三个工程分别通过 `go build ./...`、`mvn -q -DskipTests package`、`pip install -r requirements.txt && python -c "import app"`，且缺环境变量时都以非零码退出

---

## Phase 3: User Story 1 - 三种语言各自跑通登录闭环（P1）🎯 MVP

**Goal**: 每个示例从自己的「登录」入口经 IdP 完成授权码 + PKCE 登录，回到示例后能看到自己的用户信息；登出后必须重新登录。

**Independent Test**: 只启动 IdP 与该语言示例，按 quickstart 场景 A 操作——未登录 `/api/me` = 401；登录后 = 200 且 `sub` 可见；登出后再访问 = 401；浏览器 cookie 名为 `sid_<lang>`。

### Go 示例

- [x] T009 [P] [US1] 实现 `examples/go/oidc.go`：授权 URL（`response_type=code`、`code_challenge_method=S256`、`state`、`nonce`、scope 含 `offline_access`）；回调 `state` 逐字节比对；`POST {issuer}/oauth2/token` 用 **HTTP Basic** 换令牌；用 `go-oidc` 验签 `id_token`（`iss`/`aud`/`exp`/`nonce`）
- [x] T010 [P] [US1] 实现 `examples/go/session.go`：进程内会话 map + `SetCookie("sid_go", …, HttpOnly, SameSite=Lax, Path=/)`；`pending`（state/nonce/verifier）与登录态分离；`sync.Mutex` 单飞刷新且在锁内复查令牌是否已被换过
- [x] T011 [US1] 实现 `examples/go` 的 `/api/me`、`/logout`、`/`（`html/template`）：`/api/me` 字段严格按 contracts（`sub/username/name/email/tenant/deptId/roles/amr/appRoles/permissions/degraded/accessExpiresIn/lastRefreshedAt`，空值为 `""`/`[]`/`null` 且不省键）；`userinfo` 失败降级为 200 + `degraded=["userinfo"]`；支持 `SSO_USERINFO_URL_OVERRIDE`（仅覆盖 userinfo 地址）
- [x] T012 [US1] Go 冒烟与手工验收：`go vet ./... && go build ./...`；按 quickstart 场景 A 点验四条断言（401 / 200+`sub` / 登出后 401 / cookie 名 `sid_go`），并确认三类日志不含令牌

### Java 示例

- [x] T013 [P] [US1] 配置 `examples/java/src/main/resources/application.yaml` + `SecurityConfig.java`：`server.servlet.session.cookie.name: sid_java`（默认 `JSESSIONID` 必须改）；`redirect-uri: "{baseUrl}/callback"` 与 `oauth2Login().redirectionEndpoint(baseUri("/callback"))` **两处同时**设置；PKCE 开启；scope 含 `offline_access`
- [x] T014 [US1] 实现 `examples/java/.../AuthController.java` + `templates/index.html`：`/api/me`（含 `appRoles`/`permissions`、`degraded`、`accessExpiresIn`、`lastRefreshedAt`）、`/logout` 跳 `{issuer}/logout?next=/logged-out`、首页会话状态行、日志三类行
- [x] T015 [US1] Java 冒烟与手工验收：`mvn -q -DskipTests package` 在 JDK 23 下通过（失败则回退 Boot `3.4.7` 并记入 README 版本矩阵）；手工点验同 T012 的四条断言，cookie 名须为 `sid_java`

### Python 示例

- [x] T016 [P] [US1] 实现 `examples/python/app.py`：Authlib 注册（`openid profile email offline_access`）、四端点路由、`SessionMiddleware(app, session_cookie="sid_py")`（默认 `session` 必须改）、`SSO_USERINFO_URL_OVERRIDE`
- [x] T017 [US1] 实现 `examples/python/session_store.py` + `templates/index.html`：服务端会话（令牌不进 cookie/页面）、`asyncio.Lock` 单飞刷新、`userinfo` 失败降级、首页会话状态行、日志三类行
- [x] T018 [US1] Python 冒烟与手工验收：`python3 -m venv venv && ./venv/bin/pip install -r requirements.txt && ./venv/bin/python -c "import app"`；手工点验同 T012 的四条断言，cookie 名须为 `sid_py`

**Checkpoint**: 三门语言各自可独立演示（MVP 完成），`examples/README.md` 的运行说明经一次实测校对

---

## Phase 4: User Story 2 - 一条命令完成本地端到端验证（P1）

**Goal**: `bash scripts/e2e/examples.sh` 自行拉起/关闭三个示例，跑完 A0–A16 并给出可定位结论；连跑两次全绿。

**Independent Test**: IdP 在跑的前提下执行该命令 → `exit 0`、`report.txt` 中 A0–A16 全 PASS、`.tmp/e2e-examples/clients.env` 可直接 `source` 起手工实例；再执行一次仍全绿。

- [x] T019 [US2] 用真实实现替换 `scripts/e2e/examples.sh` 的占位：preflight（元数据端点可达、三端口空闲、`go`/`mvn`/`python3` 存在，失败 `exit 2` 且未开始断言）、A0 构建冒烟、拉起三示例并 `trap` 在 `EXIT/INT/TERM` 关闭（`--keep` 时保留）
- [x] T020 [US2] 实现断言 A1–A7：客户端注册复用与回调对应、专用账号可用、三示例 `/api/me` 均 401 基线、三次登录闭环、三个 `sub` 一致（任一登录失败立即中止，不重试）
- [x] T021 [US2] 实现断言 A8–A11：跨示例 SSO（`Location` 不含 `/login?id=`）、刷新轮换（新 `refresh_token` ≠ 旧值）、刷新后 `sub` 不变、**门户 `/api/v1/me` 仍 200 且示例未写 `Set-Cookie: sid=…`**
- [x] T022 [US2] 实现断言 A12–A16：登出 302 指向 `{issuer}/logout?next=/logged-out` 且 `sid_<lang>` 被清除、登出后三示例全 401、响应体无 `clientSecret`/`refresh_token`/`id_token`、`userinfo` 覆盖降级（200 + `degraded=["userinfo"]`）、三个 `Set-Cookie` 名恰为 `sid_go`/`sid_java`/`sid_py` 且互不相同
- [x] T023 [US2] 实现报告与诊断：写 `.tmp/e2e-examples/report.txt`（A0–A16 的 PASS/FAIL + 实测值 + 各示例「启动→首次 200」耗时）；失败时打印对应 `go.log`/`java.log`/`python.log` 三类日志尾部 20 行；实现 `--self-test`
- [x] T024 [US2] 实现超时与幂等：`E2E_CURL_TIMEOUT`（默认 10s）、`E2E_NET_TIMEOUT`（默认 300s）覆盖每步；连续执行两次均 `exit 0`（残留客户端与账号不影响）

**Checkpoint**: US1 与 US2 均独立通过；US2 是 SC-002/003/004/007/009/010 的唯一验证入口

---

## Phase 5: User Story 3 - 三语言横向对照（P2）

**Goal**: 三份实现的对外形状完全一致，且仓库里只有一份接入代码权威。

**Independent Test**: 逐项比对三工程的端点、环境变量名、`/api/me` 字段、日志三类行 → 100% 同名同义；`docs/integration-guide.md §9` 内不再有可复制的完整实现。

- [x] T025 [P] [US3] 一致性核对并修正差异：`examples/{go,java,python}` 的四端点路径、6 个环境变量名、`/api/me` 字段名与空值形态、`[start]/[login]/[refresh]` 日志格式逐项对齐（差异只允许存在于实现内部）
- [x] T026 [US3] 完成 `examples/README.md`：三语言对照表（端点 / 变量 / 字段 / 日志）、**已验证版本矩阵（语言版本 + 依赖精确版本 + 验证日期）**、镜像与代理排障（`PIP_INDEX_URL`、Maven mirror）、清理（可选）
- [x] T027 [US3] 收敛 `docs/integration-guide.md §9`：三语言完整代码改为「指向 `examples/` 的索引 + 差异说明」（Boot 3.5.3 而非 3.2.5、回调路径对齐 `/callback`、cookie 前缀、服务端会话）；验收：文档内无可复制的完整实现，且 §1–§8、§10–§13 未删减

---

## Phase 6: Polish & Cross-Cutting

- [x] T028 更新 `AGENTS.md §3` 目录树：补 `examples/`（go/java/python 三行）与 `scripts/e2e/examples.sh`、`scripts/e2e/lib/admin.sh`；提交带 `Protected-Change: AGENTS.md` + `Approved-By`
- [x] T029 门禁全跑：`make build && make test && make lint && make check`；`bash scripts/check-artifact-refs.sh`、`bash scripts/check-docs-commands.sh`；确认 `internal/api/docs/openapi.json` 的 sha256 与 `specs/008-integration-examples/api-evolve/baseline/manifest.json` 一致
- [x] T030 SC 复核并记录证据：SC-001（`report.txt` 首次 200 耗时）、SC-002（首次/后续两档）、SC-003（3/3）、SC-004、SC-005（无明文密钥）、SC-006（对照表与版本矩阵）、SC-007（连跑两次）、SC-008、SC-009、SC-010
- [x] T031 收尾核查：`SSO_USERINFO_URL_OVERRIDE` 未出现在常规用法中（仅调试章节）；`.tmp/e2e-examples/clients.env` 权限 `0600`；`examples/` 未被任何构建产物或镜像引用

---

### 完成证据（2026-10-24）

| 项 | 证据 |
|---|---|
| T029 门禁 | `make verify-full` = build + test + lint + check，`EXIT=0`；`check-registry` 报告 `openapi.json` 基线 sha256 `7a5cb8d1…`（未变）、路由 78/78、错误码 33/33、审计事件 37/37、环境变量 71/71/71；`check-artifact-refs` 校验 212 个路径通过；`check-docs-commands` 校验 23 个目标 / 326 处引用通过 |
| SC-001 / SC-002 | `.tmp/e2e-examples/report.txt` 记录「启动→首次 401」：go=1s、java=4s、python=1s（热缓存）；冷缓存首轮同样 17/17，仅构建阶段较慢（go/mvn 依赖下载受 `E2E_NET_TIMEOUT` 保护） |
| SC-003 | `A7 PASS sub=01M5Q256P1VV1HDS9V5HGHA2YE`（三个示例 `sub` 完全一致，3/3 组合） |
| SC-004 / SC-005 | `A12` 三示例登出均 302 到 `{issuer}/logout?next=/logged-out` 且清除 `sid_<lang>`；`A13` 登出后三示例全 401；`A14` 页面与 `/api/me` 均无 `clientSecret`/`refresh_token`/`id_token`/`access_token` |
| SC-006 | `examples/README.md §6` 三语言对照表 + 已验证版本矩阵；`A1` 断言三个 `clientName` 逐字匹配、回调与端口一一对应 |
| SC-007 | 连续三轮 `PASS=17 FAIL=0`（`run3`/`run4` 同端口连跑，`run5` 默认端口），客户端/密钥/专用账号全部复用 |
| SC-008 | `A9 PASS 三个示例各刷新一次并轮换 refresh_token`（日志 `[refresh] ok rotated=1` ×3）+ `A10 PASS` 刷新后 `sub` 不变 |
| SC-009 | `A15 PASS 200 + degraded=[userinfo]，appRoles/permissions 为 []`（真实重启 Python 示例并指向不可达 userinfo，非 SKIP） |
| SC-010 | `A16 PASS` 三个 `Set-Cookie` 名恰为 `sid_go`/`sid_java`/`sid_py` 且互不相同；`A11 PASS` 期间 IdP `/api/v1/me` 仍 200 且示例未写 `sid=` |
| T031 | `SSO_USERINFO_URL_OVERRIDE` 仅出现在 `examples/README.md §2` 与 A15 的临时环境；`.tmp/e2e-examples/clients.env` 权限 `0600`；`examples/` 已在 `.dockerignore` 中排除，未被任何镜像/构建产物引用 |

## Dependencies

```text
Phase 1 (T001–T003)
      │
Phase 2 (T004–T008)  ← 阻塞：无凭据准备与骨架，任何示例都跑不通
      │
      ├───────────────┬────────────────┐
      ▼               ▼                ▼
  Go (T009–T012)   Java (T013–T015)   Python (T016–T018)   ← US1，三者可并行
      └───────────────┴────────────────┘
                      │
                      ▼
               US2 (T019–T024)   ← 需三示例齐备才能断言 SSO
                      │
                      ▼
               US3 (T025–T027)   ← 对照与文档收敛需要三份实现
                      │
                      ▼
               Polish (T028–T031)
```

- **US1 内部**：T009/T010 可并行（不同文件），T011 依赖两者；T013→T014→T015 串行；T016→T017→T018 串行。
- **US2 内部**：T019→T020→T021→T022→T023→T024 串行（同一脚本，顺序追加）。
- **US3**：T025 依赖 US1 三份实现；T026 依赖 T025；T027 与 T026 可并行。

## Parallel Example: User Story 1

```bash
# 三门语言同时开工（不同目录，互不冲突）
Task: "实现 examples/go/oidc.go 与 session.go（T009/T010）"
Task: "配置 examples/java 的 application.yaml 与 SecurityConfig.java（T013）"
Task: "实现 examples/python/app.py 的四端点与 SessionMiddleware（T016）"

# Go 内部再并行：协议与存储分属不同文件
Task: "examples/go/oidc.go（T009）"
Task: "examples/go/session.go（T010）"
```

## Implementation Strategy

### MVP First（Go 一条链路）

1. Phase 1 → Phase 2；
2. 只做 Go 分支：T009–T012；
3. **停下验证**：quickstart 场景 A 走通、cookie 名 `sid_go`、登出后 401；
4. 此时已能回答「接入文档是否可执行」，其余语言是复述。

### Incremental Delivery

1. Go 跑通 → 补 Java（验证 Spring 侧 PKCE 与回调双处对齐的成本）；
2. 补 Python（验证 3.14 上的依赖与降级路径）；
3. 三份齐备 → US2 脚本接管验证（此后回归只看一条命令）；
4. US3 收敛文档，把「两份代码」变成「一份代码 + 索引」。

### 风险与回退

| 风险 | 触发信号 | 回退动作 |
|---|---|---|
| Java 在 JDK 23 下编译/运行失败 | T015 的 `mvn package` 报 class file 版本错误 | 回退 Boot `3.4.7`，把实测版本写入 README 矩阵 |
| Python 3.14 无可用轮子 | T018 的 `pip install` 解析失败 | 钉住可用组合；必要时降到纯 Starlette，并在 README 记录偏离 |
| cookie 前缀漏改 | T022 的 A16 失败 | 按 contracts「实现落点」列逐项修（Java `session.cookie.name`、Python `session_cookie=`） |
| 刷新把整族吊销 | A9/A10 失败且用户被踢 | 检查单飞锁是否在锁内复查令牌（T010/T017） |

## Notes

- `[P]` = 不同文件且无未完成依赖；同一脚本内的断言任务**不要**并行。
- 勾选前必须有冒烟或断言证据，不接受「代码写完了」作为完成依据。
- 示例依赖只落在 `examples/*` 清单；任何让根 `go.mod` 出现变化的改动都视为缺陷（FR-016）。
- 端到端脚本会留下客户端与专用账号（用户已允许），清理是手工动作，见 `examples/README.md`。
