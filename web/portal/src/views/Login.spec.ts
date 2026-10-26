import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/api/client'

const getCaptchaMock = vi.fn()
const loginMock = vi.fn()
const sendCodeMock = vi.fn()
const replaceMock = vi.fn()
/** 各用例按需写入 query（登出提示、next 回跳都从这里来） */
const routeQuery: Record<string, string> = {}

vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: replaceMock, push: replaceMock }),
  useRoute: () => ({ query: routeQuery }),
}))

vi.mock('@/stores/session', () => ({
  // 登录成功后 goAfterAuth 会强制刷新一次会话；返回普通用户，走默认落地分支
  useSessionStore: () => ({
    reset: vi.fn(),
    fetchMe: () => Promise.resolve({ id: 'u1', username: 'alice', displayName: 'Alice', roles: ['user'] }),
  }),
}))

vi.mock('@/api', () => ({
  getCaptcha: (...args: unknown[]) => getCaptchaMock(...args),
  login: (...args: unknown[]) => loginMock(...args),
  sendCode: (...args: unknown[]) => sendCodeMock(...args),
  resetPassword: vi.fn(),
  verifyMfa: vi.fn(),
}))

import Login from './Login.vue'

function findButton(wrapper: ReturnType<typeof mount>, label: string) {
  const button = wrapper.findAll('button').find((b) => b.text() === label)
  if (!button) throw new Error(`未找到按钮：${label}`)
  return button
}

describe('Login 登录方式切换', () => {
  beforeEach(() => {
    getCaptchaMock.mockResolvedValue({
      captchaId: 'cid-1',
      image: 'data:image/png;base64,iVBORw0KGgo=',
      expiresIn: 120,
    })
    sendCodeMock.mockResolvedValue({ expiresIn: 300, resendAfter: 60 })
  })

  it('切换到验证码登录时拉取图形验证码并要求输入', async () => {
    const wrapper = mount(Login)

    // 密码登录不展示图形验证码
    expect(wrapper.find('#login-captcha').exists()).toBe(false)

    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    expect(getCaptchaMock).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#login-captcha').exists()).toBe(true)
    expect(wrapper.find('img[alt="图形验证码"]').attributes('src')).toContain('data:image/png')
  })

  it('未填图形验证码时不发送短信/邮箱验证码', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    await wrapper.find('#username').setValue('alice@example.com')
    await findButton(wrapper, '获取验证码').trigger('click')
    await flushPromises()

    expect(sendCodeMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('请先输入图形验证码')
  })

  it('填写图形验证码后发送登录验证码会带上 captchaId 与 captchaCode', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    await wrapper.find('#username').setValue('alice@example.com')
    await wrapper.find('#login-captcha').setValue('ab12')
    await findButton(wrapper, '获取验证码').trigger('click')
    await flushPromises()

    expect(sendCodeMock).toHaveBeenCalledWith(
      expect.objectContaining({
        scene: 'login',
        channel: 'email',
        target: 'alice@example.com',
        captchaId: 'cid-1',
        captchaCode: 'ab12',
      }),
    )
  })

  it('图形验证码失效时提示并换一张', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    await wrapper.find('#username').setValue('alice@example.com')
    await wrapper.find('#login-captcha').setValue('zzzz')
    sendCodeMock.mockRejectedValueOnce(new ApiError('AUTH_CAPTCHA_INVALID', '图形验证码错误或已过期', 400))

    await findButton(wrapper, '获取验证码').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('图形验证码错误或已过期')
    // 一次性使用：失败后需重新拉取
    expect(getCaptchaMock).toHaveBeenCalledTimes(2)
  })

  it('发送成功后自动换一张图形验证码（服务端已消费）', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    await wrapper.find('#username').setValue('alice@example.com')
    await wrapper.find('#login-captcha').setValue('ab12')

    await findButton(wrapper, '获取验证码').trigger('click')
    await flushPromises()

    expect(sendCodeMock).toHaveBeenCalledTimes(1)
    // 首次进入验证码登录 + 发送成功后的刷新
    expect(getCaptchaMock).toHaveBeenCalledTimes(2)
  })
})

