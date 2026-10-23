import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMenuItem } from 'element-plus'
import type { ComponentCustomProperties } from 'vue'
import type { Router } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ClientDetail as ClientDetailData } from '@/api/types'

/**
 * 应用详情的左侧五菜单：顺序固定（FR-001），地址为 `/clients/:id/<tab>`，
 * 非法 tab 由路由兜底回「应用详情」（见 src/router.ts 的 client-tab-fallback）。
 */
const pushMock = vi.fn<(path: string) => Promise<void>>(() => Promise.resolve())
const getClientMock = vi.fn()

const routeState: { params: Record<string, string>; path: string } = {
  params: { clientId: 'demo-app', tab: 'roles' },
  path: '/clients/demo-app/roles',
}

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ push: pushMock }),
}))

vi.mock('@/api', () => ({ getClient: (id: string) => getClientMock(id) }))

import ClientDetail from './ClientDetail.vue'

const detail: ClientDetailData = {
  clientId: 'demo-app',
  clientName: '验收应用',
  description: '内部验收用',
  logoUrl: '',
  redirectUris: ['https://demo.example.com/callback'],
  postLogoutUris: [],
  grantTypes: ['authorization_code'],
  scopes: ['openid', 'profile'],
  isPublic: false,
  accessTokenTtl: 3600,
  refreshTokenTtl: 86400,
  status: 1,
  createdAt: '2026-10-20T10:00:00Z',
  updatedAt: 1792540000000,
  memberCount: 12,
}

function mountDetail() {
  return mount(ClientDetail, {
    global: {
      plugins: [ElementPlus],
      config: {
        globalProperties: { $router: { push: pushMock } as unknown as Router } as ComponentCustomProperties,
      },
      stubs: { AdminLayout: { template: '<div><slot /></div>' }, RouterView: { template: '<div />' } },
    },
  })
}

function tabIndexes(wrapper: ReturnType<typeof mountDetail>): string[] {
  return wrapper.findAllComponents(ElMenuItem).map((item) => String(item.props('index')))
}

describe('应用详情 · 左侧五菜单（FR-001 / 007 FR-004）', () => {
  beforeEach(() => {
    pushMock.mockClear()
    getClientMock.mockReset()
    getClientMock.mockResolvedValue(detail)
    routeState.params = { clientId: 'demo-app', tab: 'roles' }
  })

  it('五个 tab 的 index 挂在 /clients/:id 下，顺序固定不可调整', async () => {
    const wrapper = mountDetail()
    await flushPromises()

    expect(tabIndexes(wrapper)).toEqual([
      '/clients/demo-app/overview',
      '/clients/demo-app/members',
      '/clients/demo-app/roles',
      '/clients/demo-app/permissions',
      '/clients/demo-app/grants',
    ])
    expect(wrapper.findAllComponents(ElMenuItem).map((item) => item.text())).toEqual([
      '应用详情',
      '应用成员',
      '应用角色',
      '应用权限',
      '授权管理',
    ])
  })

  it('tab index 不含 /admin 前缀，切换由 router 携带同一 clientId', async () => {
    const wrapper = mountDetail()
    await flushPromises()

    for (const index of tabIndexes(wrapper)) {
      expect(index.startsWith('/admin')).toBe(false)
    }

    await wrapper.findAllComponents(ElMenuItem)[1]!.trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/clients/demo-app/members')
  })

  it('非法 tab 高亮回落到「应用详情」，但地址不被改写', async () => {
    routeState.params = { clientId: 'demo-app', tab: 'unknown' }
    const wrapper = mountDetail()
    await flushPromises()

    const menu = wrapper.findComponent({ name: 'ElMenu' })
    expect(menu.props('defaultActive')).toBe('/clients/demo-app/overview')
  })
})
