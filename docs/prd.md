# omino-sso 前端产品需求文档（PRD）

| 项 | 内容 |
|---|---|
| 文档版本 | v1.0 |
| 日期 | 2026-10-20 |
| 对应后端 | 后端 MVP 已实现并自测通过（协议层 `:8080`，管理 API `:8081`） |
| 依据 | [`AGENTS.md`](../AGENTS.md)（规范）、[`tech-stack-report.md`](./tech-stack-report.md)（决策） |
| 适用范围 | `web/portal`（登录 + 账号中心）、`web/admin`（管理后台） |
| 读者 | 前端开发；后端接口以本文第 7 章为准，无需再问后端 |

---

## 1. 产品定位

企业内部统一身份平台的前端部分，服务三类人：

| 角色 | 判定 | 能做什么 |
|---|---|---|
| 普通员工 | `roles` 含 `user` | 登录、查看/维护个人信息、改密、管理自己的登录设备、查看可访问应用 |
| 管理员 | `roles` 含 `admin` | 上述全部 + 用户管理、应用注册、审计查询 |
| 未登录访客 | 无有效会话 | 仅能访问登录页 |

**前端不接触任何令牌**。浏览器只持有一个 `HttpOnly` 的 `sid` cookie，由 BFF 负责换取/保管 OIDC token。前端所有请求同源、带 cookie，**不得**把任何 token 写入 `localStorage`。

---

## 2. 页面清单与路由

两个独立构建产物，同一域名下按路径分流：

```
https://sso.example.com/          →  web/portal  （Tailwind）
https://sso.example.com/admin/    →  web/admin   （Element Plus）
```

| # | 页面 | URL 路径 | 所属产物 | 权限 | 用途 |
|---|---|---|---|---|---|
| P1 | 登录页 | `/login` | portal | 公开 | 账号密码登录 + MFA 二步验证 |
| P2 | 个人信息 | `/account/profile` | portal | 登录 | 查看本人资料、部门、角色、最近登录 |
| P3 | 我的应用 | `/account/apps` | portal | 登录 | 展示可访问系统，点击跳转 |
| P4 | 登录设备 | `/account/sessions` | portal | 登录 | 查看并撤销自己的会话 |
| P5 | 修改密码 | `/account/password` | portal | 登录 | 修改本人密码（成功后强制重登） |
| A1 | 用户管理 | `/admin/users` | admin | admin | 左组织树 + 右用户表，新建/编辑用户 |
| A2 | 应用管理 | `/admin/clients` | admin | admin | 应用列表、注册应用、轮换密钥 |
| A3 | 审计日志 | `/admin/audit` | admin | admin | 全量认证事件检索 |
| X1 | 无权限 | `/403` | 两者共用 | 公开 | 非管理员访问 admin 时的兜底 |
| X2 | 不存在 | `/404` | 两者共用 | 公开 | 路由兜底 |

**默认重定向**

| 访问 | 行为 |
|---|---|
| `/` | 已登录且 `roles` 含 `admin` → `/admin/users`；否则 `/account/profile`；未登录 → `/login` |
| `/account` | → `/account/profile` |
| `/admin`、`/admin/` | → `/admin/users` |
| 任何受保护页面 + 未登录 | → `/login?next=<encodeURIComponent(原路径)>` |

---

## 3. 全局约定

### 3.1 认证态

- 会话由后端 `sid` cookie 承载，前端**不解析、不存储、不刷新**。
- 每个页面初始化时调用 `GET /api/v1/me` 作为"会话探测 + 用户信息"的唯一来源；不要另建本地用户缓存。
- 任何接口返回 `401`：清空前端内存态（Pinia），跳 `/login?next=当前路径`，并 toast「登录状态已失效，请重新登录」。
- 任何接口返回 `403 FORBIDDEN`：跳 `/403`。

### 3.2 统一响应格式

所有 `/api/v1/**` 响应：

```json
{ "code": "OK", "message": "success", "data": { }, "traceId": "01J..." }
```

- `code === "OK"` 即成功；失败时 `data` 恒为 `null`，`code` 为大写下划线错误码。
- **不要**用 HTTP 状态码推断业务含义时忽略 `code`；两者同时判断：`res.ok && code === "OK"`。
- 页面错误提示文案优先展示后端 `message`（已是中文），前端只对少数错误码做自定义文案（见 3.4）。

### 3.3 分页约定

请求：`page`（从 1 开始，默认 1）、`pageSize`（默认 20，最大 100，超过报 `VALIDATION_FAILED`）。
响应 `data`：`{ "list": [], "total": 0, "page": 1, "pageSize": 20 }`。
列表为空时展示空态文案，不得隐藏分页器（total=0 时页码禁用）。

### 3.4 错误码 → UI 处理