describe('Login 交互细节', () => {
  beforeEach(() => {
    getCaptchaMock.mockResolvedValue({
      captchaId: 'cid-3',
      image: 'data:image/png;base64,iVBORw0KGgo=',
      expiresIn: 120,
    })
    sendCodeMock.mockResolvedValue({ expiresIn: 300, resendAfter: 60 })
  })

  it('切换到邮箱验证码时清空非邮箱格式的账号', async () => {
    const wrapper = mount(Login)
    await wrapper.find('#username').setValue('张三')

    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    expect((wrapper.find('#username').element as HTMLInputElement).value).toBe('')
  })

  it('切换到邮箱验证码时保留已填的邮箱（无需重输）', async () => {
    const wrapper = mount(Login)
    await wrapper.find('#username').setValue('alice@example.com')

    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    expect((wrapper.find('#username').element as HTMLInputElement).value).toBe('alice@example.com')
  })

  it('切换到手机验证码时清空邮箱格式的账号', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()
    await wrapper.find('#username').setValue('alice@example.com')

    await findButton(wrapper, '手机验证码').trigger('click')
    await flushPromises()

    expect((wrapper.find('#username').element as HTMLInputElement).value).toBe('')
  })

  it('重新输入图形验证码后旧错误立即消失', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()
    await wrapper.find('#username').setValue('alice@example.com')
    await wrapper.find('#login-captcha').setValue('zzzz')
    sendCodeMock.mockRejectedValueOnce(new ApiError('AUTH_CAPTCHA_INVALID', '图形验证码错误或已过期', 400))

    await findButton(wrapper, '获取验证码').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('图形验证码错误或已过期')

    await wrapper.find('#login-captcha').setValue('ab12')
    expect(wrapper.text()).not.toContain('图形验证码错误或已过期')
  })

  it('发送成功用轻提示展示，并在数秒后自动消失', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mount(Login)
      await findButton(wrapper, '邮箱验证码').trigger('click')
      await vi.advanceTimersByTimeAsync(0)
      await wrapper.find('#username').setValue('alice@example.com')
      await wrapper.find('#login-captcha').setValue('ab12')

      await findButton(wrapper, '获取验证码').trigger('click')
      await vi.advanceTimersByTimeAsync(0)

      expect(wrapper.text()).toContain('验证码已发送至 a***@example.com')
      await vi.advanceTimersByTimeAsync(3500)
      expect(wrapper.text()).not.toContain('验证码已发送至')
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('Login 验证码登录失败提示', () => {
  beforeEach(() => {
    getCaptchaMock.mockResolvedValue({
      captchaId: 'cid-2',
      image: 'data:image/png;base64,iVBORw0KGgo=',
      expiresIn: 120,
    })
    sendCodeMock.mockResolvedValue({ expiresIn: 300, resendAfter: 60 })
  })

  it('验证码登录失败时给出绑定指引而不是「密码错误」', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    await wrapper.find('#username').setValue('nobody@example.com')
    await wrapper.find('#login-captcha').setValue('ab12')
    await wrapper.find('#login-code').setValue('123456')
    loginMock.mockRejectedValueOnce(
      new ApiError('AUTH_INVALID_CREDENTIALS', '用户名或密码错误', 401),
    )

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('验证码登录失败')
    expect(text).toContain('账号中心绑定')
    expect(text).not.toContain('用户名或密码错误')
  })

  it('验证码登录失败后自动换一张图形验证码，重试无需再失败一次', async () => {
    const wrapper = mount(Login)
    await findButton(wrapper, '邮箱验证码').trigger('click')
    await flushPromises()

    await wrapper.find('#username').setValue('alice@example.com')
    await wrapper.find('#login-captcha').setValue('ab12')
    await wrapper.find('#login-code').setValue('111111')
    loginMock.mockRejectedValueOnce(new ApiError('AUTH_CODE_INVALID', '验证码错误或已过期', 400))

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('验证码错误或已过期')
    // 首次进入验证码登录 + 提交失败后的刷新（服务端已消费）
    expect(getCaptchaMock).toHaveBeenCalledTimes(2)
  })
})

describe('Login 登出提示（FR-023）', () => {
  beforeEach(() => {
    for (const key of Object.keys(routeQuery)) delete routeQuery[key]
    replaceMock.mockClear()
  })

  it('带 ?loggedOut=1 时显示「已退出」并清除该参数', async () => {
    routeQuery.loggedOut = '1'

    const wrapper = mount(Login)
    await flushPromises()

    expect(wrapper.text()).toContain('已退出登录')
    // 参数必须被清掉：刷新或前进后退不应重复弹提示，也不能把 loggedOut 带进回跳地址
    expect(replaceMock).toHaveBeenCalledWith({ path: '/login', query: {} })
  })

  it('没有该参数时不显示登出提示', async () => {
    const wrapper = mount(Login)
    await flushPromises()

    expect(wrapper.text()).not.toContain('已退出登录')
    expect(replaceMock).not.toHaveBeenCalled()
  })
})

