# Phase 1 Quickstart: 组织架构增删改的端到端验证

**Date**: 2026-10-21 | **Spec**: [spec.md](./spec.md) | **Contract**: [contracts/admin-orgs-api.md](./contracts/admin-orgs-api.md)

目标是让评审者不读源码，也能在 10 分钟内验证本特性的全部验收场景。所有命令在仓库根目录执行。

---

## 0. 前置条件

```bash
docker compose up -d mysql redis          # MySQL 127.0.0.1:3307 / Redis 127.0.0.1:6380
cp .env.example .env                      # 已有 .env 则跳过
make keys                                 # 生成 configs/dev-signing-key.pem
make migrate-up                           # 首次由 compose 自动建表，若已初始化可跳过
make docs                                 # 重新生成 openapi.json（新增 3 个端点后必须执行）
make dev                                  # 启动 :8080 与 :8081
```

- 管理员账号来自 `.env`：`ADMIN_BOOTSTRAP_USERNAME=admin` / `ADMIN_BOOTSTRAP_PASSWORD=Admin@12345`（仅在 `users` 为空时创建）。
- 种子数据：`总公司`（根）→ `技术中心` → `平台组`；`admin` 归属 `总公司`，`zhangsan` / `lisi` 归属 `平台组`。

先登录并保存会话：

```bash
curl -s -c /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@12345","clientId":"portal"}' \
  http://127.0.0.1:8081/api/v1/auth/login
```

期望：`code="OK"`，响应头含 `Set-Cookie: sid=...; HttpOnly`。后续请求统一带 `-b /tmp/sso-cookie.txt`。

辅助命令（取组织树快照，用于对比"失败后数据零变化"）：

```bash
BASE=http://127.0.0.1:8081/api/v1
TREE() { curl -s -b /tmp/sso-cookie.txt "$BASE/admin/orgs/tree"; }
MINE() { TREE | python3 -c 'import json,sys;d=json.load(sys.stdin)["data"];[print(x["id"],repr(x["parentId"]),x["name"],x["path"],x["sort"],x["userCount"]) for x in d]'; }
```

---

## 1. 创建根级部门（US1 / FR-001、FR-005）

```bash
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d '{"name":"  技术中心二期  ","sort":5}' "$BASE/admin/orgs"
```

期望：HTTP `201`；`data.name == "技术中心二期"`（首尾空白被去除）；`parentId == null`；`path` 为 `/新ID/`；`userCount == 0`；随后 `TREE` 中出现该根节点且排在 `sort` 更大的节点之后。

## 2. 创建子部门（US1 / FR-002）

```bash
PARENT=$(curl -s -b /tmp/sso-cookie.txt "$BASE/admin/orgs/tree" | python3 -c 'import json,sys;print([n["id"] for n in json.load(sys.stdin)["data"] if n["name"]=="技术中心二期"][0])')
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d "{\"parentId\":\"$PARENT\",\"name\":\"平台工程组\"}" "$BASE/admin/orgs"
```

期望：`201`；`data.parentId == $PARENT`；`path` 以父路径为前缀并以自身 ID 结尾（`/父ID/子ID/`）。

## 3. 同级重名被拒（FR-003 / SC-002）

```bash
BEFORE=$(MINE)
curl -s -o /tmp/resp.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt \
  -H 'Content-Type: application/json' -d "{\"parentId\":\"$PARENT\",\"name\":\"平台工程组\"}" \
  "$BASE/admin/orgs"
cat /tmp/resp.json; AFTER=$(MINE); [ "$BEFORE" = "$AFTER" ] && echo "零写入 OK"
```

期望：`409` + `code=ORG_NAME_ALREADY_EXISTS`；前后快照完全一致。
另验证大小写敏感：用 `平台工程组` 的 ASCII 变体（如 `Demo` 与 `demo`）创建两次，第二次应成功。

## 4. 名称非法被拒（FR-004）

依次提交 `{"name":""}`、`{"name":"   "}`、`{"name":"<65 个字符>"}`、含 `\u0007` 的名称。
期望：全部 `400 VALIDATION_FAILED`，且快照不变。

## 5. 重命名与调整排序（US2 / FR-006）

```bash
NODE=$(取"平台工程组"的 id)
curl -s -b /tmp/sso-cookie.txt -X PATCH -H 'Content-Type: application/json' \
  -d '{"name":"平台工程一组","sort":9}' "$BASE/admin/orgs/$NODE"
```