| code | HTTP | 前端行为 |
|---|---|---|
| `VALIDATION_FAILED` | 400 | 表单字段级报错（能定位字段时），否则顶部 toast |
| `UNAUTHENTICATED` | 401 | 跳登录（3.1） |
| `AUTH_INVALID_CREDENTIALS` | 401 | 登录表单下方红色提示「用户名或密码错误」 |
| `AUTH_CAPTCHA_INVALID` | 400 | 表单提示「验证码错误」（本阶段无验证码接口，不会出现，见 §8） |
| `AUTH_MFA_INVALID` | 400 | MFA 输入框下方提示「动态验证码错误」，保留输入框可重试 |
| `AUTH_MFA_TICKET_EXPIRED` | 400 | 回到第一步（账号密码），提示「验证已超时，请重新登录」 |
| `AUTH_ACCOUNT_LOCKED` | 423 | 提示「账号已锁定，请 15 分钟后再试或联系管理员」 |
| `AUTH_ACCOUNT_DISABLED` | 403 | 提示「账号已停用，请联系管理员」 |
| `FORBIDDEN` | 403 | 跳 `/403` |
| `NOT_FOUND` | 404 | 列表场景 toast；页面场景跳 `/404` |
| `USERNAME_ALREADY_EXISTS` | 409 | 用户名字段报错「用户名已存在」 |
| `EMAIL_ALREADY_EXISTS` | 409 | 邮箱字段报错「邮箱已存在」 |
| `WEAK_PASSWORD` | 400 | 密码字段报错「密码需 8–128 位，且同时包含字母与数字」 |
| `RATE_LIMITED` | 429 | toast「操作过于频繁，请稍后再试」 |
| `INTERNAL_ERROR` | 500 | toast「服务异常，请稍后重试」+ 控制台输出 `traceId` |
| `DEPENDENCY_UNAVAILABLE` | 503 | 全屏错误页「服务暂不可用」+ 重试按钮 |

### 3.5 展示规范

- 时间：后端返回 RFC3339 字符串（UTC），前端统一转本地时区，格式 `YYYY-MM-DD HH:mm`；`null` 显示 `-`。
- 布尔：`true/false` 显示 `是/否` 或开关，不显示 `true`。
- 敏感值（`clientSecret`）：只在创建/轮换后的一次性弹窗中展示，关闭即销毁，不写日志、不进 URL、不进 Pinia 持久化。
- 语言：简体中文，不提供切换。

---

## 4. 页面详述

### P1 登录页 `/login`

**用途**：门户与后台的统一入口；支持 MFA 二步。
**注意（2026-10-25，009 改写）**：本页是**所有未登录 SSO 的第一落点**。接入方发起授权时，IdP 把未登录浏览器 302 到 `PORTAL_LOGIN_URL`（即本页），并带 `next=<IdP>/login?id=…&portal=1&app=<应用名>`；用户登录成功后整页跳回该地址完成授权，再由 IdP 302 回接入方。协议层 `:8080/login` 的内置 HTML 页**降级保留**（未配置门户地址的 dev/test、或循环防护触发时使用），前端不重写它。

**副标题（SSO 场景）**：`next` 指向 IdP 登录域时显示「登录后返回 <应用名>」（取 `app`；缺失/超长/含控制字符则回落「所访问的应用」），**不得**显示 `/login` 这类技术路径。

**布局**：居中卡片（宽 340–400px），左侧品牌区（大屏 ≥1024px 时显示，含产品名「统一身份认证」与一句话说明），右侧表单。

**核心元素**

| 元素 | 类型 | 文案/占位 | 规则 |
|---|---|---|---|
| 页面标题 | h1 | 统一身份认证 | 固定 |
| 副标题 | p | 登录后返回 {来源系统名或"统一门户"} | 由 `?next` 推断，无法推断时固定「统一门户」 |
| 用户名 | input | 请输入用户名 | 必填，trim 后 1–64 字符 |
| 密码 | input[type=password] | 请输入密码 | 必填，8–128；右侧「显示/隐藏」图标 |
| 记住我 | checkbox | 7 天内免登录 | 默认不勾选 |
| 主按钮 | button | 登录 | 提交中显示 loading + 禁用 |
| 错误提示区 | 表单顶部 | — | 红色背景条，展示后端 `message` |
| MFA 标题 | h1 | 两步验证 | 第二步替换标题 |
| MFA 说明 | p | 请输入身份验证器中的 6 位动态码 | — |
| MFA 输入 | input | 6 位数字 | 必填，纯数字，maxlength=6 |
| MFA 主按钮 | button | 验证 | loading + 禁用 |
| 返回上一步 | 文字按钮 | 返回 | 清空 ticket，回到账号密码步骤（**不调接口**，ticket 5 分钟后自然失效） |

**数据来源**

| 动作 | 接口 | 关键字段 |
|---|---|---|
| 第一步登录 | `POST /api/v1/auth/login` | 见 §7.1 |
| 第二步验证 | `POST /api/v1/auth/mfa/verify` | 见 §7.2 |

**交互流程**

1. 用户填用户名 + 密码（可选勾选「记住我」）→ 点「登录」。
2. 前端做基础校验，不通过则字段报错，不发请求。
3. 发 `POST /api/v1/auth/login`，body：
   `{ "username": "...", "password": "...", "clientId": "portal", "rememberMe": false }`
   （`captchaId`/`captchaCode` 本阶段不传。）
4. 响应 `data.mfaRequired === false`（同时存在 `redirectTo` 与 `user`）：
   - 会话 cookie 已由后端下发，前端无需处理；
   - 若 URL 有 `next`，跳 `next`；否则按 `user` 无法判断角色 → 调 `GET /api/v1/me` 拿 `roles`，含 `admin` 跳 `/admin/users`，否则跳 `/account/profile`。
5. 响应 `data.mfaRequired === true`：记录 `mfaTicket` 到组件状态（**不落 localStorage**），切换到第二步；`data.expiresIn` 为 300 秒，可据此展示倒计时（可选）。
6. 第二步输入 6 位码 → `POST /api/v1/auth/mfa/verify`：
   `{ "mfaTicket": "...", "code": "123456" }`
7. 成功（`data.redirectTo` + `data.user`）→ 跳转逻辑同 4；失败按 §3.4 处理，`AUTH_MFA_INVALID` 时保留 ticket 允许重试，`AUTH_MFA_TICKET_EXPIRED` 时回第一步。

**状态与错误**

