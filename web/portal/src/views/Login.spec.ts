import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/api/client'

const getCaptchaMock = vi.fn()
const loginMock = vi.fn()
const sendCodeMock = vi.fn()
const replaceMock = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: replaceMock, push: replaceMock }),
  useRoute: () => ({ query: {} }),
}))

vi.mock('@/stores/session', () => ({
  useSessionStore: () => ({ reset: vi.fn(), fetchMe: vi.fn() }),
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
