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
  detail: Record<string, unknown> | null
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