| 场景 | 表现 |
|---|---|
| 必填未填 | 字段下方红字，按钮仍可点但提交被拦截 |
| 密码错误 | 表单顶部红条「用户名或密码错误」，清空密码框，焦点回密码框 |
| 连续错误 5 次 | 后端返回 423，页面进入锁定提示，10 秒内禁用提交 |
| 请求超时/网络失败 | 红条「网络异常，请重试」 |
| 已在登录页且已登录 | 进页面时探测 `GET /api/v1/me`，成功则直接跳目标页 |

**跳转**

- 登录成功 → `next` 或 `/account/profile` / `/admin/users`
- 登录页不提供「注册」「忘记密码」入口（§8）

---

### P2 个人信息 `/account/profile`

**用途**：员工查看本人身份信息，是登录后的默认落地页。

**布局**：左右两栏。左侧头像卡片（头像、姓名、用户名、角色标签）；右侧分组信息卡。

**核心元素**

| 元素 | 类型 | 说明 |
|---|---|---|
| 页面标题 | h1 | 个人信息 |
| 头像 | img/占位 | `avatarUrl`，为空显示姓名首字 |
| 姓名 | 文本 | `displayName` |
| 用户名 | 文本 | `username`，等宽字体 |
| 角色标签 | tag 组 | `roles`：`admin` → 「管理员」蓝色，`user` → 「普通用户」灰色 |
| 基本信息卡 | 定义列表 | 邮箱、手机号、部门路径、租户 |
| 安全状态卡 | 定义列表 | MFA 状态、最近登录时间、账号创建时间 |
| 修改密码按钮 | 主按钮 | → `/account/password` |
| 登录设备按钮 | 次按钮 | → `/account/sessions` |

**数据来源**：`GET /api/v1/me`（§7.3），一次请求填充整页。

| 页面位置 | 字段 |
|---|---|
| 头像 | `avatarUrl`（null → 首字占位） |
| 姓名 | `displayName` |
| 用户名 | `username` |
| 角色标签 | `roles[]` |
| 邮箱 | `email`（null → `未绑定`） |
| 手机号 | `phone`（null → `未绑定`） |
| 部门 | `deptPath[]` 用 ` / ` 连接；为空显示 `-` |
| 租户 | `tenantId` |
| MFA 状态 | `mfaEnabled` / `mfaRequired`：已启用 / 未启用；`mfaRequired=true` 时额外标「管理员强制」 |
| 最近登录 | `lastLoginAt` |
| 创建时间 | `createdAt` |

**交互**
- 本页只读，**不提供编辑**（后端无更新个人资料接口）。
- 点击「修改密码」「登录设备」为路由跳转，不调接口。

**状态**：loading 用骨架屏；接口失败展示整页错误 + 重试按钮。

---

### P3 我的应用 `/account/apps`

**用途**：员工查看自己可访问的内部系统并一键跳转，替代"记住一堆地址"。

**核心元素**

| 元素 | 类型 | 说明 |
|---|---|---|
| 标题 | h1 | 我的应用 |
| 副标题 | p | 共 N 个应用 |
| 搜索框 | input | 按名称前端过滤（不调接口） |
| 应用卡片 | 卡片网格 | 每卡：图标、名称、描述、最近访问时间、跳转按钮 |
| 空态 | empty | 「暂无可访问的应用，请联系管理员开通」 |

**数据来源**：`GET /api/v1/me/apps` → `data` 为数组（§7.4）

| 卡片元素 | 字段 |
|---|---|
| 图标 | `logoUrl`（为空显示名称首字方块） |
| 名称 | `name` |
| 描述 | `description`（为空显示 `-`） |
| 最近访问 | `lastAccessAt`（null → 「尚未访问」） |
| 跳转地址 | `homeUrl` |

**交互**
1. 进页面拉列表。
2. 点卡片或「打开」→ `window.location.href = homeUrl`（新标签页 `target=_blank rel=noopener`）。
   - 该地址是业务系统入口，其自身会发起 OIDC 授权并命中 SSO 会话。
3. `homeUrl` 为空时卡片禁用并提示「该应用未配置入口地址」。

**注意**：本页**不做**"反向拉起授权"（不在前端拼 `/oauth2/authorize`），跳转由目标系统发起。

---

### P4 登录设备 `/account/sessions`

**用途**：查看当前账号在哪些设备/IP 登录，发现异常可立即踢下线。

**核心元素**

| 元素 | 类型 | 说明 |
|---|---|---|
| 标题 | h1 | 登录设备 |
| 说明 | p | 共 N 个活跃会话；修改密码会退出全部设备 |
| 会话表格 | table | 列：设备（UA 摘要）、IP、登录时间、最近活跃、状态、操作 |
| 当前会话标记 | tag | `current=true` 显示「本机」绿色标签 |
| 撤销按钮 | 次按钮（危险色） | 文案「下线」；当前会话显示「退出登录」 |
| 空态 | empty | 「暂无登录记录」（正常不会出现） |

**数据来源**：`GET /api/v1/me/sessions` → 数组（§7.5）
撤销：`DELETE /api/v1/me/sessions/:sid`（§7.6）

| 列 | 字段 |
|---|---|
| 设备 | `userAgent`（截断 60 字符，hover 显示全文；为空显示 `未知设备`） |
| IP | `ip` |
| 登录时间 | `createdAt` |
| 最近活跃 | `lastSeenAt` |
| 状态 | `current` → 「本机」/「其他设备」 |
| 操作 | `sid`（仅用于请求路径，**不在界面展示**） |

