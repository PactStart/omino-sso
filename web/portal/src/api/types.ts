/** 与后端 /api/v1/** 契约一一对应（见 PRD 第 7 章） */

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

export type LoginType = 'password' | 'email_code' | 'sms_code'
export type VerifyChannel = 'sms' | 'email'
export type VerifyScene = 'bind_phone' | 'bind_email' | 'login' | 'find_password' | 'mfa_unbind'

export interface LoginPayload {
  loginType?: LoginType
  identifier?: string
  username?: string
  password?: string
  code?: string
  captchaId?: string
  captchaCode?: string
  clientId: string
  rememberMe: boolean
}

/**
 * 发送验证码。target 仅在 bind_* 场景与未登录的 login/find_password 场景需要；
 * 已登录的 mfa_unbind/find_password 由服务端按会话解析目标（联系方式不经浏览器）。
 */
export interface SendCodePayload {
  scene: VerifyScene
  channel: VerifyChannel
  target?: string
  captchaId?: string
  captchaCode?: string
}

export interface CaptchaResult {
  captchaId: string
  /** 可直接用于 <img src> 的 PNG data URL（位图，响应不含明文验证码） */
  image: string
  expiresIn: number
}

export interface SendCodeResult {
  expiresIn: number
  resendAfter: number
}

export interface ResetPasswordPayload {
  channel: VerifyChannel
  target: string
  code: string
  newPassword: string
}

export interface BindPhoneResult {
  phone: string
}

export interface BindEmailResult {
  email: string
}

export interface MfaBindStart {
  otpauthUrl: string
  mfaSetupTicket: string
  expiresIn: number
}

export interface MfaBoundResult {
  bound: boolean
}

export interface LoginUserBrief {
  id: string
  username: string
  displayName: string
}

/** mfaRequired=true 时只有 mfaTicket/expiresIn；false 时含 redirectTo/user */
export interface LoginResult {
  mfaRequired: boolean
  mfaTicket: string | null
  expiresIn: number
  redirectTo?: string
  user?: LoginUserBrief
}

export interface MfaVerifyPayload {
  mfaTicket: string
  code: string
}

export interface MfaVerifyResult {
  redirectTo: string
  user: LoginUserBrief
}

export interface AppItem {
  clientId: string
  name: string
  description: string
  logoUrl: string
  homeUrl: string
  lastAccessAt: string | null
  /** 是否为该应用的应用管理员（006）：为 true 时「我的应用」出现「管理」入口 */
  isAppAdmin: boolean
}

/* ---------------- 门户应用管理（006-app-admin-role） ---------------- */

export interface Paged<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

/** 只读组织树节点（与 admin 端同构，userCount 为直属人数） */
export interface OrgNode {
  id: string
  name: string
  parentId: string | null
  path: string
  userCount: number
  children: OrgNode[]
}

/** 管理页摘要（GET /me/apps/:id）：不含回调地址、scope、TTL 与密钥 */
export interface AppManageContext {
  clientId: string
  name: string
  description: string
  logoUrl: string
  status: number
  memberCount: number
}

/**
 * 应用详情（GET /me/apps/:id/detail）。
 * 字段与 admin 端 ClientDetail 一致，供门户「应用信息」页展示；
 * 但门户只有名称 / 描述 / Logo / 回调地址可改，其余均为只读。
 */
export interface AppDetail {
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
  /** 1=启用，0=停用（门户只读） */
  status: number
  createdAt: string
  /** Unix 毫秒；同时作为乐观锁版本值 */
  updatedAt: number
  memberCount: number
}

/** 门户可编辑字段白名单：缺失即不变，description / logoUrl 传空串表示清空 */
export interface UpdateAppBasicInfoPayload {
  clientName?: string
  description?: string
  logoUrl?: string
  redirectUris?: string[]
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
  /** 应用管理员身份（门户只读展示，切换入口只在 admin 端） */
  isAdmin: boolean
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

export interface SessionItem {
  sid: string
  ip: string
  userAgent: string | null
  createdAt: string
  lastSeenAt: string
  current: boolean
}

export interface ChangePasswordPayload {
  oldPassword: string
  newPassword: string
}

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

/** 上传确认后的文件记录 */
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
