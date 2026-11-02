import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppMember, MemberPage } from '@/api/types'

const mocks = vi.hoisted(() => ({
  listAppMembers: vi.fn(),
  listAppMemberIds: vi.fn(),
  listMemberCandidates: vi.fn(),
  listCandidateIds: vi.fn(),
  addAppMembers: vi.fn(),
  removeAppMembers: vi.fn(),
  listOrgTree: vi.fn(),
  listOrgGrants: vi.fn(),
  addOrgGrants: vi.fn(),
  removeOrgGrants: vi.fn(),
}))

/** ElMessageBox.confirm 的替身：由每个用例决定「确认」还是「取消」 */
const confirmMock = vi.hoisted(() => vi.fn())

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>()
  return { ...actual, ElMessageBox: { ...actual.ElMessageBox, confirm: confirmMock } }
})
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

function page(list: AppMember[]): MemberPage {
  return {
    list,
    total: list.length,
    page: 1,
    pageSize: 20,
    visibleTotal: 40,
    orgGrantHints: [],
  }
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
  orgGrants: [{ orgId: 'd1', deptName: '技术中心', includeSubDepts: true }],
}

const plainMember: AppMember = {
  ...adminMember,
  userId: '01M5HMP2WY4K6449YWWJDXGNMQ',
  username: 'lisi',
  displayName: '李四',
  isAdmin: false,
}

/** 纯手动成员：移除时不应出现「仍可通过组织授权」提示（FR-017） */
const manualOnlyMember: AppMember = {
  ...adminMember,
  userId: '01M5HMP300000000000000000A',
  username: 'wangwu',
  displayName: '王五',
  isAdmin: false,
  orgGrants: [],
}

/** 命中 3 条组织授权：来源列折叠为「手动 + 3 个组织」（FR-016） */
const manyGrantsMember: AppMember = {
  ...adminMember,
  userId: '01M5HMP300000000000000000B',
  username: 'zhaoliu',
  displayName: '赵六',
  isAdmin: false,
  roleCount: 0,
  orgGrants: [
    { orgId: 'd1', deptName: '技术中心', includeSubDepts: true },
    { orgId: 'd2', deptName: '市场部', includeSubDepts: true },
    { orgId: 'd3', deptName: '财务部', includeSubDepts: false },
  ],
}

const orgTree = [
  {
    id: 'root',
    name: '总公司',
    parentId: null,
    path: '/root/',
    userCount: 5,
    children: [
      { id: 'd1', name: '技术中心', parentId: 'root', path: '/root/d1/', userCount: 10, children: [] },
    ],
  },
]

async function mountMembers() {
  const wrapper = mount(Members)
  await flushPromises()
  return wrapper
}

async function clickButton(wrapper: ReturnType<typeof mount>, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`未找到按钮：${label}`)
  await btn.trigger('click')
  await flushPromises()
}

/** 触发成员表的 @select（表格勾选回调），等价于点复选框 */
function selectMembers(wrapper: ReturnType<typeof mount>, rows: AppMember[]) {
  const table = wrapper
    .findAllComponents({ name: 'ElTable' })
    .find((t) => {
      const data = t.props('data') as AppMember[] | undefined
      return Array.isArray(data) && data.length > 0 && typeof data[0]?.userId === 'string'
    })
  if (!table) throw new Error('未找到成员表格')
  table.vm.$emit('select', rows, true)
}

/** 直接驱动待提交草稿（jsdom 里点 el-tree-select 的树节点不可靠） */
async function pickDepartments(wrapper: ReturnType<typeof mount>, ids: string[]) {
  const state = (wrapper.vm as unknown as { $: { setupState: Record<string, unknown> } }).$.setupState
  state.grantPicker = ids
  await flushPromises()
}