**交互**
1. 进页面拉列表。
2. 点「下线」→ 二次确认弹窗「确定让该设备下线？对方需要重新登录」→ `DELETE /api/v1/me/sessions/{sid}`。
3. 成功：toast「已下线」，重新拉列表（不要本地移除，以服务端为准）。
4. 点「退出登录」（当前会话）→ 先调注销接口（见 P5 说明的登出流程）→ 跳 `/login`。
5. 失败：`NOT_FOUND` → toast「该会话已失效」并刷新列表；其他按 §3.4。

---

### P5 修改密码 `/account/password`

**用途**：员工自助改密。**改密成功后所有会话立即失效**，包括当前会话。

**核心元素**

| 元素 | 类型 | 文案/规则 |
|---|---|---|
| 标题 | h1 | 修改密码 |
| 说明 | 提示条 | 「修改成功后需要使用新密码重新登录，所有设备将退出」 |
| 原密码 | input[password] | 必填，8–128 |
| 新密码 | input[password] | 必填，8–128，需含字母与数字 |
| 确认新密码 | input[password] | 必填，必须与新密码一致（前端校验） |
| 强度提示 | 文本 | 弱：长度不足/无数字；中：仅满足下限；强：≥12 位且含符号（纯前端提示，不拦截） |
| 提交按钮 | 主按钮 | 确认修改 |
| 取消按钮 | 次按钮 | 返回 `/account/profile` |

**数据来源**：`POST /api/v1/me/password`（§7.7）
请求：`{ "oldPassword": "...", "newPassword": "..." }`；成功 `data: null`。

**交互**
1. 前端校验：两次新密码一致、新密码满足规则、新旧不同。
2. 提交 → 成功：toast「密码已修改，请重新登录」→ **强制**跳 `/login?reason=password-changed`。
3. 失败映射：
   - `AUTH_INVALID_CREDENTIALS`（后端用于"原密码错误"）→ 原密码字段报错「原密码错误」
   - `WEAK_PASSWORD` → 新密码字段报错
   - `VALIDATION_FAILED` 且 message 含「新密码不能与原密码相同」→ 新密码字段报错
   - 其余按 §3.4

**登出（全站通用）**：顶栏用户菜单 →「退出登录」→ `POST /api/v1/auth/logout`（`data: null`）→ 成功或失败**都**跳 `/login`（前端清空内存态即可）。

---

### A1 用户管理 `/admin/users`

**用途**：管理员按组织架构维护账号。

**布局**：左侧组织树（宽 240px，可折叠），右侧工具栏 + 用户表格 + 分页。移动端不做（<1280px 提示「请使用桌面端」）。

**核心元素**

| 区域 | 元素 | 说明 |
|---|---|---|
| 左栏 | 组织树 | 数据来自 `GET /admin/orgs/tree`；支持展开/折叠、关键字高亮；点击节点按 `deptId` 过滤右侧表格；顶部「全部用户」虚拟根节点 |
| 左栏 | 节点人数 | 每个节点右侧显示 `userCount` |
| 工具栏 | 搜索框 | placeholder「搜索用户名或姓名」，回车触发 |
| 工具栏 | 状态筛选 | 下拉：全部 / 正常(`active`) / 停用(`disabled`) / 锁定(`locked`) |
| 工具栏 | 新建用户按钮 | 主按钮，打开抽屉（§5.1） |
| 表格 | 列：用户名、姓名、部门、角色、状态、MFA、最近创建时间、操作 | 见下表 |
| 表格 | 操作列 | 「编辑」文字按钮（打开抽屉 §5.2） |
| 分页 | 页码 + 每页条数 | 20 / 50 / 100 |

**数据来源**

| 数据 | 接口 | 字段 |
|---|---|---|
| 组织树 | `GET /api/v1/admin/orgs/tree` | `id,name,parentId,path,userCount,children[]` |
| 用户列表 | `GET /api/v1/admin/users?keyword=&deptId=&status=&page=&pageSize=` | `{list,total,page,pageSize}` |

列表行字段映射：

| 列 | 字段 |
|---|---|
| 用户名 | `username`（等宽） |
| 姓名 | `displayName` |
| 部门 | 前端用 `deptId` 在组织树中查名称（无则显示 `-`） |
| 角色 | `roles[]`（`admin` → 管理员，`user` → 普通用户） |
| 状态 | `status`：`active` 绿「正常」/ `disabled` 灰「停用」/ `locked` 橙「锁定」 |
| MFA | `mfaEnabled`（已绑定/未绑定）+ `mfaRequired` 时加「强制」标记 |
| 创建时间 | `createdAt` |
| 操作 | `id`（用于 PATCH，界面不展示） |

**交互**
1. 进页面并行请求组织树与用户列表（默认 page=1、pageSize=20、无筛选）。
2. 点树节点 → 设 `deptId`、`page=1`，重新请求列表；同时高亮节点。
3. 搜索/筛选变更 → `page=1` 重新请求。
4. 分页/改每页条数 → 重新请求（保留当前筛选条件）。
5. 新建成功（§5.1）→ 关闭抽屉、toast「用户已创建」、刷新列表。
6. 编辑成功（§5.2）→ 关闭抽屉、toast「已保存」、刷新列表。
7. 列表请求失败：表格区展示错误占位 + 「重试」，不清空已加载数据。

**状态与错误**
- 组织树加载失败：左栏显示「加载失败，点击重试」，不影响右侧列表（仍展示全部用户）。
- 无数据：表格空态「没有符合条件的用户」，若已选部门则追加「清除部门筛选」。

---

### A2 应用管理 `/admin/clients`

**用途**：注册接入本 IdP 的业务系统，管理其回调地址、scope 与密钥。

**核心元素**