期望：`200`；`name` 与 `sort` 更新；`parentId`、`path`、`userCount` 不变。

空 body 幂等性检查（E27 / 契约 §2）：

```bash
curl -s -o /tmp/noop.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt -X PATCH \
  -H 'Content-Type: application/json' -d '{}' "$BASE/admin/orgs/$NODE"
```

期望：`200` 且 `data` 与修改后节点一致；`organizations` 无 UPDATE、审计不新增记录。

## 6. 移动部门（US2 / FR-007、FR-008）

```bash
DEST=$(取"技术中心"的 id)   # 与当前父"技术中心二期"平级的另一根节点或其子节点
curl -s -b /tmp/sso-cookie.txt -X PATCH -H 'Content-Type: application/json' \
  -d "{\"parentId\":\"$DEST\"}" "$BASE/admin/orgs/$NODE"
```

期望：`200`；该节点及全部后代的 `path` 都以新父路径为前缀；`sort` 保持 9（移动不隐式改写）；用户归属与人数不变。

移回根级：`-d '{"parentId":null}'` → 该节点变成根节点，`path` 变为 `/自身ID/`。

## 7. 成环被拒（FR-008）

对"技术中心二期"下的某个节点 A，把它移动到自己的子节点 B 下：

```bash
curl -s -o /tmp/resp.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt -X PATCH \
  -H 'Content-Type: application/json' -d "{\"parentId\":\"$B\"}" "$BASE/admin/orgs/$A"
```

期望：`400 ORG_INVALID_PARENT`；`TREE` 结构与请求前逐字节一致。把 A 的父设为 A 自身同样返回该码。

## 8. 层级上限 10 级（FR-019）

从根开始连续创建 9 层（第 10 层成功），再尝试创建第 11 层：

期望：第 10 层 `201`；第 11 层 `400 VALIDATION_FAILED`，`message == "层级不能超过 10 级"`；把一棵深度 3 的子树移入第 8 层的部门下同样被拒（8 + 3 > 10），移到第 7 层则成功。

## 9. 并发同名创建（Edge Case / C-3）

```bash
for i in 1 2 3 4 5; do
  curl -s -o /tmp/c$i.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt \
    -H 'Content-Type: application/json' -d "{\"parentId\":\"$PARENT\",\"name\":\"并发部\"}" \
    "$BASE/admin/orgs" &
done; wait
cat /tmp/c*.json | grep -o 'ORG_NAME_ALREADY_EXISTS' | wc -l
```

期望：5 个请求中恰好 1 个 `201`，其余 4 个 `409 ORG_NAME_ALREADY_EXISTS`；树中"并发部"只有 1 个。

## 10. 删除保护（US3 / FR-010）

```bash
curl -s -o /tmp/r1.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/orgs/$技术中心二期"   # 有子部门
curl -s -o /tmp/r2.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/orgs/$平台组"        # 有账号
curl -s -o /tmp/r3.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/orgs/$并发部"        # 空叶子
```

期望：依次 `409 ORG_HAS_CHILDREN`、`409 ORG_HAS_USERS`、`200` 且 `data:null`；第三次之后树中不再有该节点；再次删除同一 ID 返回 `404 NOT_FOUND`。

## 11. 鉴权（FR-013 / C-1）

不带 cookie 调用三个写端点：

```bash
# POST 的目标是集合路径 /admin/orgs（无 :id），PATCH/DELETE 才带 :id；
# 用 /admin/orgs/x 调 POST 会落到 NoRoute 返回 404，属错误用法。
for m in POST PATCH DELETE; do
  target="$BASE/admin/orgs"; [ "$m" != "POST" ] && target="$BASE/admin/orgs/x"
  curl -s -o /tmp/a.json -w "$m %{http_code} " -X $m -H 'Content-Type: application/json' -d '{}' "$target"
  python3 -c 'import json;print(json.load(open("/tmp/a.json"))["code"])'
done
```

期望：全部 `401 UNAUTHENTICATED`（无 cookie 时 Auth 中间件在触达业务前返回）；用普通用户 `zhangsan` 的会话调用得到 `403 FORBIDDEN`。

## 12. 审计留痕（FR-014）

