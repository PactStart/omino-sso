import { ADMIN_HOME } from '@/config'

/**
 * 登录后的回跳目标。
 *
 * 四域部署下 portal 与 admin 不同域，admin 传来的 `next` 必须是绝对地址，
 * 否则门户会把它当成自己的路由（`/users` 在门户上不存在）。
 */
export type NextTarget =
  | { kind: 'relative'; path: string }
  | { kind: 'absolute'; url: string }

/** 允许回跳的 origin：门户自身 + 管理后台（`VITE_ADMIN_HOME` 的 origin）。 */
function allowedOrigins(): string[] {
  const origins = [window.location.origin]
  try {
    origins.push(new URL(ADMIN_HOME, window.location.origin).origin)
  } catch {
    // 配置非法时只信任自身 origin
  }
  return origins
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