| 区域 | 元素 | 说明 |
|---|---|---|
| 标题 | h1 | 应用管理 |
| 说明 | p | 应用接入使用 OIDC 授权码 + PKCE(S256) |
| 搜索框 | input | 按 `clientId` 或名称搜索（服务端 `keyword`） |
| 注册应用按钮 | 主按钮 | 打开抽屉（§5.3） |
| 表格 | 列：应用名、clientId、类型、scope、描述、创建时间、操作 | |
| 表格 | 操作 | 「查看详情」→ 只读抽屉（§5.4）；「轮换密钥」→ 确认弹窗（§5.5），公共客户端禁用该按钮并提示「公共客户端无密钥」 |
| 分页 | 同上 | |

**数据来源**：`GET /api/v1/admin/clients?keyword=&page=&pageSize=` → `{list,total,page,pageSize}`

| 列 | 字段 |
|---|---|
| 应用名 | `clientName`（空 `-`） |
| clientId | `clientId`（等宽，可复制） |
| 类型 | `isPublic` → true「公共客户端(PKCE)」/ false「机密客户端」 |
| scope | `scopes[]`，逗号分隔，超 3 个折叠为 `+N` |
| 描述 | `description`（空 `-`） |
| 创建时间 | `createdAt` |
| 详情 | `redirectUris[]`、`postLogoutUris[]`、`grantTypes[]`、`logoUrl` |

**交互**
1. 进页面拉列表。
2. 搜索（回车）→ page=1 重新请求。
3. 注册成功（§5.3）→ 关闭抽屉 → **立即弹出密钥一次性展示弹窗**（公共客户端无此弹窗）→ 刷新列表。
4. 轮换密钥成功（§5.5）→ 密钥一次性弹窗 → 提示「旧密钥立即失效，请尽快更新接入方配置」。

**明确不支持**：编辑应用、删除应用（后端无接口，见 §8）。

---

### A3 审计日志 `/admin/audit`

**用途**：安全事件检索，用于排查"谁在什么时候登录了哪个系统、是否失败"。

**核心元素**

| 区域 | 元素 | 说明 |
|---|---|---|
| 标题 | h1 | 审计日志 |
| 筛选条 | 用户 ID 输入 | `userId`（精确匹配，26 位 ULID） |
| 筛选条 | 应用选择 | `clientId`，下拉来源 `GET /admin/clients?pageSize=100`（首屏懒加载） |
| 筛选条 | 事件类型下拉 | 14 个枚举，见下 |
| 筛选条 | 结果下拉 | 全部 / `success` / `failure` |
| 筛选条 | 时间范围 | 日期时间选择器 → 转 RFC3339 传 `from`/`to` |
| 筛选条 | 查询 / 重置按钮 | 查询 → page=1 请求；重置 → 清空条件并请求 |
| 表格 | 列：时间、事件、用户、应用、IP、结果、详情 | 时间列宽 160，详情列可展开 |
| 分页 | 同上 | |

**事件类型下拉**（value → 展示）

`login_success` 登录成功 · `login_failure` 登录失败 · `mfa_success` MFA 成功 · `mfa_failure` MFA 失败 · `logout` 登出 · `password_changed` 修改密码 · `token_issued` 令牌签发 · `token_revoked` 令牌撤销 · `code_reused` 授权码重放 · `refresh_reused` 刷新令牌重放 · `user_created` 创建用户 · `user_updated` 更新用户 · `client_created` 注册应用 · `client_secret_rotated` 轮换密钥

**数据来源**：`GET /api/v1/admin/audit-logs?userId=&clientId=&eventType=&result=&from=&to=&page=&pageSize=`

| 列 | 字段 |
|---|---|
| 时间 | `createdAt` |
| 事件 | `eventType`（按上表映射中文；未知值原样展示） |
| 用户 | `username`（null → `userId` 前 8 位；都为空 → `-`） |
| 应用 | `clientId`（null → `-`） |
| IP | `ip` |
| 结果 | `result`：`success` 绿标签 / `failure` 红标签 |
| 详情 | `detail`（对象或 null，形如 `{"message":"refresh token reuse detected"}`；展开区以 key-value 展示；null 显示 `-`） |

**交互**
1. 进页面：默认查最近 7 天（`from` = 当前时间 - 7 天，`to` = 当前时间），page=1。
2. 查询条件变化不自动触发，点「查询」才请求。
3. 行点击 → 展开行内详情（不跳页）。
4. 失败按 §3.4；`from > to` 前端拦截提示「开始时间不能晚于结束时间」。

**明确不支持**：导出 CSV、图表统计、日志删除（§8）。

---

### X1 `/403`、X2 `/404`

- 居中插画 + 标题 + 一句说明 + 「返回首页」按钮（→ `/`）+ 「重新登录」（→ `/login`）。
- 不调任何接口。

---

## 5. 弹窗与抽屉（不占路由）

> 后端没有"单条查询"接口（`GET /users/:id`、`GET /clients/:id` 均不存在），因此**详情/编辑一律用列表行数据填充抽屉**，不做可深链的独立详情页。

### 5.1 新建用户抽屉

| 字段 | 控件 | 必填 | 校验 |
|---|---|---|---|
| 用户名 | input | 是 | 3–64，`^[a-zA-Z][a-zA-Z0-9._-]{2,63}$` |
| 姓名 | input | 是 | 1–64 |
| 邮箱 | input | 否 | 邮箱格式，≤128 |
| 手机号 | input | 否 | `^1[3-9]\d{9}$` |
| 初始密码 | input[password] | 是 | 8–128，含字母与数字 |
| 所属部门 | 树选择器 | 是 | 数据来自 `GET /admin/orgs/tree`，提交 26 位 `id` |
| 角色 | 多选 | 否 | 选项：`user`（普通用户）、`admin`（管理员）；默认 `user` |
| 强制 MFA | 开关 | 否 | 默认关 |
| 发送通知 | 开关 | 否 | **固定关闭且禁用**，提示「本阶段不发送通知」 |

