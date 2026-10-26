import { ADMIN_HOME, SSO_BASE_URL } from '@/config'

/**
 * 登录后的回跳目标。
 *
 * 四域部署下 portal 与 admin 不同域，admin 传来的 `next` 必须是绝对地址，
 * 否则门户会把它当成自己的路由（`/users` 在门户上不存在）。
 */
export type NextTarget =
  | { kind: 'relative'; path: string }
  | { kind: 'absolute'; url: string }

/** 统一身份登录域的 origin；未配置或非法时返回 null（不加入白名单）。 */
function ssoOrigin(): string | null {
  if (!SSO_BASE_URL) return null
  try {
    return new URL(SSO_BASE_URL).origin
  } catch {
    return null
  }
}

/** 允许回跳的 origin：门户自身 + 管理后台（`VITE_ADMIN_HOME`）+ 统一身份登录域（`VITE_SSO_BASE_URL`）。 */
function allowedOrigins(): string[] {
  const origins = [window.location.origin]
  try {
    origins.push(new URL(ADMIN_HOME, window.location.origin).origin)
  } catch {
    // 配置非法时只信任自身 origin
  }
  const sso = ssoOrigin()
  if (sso && !origins.includes(sso)) origins.push(sso)
  return origins
}

/**
 * 目标是否指向统一身份登录入口（未登录 SSO 的回跳）。
 *
 * 只用于文案：指向 IdP 时不展示 `/login` 这类技术路径，改为展示来源应用名。
 */
export function isSsoTarget(target: NextTarget | null): boolean {
  if (!target || target.kind !== 'absolute') return false
  const sso = ssoOrigin()
  if (!sso) return false
  try {
    return new URL(target.url).origin === sso
  } catch {
    return false
  }
}

/**
 * 来源应用名（IdP 下发的 `?app=`）。
 *
 * 仅作文本展示：去控制字符、超长截断；空/非法一律返回空串（调用方回落固定文案）。
 * 绝不参与跳转决策，也不拼进任何 URL 或 HTML 属性。
 */
export function appLabel(raw: unknown): string {
  if (typeof raw !== 'string') return ''
  const cleaned = raw.replace(/[\u0000-\u001F\u007F]/g, '').trim()
  if (cleaned === '') return ''
  return cleaned.length > 64 ? `${cleaned.slice(0, 64)}…` : cleaned
}

/**
 * 解析 `?next=`。
 *
 * 只接受两类：① 站内相对路径（以单个 `/` 开头的绝对路径）；② origin 在白名单内的绝对地址。
 * `//evil.com`（协议相对）、`javascript:`、白名单外的域名一律丢弃 —— 否则就是开放重定向。
 * 登录页自身（`/login*`）与服务不可用页（`/offline*`）也丢弃：登录成功后停在登录页是原地打转，
 * 停在 `/offline` 则会把"刚恢复可用"的用户带回一张报错页（contracts/admin-routes.md §4）。
 */
export function resolveNext(raw: unknown): NextTarget | null {
  if (typeof raw !== 'string') return null
  const value = raw.trim()
  if (value === '') return null

  if (value.startsWith('/')) {
    if (value.startsWith('//') || value.startsWith('/login') || value.startsWith('/offline')) {
      return null
    }
    return { kind: 'relative', path: value }
  }

  let url: URL
  try {
    url = new URL(value)
  } catch {
    return null
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return null
  return allowedOrigins().includes(url.origin) ? { kind: 'absolute', url: url.toString() } : null
}

/** 回跳提示文案：绝对地址只展示 pathname，避免整条 URL 撑爆副标题。 */
export function nextLabel(target: NextTarget | null): string {
  if (!target) return ''
  if (target.kind === 'relative') return target.path
  try {
    return new URL(target.url).pathname
  } catch {
    return ''
  }
}