```bash
curl -s -b /tmp/sso-cookie.txt "$BASE/admin/audit-logs?eventType=org_created&pageSize=5"
curl -s -b /tmp/sso-cookie.txt "$BASE/admin/audit-logs?eventType=org_deleted&result=failure&pageSize=5"
```

期望：成功创建/修改/删除各有一条 `success` 记录，`detail` 含 `orgId`；删除被拒（有子部门/有账号）的记录 `result=failure` 且 `detail.message` 可读；401/403 与名称格式错误**不产生**审计记录。

审计边界的两个关键断言（E24，`400` 内部两种处理相反）：

```bash
# 层级超限（400 VALIDATION_FAILED）→ 必须留痕；DEPTH10_ID 取场景 8 中第 10 层的节点 id
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' -d "{\"parentId\":\"$DEPTH10_ID\",\"name\":\"第11层\"}" "$BASE/admin/orgs" >/dev/null
curl -s -b /tmp/sso-cookie.txt "$BASE/admin/audit-logs?eventType=org_created&result=failure&pageSize=1"
# 名称非法（400 VALIDATION_FAILED）→ 不得留痕
BEFORE=$(curl -s -b /tmp/sso-cookie.txt "$BASE/admin/audit-logs?pageSize=1" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["total"])')
curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' -d '{"name":"   "}' "$BASE/admin/orgs" >/dev/null
AFTER=$(curl -s -b /tmp/sso-cookie.txt "$BASE/admin/audit-logs?pageSize=1" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["total"])')
[ "$BEFORE" = "$AFTER" ] && echo "名称非法未留痕 OK"
```

期望：层级超限有一条 `org_created` + `failure`；名称非法前后审计总数不变。

浏览器侧（审计日志页，FR-014 的"可筛选"）：

1. 事件下拉中出现"创建组织 / 更新组织 / 删除组织"三个中文选项（而非 `org_created` 等原始键）。
2. 选"删除组织"+ 结果"失败" → 列表只剩删除被拒的记录，事件列显示中文标签。

## 13. 管理后台界面（US4 / FR-017）

```bash
cd web/admin && npm run dev     # 打开终端提示的地址，用 admin 登录
```

在"用户管理 → 组织架构"面板依次验证：

1. 面板顶部的"新增根部门"始终可见（含空树、未选中节点时）；点击后输入名称保存 → 树自动刷新，新根节点出现。
2. 任一节点右侧操作区可"新增子部门 / 重命名 / 移动到… / 删除"；节点深度达到 10 时"新增子部门"被禁用。
3. "移动到…"的候选列表中**看不到**该节点自身、其任何后代，以及会使层级超过 10 级的目标（置灰并给出原因）；选择一个新上级（或"根级"）并确认 → 该节点及其子部门出现在新位置，排序值保持不变。
4. 删除含账号的部门 → 弹出确认后提示"部门下仍有 N 个账号，请先迁移"，右侧列表自动按该部门筛选，节点仍在。
5. 删除当前选中部门 → 选中态回到"全部用户"，右侧列表同步刷新。
6. 节点数超过 50 时出现关键字过滤；树默认展开 3 层，搜索命中的路径自动展开到该节点。
7. 后端返回 409/400 时，提示文案与错误码对应，不出现堆栈或英文原始错误；操作按钮有可读的 `title`/`aria-label`。

## 14. 并发：删除部门 vs 创建账号（C-8 / 边界用例）

先建一个空部门 X（拿到 `X_ID`），然后同时发起删除 X 与创建一个 `deptId = X_ID` 的账号。用户名带时间戳，保证可重复执行：

```bash
RACE_USER="raceuser$(date +%s)"
curl -s -o /tmp/del.json -w 'DELETE %{http_code}\n' -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/orgs/$X_ID" &
curl -s -o /tmp/new.json -w 'CREATE %{http_code}\n' -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
  -d "{\"username\":\"$RACE_USER\",\"displayName\":\"并发用户\",\"password\":\"Race@12345\",\"deptId\":\"$X_ID\"}" \
  "$BASE/admin/users" &
wait
```

期望：两个请求中**恰好一个成功**——
- 删除成功 + 创建失败，且失败原因必须是 `400 VALIDATION_FAILED`（message 含"部门不存在"），**不能是 `USERNAME_ALREADY_EXISTS` 之类的无关原因**；或
- 创建成功 + 删除失败（`409 ORG_HAS_USERS`）。

