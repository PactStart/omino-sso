# 契约：健康探针

**Feature**: `007-deployment` | **覆盖需求**: FR-015 / SC-001

## 1. 端点

| 端点 | 端口 | 语义 | 依赖 |
|---|---|---|---|
| `GET /healthz` | `8080` 与 `8081` | 进程存活 | 无 |
| `GET /readyz` | `8080` 与 `8081` | 可接流量 | MySQL + Redis |

> 两个端口**都**提供 `/readyz`（复核 N7 决策①）：容器 healthcheck 只打 `:8081/healthz`、公网只放行 `/healthz`，但保留两个端口的依赖状态查询便于就地排查。实现共用同一层 `store.Ping` 包装，禁止两份逻辑各自演化。

- 路径在**根**，不在 `/api/v1` 下；**不套**统一响应 `{code,message,data,traceId}`。
- **不受限流**：`middleware.RateLimit` 必须对 `/healthz`、`/readyz` 短路（与 `/docs/` 同处）。否则 `RATE_LIMIT_API_PER_MIN` 调小或探活频率提高时，探针 429 → 容器 unhealthy → 重启风暴。
- 仅接受 `GET`；其他方法返回 `405`。
- 响应头带 `Cache-Control: no-store`（防止 LB/CDN 缓存 200 掩盖故障）。
- 全程不超过 1s（`/readyz` 的 ping 超时 1s，超时即判定该依赖失败）。

## 2. 响应

**`/healthz`（恒 200）**

```json
{"status":"ok"}
```

**`/readyz` 成功（200）**

```json
{"status":"ready"}
```

**`/readyz` 失败（503）**

```json
{"status":"unready","failing":["mysql"]}
```

`failing` 取值只能是 `mysql` / `redis`，**不得**回传错误原文、DSN、主机地址（宪法 IV：不泄露内部细节）。

## 3. 暴露面

| 域名 | `/healthz` | `/readyz` |
|---|:--:|:--:|
| `sso.<主域>` | 公网 200 | 公网 **404** |
| `api.<主域>` | 内网 200 | 内网 200（该域本身公网不可达） |
| `portal.<主域>` / `admin.<主域>` | 由 nginx 直接返回 200，不穿透后端 | 不提供（404） |

## 4. 容器 healthcheck

```yaml
healthcheck:
  test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8081/healthz"]
  interval: 10s
  timeout: 3s
  retries: 5
  start_period: 20s
```

- `sso` 容器打 `:8081/healthz`（业务面）；`:8080/healthz` 作为**附加**检查（`/readyz` 只用于人工/编排启动门禁）。
- `portal` / `admin` 容器打自身 `http://127.0.0.1/healthz`（nginx 返回 200），不穿透到后端。
- 基础镜像 `alpine` 自带 `wget`（busybox），不额外装 `curl`。

## 5. 文档与门禁

- **不加** swag 注解 → `internal/api/docs/openapi.json` 零变化，`make docs-check` 必须保持绿。
- 路由登记：`AGENTS.md §4.0`（端口与路径表）增加两行；`scripts/check-registry.sh` 只比对 `/api/v1*`，不会误报。
- 单测（2026-10-24 实现口径）：`internal/api/health_test.go` 固定三条隐性契约（配额=1 时连打 10 次全 200、响应不含 `code/message/data/traceId`、`/readyz` 503 且 `failing` 只含 `mysql`/`redis`）；`internal/oidc/health_test.go` 断言 `:8080` 侧同语义（含 405 与 IP 提取），防止两个 server 的探针逻辑分叉。

## 6. 必须有用例固定的三条隐性契约

| # | 契约 | 用例 |
|---|---|---|
| 1 | 不受限流 | 设 `RATE_LIMIT_API_PER_MIN=1`，连续请求 `/healthz` 10 次，全部 200（不得出现 429） |
| 2 | 不套统一响应 | 响应体能被解析为 `{"status":...}`，且**不含** `code`/`message`/`data`/`traceId` |
| 3 | 不污染契约 | `make docs-check` 绿（生成物与入库文件一致）**且** `scripts/check-registry.sh` 校验 `internal/api/docs/openapi.json` 的 sha256 与 `api-evolve/baseline/manifest.json` 一致（复核 N4：`docs-check` 本身不读 baseline，故由 `check-registry` 承载该断言） |

> 这三条最容易在后续重构中被"顺手统一"改坏（例如有人给探针套 `response.Success`、或把 `RateLimit` 改成只对 `/api` 生效）。
