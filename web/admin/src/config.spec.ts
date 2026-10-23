import { describe, expect, it } from 'vitest'
import { portalLoginUrl } from './config'

/**
 * admin 是独立构建产物、独立域名（admin.<主域>），门户登录页地址必须是门户域，
 * 且不得拼出 `/admin` 冗余段（007 FR-004 / SC-002）。
 */
describe('portalLoginUrl（007 FR-004）', () => {
  it('主动登出（无 next）带 loggedOut=1，且不带 next 参数', () => {
    const url = portalLoginUrl()

    expect(url.endsWith('/login?loggedOut=1')).toBe(true)
    expect(url).not.toContain('next=')
    expect(url).not.toContain('/admin')
  })

  it('会话失效回跳把 next 转成绝对地址（当前 admin 域 origin + 路径）', () => {
    const url = portalLoginUrl('/clients/demo-app/roles')
    const next = new URL(url).searchParams.get('next')

    expect(next).toBe(`${window.location.origin}/clients/demo-app/roles`)
    expect(url).not.toContain('/admin')
  })

  it('登录地址本身不含 /admin 段', () => {
    const base = new URL(portalLoginUrl('/users'))

    expect(base.pathname).toBe('/login')
    expect(base.pathname).not.toContain('/admin')
  })
})
