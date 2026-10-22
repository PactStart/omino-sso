import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/client'
import type { AppManageContext } from '@/api/types'

const getManageContextMock = vi.fn()
const pushMock = vi.fn()
const replaceMock = vi.fn()

const routeState: { params: Record<string, string>; path: string } = {
  params: { clientId: 'demo-app' },
  path: '/account/apps/demo-app/manage/members',
}

vi.mock('@/api/app', () => ({ getManageContext: (id: string) => getManageContextMock(id) }))
vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ push: pushMock, replace: replaceMock }),
}))

import AppAdminLayout from './AppAdminLayout.vue'

const ctx: AppManageContext = {
  clientId: 'demo-app',
  name: '验收应用',
  description: '内部验收用',
  logoUrl: '',
  status: 1,
  memberCount: 12,
}

function mountLayout() {
  return mount(AppAdminLayout, {
    global: { stubs: { RouterView: { template: '<div class="stub-view" />' } } },
  })
}

function findButton(wrapper: VueWrapper, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`未找到按钮：${label}`)
  return btn
}

describe('门户应用管理壳（006）', () => {
  beforeEach(() => {
    getManageContextMock.mockReset()
    pushMock.mockReset()
    replaceMock.mockReset()
    routeState.path = '/account/apps/demo-app/manage/members'
  })

  it('无权限时给出明确提示与返回入口，而不是空白页（FR-016）', async () => {
    getManageContextMock.mockRejectedValue(new ApiError('FORBIDDEN', '无权限执行该操作', 403))
    const wrapper = mountLayout()
    await flushPromises()

    expect(wrapper.text()).toContain('无权限管理该应用')
    expect(findButton(wrapper, '返回我的应用').exists()).toBe(true)
    expect(wrapper.find('.skeleton').exists()).toBe(false)
  })

  it('有权限时展示应用摘要与五个 tab（应用信息页承载基础信息，密钥仍不可见）', async () => {
    getManageContextMock.mockResolvedValue(ctx)
    const wrapper = mountLayout()
    await flushPromises()

    expect(getManageContextMock).toHaveBeenCalledWith('demo-app')
    expect(wrapper.text()).toContain('验收应用')
    expect(wrapper.text()).toContain('共 12 名成员')
    for (const label of ['应用信息', '成员', '角色', '权限', '授权']) {
      expect(findButton(wrapper, label).exists()).toBe(true)
    }
    // 管理壳本身不渲染应用配置与密钥；密钥入口在门户任何页面都不得出现（FR-007）
    expect(wrapper.text()).not.toContain('密钥')
    expect(wrapper.text()).not.toContain('设为管理员')
  })

  it('切换 tab 通过 router 替换地址，保持同一 clientId', async () => {
    getManageContextMock.mockResolvedValue(ctx)
    const wrapper = mountLayout()
    await flushPromises()

    await findButton(wrapper, '角色').trigger('click')
    expect(replaceMock).toHaveBeenCalledWith('/account/apps/demo-app/manage/roles')
  })
})
