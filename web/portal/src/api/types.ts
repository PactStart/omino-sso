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

export interface LoginPayload {
  username: string
  password: string
  clientId: string
  rememberMe: boolean
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