describe('Login 登录后回跳（FR-009 / plan D4）', () => {
  const realOrigin = window.location.origin
  const assignMock = vi.fn()
  const locationReplaceMock = vi.fn()
  let locationDescriptor: PropertyDescriptor | undefined

  beforeEach(() => {
    for (const key of Object.keys(routeQuery)) delete routeQuery[key]
    replaceMock.mockReset()
    assignMock.mockReset()
    locationReplaceMock.mockReset()
    loginMock.mockReset()
    loginMock.mockResolvedValue({
      mfaRequired: false,
      mfaTicket: null,
      expiresIn: 0,
      redirectTo: '/account',
      user: { id: 'u1', username: 'alice', displayName: 'Alice' },
    })

    // jsdom 把 location.assign 定义为不可重定义的 own property，直接 spy 会抛
    // "Cannot redefine property: assign"；整体替换 location 才能观察到「整页跳转」分支。
    locationDescriptor = Object.getOwnPropertyDescriptor(window, 'location')
    Object.defineProperty(window, 'location', {
      configurable: true,
      writable: true,
      value: {
        origin: realOrigin,
        href: `${realOrigin}/login`,
        pathname: '/login',
        search: '',
        assign: assignMock,
        replace: locationReplaceMock,
      },
    })
  })

  afterEach(() => {
    if (locationDescriptor) Object.defineProperty(window, 'location', locationDescriptor)
    vi.unstubAllEnvs()
    vi.resetModules()
    for (const key of Object.keys(routeQuery)) delete routeQuery[key]
  })

  async function loginWithPassword(component: typeof Login) {
    const wrapper = mount(component)
    await wrapper.find('#username').setValue('alice')
    await wrapper.find('#password').setValue('S3cure!pass2026')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    return wrapper
  }

  it('admin 域的绝对 next 走整页跳转，不经过 router.replace（跨域回跳）', async () => {
    // ADMIN_HOME 决定白名单里第二个 origin，必须在模块加载前注入（config 在求值时读取）
    vi.stubEnv('VITE_ADMIN_HOME', 'https://admin.example.com/')
    vi.resetModules()
    const { default: LoginFresh } = await import('./Login.vue')
    routeQuery.next = 'https://admin.example.com/clients/01ABC?tab=members'

    await loginWithPassword(LoginFresh as typeof Login)

    expect(assignMock).toHaveBeenCalledWith('https://admin.example.com/clients/01ABC?tab=members')
    // 绝对地址绝不能进 SPA 路由（门户域上没有该路由）
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('站内相对 next 走 router.replace，不改写成外域', async () => {
    routeQuery.next = '/account/apps'

    await loginWithPassword(Login)

    expect(replaceMock).toHaveBeenCalledWith('/account/apps')
    expect(assignMock).not.toHaveBeenCalled()
    expect(locationReplaceMock).not.toHaveBeenCalled()
  })

  it('白名单外的绝对 next 被丢弃，落到默认页（防开放重定向）', async () => {
    routeQuery.next = 'https://evil.example.com/users'

    await loginWithPassword(Login)

    expect(assignMock).not.toHaveBeenCalled()
    expect(replaceMock).toHaveBeenCalledWith('/account/profile')
  })
})

describe('SSO 落点副标题（009）', () => {
  it('next 指向统一身份登录域时显示来源应用名，且不暴露技术路径', async () => {
    vi.resetModules()
    const { default: LoginFresh } = await import('./Login.vue')
    routeQuery.next = 'http://localhost:8080/login?id=01ABC&portal=1'
    routeQuery.app = '合规平台'

    const wrapper = mount(LoginFresh)
    const text = wrapper.text()
    expect(text).toContain('登录后返回 合规平台')
    expect(text).not.toContain('/login')

    delete routeQuery.next
    delete routeQuery.app
  })

  it('app 缺失时回落固定文案，同样不展示 /login', async () => {
    vi.resetModules()
    const { default: LoginFresh } = await import('./Login.vue')
    routeQuery.next = 'http://localhost:8080/login?id=01ABC&portal=1'

    const wrapper = mount(LoginFresh)
    const text = wrapper.text()
    expect(text).toContain('登录后返回 所访问的应用')
    expect(text).not.toContain('/login')

    delete routeQuery.next
  })
})
