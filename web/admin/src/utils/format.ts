/** RFC3339(UTC) → 本地时区 YYYY-MM-DD HH:mm（PRD §3.5） */
export function formatTime(value?: string | null): string {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function orDash(value?: string | null): string {
  const text = (value ?? '').trim()
  return text || '-'
}

export const ROLE_LABEL: Record<string, string> = {
  admin: '管理员',
  user: '普通用户',
}

export function roleLabel(role: string): string {
  return ROLE_LABEL[role] ?? role
}

export const STATUS_LABEL: Record<string, string> = {
  active: '正常',
  disabled: '停用',
  locked: '锁定',
}

export function statusLabel(status: string): string {
  return STATUS_LABEL[status] ?? status
}

export function statusTagType(status: string): 'success' | 'info' | 'warning' | 'danger' {
  if (status === 'active') return 'success'
  if (status === 'disabled') return 'info'
  if (status === 'locked') return 'warning'
  return 'danger'
}

/** 审计事件枚举 → 中文（PRD §4 A3） */
export const EVENT_LABEL: Record<string, string> = {
  login_success: '登录成功',
  login_failure: '登录失败',
  mfa_success: 'MFA 成功',
  mfa_failure: 'MFA 失败',
  logout: '登出',
  password_changed: '修改密码',
  token_issued: '令牌签发',
  token_revoked: '令牌撤销',
  code_reused: '授权码重放',
  refresh_reused: '刷新令牌重放',
  user_created: '创建用户',
  user_updated: '更新用户',
  client_created: '注册应用',
  client_secret_rotated: '轮换密钥',
  org_created: '创建组织',
  org_updated: '更新组织',
  org_deleted: '删除组织',
  client_updated: '更新应用',
  app_member_added: '添加应用成员',
  app_member_removed: '移除应用成员',
  app_role_created: '创建应用角色',
  app_role_updated: '更新应用角色',
  app_role_deleted: '删除应用角色',
  app_permission_created: '创建应用权限',
  app_permission_updated: '更新应用权限',
  app_permission_deleted: '删除应用权限',
  app_grant_updated: '变更应用授权',
}

export function eventLabel(eventType: string): string {
  return EVENT_LABEL[eventType] ?? eventType
}
