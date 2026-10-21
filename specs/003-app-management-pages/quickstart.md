# Quickstart: 应用详情子页面与应用内授权体系

**Feature**: `003-app-management-pages` | 适用环境：本地 `docker compose`（MySQL `3307`、Redis `6380`）+ `make run`

本指南用于**端到端验收**，不替代 `tasks.md` 与自动化测试。所有 `curl` 均以 `BASE=http://127.0.0.1:8081/api/v1`、`ISSUER=http://127.0.0.1:8080` 为前提。

## 0. 前置条件

```bash
make up            # MySQL + Redis
make migrate-up    # 应用 migrations/0001_init + 0002_clients_meta + 0003_app_auth
make run           # :8080（OIDC）+ :8081（管理 API）
```

登录拿管理会话（后续所有管理接口都带 `-b /tmp/sso-cookie.txt`）：

```bash
BASE=http://127.0.0.1:8081/api/v1
curl -s -c /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@12345","clientId":"portal"}' \
  "$BASE/auth/login"
```

创建一个专用于验收的应用（`client_secret` 只在创建时返回一次，记下来）：

```bash
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"clientId":"demo-app","clientName":"验收应用","redirectUris":["https://demo.example.com/cb"],"scopes":["openid","profile"]}' \
  "$BASE/admin/clients"
```

## 1. 子页面导航（FR-001 / FR-002）

浏览器打开 `http://127.0.0.1:8081`（或前端 dev server），进入"应用"：

- 点击"demo-app"所在行的"详情" → **不再出现抽屉**，地址变为 `/clients/demo-app/overview`
- 左侧五项：应用详情 / 应用成员 / 应用角色 / 应用权限 / 授权管理
- 刷新页面 → 仍停在 `demo-app` 的当前菜单
- 点击菜单切换 → 地址栏 `tab` 变化，内容区随之切换

**期望**：五个菜单顺序固定，默认停在"应用详情"。

## 2. 详情编辑（FR-003 ~ FR-006）

```bash
curl -s -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app"      # 详情读取（刷新直达的数据来源）
curl -s -b /tmp/sso-cookie.txt -X PATCH -H 'Content-Type: application/json' \
  -d '{"clientName":"验收应用（改）","accessTokenTtl":1800}' \
  "$BASE/admin/clients/demo-app"
```

**期望**：`GET` 返回 `clientId`/`status`/`accessTokenTtl`/`refreshTokenTtl`/`updatedAt`/`memberCount`，**不含** `client_secret_hash`；`PATCH` 后 `clientName` 为新值、`accessTokenTtl=1800`，列表页名称同步；审计新增一条应用更新记录（`redirectUris` 变更时 detail 带 `before`/`after`）。

非法值必须被拒：

```bash
curl -s -b /tmp/sso-cookie.txt -X PATCH -H 'Content-Type: application/json' \
  -d '{"redirectUris":["ftp://x"]}' "$BASE/admin/clients/demo-app"
```

**期望**：`400 VALIDATION_FAILED`，原值不变。

## 3. 成员批量添加与移除（FR-008 ~ FR-011）

候选筛选（应不含已有成员）：

```bash
curl -s -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app/members/candidates?keyword=zhang&pageSize=10"
```

批量添加（取 2 个候选 `userId`，重复提交一次验证幂等）：

```bash
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"userIds":["U1","U2"]}' "$BASE/admin/clients/demo-app/members"
```

**期望**：首次 `{"added":2,"skipped":0}`；再次提交同一请求返回 `{"added":0,"skipped":2}`，成员列表不出现重复行。

批量上限：一次提交 501 个 `userId` → `400 VALIDATION_FAILED`（前端"全选全部"按 500 分片顺序提交并汇总计数）。按部门整批加入：`-d '{"deptId":"<部门ID>"}'` → 返回 `{"added":…,"existing":…,"disabled":…}`。

移除（先给其中一人绑角色，验证级联）：

```bash
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"userIds":["U1"]}' "$BASE/admin/clients/demo-app/members/remove"
```

**期望**：`{"removed":1,"roleBindingsRemoved":1,"notMember":0}`，该用户从成员列表与该角色成员中同时消失。

跨页全选：成员数 > 20 时，在任意页点"全选全部成员"，取消 1 人后移除 → 实际移除数 = 总数 − 1（SC-005）。

## 4. 角色 CRUD 与唯一性（FR-015 ~ FR-018）

```bash
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"name":"opsAdmin","description":"运维管理员"}' "$BASE/admin/clients/demo-app/roles"        # 201

curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"name":"opsAdmin"}' "$BASE/admin/clients/demo-app/roles"                                    # 409 APP_ROLE_NAME_EXISTS

curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"name":"ops-admin"}' "$BASE/admin/clients/demo-app/roles"                                   # 400 VALIDATION_FAILED
```

