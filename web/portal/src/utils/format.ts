/** RFC3339(UTC) → 本地时区 YYYY-MM-DD HH:mm；null/非法值 → "-"（PRD §3.5） */
export function formatTime(value?: string | null): string {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '-'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** 姓名首字作为头像占位 */
export function initialOf(name?: string | null): string {
  const text = (name ?? '').trim()
  return text ? text.slice(0, 1).toUpperCase() : '?'
}

/** 空串/null 统一显示占位符 */
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
