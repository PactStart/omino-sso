# Quickstart Validation: API 接口文档与自助调试门户

**Feature**: `001-api-docs` | **Date**: 2026-10-20

本文件是**验收操作手册**：每条场景给出可直接执行的命令与判定标准。契约细节不在此复述，见 [contracts/doc-endpoints.md](./contracts/doc-endpoints.md)、[contracts/annotation-conventions.md](./contracts/annotation-conventions.md)、[data-model.md](./data-model.md)。

---

## 前置条件

| 项 | 要求 |
|---|---|
| 工作目录 | 仓库根 `/Users/apple/ai_workspace/omino-sso`，分支 `001-api-docs` |
| Go | 1.24.x（`go version` 通过） |
| 网络 | **不需要**。生成与校验必须离线完成（FR-014）；仅 `go mod download` 首次需要 |
| MySQL / Redis | **不需要**用于场景 1–3；仅场景 4/5（启动服务）需要，走既有 `docker-compose.yml` |
| 环境变量 | 场景 4/5 需要 `.env`（复制 `.env.example` 后填 DB/Redis） |

```bash
cp -n .env.example .env      # 首次
docker compose up -d mysql redis
make keys && make migrate-up
```

---

## 场景 0：前置 spike —— Try it out 是否真的携带会话 Cookie（写代码之前做）

这是 critique X1 提出的、决定 US1 核心价值能否成立的一步。不写业务代码，只用现有 `make dev` 加浏览器验证：

```bash
APP_ENV=dev make dev &
# 浏览器打开 http://127.0.0.1:8081/docs/index.html（先用任意临时页面替代亦可）
# 1) 调 POST /api/v1/auth/login 拿会话；2) 紧接着调 GET /api/v1/me
```

**观察点**：开发者工具 Network 面板中，第二个请求的 **Request Headers 是否包含 `Cookie: sid=...`**。

| 结果 | 含义 | 后续动作 |
|---|---|---|
| 携带 | US1 的「Try it out 闭环」成立 | 按计划实施 |
| 不携带 | 登录后调任何受保护接口都会 401，US1 退化为纯阅读页 | 先评估 swagger-ui 的 `requestInterceptor` / `withCredentials` 能否补齐；若不能，把 US1 的验收动作改为「页面内查看 + 手工 curl 调通」，相应下调 SC-002，并回写 `research.md` 风险表 |

**结论必须回写** `research.md` 的「未决与风险登记」对应行，不得只在会话里口头确认。

---

## 场景 1：生成文档（SC-005 / FR-011 / FR-014）

```bash
time make docs
```

**期望**

- 退出码 0，耗时 < 30s。
- 产出文件 `internal/api/docs/openapi.json`，顶层 `"openapi": "3.0.3"`。
- `.tmp/swagger/swagger.json` 存在（中间产物，被 gitignore）。
- 过程中**没有**连接 MySQL / Redis 的日志（无需 `docker compose up` 也能成功）。

```bash
python3 -c "import json;d=json.load(open('internal/api/docs/openapi.json'));print(d['openapi']);print(sum(len([m for m in v if m in ('get','post','put','patch','delete')]) for v in d['paths'].values()))"
```

**期望**：`3.0.3` 与 `16`（AC-1、SC-001）。

**断网复跑（critique E9）**：确保 swag CLI 已通过 `go mod download` 进入本地模块缓存后，断开网络再执行一次：

```bash
GOPROXY=off make docs; echo "exit=$?"
```

**期望**：`exit=0`。若失败并提示无法下载 `swaggo/swag`，说明 CLI 未被 `tools.go` 纳入 `go.mod`，与 FR-014 的"离线可生成"冲突，必须先修。

## 场景 2：幂等性与漂移校验（SC-003 / INV-2）

```bash
make docs && md5sum internal/api/docs/openapi.json    # 记录哈希 H1
make docs && md5sum internal/api/docs/openapi.json    # 记录哈希 H2
make docs-check; echo "exit=$?"
```

**期望**

- H1 == H2（连续生成逐字节一致）。
- `make docs-check` 输出 `exit=0`，且**不修改**工作区（`git status --porcelain internal/api/docs/` 为空）。

**反例（必须失败）**：在任一 handler 上临时加一个必填 `@Param`（或改一处 `binding:"required"`）后不重新生成：

```bash
make docs-check; echo "exit=$?"
git checkout -- internal/api docs 2>/dev/null
```

**期望**：`exit=1`，输出里含**具体定位**，形如 `POST /api/v1/admin/users: requestBody.email.required 20 -> 21`（而非仅"文件已变更"）。

## 场景 3：套件与静态检查（门禁）

```bash
make build && go test ./... && make lint
ls -lh bin/sso          # 记录体积，与本特性上线前对比（预期 +3~5MB，critique E6）
```

**期望**：全部通过。新增单测至少覆盖：