**期望**：`Admin` 与 `admin` 视为不同角色（区分大小写）；描述 101 字符被拒。

## 5. 权限树与删除保护（FR-019 ~ FR-023）

```bash
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"code":"order:read","name":"订单查询"}' "$BASE/admin/clients/demo-app/permissions"

RID=$(curl -s -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app/permissions" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["list"][0]["id"])')

curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d "{\"code\":\"order:read:detail\",\"name\":\"订单详情\",\"parentId\":\"$RID\"}" \
  "$BASE/admin/clients/demo-app/permissions"

curl -s -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/clients/demo-app/permissions/$RID"   # 409
```

**期望**：删除父权限返回 `409 APP_PERMISSION_HAS_CHILDREN`（message 含"1 个子权限"）；把 `parentId` 设为自身返回 `400 APP_PERMISSION_INVALID_PARENT`；页面上"树形/列表"切换后条数与层级一致（SC-004）。

## 6. 授权：角色 ↔ 权限 / 角色 ↔ 成员（FR-024 ~ FR-026）

```bash
# 角色绑 2 个权限（覆盖式）
PID2=$(curl -s -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app/permissions" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["list"][1]["id"])')
ROLE=$(curl -s -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app/roles" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["list"][0]["id"])')

curl -s -b /tmp/sso-cookie.txt -X PUT -H 'Content-Type: application/json' \
  -d "{\"permissionIds\":[\"$RID\",\"$PID2\"]}" \
  "$BASE/admin/clients/demo-app/roles/$ROLE/permissions"

# 角色绑成员：U3 还不是成员，应自动加入
curl -s -b /tmp/sso-cookie.txt -X PUT -H 'Content-Type: application/json' \
  -d '{"userIds":["U3"]}' "$BASE/admin/clients/demo-app/roles/$ROLE/members"
```

**期望**：第二个请求返回 `autoJoinedMembers:1`；成员列表出现 U3；角色行计数显示"2 个权限 / 1 名成员"（重复提交结果不变）。

并发编辑：在请求体里带一个过期的 `updatedAt`（例如 `1`）重复提交 → `409 APP_RESOURCE_STALE`，服务端数据不变；不带 `updatedAt` 时不校验（兼容脚本）。

## 7. 门户可见性（FR-012 / FR-013）

1. 用**非成员**员工账号登录门户 → "我的应用"**不含** demo-app。
2. 管理员把该员工加入 demo-app → 员工重新进入门户 → 出现 demo-app。
3. 移除该员工 → 再次进入 → 消失。

**期望**：可见性只由成员关系与 `status` 决定；管理员账号若未加入也看不到。

补充检查（FR-035）：用一个**不属于任何应用**的员工账号进入门户，"我的应用"必须展示空状态说明与"联系管理员开通"的引导，而不是一片空白。

## 8. userinfo 返回应用内角色与权限（FR-027 ~ FR-030、SC-003）

用 `demo-app` 走一次授权码 + PKCE（需要浏览器内已登录门户的会话 cookie）：

```bash
V=$(python3 -c 'import secrets;print(secrets.token_urlsafe(48))')
C=$(python3 -c "import hashlib,base64,sys;print(base64.urlsafe_b64encode(hashlib.sha256(sys.argv[1].encode()).digest()).rstrip(b'=').decode())" "$V")
LOC=$(curl -s -o /dev/null -w '%{redirect_url}' -b /tmp/sso-cookie.txt \
  "$ISSUER/oauth2/authorize?client_id=demo-app&response_type=code&scope=openid%20profile&redirect_uri=https%3A%2F%2Fdemo.example.com%2Fcb&code_challenge=$C&code_challenge_method=S256")
CODE=$(python3 -c "import sys,urllib.parse as u;print(u.parse_qs(u.urlparse(sys.argv[1]).query)['code'][0])" "$LOC")

TOKEN=$(curl -s -u 'demo-app:<client_secret>' -d "grant_type=authorization_code&code=$CODE&redirect_uri=https://demo.example.com/cb&code_verifier=$V" \
  "$ISSUER/oauth2/token")

curl -s -H "Authorization: Bearer $(echo "$TOKEN" | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')" \
  "$ISSUER/oauth2/userinfo"
```

**期望**：

```json
{"sub":"…","name":"…","preferred_username":"…","roles":["user"],
 "tenant":"default","dept_id":"…",
 "app_roles":["opsAdmin"],"permissions":["order:read","order:read:detail"]}
```