随后用 MySQL 校验不存在悬空引用：

```bash
mysql -h127.0.0.1 -P3307 -uroot -pomino_dev_pwd omino_sso \
  -e "SELECT COUNT(*) AS dangling FROM users u LEFT JOIN organizations o ON o.id=u.dept_id WHERE o.id IS NULL;"
```

期望：`dangling = 0`。

顺带验证既有端点的行为修正（`PATCH /admin/users/:id` 不再静默写入不存在的部门）：

```bash
UID_=$(取任一用户 id)
curl -s -o /tmp/upd.json -w '%{http_code}\n' -b /tmp/sso-cookie.txt -X PATCH \
  -H 'Content-Type: application/json' -d '{"deptId":"01J00000000000000000000000"}' \
  "$BASE/admin/users/$UID_"
```

期望：`400 VALIDATION_FAILED`，该用户的 `dept_id` 保持原值。

---

## 15. 回归与门禁

```bash
make docs-check     # 文档与注解一致（新增端点必须已 make docs）
make test           # 含 TestDocsPathsMatchRouterRoutes / TestAnnotationErrorCodesAreRegistered / 组织路由断言
make build          # 内部先跑 docs-check，再编译
make lint
make build-web

# DB 行为集成测试（可选，默认 make test 会跳过）
# 首次运行前建库；测试自身会执行 migrations/0001_init.up.sql 建表（禁止 AutoMigrate）
# 每个用例前 TRUNCATE organizations / users / audit_events：迁移只有 CREATE TABLE IF NOT EXISTS，无 DROP，不清理会残留上次数据
mysql -h127.0.0.1 -P3307 -uroot -pomino_dev_pwd -e "CREATE DATABASE IF NOT EXISTS omino_sso_test"
TEST_MYSQL_DSN='root:omino_dev_pwd@tcp(127.0.0.1:3307)/omino_sso_test?parseTime=false&loc=UTC' \
  go test ./internal/service/ -run 'TestOrg' -v
```

期望：全部通过；`openapi.json` 为 16 条 path / **19 个操作**（两处 `ops != 16` 断言已同步改为 19）；`GET /docs/`（非 prod）中"组织架构"标签下可见 4 个组织端点，且 Try it out 可直接调用。

### 15.1 规模与响应时间（SC-003 / SC-004）

构造 10 个根部门 × 50 个子部门 = 500 个节点（全部走接口，不写库、不新增种子文件）：

```bash
BASE=http://127.0.0.1:8081/api/v1
for r in $(seq 1 10); do
  ROOT=$(curl -s -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
    -d "{\"name\":\"压测根$r\"}" "$BASE/admin/orgs" | python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["id"])')
  for c in $(seq 1 50); do
    curl -s -o /dev/null -b /tmp/sso-cookie.txt -H 'Content-Type: application/json' \
      -d "{\"parentId\":\"$ROOT\",\"name\":\"子部门$c\"}" "$BASE/admin/orgs"
  done
done

# 树查询（500 节点 / 单节点 50 直接子部门）
curl -s -o /dev/null -w 'tree %{time_total}s\n' -b /tmp/sso-cookie.txt "$BASE/admin/orgs/tree"
# 单次写操作
curl -s -o /dev/null -w 'create %{time_total}s\n' -b /tmp/sso-cookie.txt \
  -H 'Content-Type: application/json' -d '{"name":"计时部门"}' "$BASE/admin/orgs"
```

期望：两条 `time_total` 均 ≤ 1.0s（SC-004）；创建成功后**无需手动刷新**，下一次树查询即包含"计时部门"（SC-003）。

验收后按 §16 自底向上清理由本节产生的 501 个节点（先删子部门，再删根部门）。

## 16. 清理与回滚

```bash
curl -s -b /tmp/sso-cookie.txt -X DELETE "$BASE/admin/orgs/<本次创建的节点>"   # 自底向上删除
rm -f /tmp/sso-cookie.txt /tmp/resp.json /tmp/r*.json /tmp/c*.json /tmp/a.json
```

保留 `总公司 / 技术中心 / 平台组` 与既有账号归属，避免影响演示数据。

**回滚**：本特性无迁移、无配置项，回退方式即回退二进制/前端产物。新增端点消失后旧前端不会调用它们；已创建的组织数据仍可被旧版本读取（`organizations` 结构与索引未变），无需数据修复。