describe('门户 · 应用成员页（006）', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((fn) => fn.mockReset())
    mocks.listAppMembers.mockResolvedValue(page([adminMember, plainMember]))
    mocks.listOrgTree.mockResolvedValue([])
    mocks.listOrgGrants.mockResolvedValue({
      list: [
        {
          orgId: 'd1',
          deptName: '技术中心',
          deptPath: '/root/d1/',
          includeSubDepts: true,
          coveredUsers: 42,
          coveredDisabledUsers: 3,
          uniqueUsers: 37,
          grantedBy: 'admin',
          grantedAt: '2026-11-01T10:00:00Z',
          updatedAt: '2026-11-01T10:00:00Z',
        },
      ],
      total: 1,
    })
    mocks.listAppMemberIds.mockResolvedValue({ userIds: [] })
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

  it('组织授权卡片与成员来源列（010 / FR-016 / FR-020）', async () => {
    const wrapper = await mountMembers()
    const text = wrapper.text()

    expect(mocks.listOrgGrants).toHaveBeenCalledWith('demo-app')
    // 授权卡片：部门 + 两个口径的人数
    expect(text).toContain('技术中心')
    expect(text).toContain('在职启用账号数')
    expect(text).toContain('42')
    expect(text).toContain('37')
    // 成员来源：手动 + 组织
    expect(text).toContain('手动 + 技术中心')
    // 页头两个数字
    expect(text).toContain('手动成员')
    expect(text).toContain('实际可见')
  })

  it('门户也已下线「按部门加入」入口（FR-027）', async () => {
    const wrapper = await mountMembers()

    expect(wrapper.text()).not.toContain('按部门加入')
    expect(wrapper.text()).toContain('添加组织授权')
  })

  it('移除成员时提示仍可通过组织授权访问（FR-017）', async () => {
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountMembers()

    selectMembers(wrapper, [adminMember])
    await flushPromises()
    await clickButton(wrapper, '移除选中')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(String(confirmMock.mock.calls[0][0])).toContain(
      '其中 1 人仍可通过 1 个组织授权继续访问该应用',
    )
    expect(mocks.removeAppMembers).not.toHaveBeenCalled()
  })

  it('只选纯手动成员时不给组织授权提示（FR-017）', async () => {
    mocks.listAppMembers.mockResolvedValue(page([manualOnlyMember]))
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountMembers()

    selectMembers(wrapper, [manualOnlyMember])
    await flushPromises()
    await clickButton(wrapper, '移除选中')

    expect(String(confirmMock.mock.calls[0][0])).not.toContain('仍可通过')
  })

  it('授予组织树顶层节点需二次确认，取消则不提交（FR-006）', async () => {
    mocks.listOrgTree.mockResolvedValue(orgTree)
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountMembers()

    await clickButton(wrapper, '添加组织授权')
    await pickDepartments(wrapper, ['root'])
    await clickButton(wrapper, '授权')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    const message = String(confirmMock.mock.calls[0][0])
    expect(message).toContain('是组织树顶层节点')
    expect(message).toContain('15')
    expect(mocks.addOrgGrants).not.toHaveBeenCalled()
  })

  it('非根节点直接提交，请求体带 includeSubDepts=true（FR-002）', async () => {
    mocks.listOrgTree.mockResolvedValue(orgTree)
    mocks.addOrgGrants.mockResolvedValue({ added: 1, updated: 0, unchanged: 0, coveredUsers: 10 })
    const wrapper = await mountMembers()

    await clickButton(wrapper, '添加组织授权')
    await pickDepartments(wrapper, ['d1'])
    await clickButton(wrapper, '授权')

    expect(confirmMock).not.toHaveBeenCalled()
    expect(mocks.addOrgGrants).toHaveBeenCalledWith('demo-app', {
      grants: [{ deptId: 'd1', includeSubDepts: true }],
    })
  })

  it('撤销确认框给出影响人数、其它来源人数与停用账号数（FR-007）', async () => {
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountMembers()

    await clickButton(wrapper, '撤销')

    const message = String(confirmMock.mock.calls[0][0])
    expect(message).toContain('撤销后 37 人将不再看到该应用')
    expect(message).toContain('另有 5 人')
    expect(message).toContain('3 个停用账号')
    expect(mocks.removeOrgGrants).not.toHaveBeenCalled()
  })

  it('成员命中 3 条组织授权时来源列折叠（FR-016）', async () => {
    mocks.listAppMembers.mockResolvedValue(page([manyGrantsMember]))
    const wrapper = await mountMembers()

    expect(wrapper.text()).toContain('手动 + 3 个组织')
  })

  it('迁移提示只读：关闭不发起任何写请求（FR-031）', async () => {
    mocks.listAppMembers.mockResolvedValue({
      ...page([adminMember]),
      orgGrantHints: [{ orgId: 'd1', deptName: '技术中心', memberCount: 118 }],
    })
    const wrapper = await mountMembers()

    expect(wrapper.text()).toContain('118')
    const closeBtn = wrapper.find('.el-alert__close-btn')
    if (!closeBtn.exists()) throw new Error('迁移提示应可关闭')
    await closeBtn.trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('118')
    for (const fn of [
      mocks.addAppMembers,
      mocks.removeAppMembers,
      mocks.addOrgGrants,
      mocks.removeOrgGrants,
    ]) {
      expect(fn).not.toHaveBeenCalled()
    }
  })
})