- `roles` 仍为平台角色（不受影响）
- 解绑角色后**立即**再次调用 → `app_roles` 与 `permissions` 均为 `[]`（不是 `null`）
- 用另一个应用（未给该用户分配角色）的令牌调用 → 两个字段均为 `[]`

## 9. 清理与门禁

```bash
curl -s -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/clients/demo-app"
rm -f /tmp/sso-cookie.txt
```

交付前必须全绿：

```bash
make build            # 含 docs-check（26 path / 36 操作）
make test             # 含 router/docs 计数断言
make lint
make build-web        # 前端构建与类型检查
TEST_MYSQL_DSN='root:omino_dev_pwd@tcp(127.0.0.1:3307)/omino_sso_test?parseTime=true&multiStatements=true' \
  go test ./internal/service/ -run 'TestAppAuth'
```

**预期结果**：全部通过；`git grep -n "h\.st\." internal/api/admin/` 无新增命中（分层约束）；`migrations/` 只新增 `0003_app_auth.*` 两个文件。

**协议一致性（`AGENTS.md` §8.5，不可跳过）**：本特性改了 `/oauth2/userinfo` 的 claim 输出，发布前必须跑：

```bash
docker run --rm -p 8080:8080 -e CONFORMANCE_SERVER=http://host.docker.internal:8080 \
  openidentity/conformance-suite
# UI 选 basic OP + config OP profile，全部用例通过；把镜像版本与通过数写入 commit message
```

## 10. 上线前检查与回滚（运维）

### 10.1 发布前：确认没有"零成员"的启用应用

```bash
curl -s -b /tmp/sso-cookie.txt "$BASE/admin/clients?hasMembers=false&pageSize=50" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];print(len(d["list"]),"个启用应用无成员")'
```

**期望**：`0 个`（或剩余项都有意为之，如尚未开放的新应用）。每个条目都要在成员页用"按部门加入"完成补员；这一步与成员制语义切换（`ListForUser`）必须在**同一次发布**内，否则会出现"应用对全员消失、管理员无补救入口"的窗口（FR-013、FR-032、SC-007）。

### 10.2 回滚顺序（顺序颠倒会丢配置）

1. **先回退二进制**到不读新表的上一版本：`client_members` / `client_roles` / `client_permissions` / 两类绑定 / `access_tokens` 全部保留。此时门户恢复"全员可见"，属预期降级。
2. 观察稳定 **≥1 个发布周期**。
3. 确认不再回滚后，才可执行 `migrations/0003_app_auth.down.sql`（或 `make migrate-down`）——它按逆序 `DROP TABLE`，**不可逆地删除全部成员、角色、权限与绑定配置**。

> **禁止**在回退二进制之前执行 down：那会先丢掉全部配置，而旧二进制根本没有读过这些表，纯属多余损失。

## 11. 性能核对（SC-002 / SC-004）

### 11.1 造数（仅测试库）

在 `internal/service/app_auth_integration_test.go` 增加一个 `TEST_MYSQL_DSN` 门控的夹具用例 `TestAppAuthPerfFixture`：写入 10,000 个用户（`perf00001`…）、把其中 500 人加入 `demo-app`、为该应用建 200 个权限节点（含 3 层父子）。用 `PERF_FIXTURE=1` 显式开启，默认 `t.Skip`，不进入常规 `go test ./...`。

```bash
PERF_FIXTURE=1 \
TEST_MYSQL_DSN='root:omino_dev_pwd@tcp(127.0.0.1:3307)/omino_sso_test?parseTime=true&multiStatements=true' \
  go test ./internal/service/ -run TestAppAuthPerfFixture -v
```

### 11.2 计时与执行计划

```bash
time curl -s -o /dev/null -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app/members?pageSize=50"
time curl -s -o /dev/null -b /tmp/sso-cookie.txt "$BASE/admin/clients/demo-app/members/candidates?keyword=perf&pageSize=50"
time curl -s -o /dev/null -b /tmp/sso-cookie.txt "$BASE/admin/clients?hasMembers=false&pageSize=50"
```

**期望**：三者均 ≤ 1s（SC-002）。再对成员列表 / 候选 / `memberCount` 三条 SQL 跑 `EXPLAIN`：`memberCount` 必须走索引（无 `Using filesort` / `Using temporary`）；成员列表与候选关键字因跨表排序列 / `display_name` 前导通配符而允许文件排序（规模有界，实测 10k 用户分别 18ms / 242ms），真实门禁以 SC-002 为准。

### 11.3 权限树（SC-004）

用 11.1 造的 200 个节点：在"应用权限"页切树/列表各 5 次，并执行"展开全部/收起全部"，每次耗时 ≤ 1s 且节点无错位。