提交：`POST /api/v1/admin/users`，成功 201 返回用户对象 → 关闭抽屉 + toast + 刷新列表。
错误：409 → 对应字段报错；400 部门不存在 → 部门字段报错「部门不存在」。

### 5.2 编辑用户抽屉

预填当前行数据（`id` 取自列表行，不发 GET）。
可改：姓名、邮箱、手机号、部门、角色、状态（正常/停用/锁定）、强制 MFA。
不可改：用户名（后端不支持）、密码（无接口，见 §8）。
提交：`PATCH /api/v1/admin/users/{id}`，只提交发生变化的字段；成功返回用户对象 → 关闭 + toast + 刷新。
注意：把状态改为 `disabled` 会让该用户立即无法登录（后端登录时校验），需二次确认文案「停用后该用户将无法登录，确定？」。

### 5.3 注册应用抽屉

| 字段 | 控件 | 必填 | 校验 |
|---|---|---|---|
| 应用名称 | input | 是 | 1–128 |
| 描述 | textarea | 否 | ≤255 |
| 图标地址 | input | 否 | URL，≤512 |
| 类型 | radio | 是 | 公共客户端（PKCE）/ 机密客户端；默认公共 |
| 回调地址 | 动态列表 textarea | 是 | ≥1 条，每条合法 URL，**必须精确填写**（不做通配） |
| 授权类型 | 多选 | 否 | 默认勾选 `authorization_code`、`refresh_token` |
| scope | 多选 | 否 | 默认 `openid`、`profile`、`email`；如需 refresh_token 必须勾 `offline_access`（有说明文案） |

提交：`POST /api/v1/admin/clients` → 201 `{ clientId, clientSecret }`
- 机密客户端：`clientSecret` 非空 → 弹一次性密钥弹窗（§5.5）。
- 公共客户端：`clientSecret` 为 `null` → 只 toast「应用已创建，请将 clientId 配置到接入方」。
- 错误：`VALIDATION_FAILED`（URL 非法等）→ 字段报错。

### 5.4 应用详情抽屉（只读）

展示：`clientId`（可复制）、应用名、描述、图标、类型、`redirectUris[]`、`postLogoutUris[]`、`grantTypes[]`、`scopes[]`、`createdAt`。
所有字段来自列表行，不额外请求。

### 5.5 密钥弹窗（一次性）

| 元素 | 说明 |
|---|---|
| 标题 | 应用密钥（仅显示一次） |
| 提示条 | 「关闭后无法再次查看，请立即保存到接入方配置」 |
| clientId | 只读，带复制按钮 |
| clientSecret | 只读、等宽、带复制按钮；**默认打码**，点「显示」才明文 |
| 关闭按钮 | 关闭后组件状态清空 |

来源：`POST /admin/clients`（创建）或 `POST /admin/clients/{id}/secret`（轮换，请求体 `{"gracePeriodSeconds": 0}`；响应含 `expiresAt`，当前恒为 `null`，为 `null` 时提示「旧密钥已立即失效」）。

---

## 6. 跳转逻辑总表

| 起点 | 触发 | 终点 |
|---|---|---|
| 任意页 | 未登录且访问受保护路由 | `/login?next=<当前路径>` |
| `/login` | 登录成功且 `roles` 含 `admin` 且无 `next` | `/admin/users` |
| `/login` | 登录成功且无 admin 角色且无 `next` | `/account/profile` |
| `/login` | 登录成功且有 `next` | `next` |
| `/login` | 已登录状态下访问 | 同「登录成功」分支 |
| `/account/profile` | 点「修改密码」 | `/account/password` |
| `/account/profile` | 点「登录设备」 | `/account/sessions` |
| `/account/password` | 改密成功 | `/login?reason=password-changed` |
| `/account/password` | 点「取消」 | `/account/profile` |
| `/account/sessions` | 撤销当前会话 / 顶栏退出 | `/login` |
| `/account/apps` | 点应用卡片 | 外部 `homeUrl`（新标签） |
| 顶栏（admin） | 点「用户管理 / 应用管理 / 审计日志」 | `/admin/users`、`/admin/clients`、`/admin/audit` |
| 顶栏 | 点「返回门户」 | `/account/profile` |
| 顶栏 | 点「退出登录」 | 调注销接口后 `/login` |
| 任意 admin 页 | 访问者无 `admin` 角色 | `/403` |
| 任意页 | 接口 401 | `/login?next=<当前路径>` |
| 任意页 | 路由未匹配 | `/404` |

---

## 7. 接口契约速查

> 全部为同源请求，`credentials: 'include'`；响应统一 §3.2 格式，以下只列 `data`。

### 7.1 `POST /api/v1/auth/login`（公开）

请求：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `username` | string | 是 | 1–64 |
| `password` | string | 是 | 8–128 |
| `captchaId` | string | 否 | 本阶段不传（无发码接口） |
| `captchaCode` | string | 否 | 同上 |
| `clientId` | string | 否 | 默认 `portal` |
| `rememberMe` | bool | 否 | true 时会话 7 天，否则 8 小时 |

响应 A（需要 MFA）：

```json
{ "mfaRequired": true, "mfaTicket": "9SmnjbsiVNOw_75gJq1anCAu", "expiresIn": 300 }
```

响应 B（登录完成）：

