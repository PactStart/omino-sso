import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/client'
import type { AppDetail } from '@/api/types'

const mocks = vi.hoisted(() => ({
  getAppDetail: vi.fn(),
  updateAppBasicInfo: vi.fn(),
  setAppLogo: vi.fn(),
  clearAppLogo: vi.fn(),
}))

vi.mock('@/api/app', () => mocks)
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { clientId: 'demo-app' } }) }))

import Overview from './Overview.vue'

const detail: AppDetail = {
  clientId: 'demo-app',
  clientName: '验收应用',
  description: '内部验收用',
  logoUrl: 'https://cdn.example.com/logo/demo.png',
  redirectUris: ['https://demo.example.com/cb'],
  postLogoutUris: ['https://demo.example.com/signed-out'],
  grantTypes: ['authorization_code', 'refresh_token'],
  scopes: ['openid', 'profile'],
  isPublic: false,
  accessTokenTtl: 900,
  refreshTokenTtl: 2592000,
  status: 1,
  createdAt: '2026-10-20T10:00:00Z',
  updatedAt: 1792540000000,
  memberCount: 12,
}

async function mountOverview() {
  const wrapper = mount(Overview, {
    // ImageUpload 依赖直传凭证与真实文件选择，这里用可点击的替身模拟「上传成功」事件。
    global: {
      stubs: {
        ImageUpload: {
          template:
            '<button type="button" class="stub-logo" @click="$emit(\'success\', \'https://cdn.example.com/logo/new.png\')">logo</button>',
        },
      },
    },
  })
  await flushPromises()
  return wrapper
}

function findButton(wrapper: ReturnType<typeof mount>, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`未找到按钮：${label}`)
  return btn
}

describe('门户 · 应用信息页（006 口径调整）', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((fn) => fn.mockReset())
    mocks.getAppDetail.mockResolvedValue({ ...detail })
    mocks.updateAppBasicInfo.mockResolvedValue({ ...detail })
  })

  it('只读展示回调地址之外的配置，且不出现启停开关与 TTL 输入框', async () => {
    const wrapper = await mountOverview()

    expect(mocks.getAppDetail).toHaveBeenCalledWith('demo-app')
    const text = wrapper.text()
    expect(text).toContain('https://demo.example.com/signed-out')
    expect(text).toContain('15 分钟')
    expect(text).toContain('30 天')
    expect(text).toContain('启用')
    // 只读字段不得是控件（状态 / TTL 只在 admin 端可改）
    expect(wrapper.find('.el-switch').exists()).toBe(false)
    expect(wrapper.find('.el-input-number').exists()).toBe(false)
  })

  it('保存只提交白名单四字段 + 版本号', async () => {
    const wrapper = await mountOverview()

    await findButton(wrapper, '保存').trigger('click')
    await flushPromises()

    expect(mocks.updateAppBasicInfo).toHaveBeenCalledTimes(1)
    const [clientId, payload] = mocks.updateAppBasicInfo.mock.calls[0]
    expect(clientId).toBe('demo-app')
    expect(payload).toEqual({
      clientName: '验收应用',
      description: '内部验收用',
      logoUrl: 'https://cdn.example.com/logo/demo.png',
      redirectUris: ['https://demo.example.com/cb'],
      updatedAt: detail.updatedAt,
    })
    expect(payload).not.toHaveProperty('status')
    expect(payload).not.toHaveProperty('accessTokenTtl')
    expect(payload).not.toHaveProperty('postLogoutUris')
  })

  it('上传 Logo 后保存使用最新版本号，不再被判 409', async () => {
    mocks.getAppDetail
      .mockResolvedValueOnce({ ...detail })
      .mockResolvedValueOnce({
        ...detail,
        logoUrl: 'https://cdn.example.com/logo/new.png',
        updatedAt: detail.updatedAt + 1000,
      })
    const wrapper = await mountOverview()

    await wrapper.find('.stub-logo').trigger('click')
    await flushPromises()
    // Logo 落库会刷新 clients.updated_at，页面必须重新取版本号
    expect(mocks.getAppDetail).toHaveBeenCalledTimes(2)

    await findButton(wrapper, '保存').trigger('click')
    await flushPromises()

    expect(mocks.updateAppBasicInfo).toHaveBeenCalledWith(
      'demo-app',
      expect.objectContaining({
        logoUrl: 'https://cdn.example.com/logo/new.png',
        updatedAt: detail.updatedAt + 1000,
      }),
    )
  })

  it('保存遇到 409 冲突时提示并重新拉取回滚表单', async () => {
    mocks.updateAppBasicInfo.mockRejectedValueOnce(
      new ApiError('APP_RESOURCE_STALE', '已被他人修改', 409),
    )
    const wrapper = await mountOverview()

    await wrapper.get('textarea').setValue('我本地改的描述')
    await findButton(wrapper, '保存').trigger('click')
    await flushPromises()

    // 冲突必须当失败处理：页内提示 + 重新拉详情（初始 1 次 + 冲突后 1 次）
    expect(wrapper.text()).toContain('已被他人修改，请刷新后重试')
    expect(mocks.getAppDetail).toHaveBeenCalledTimes(2)
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('内部验收用')
  })

  it('回调地址为空时拦截保存并提示', async () => {
    mocks.getAppDetail.mockResolvedValue({ ...detail, redirectUris: [] })
    const wrapper = await mountOverview()

    await findButton(wrapper, '保存').trigger('click')
    await flushPromises()

    expect(mocks.updateAppBasicInfo).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('至少填写 1 条回调地址')
  })
})