- `tools/openapi3` 的 2.0→3.0 转换与 `-check` 比对（正负样例）；
- `internal/api/docs` 的 info 注入（环境 + 版本）；
- prod / 非 prod 路由注册差异（CT-4、CT-5）；
- **8 处具名化的语义等价性**（`gin.H` 与 `dto.X` 同值 → 反序列化 `DeepEqual` + 键集合相同，AC-7）；此处禁止用"手工 curl 比对"代替，手工比对无法防回归。

`make build` 已依赖 `docs-check`，因此构建通过即说明内嵌文档与源码一致（critique E1）。体积实测值回写 `data-model.md` §8。

## 场景 4：非 prod 环境访问文档（SC-002 / FR-015 / FR-020）

```bash
APP_ENV=dev make dev &          # :8080 协议层 / :8081 管理 API
sleep 2
curl -s -o /dev/null -w '%{http_code} %{content_type}\n' http://127.0.0.1:8081/docs/index.html
curl -s http://127.0.0.1:8081/docs/openapi.json | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['openapi'], '|', d['info']['description'].splitlines()[0])"
```

**期望**

- 第一行：`200 text/html; charset=utf-8`。
- 第二行：`3.0.3 | 环境：dev`（`APP_ENV` 实际值出现在 `info.description` 中 → FR-019、CT-3）。
- 响应头**不含** `Set-Cookie`（CT-6）。

**人工验证（SC-002 的核心）**：浏览器打开 `http://127.0.0.1:8081/docs/index.html`：

1. 页面列出 6 个分组、16 个端点；
2. 先在 `/api/v1/auth/login` 处用真实开发账号登录（浏览器获得 `sid` cookie）；
3. 再调用 `GET /api/v1/me`，返回 `code="OK"` 且 `data.username` 正确；
4. 全程未查阅源码、未使用其它工具，耗时 < 10 分钟。

**离线校验（FR-020）**：打开浏览器 Network 面板并勾选 "Disable cache"，刷新文档页，确认**所有**请求的域名均为 `127.0.0.1:8081`，无 `unpkg` / `cdn.jsdelivr` / `fonts.googleapis` 等外部域。

**Redis 不可用时的可用性（CT-7，critique E3）**：

```bash
docker compose stop redis
curl -s -o /dev/null -w 'docs=%{http_code}\n' http://127.0.0.1:8081/docs/index.html
curl -s -o /dev/null -w 'spec=%{http_code}\n' http://127.0.0.1:8081/docs/openapi.json
docker compose start redis
```

**期望**：两行均为 `200`。若为 429/503，说明限流中间件仍覆盖 `/docs/`（或 fail-closed），需按 `contracts/doc-endpoints.md` §3 增加前缀跳过。

## 场景 5：prod 环境不暴露文档（SC-004 / FR-016）

```bash
APP_ENV=prod make dev &
sleep 2
for p in /docs/index.html /docs/ /docs/openapi.json /docs/swagger-ui.css /docs-does-not-exist; do
  printf '%-28s ' "$p"; curl -s -o /tmp/body.json -w '%{http_code} ' "http://127.0.0.1:8081$p"; \
  python3 -c "import json,sys;d=json.load(open('/tmp/body.json'));print(d['code'], d['data'])"
done
```

**期望**：五行全部为 `404 NOT_FOUND None`，即文档路径与不存在的路径响应完全同构（CT-4、CT-5）。业务接口不受影响：

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:8081/api/v1/auth/login -H 'Content-Type: application/json' -d '{}'
```

**期望**：`400`（校验失败，说明路由仍正常工作）。

## 场景 6：流水线挂载说明可执行（FR-023）

```bash
grep -nE "docs-check|/docs/" README.md
```

**期望**：`README.md` 的「开发流程规范」段落中出现三条命令（生成 / 校验 / 预览）、一行可直接粘贴到 CI 的调用示例（命令名与实际 `Makefile` 目标一致），以及一行文档入口地址（`http://127.0.0.1:8081/docs/`，仅非 prod），使文档可被发现（critique P5）。

---

## 验收对照表

| 场景 | 覆盖需求 | 覆盖成功标准 |
|---|---|---|
| 0 | US1 的前置可行性（critique X1） | SC-002 能否成立 |
| 1 | FR-011、FR-014、FR-021 | SC-001、SC-005 |
| 2 | FR-012、FR-013 | SC-003 |
| 3 | 宪法门禁、INV-2、AC-7 | — |
| 4 | FR-015、FR-019、FR-020、CT-7 | SC-002 |
| 5 | FR-016、FR-017、FR-018 | SC-004 |
| 6 | FR-023 | — |
| 场景 4 的第 1 步 | FR-001、FR-002、FR-004~FR-008、FR-022 | SC-006 |

## 清理

```bash
rm -rf .tmp            # 生成中间产物
kill %1 %2 2>/dev/null # 停止后台服务
git status --porcelain # 期望只有预期变更
```