```json
{
  "mfaRequired": false,
  "mfaTicket": null,
  "expiresIn": 0,
  "redirectTo": "/account",
  "user": { "id": "01J9Z...", "username": "zhangsan", "displayName": "张三" }
}
```

### 7.2 `POST /api/v1/auth/mfa/verify`（公开）

请求：`{ "mfaTicket": "...", "code": "123456" }`（`code` 固定 6 位）
响应：`{ "redirectTo": "/account", "user": { "id", "username", "displayName" } }`

### 7.3 `GET /api/v1/me`（需登录）

```json
{
  "id": "01J...", "username": "zhangsan", "displayName": "张三",
  "email": "zhangsan@example.com", "phone": "13800000001",
  "avatarUrl": "https://cdn.example.com/avatar/zs.png",
  "deptId": "01J...", "deptPath": ["总公司", "技术中心", "平台组"],
  "tenantId": "default", "roles": ["user", "admin"],
  "mfaEnabled": true, "mfaRequired": false,
  "lastLoginAt": "2026-10-20T09:00:00Z", "createdAt": "2026-01-05T02:11:00Z"
}
```

`avatarUrl`、`phone`、`email`、`lastLoginAt` 可能为 `null`。

### 7.4 `GET /api/v1/me/apps`（需登录）

`data` 为数组：

```json
[{
  "clientId": "portal", "name": "统一门户",
  "description": "统一身份认证门户，管理个人信息、会话与授权",
  "logoUrl": "https://cdn.example.com/logo/portal.svg",
  "homeUrl": "http://localhost:5173",
  "lastAccessAt": "2026-10-20T09:00:00Z"
}]
```

`description`/`logoUrl` 可能为空串，`lastAccessAt` 可能为 `null`。

### 7.5 `GET /api/v1/me/sessions`（需登录）

```json
[{ "sid": "d7c9...", "ip": "::1", "userAgent": "curl/8.4.0",
   "createdAt": "2026-10-20T09:00:00Z", "lastSeenAt": "2026-10-20T09:00:00Z",
   "current": true }]
```

`userAgent` 可能为 `null`。

### 7.6 `DELETE /api/v1/me/sessions/{sid}`（需登录）

成功 `data: null`；`sid` 不属于当前用户或不存在 → 404 `NOT_FOUND`。

### 7.7 `POST /api/v1/me/password`（需登录）

请求 `{ "oldPassword": "...", "newPassword": "..." }`；成功 `data: null`，**同时吊销该用户全部会话**。
错误：`AUTH_INVALID_CREDENTIALS`（原密码错）、`WEAK_PASSWORD`、`VALIDATION_FAILED`（新旧相同）。

### 7.8 `POST /api/v1/auth/logout`（需登录）

成功 `data: null`，清除 cookie 并失效服务端会话。

### 7.9 `GET /api/v1/admin/users`（admin）

查询参数：`keyword`、`deptId`、`status`(`active|disabled|locked`)、`page`、`pageSize`。

```json
{ "list": [{
    "id": "01J...", "username": "lisi", "displayName": "李四",
    "email": "lisi@example.com", "phone": null,
    "deptId": "01J...", "tenantId": "default", "status": "active",
    "mfaEnabled": false, "mfaRequired": false, "roles": ["user"],
    "createdAt": "2026-10-20T10:12:33Z", "updatedAt": "2026-10-20T10:12:33Z"
  }],
  "total": 4, "page": 1, "pageSize": 20 }
```

### 7.10 `POST /api/v1/admin/users`（admin，201）

请求：

```json
{ "username": "lisi", "displayName": "李四", "email": "lisi@example.com",
  "phone": "13800000000", "password": "Init!pass2026",
  "deptId": "01J...", "tenantId": "default", "roleIds": ["user"],
  "mfaRequired": false, "sendNotify": false }
```

响应：单个用户对象（字段同 7.9 行）。
错误：409 `USERNAME_ALREADY_EXISTS` / `EMAIL_ALREADY_EXISTS`；400 `WEAK_PASSWORD`、`VALIDATION_FAILED`（部门不存在也是 400）。

### 7.11 `PATCH /api/v1/admin/users/{id}`（admin）

请求（全部可选，只传要改的）：

```json
{ "displayName": "李四", "email": "...", "phone": "...", "deptId": "01J...",
  "status": "disabled", "mfaRequired": true, "roleIds": ["user","admin"] }
```

响应：更新后的用户对象。不存在 → 404。

### 7.12 `GET /api/v1/admin/orgs/tree`（admin）

```json
[{ "id": "01J...", "name": "总公司", "parentId": null,
   "path": "/01J.../", "userCount": 0,
   "children": [{ "id": "01J...", "name": "技术中心", "parentId": "01J...",
                  "path": "/01J.../01J.../", "userCount": 0, "children": [] }] }]
```

`userCount` 为**该节点直挂人数**，不含子部门。

### 7.13 `GET /api/v1/admin/clients`（admin）

```json
{ "list": [{ "clientId": "portal", "clientName": "统一门户",
    "description": "...", "logoUrl": "https://...",
    "isPublic": true, "redirectUris": ["http://localhost:5173/callback"],
    "grantTypes": ["authorization_code","refresh_token"],
    "scopes": ["openid","profile","email","offline_access"],
    "postLogoutUris": ["http://localhost:5173/"],
    "createdAt": "2026-10-20T10:14:44Z" }],
  "total": 2, "page": 1, "pageSize": 20 }
```

### 7.14 `POST /api/v1/admin/clients`（admin，201）

请求：

```json
{ "clientName": "报表系统", "description": "月度经营报表",
  "logoUrl": "https://cdn.example.com/logo/report.png",
  "redirectUris": ["http://localhost:7070/cb"],
  "grantTypes": ["authorization_code","refresh_token"],
  "scopes": ["openid","profile","offline_access"],
  "isPublic": true }
```

