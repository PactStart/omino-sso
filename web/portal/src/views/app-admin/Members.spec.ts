import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppMember, Paged } from '@/api/types'

const mocks = vi.hoisted(() => ({
  listAppMembers: vi.fn(),
  listAppMemberIds: vi.fn(),
  listMemberCandidates: vi.fn(),
  listCandidateIds: vi.fn(),
  addAppMembers: vi.fn(),
  addAppMembersByDept: vi.fn(),
  removeAppMembers: vi.fn(),
  listOrgTree: vi.fn(),
}))

vi.mock('@/api/app', () => mocks)
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { clientId: 'demo-app' } }) }))

import Members from './Members.vue'

// el-table 依赖 ResizeObserver，jsdom 未实现。
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

function page(list: AppMember[]): Paged<AppMember> {
  return { list, total: list.length, page: 1, pageSize: 20 }
}

const adminMember: AppMember = {
  userId: '01M5HMP2RCCP3CGT4ZX3Z3Y6ZW',
  username: 'zhangsan',
  displayName: '张三',
  deptId: 'd1',
  deptName: '技术中心',
  status: 1,
  joinedAt: '2026-10-20T10:00:00Z',
  roleCount: 2,
  isAdmin: true,
}

const plainMember: AppMember = {
  ...adminMember,
  userId: '01M5HMP2WY4K6449YWWJDXGNMQ',
  username: 'lisi',
  displayName: '李四',
  isAdmin: false,
}

async function mountMembers() {
  const wrapper = mount(Members)
  await flushPromises()
  return wrapper
}

describe('门户 · 应用成员页（006）', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((fn) => fn.mockReset())
    mocks.listAppMembers.mockResolvedValue(page([adminMember, plainMember]))
    mocks.listOrgTree.mockResolvedValue([])
  })

  it('按 clientId 拉取成员并渲染表格', async () => {
    const wrapper = await mountMembers()

    expect(mocks.listAppMembers).toHaveBeenCalledWith(
      'demo-app',
      expect.objectContaining({ page: 1, pageSize: 20 }),
    )
    const text = wrapper.text()
    expect(text).toContain('zhangsan')
    expect(text).toContain('lisi')
  })

  it('「身份」列只读展示应用管理员 / 普通成员', async () => {
    const wrapper = await mountMembers()
    const text = wrapper.text()

    expect(text).toContain('应用管理员')
    expect(text).toContain('普通成员')
  })

  it('门户不提供「设为管理员 / 取消管理员」入口（FR-007 / US3-AC6）', async () => {
    const wrapper = await mountMembers()
    const text = wrapper.text()

    expect(text).not.toContain('设为管理员')
    expect(text).not.toContain('取消管理员')
    // 页面里任何按钮都不得承载该能力
    const buttonTexts = wrapper.findAll('button').map((b) => b.text())
    expect(buttonTexts.some((t) => t.includes('管理员'))).toBe(false)
  })
})
