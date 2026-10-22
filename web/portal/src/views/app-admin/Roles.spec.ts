import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, errorMessage } from '@/api/client'
import type { AppRole, Paged } from '@/api/types'

const mocks = vi.hoisted(() => ({
  listAppRoles: vi.fn(),
  createAppRole: vi.fn(),
  getAppRole: vi.fn(),
  updateAppRole: vi.fn(),
  deleteAppRole: vi.fn(),
}))

vi.mock('@/api/app', () => mocks)
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { clientId: 'demo-app' } }) }))

import Roles from './Roles.vue'

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

const role: AppRole = {
  id: '01M5HMP30000000000000000001',
  clientId: 'demo-app',
  name: 'opsAdmin',
  description: '运维管理员',
  permissionCount: 2,
  memberCount: 1,
  createdAt: '2026-10-20T10:00:00Z',
  updatedAt: 1792540000000,
}

describe('门户 · 应用角色页（006）', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((fn) => fn.mockReset())
    const paged: Paged<AppRole> = { list: [role], total: 1, page: 1, pageSize: 20 }
    mocks.listAppRoles.mockResolvedValue(paged)
  })

  it('按 clientId 拉取角色并渲染列表', async () => {
    const wrapper = mount(Roles)
    await flushPromises()

    expect(mocks.listAppRoles).toHaveBeenCalledWith('demo-app')
    expect(wrapper.text()).toContain('opsAdmin')
  })

  it('重名错误提示与 admin 端一致：透传后端文案（两端口径相同）', () => {
    // 006 起两端共用同一后端 handler；CUSTOM_MESSAGE 均不含 APPL 业务码，
    // 因此都透传后端 message —— 这就是 FR-006「同一操作提示一致」的实现基础。
    const err = new ApiError('APP_ROLE_NAME_EXISTS', '角色名已存在', 409)
    expect(errorMessage(err)).toBe('角色名已存在')
  })
})