响应：`{ "clientId": "ESRS...", "clientSecret": null }`（机密客户端返回明文，仅此一次）。

### 7.15 `POST /api/v1/admin/clients/{id}/secret`（admin）

请求：`{ "gracePeriodSeconds": 0 }`（当前忽略该值，旧密钥立即失效）
响应：`{ "clientId": "...", "clientSecret": "yCwm...", "expiresAt": null }`

### 7.16 `GET /api/v1/admin/audit-logs`（admin）

参数：`userId`、`clientId`、`eventType`、`result`、`from`、`to`（RFC3339 或 Unix 毫秒）、`page`、`pageSize`。

```json
{ "list": [{ "id": "01J...", "eventType": "refresh_reused",
    "userId": "01J...", "username": "zhangsan", "clientId": "portal",
    "ip": "::1", "userAgent": "curl/8.4.0", "result": "failure",
    "detail": { "message": "refresh token reuse detected, family revoked" },
    "createdAt": "2026-10-20T10:20:00Z" }],
  "total": 20, "page": 1, "pageSize": 20 }
```

`userId`/`username`/`clientId`/`userAgent`/`detail` 均可能为 `null`。

### 7.17 协议层（前端不直接调用，仅说明）

`GET /oauth2/authorize`、`POST /oauth2/token`、`GET /oauth2/userinfo`、`GET /oauth2/jwks`、`POST /oauth2/introspect`、`POST /oauth2/revoke`、`GET /oauth2/end_session`、`GET /.well-known/openid-configuration` 均属 RP 与 IdP 之间的交互，浏览器只会被重定向经过，前端代码无感知。

---

## 8. 不做的页面与功能

> 以下内容**本阶段不实现**，与 `AGENTS.md` §7.2 非目标一致；其中标注「无接口」的项即使想做也需后端先补 API。

**页面层面**

| 不做 | 原因 |
|---|---|
| 注册 / 邀请 / 开放自助开通 | 非目标 2：账号只能由管理员创建 |
| 忘记密码 / 密码重置 | 非目标 4：无邮件/短信通道，也无对应接口 |
| MFA 绑定 / 解绑 / 更换验证器 | **无接口**（后端只有校验，没有绑定接口）；MFA 由管理员或种子数据预置 |
| 图形验证码 | **无发码接口**；登录请求虽预留 `captchaId`/`captchaCode`，页面不展示 |
| 个人资料编辑、头像上传 | **无接口**；`avatarUrl` 只读 |
| 用户独立详情页 `/admin/users/:id` | **无单条查询接口**；用列表行 + 抽屉替代 |
| 组织架构增删改 | **无接口**；组织树只读，仅用于筛选 |
| 应用编辑 / 删除 / 查看密钥明文 | **无接口**；密钥只在创建/轮换时一次性展示 |
| 管理员重置用户密码 | **无接口**；如需，走后续版本 |
| 审计导出、图表统计、日志删除 | 非目标；无接口 |
| 租户管理、计费、配额、开发者门户 | 非目标 5 |
| SAML / CAS / SCIM 配置界面 | 非目标 1、2 |
| 多语言切换（i18n） | 非目标 5：UI 恒为简体中文 |
| 暗色主题切换 | 非目标；保留 CSS 变量便于后续扩展，但不做开关 |
| 协议层内置登录页 `/login`（:8080 HTML）、授权页、登出页 | 后端已渲染，前端**不重写**（内置页仅作降级与循环兜底）；主链路的登录页是本 SPA 的 `/login`（见 P1），两者职责不重叠 |
| 移动端管理后台 | 非目标；admin 最小宽度 1280px，窄屏仅提示 |
| token 存储 / 静默刷新 / PKCE 计算 | 由 BFF 负责；前端接触 token 即违规 |

**交互层面（容易越界的地方，明确禁止）**

- 不在前端拼 `/oauth2/authorize` 链接做"静默登录"。
- 不在前端判断"密码是否符合策略"后阻止提交——只做提示，最终以服务端 `WEAK_PASSWORD` 为准。
- 不在前端缓存用户信息到 `localStorage`；刷新页面重新 `GET /me`。
- 不对 `clientSecret` 做任何持久化（包括 Pinia、sessionStorage、URL、埋点）。
- 不实现"记住用户名"以外的持久化（`rememberMe` 只影响会话时长，由后端处理）。

---

## 9. 前端验收清单

- [ ] 未登录访问任意受保护路由 → `/login?next=...`；登录后回到原路径
- [ ] 登录响应 `mfaRequired=true` 时进入第二步；`AUTH_MFA_TICKET_EXPIRED` 能回退第一步
- [ ] 密码错误 5 次后收到 423，页面提示锁定且不再疯狂重试
- [ ] admin 与普通用户登录后落地页不同；普通用户访问 `/admin/*` 得到 `/403`
- [ ] `/account/profile` 的 `deptPath` 正确渲染为 `总公司 / 技术中心 / 平台组`
- [ ] 改密成功后**必然**回到登录页，旧会话不可用
- [ ] 撤销"其他设备"后列表刷新，被撤销设备的下一次请求 401
- [ ] 注册机密应用后密钥弹窗只出现一次，关闭后再也看不到
- [ ] 公共客户端的「轮换密钥」按钮禁用
- [ ] 审计页时间范围反了时前端拦截，不发请求
- [ ] 所有错误提示都来自 `code`/`message` 映射，没有出现英文或裸 `500`
- [ ] 全站无 `localStorage` 中的 token / secret
