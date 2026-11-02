/** 与后端 /api/v1/** 契约一一对应（PRD 第 7 章） */

export interface Me {
  id: string
  username: string
  displayName: string
  email: string | null
  phone: string | null
  avatarUrl: string | null
  deptId: string
  deptPath: string[]
  tenantId: string
  roles: string[]
  mfaEnabled: boolean
  mfaRequired: boolean
  lastLoginAt: string | null
  createdAt: string
}

export interface Paged<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

export type UserStatus = 'active' | 'disabled' | 'locked'

export interface UserRow {
  id: string
  username: string
  displayName: string
  avatarUrl: string | null
  email: string | null
  phone: string | null
  deptId: string
  tenantId: string
  status: UserStatus
  mfaEnabled: boolean
  mfaRequired: boolean
  roles: string[]
  createdAt: string
  updatedAt: string
}

export interface OrgNode {
  id: string
  name: string
  parentId: string | null
  path: string
  userCount: number
  children: OrgNode[]
}

/** 组织节点详情（POST/PATCH /api/v1/admin/orgs[/:id] 的响应） */
export interface OrgDetail {
  id: string
  parentId: string | null
  name: string
  path: string
  sort: number
  tenantId: string
  userCount: number
  createdAt: string
}

export interface CreateOrgPayload {
  /** 缺失或 null 表示根级 */
  parentId?: string | null
  name: string
  sort?: number
}

export interface UpdateOrgPayload {
  name?: string
  sort?: number
  /** 缺失 = 不变；null = 移到根级；值 = 移到该节点下 */
  parentId?: string | null
}

export interface CreateUserPayload {
  username: string
  displayName: string
  email?: string
  phone?: string
  password: string
  deptId: string
  tenantId?: string
  roleIds: string[]
  mfaRequired: boolean
  sendNotify: boolean
}

export interface UpdateUserPayload {
  displayName?: string
  email?: string
  phone?: string
  deptId?: string
  status?: UserStatus
  mfaRequired?: boolean
  roleIds?: string[]
}

export interface ClientRow {
  clientId: string
  clientName: string
  description: string
  logoUrl: string
  isPublic: boolean
  redirectUris: string[]
  grantTypes: string[]
  scopes: string[]
  postLogoutUris: string[]
  createdAt: string
  /** 该应用的成员总数（003 新增字段） */
  memberCount: number
}

/* ---------------- 003 应用子页面：详情 / 成员 / 角色 / 权限 / 授权 ---------------- */

/** 应用详情（GET 与 PATCH /admin/clients/:id 共用，不含任何密钥字段） */
export interface ClientDetail {
  clientId: string
  clientName: string
  description: string
  logoUrl: string
  /** 首页地址（011）：注册与编辑必填，门户「我的应用」入口 */
  homeUrl: string
  redirectUris: string[]
  postLogoutUris: string[]
  grantTypes: string[]
  scopes: string[]
  isPublic: boolean
  accessTokenTtl: number
  refreshTokenTtl: number
  /** 1=启用，0=停用 */
  status: number
  createdAt: string
  /** Unix 毫秒；同时作为乐观锁版本值（FR-034） */
  updatedAt: number
  memberCount: number
}

/** 缺失即不变；description / logoUrl 传空串表示清空；homeUrl 为必填字段，不接受空串 */
export interface UpdateClientPayload {
  clientName?: string
  description?: string
  logoUrl?: string
  homeUrl?: string
  redirectUris?: string[]
  postLogoutUris?: string[]
  accessTokenTtl?: number
  refreshTokenTtl?: number
  status?: number
  updatedAt?: number
}

export interface AppMember {
  userId: string
  username: string
  displayName: string
  deptId: string
  deptName: string
  /** 1=正常，其他=停用/锁定（成员关系保留，仅标记） */
  status: number
  joinedAt: string
  roleCount: number
  /** 是否为该应用的应用管理员（006）：门户「管理」入口的唯一判据 */
  isAdmin: boolean
  /** 该成员命中的组织授权（010）：空数组表示「仅手动成员」 */
  orgGrants: OrgGrantRef[]
}

/** 成员命中的某条组织授权（010，来源列） */
export interface OrgGrantRef {
  orgId: string
  deptName: string
  includeSubDepts: boolean
}

/** 组织授权（010）：门户可见性按部门授予，可含子部门 */
export interface OrgGrant {
  orgId: string
  deptName: string
  deptPath: string
  includeSubDepts: boolean
  /** 命中该授权的**在职启用账号数**（列头须标注口径） */
  coveredUsers: number
  /** 命中该授权但账号已停用的数量（仅用于撤销确认说明） */
  coveredDisabledUsers: number
  /** 仅因该授权可见的在职启用账号数（撤销确认的「X 人将不再可见」） */
  uniqueUsers: number
  grantedBy: string
  grantedAt: string
  updatedAt: string
}

/** 存量迁移提示：某部门快照成员数达阈值且该部门未被授权（010 FR-031） */
export interface OrgGrantHint {
  orgId: string
  deptName: string
  memberCount: number
}

export interface OrgGrantList {
  list: OrgGrant[]
  total: number
}

export interface AddOrgGrantsPayload {
  grants: { deptId: string; includeSubDepts: boolean }[]
}

export interface AddOrgGrantsResult {
  added: number
  updated: number
  unchanged: number
  coveredUsers: number
}

export interface RemoveOrgGrantsResult {
  removed: number
  impactUsers: number
  remainingVisibleUsers: number
}

