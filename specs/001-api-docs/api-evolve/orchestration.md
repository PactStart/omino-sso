# API Evolve · Orchestration — `001-api-docs`

**状态说明**：`after_specify`（detect）与 `after_plan`（baseline）两个 hook 在本特性执行期间均为 `optional: true` 且未被执行，故 15:07 那次门禁判定时状态目录只有本文件。**2026-10-20T15:20 已用 `/speckit.api-evolve.orchestrate` 回填全量状态**（detect → baseline → diff → classify → bump → client-impact → gate），`after_plan` 的 baseline 亦已在 `.specify/extensions.yml` 改为 `optional: false`，后续特性不再出现无基线的情况。缺省策略（项目根与特性目录均无 `policy.yml`）下的门禁判定如下。

契约面事实：本特性不修改任何既有端点的请求/响应契约，仅新增 16 个端点的人类可读文档（`internal/api/docs/openapi.json`）与 `/docs/*` 非业务路由；`/oauth2/*`、`/.well-known/*` 由 `zitadel/oidc/v3` 生成，未改动。

---

## Gate — 2026-10-20T15:07:00+08:00

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking` / `removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `diff.json`，不存在 `breaking` / `removal` 条目；本特性零契约删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 隐含 bump 为 `none`（纯增量文档，未触碰既有契约面），非 `major`，无需 `bump.json` |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活于契约 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但其弃用 `published_in_version` 为 `null` | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml` 要求迁移文档；且无 breaking/removal，无迁移面 |

Exit code: `0`

### 本次门禁执行方式

规则 1/3/4 为空集判定（无 `diff.json`、无 `deprecations.json`）；规则 2/5 依赖 `bump.json`、`policy.yml`，两者均不存在时按缺省策略取「无 major 变更」「不要求迁移文档」。逐条核验命令：

```bash
for f in detected.json diff.json classification.md policy.yml deprecations.json bump.json client-impact.md orchestration.md; do
  [ -e "specs/001-api-docs/api-evolve/$f" ] && echo "FOUND $f" || echo "absent $f"
done   # 除本文件外全部 absent
ls api-evolve 2>/dev/null   # 项目根无 api-evolve/policy.yml
```

---

## Orchestration — 2026-10-20T15:20:00+08:00

Result: **PASS**（backfill，exit 0）

| Phase | Artifact | Result |
|---|---|---|
| detect | `detected.json` | 1 个契约文件：`internal/api/docs/openapi.json`（openapi-3，16 端点 / 22 schema） |
| baseline | `baseline/manifest.json` + `baseline/internal/api/docs/openapi.json` | 冻结 sha256 `e29a35aa…`（未用 `--rebaseline`，此前无基线） |
| diff | `diff.json` | `entries: []`（工作契约与基线逐字节一致） |
| classify | `classification.md` | additive 0 / non-breaking 0 / breaking 0 / removal 0 |
| bump | `bump.json` | implied `none`，`1.0 → 1.0`，`confirmed: true` |
| client-impact | `client-impact.md` | 无 breaking，无受影响消费方 |
| gate | 本文件「Gate — 2026-10-20T15:07」 | 5 条规则全 PASS |

**为何 diff 为空**：本特性是管理 API 契约的**首次建立**而非修改 —— 16 个端点的请求/响应在实施前后未变（T009 具名化只改 Go 类型，JSON 键集经 `dto_test.go` 断言等价）；`/oauth2/*` 由协议库生成未改动；`/docs/*` 属非业务面不计入管理 API 契约。因此基线直接取交付后状态，供下一个特性作为 diff 锚点。

**已知限制**：`policy.yml` 与 `clients:` 段仍未建立。下一个出现 breaking 变更的特性必须先补 `clients:` 段，否则 `client-impact` 只能给出空表。
