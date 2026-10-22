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
