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
}

export interface CreateClientPayload {
  clientName: string
  description?: string
  logoUrl?: string
  redirectUris: string[]
  grantTypes: string[]
  scopes: string[]
  isPublic: boolean
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
