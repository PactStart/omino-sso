# API Evolve · Orchestration — `005-verification-code-auth`

**状态说明**：本特性在 `after_specify`（detect）与 `after_plan`（baseline）阶段**未产出** `detected.json` / `baseline/`（特性目录无 `api-evolve/` 目录，`specs/005-verification-code-auth/` 下仅 `checklists/`、`contracts/`、`critiques/` 与 4 份 md）。缺省策略（项目根与特性目录均无 `policy.yml`）下的门禁判定如下。

契约面事实：本特性为**纯 additive**——新增 `/api/v1/auth/code`、`/auth/captcha`、`/auth/password/reset`、`/me/phone`、`/me/email`、`/me/password/reset`、`/me/mfa/{bind,bind/confirm,unbind}`、`/admin/send-records`；扩展 `/auth/login` 的 `loginType`（新增 `email_code`/`sms_code` 取值，既有 `password` 语义不变）；新增错误码 `AUTH_CODE_INVALID`、`VERIFY_TARGET_IN_USE`、`VERIFY_SEND_FAILED`、`VERIFY_TARGET_NOT_FOUND`、`MFA_ALREADY_BOUND`、`MFA_NOT_BOUND`、`AUTH_CAPTCHA_INVALID`、`AUTH_MFA_TICKET_EXPIRED`。未删除或收窄任何既有端点/字段，故无 `breaking` / `removal`。

---

## Gate — 2026-10-22

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking — `breaking`/`removal` 无对应 `deprecations.json` 行 | 通过（空集） | 无 `diff.json`，不存在 `breaking`/`removal` 条目；本特性零契约删除/收窄 |
| 2 | Bump confirmed — 隐含 bump 为 `major` 且 `bump.json.confirmed = false` | 通过（不适用） | 无 `bump.json`，隐含 bump 为 `minor`（纯 additive），无需确认 |
| 3 | Sunset honored — `published` 且已过 `sunset_at` 的弃用仍存活于契约 | 通过（空集） | 无 `deprecations.json` |
| 4 | Removal preceded by publish — 有 `removal` 但其弃用 `published_in_version` 为 `null` | 通过（空集） | 无 `removal` 条目 |
| 5 | Migration doc present — 策略要求 `migration_doc` 但文件缺失 | 通过（不适用） | 无 `policy.yml`；无 breaking/removal，无迁移面 |

Exit code: `0`

### 本次门禁执行方式

逐条核验状态文件（全部 absent），规则 1/3/4 为空集判定；规则 2/5 依赖 `bump.json`、`policy.yml`，两者不存在时按缺省策略取「非 major 变更」「不要求迁移文档」。

```bash
for f in detected.json diff.json classification.md policy.yml deprecations.json bump.json client-impact.md orchestration.md; do
  [ -e "specs/005-verification-code-auth/api-evolve/$f" ] && echo "FOUND $f" || echo "absent $f"
done   # 全部 absent（本次门禁执行时才创建本文件）
```

### 备注

- `/auth/code` 的 `login` 场景在 2026-10-22 由「统一成功响应（防枚举）」改为返回 `404 VERIFY_TARGET_NOT_FOUND`（见 `spec.md` 边缘用例与 `contracts/verification-api.md`）。该端点本特性首次引入、尚未发布，**不构成对外 breaking**，但属行为约定变更，已在 spec/tasks（T060）中显式记录。
- 前端契约文件（`web/portal`、`web/admin` 的 API 客户端）随特性同步新增，无跨版本客户端兼容负担。

---

## Gate — 2026-10-22（Phase 13 implement 前二次执行）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking | 通过（空集） | 无 `diff.json`/`deprecations.json`；本轮仅新增熔断相关配置与内部逻辑，无端点增删 |
| 2 | Bump confirmed | 通过（不适用） | 无 `bump.json`，无 major 隐含变更 |
| 3 | Sunset honored | 通过（空集） | 无弃用条目 |
| 4 | Removal preceded by publish | 通过（空集） | 无 `removal` |
| 5 | Migration doc present | 通过（不适用） | 无 `policy.yml`，无 breaking/removal |

Exit code: `0`

备注：`internal/api/docs/openapi.json` 处于未提交修改状态（上一轮 404 `VERIFY_TARGET_NOT_FOUND` 的注解产物），`make docs-check` 已确认与源码一致，属 additive 变更。

---

## Gate — 2026-10-22（Phase 14 implement 前）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking | 通过（空集） | 无 `diff.json` / `deprecations.json`；本轮改动为：新增业务集成测试、`notification.Service.Policy()`、`/auth/code` 静默分支读取配置值、契约与 AGENTS 文档口径对齐。无端点增删、无请求/响应字段变更。 |
| 2 | Bump confirmed | 通过（不适用） | 无 `bump.json`，无 major 隐含变更。 |
| 3 | Sunset honored | 通过（空集） | 无弃用条目。 |
| 4 | Removal preceded by publish | 通过（空集） | 无 `removal`。 |
| 5 | Migration doc present | 通过（不适用） | 无 `policy.yml`，无 breaking/removal。 |

Exit code: `0`

备注：`VERIFY_TARGET_NOT_FOUND` 的 404 行为属本特性首次引入、未发布的端点，`contracts/verification-api.md` 本轮仅补全 `target` 必填性与图形码口径，未改变报文结构，不构成对外 breaking。

---

## Gate — 2026-10-22（Phase 15 implement 前）

Result: **PASS**

| # | Rule | Verdict | Evidence |
|---|---|---|---|
| 1 | Declared breaking | 通过（空集） | 无 `diff.json` / `deprecations.json`；本轮仅修订 `quickstart.md` 的迁移命令、配置清单与校验命令，不含端点/报文变更 |
| 2 | Bump confirmed | 通过（不适用） | 无 `bump.json`，无 major 隐含变更 |
| 3 | Sunset honored | 通过（空集） | 无弃用条目 |
| 4 | Removal preceded by publish | 通过（空集） | 无 `removal` |
| 5 | Migration doc present | 通过（不适用） | 无 `policy.yml`，无 breaking/removal |

Exit code: `0`