/** 成员列表响应：在 Paged 之上新增可见总人数与迁移提示（仅 page=1 返回） */
export interface MemberPage extends Paged<AppMember> {
  visibleTotal: number | null
  orgGrantHints?: OrgGrantHint[]
}

/** 设置/取消应用管理员的结果 */
export interface MemberAdminResult {
  userId: string
  isAdmin: boolean
}

export interface AppCandidate {
  userId: string
  username: string
  displayName: string
  deptId: string
  deptName: string
  /** 已可通过组织授权访问（010）：候选集合不变，仅提示 */
  orgCovered: boolean
}

export interface MemberQuery {
  keyword?: string
  deptId?: string
  page?: number
  pageSize?: number
}

/** all=true 时的响应：当前筛选下的全部 userId（上限 5000） */
export interface UserIdSet {
  userIds: string[]
}

export interface AddMembersByUsersResult {
  added: number
  skipped: number
}

/** 按部门整批加入的三类计数 */
export interface AddMembersByDeptResult {
  added: number
  existing: number
  disabled: number
}

export type AddMembersResult = Partial<AddMembersByUsersResult & AddMembersByDeptResult>

export interface RemoveMembersResult {
  removed: number
  roleBindingsRemoved: number
  notMember: number
}

export interface AppRole {
  id: string
  clientId: string
  name: string
  description: string
  permissionCount: number
  memberCount: number
  createdAt: string
  updatedAt: number
}

export interface RoleDetail {
  id: string
  clientId: string
  name: string
  description: string
  permissionIds: string[]
  memberIds: string[]
  createdAt: string
  updatedAt: number
}

export interface CreateRolePayload {
  name: string
  description?: string
}

export interface UpdateRolePayload {
  name?: string
  description?: string
  updatedAt?: number
}

export interface AppPermission {
  id: string
  clientId: string
  parentId: string | null
  code: string
  name: string
  description: string
  sort: number
  childCount: number
  roleCount: number
  createdAt: string
  updatedAt: number
}

export interface CreatePermissionPayload {
  code: string
  name: string
  description?: string
  parentId?: string | null
  sort?: number
}

export interface UpdatePermissionPayload {
  code?: string
  name?: string
  description?: string
  /** null = 移到根级 */
  parentId?: string | null
  sort?: number
  updatedAt?: number
}

export interface PermissionGrantResult {
  permissionIds: string[]
  added: number
  removed: number
}

export interface MemberGrantResult {
  memberIds: string[]
  added: number
  removed: number
  /** 顺带补入应用的成员数（FR-025） */
  autoJoinedMembers: number
}

export interface CreateClientPayload {
  clientName: string
  description?: string
  logoUrl?: string
  /** 首页地址（011）：必填 */
  homeUrl: string
  redirectUris: string[]
  grantTypes: string[]
  scopes: string[]
  isPublic: boolean
  /** 已上传（target_id 为空）的 logo 文件记录 ID；创建成功后由服务端回填绑定 */
  fileId?: string
}

export interface CreatedClient {
  clientId: string
  clientSecret: string | null
}

export interface RotatedSecret {
  clientId: string
  clientSecret: string
  expiresAt: string | null
}

export interface AuditRow {
  id: string
  eventType: string
  userId: string | null
  username: string | null
  clientId: string | null
  ip: string | null
  userAgent: string | null
  result: 'success' | 'failure'
  /** 服务端返回 detail 列的 JSON 文本（不是对象），展示前需 parse */
  detail: string | null
  createdAt: string
}

export interface AuditQuery {
  userId?: string
  clientId?: string
  eventType?: string
  result?: string
  from?: string
  to?: string
  page?: number
  pageSize?: number
}

/* ---------------- 004 文件上传 ---------------- */

/** 预签名直传凭证（POST /files/upload-credential 响应） */
export interface UploadCredential {
  key: string
  uploadUrl: string
  accessKeyId: string
  securityToken: string
  policy: string
  signature: string
  expiresAt: string
}

export interface FileRow {
  id: string
  provider: string
  scene: 'app_logo' | 'user_avatar'
  format: string
  size: number
  url: string
  status: 'active' | 'disabled' | 'deleted'
  creator: string
  /** 创建人用户名（后端解析，列表展示用） */
  creatorName: string
  createdAt: string
}

export interface FileQuery {
  scene?: string
  status?: string
  keyword?: string
  /** 创建人用户 ID */
  creatorId?: string
  page?: number
  pageSize?: number
}

export interface FileUploadPayload {
  key: string
  format: string
  size: number
}

/** 通用上传确认（POST /files/confirm）；targetId 为空表示先落文件、后绑定 */
export interface FileConfirmPayload {
  scene: 'app_logo' | 'user_avatar'
  key: string
  format: string
  size: number
  targetId?: string
}

/* ---------------- 005 发送记录 ---------------- */

export interface SendRecordRow {
  id: string
  channel: 'sms' | 'email'
  scene: 'bind_phone' | 'bind_email' | 'login' | 'find_password' | 'mfa_unbind'
  /** 服务端脱敏后的接收方 */
  target: string
  result: 'success' | 'failed'
  provider: 'chuanglan' | 'smtp' | 'mock'
  error: string | null
  /** Unix 毫秒 */
  createdAt: number
}

export interface SendRecordQuery {
  channel?: string
  scene?: string
  result?: string
  startTime?: string
  endTime?: string
  page?: number
  pageSize?: number
}

/** 发送记录分页载荷（契约 §8：items + total） */
export interface SendRecordPage {
  items: SendRecordRow[]
  total: number
  page: number
  pageSize: number
}
