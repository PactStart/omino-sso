import { describe, expect, it, vi } from 'vitest'

import { nextLabel, resolveNext } from './redirect'

describe('回跳目标解析（四域 next 白名单）', () => {
  it('站内相对路径原样返回', () => {
    expect(resolveNext('/account/profile')).toEqual({ kind: 'relative', path: '/account/profile' })
    expect(resolveNext('/apps/01ABC?tab=roles')).toEqual({
      kind: 'relative',
      path: '/apps/01ABC?tab=roles',
    })
  })

  it('丢弃登录页自身，避免登录成功后原地打转', () => {
    expect(resolveNext('/login')).toBeNull()
    expect(resolveNext('/login?next=/x')).toBeNull()
  })

  it('丢弃服务不可用页（登录成功后不该落回报错页）', () => {
    expect(resolveNext('/offline')).toBeNull()
    expect(resolveNext('/offline?next=/account/profile')).toBeNull()
  })

  it('本站 origin 的绝对地址被接受（跨域回跳的基础）', () => {
    const url = `${window.location.origin}/account/profile`
    expect(resolveNext(url)).toEqual({ kind: 'absolute', url })
  })

  it('协议相对与非法协议一律丢弃（防开放重定向）', () => {
    expect(resolveNext('//evil.example.com/users')).toBeNull()
    expect(resolveNext('javascript:alert(1)')).toBeNull()
    expect(resolveNext('https://evil.example.com/users')).toBeNull()
    expect(resolveNext('')).toBeNull()
    expect(resolveNext(undefined)).toBeNull()
    expect(resolveNext(['/a'])).toBeNull()
  })

  it('信任 VITE_ADMIN_HOME 的 origin：admin 域的回跳才放行', async () => {
    vi.stubEnv('VITE_ADMIN_HOME', 'https://admin.example.com/users')
    vi.resetModules()
    const mod = await import('./redirect')

    expect(mod.resolveNext('https://admin.example.com/users')).toEqual({
      kind: 'absolute',
      url: 'https://admin.example.com/users',
    })
    expect(mod.resolveNext('https://admin.example.com/clients/01ABC?tab=members')).toEqual({
      kind: 'absolute',
      url: 'https://admin.example.com/clients/01ABC?tab=members',
    })
    // 同主域但不同 origin（portal 自己）也在白名单内
    expect(mod.resolveNext(`${window.location.origin}/apps`)).not.toBeNull()
    // 白名单外的域名仍然拒绝
    expect(mod.resolveNext('https://other.example.com/users')).toBeNull()

    vi.unstubAllEnvs()
    vi.resetModules()
  })

  it('提示文案：绝对地址只展示 pathname', () => {
    expect(nextLabel({ kind: 'relative', path: '/apps/01ABC' })).toBe('/apps/01ABC')
    expect(
      nextLabel({ kind: 'absolute', url: 'https://admin.example.com/clients/01ABC?tab=roles' }),
    ).toBe('/clients/01ABC')
    expect(nextLabel(null)).toBe('')
  })
})
