# Quickstart 验证指南：图片上传与对象存储

本指南提供端到端验证场景，证明特性可用。契约与数据模型细节见 [contracts/file-upload-api.md](./contracts/file-upload-api.md) 与 [data-model.md](./data-model.md)。

## 前置条件

1. 启动基础设施：`docker compose up -d mysql redis`（以及已配置的阿里云 OSS bucket）。
2. 配置环境变量（`.env`，见 [AGENTS.md §9](../../../AGENTS.md) 与 research.md 决策 6）：
   ```text
   OSS_PROVIDER=aliyun_oss
   OSS_ENDPOINT=oss-cn-hangzhou.aliyuncs.com
   OSS_BUCKET=<bucket>
   OSS_ACCESS_KEY_ID=<ak>
   OSS_ACCESS_SECRET=<sk>
   OSS_PUBLIC_BASE_URL=https://<bucket>.oss-cn-hangzhou.aliyuncs.com
   OSS_CREDENTIAL_TTL=300
   ```
3. 启动后端与前端：`make dev`（或分别启动 `:8080`/`:8081` + portal/admin）。
4. 已有一个管理员账号与一个普通员工账号。

## 验证场景

### 场景 A：管理员上传应用 Logo（P1）

1. 管理员登录 admin，进入「应用管理」→ 新建/编辑某应用。
2. 上传组件选择一张 PNG，**未启用裁剪**（默认关闭）→ 直接上传。
3. 断言：保存后，「我的应用」列表显示该 logo，无破损；不再需要粘贴外部 URL。
4. 替换/清除 logo：断言旧 logo 立即失效，清除后回退名称首字占位。

### 场景 B：员工裁剪并上传头像（P2）

1. 员工登录 portal，进入「账号中心」。
2. 上传组件选择一张较大图片，组件启用裁剪（`enableCrop=true`）→ 拖选矩形区域 → 上传。
3. 断言：头像立即更新为裁剪后的图片，无破损；再次替换/清除即时生效。

### 场景 C：管理员查看与管理文件记录（P3 / FR-014、FR-015）

1. 管理员进入「上传文件」管理页。
2. 断言：以分页看到每次上传记录，字段含 provider/scene/format/size/url/status/creator/createdAt。
3. 对某条记录更新状态为 `disabled` / `deleted`。
4. 断言：状态更新，关联 logo/头像在展示方相应失效或回退占位；置 `deleted` 后对象不再可访问。

### 场景 D：图片失联优雅降级（P3）

1. 清除某应用 logo 或使头像对象失效。
2. 断言：门户「我的应用」与账号中心回退为名称/姓名首字占位，无破损图标。

## 后端测试命令

```bash
make test          # 单元/集成（含上传凭证签发、files 记录、状态转移）
make lint          # 静态检查
make build         # 编译
make build-web     # 前端构建
```

## 预期结果

- 直传凭证签发成功且含 `policy`/`signature`/`expiresAt`；图片字节不经服务端。
- 每次上传产生一条 `files` 记录（SC-006，100% 可追溯）。
- 不合规图片（超 5MB / 非 JPG·PNG·WebP）被拒绝且无悬挂资源（SC-004）。
- 失联场景回退占位，破损图片为 0（SC-005）。
