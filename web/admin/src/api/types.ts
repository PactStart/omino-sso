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

/** 缺失即不变；description / logoUrl 传空串表示清空 */
export interface UpdateClientPayload {
  clientName?: string
  description?: string
  logoUrl?: string
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
}

export interface AppCandidate {
  userId: string
  username: string
  displayName: string
  deptId: string
  deptName: string
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
  createdAt: string
}

export interface FileQuery {
  scene?: string
  status?: string
  keyword?: string
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
