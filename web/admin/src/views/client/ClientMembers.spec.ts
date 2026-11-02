import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppMember, MemberPage, OrgGrant, OrgNode } from '@/api/types'

/**
 * 应用成员页的组织授权（010）：
 * ① 授权卡片渲染部门与两个口径的人数；② 成员来源列区分手动 / 组织授权；
 * ③ 页头同时给出「手动成员 / 实际可见」两个数字；④ 「按部门加入」入口已下线（FR-027）。
 */
const mocks = vi.hoisted(() => ({
  listAppMembers: vi.fn(),
  listAppMemberIds: vi.fn(),
  listMemberCandidates: vi.fn(),
  listCandidateIds: vi.fn(),
  addAppMembers: vi.fn(),
  removeAppMembers: vi.fn(),
  setAppMemberAdmin: vi.fn(),
  getOrgTree: vi.fn(),
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
vi.mock('@/api', () => mocks)
vi.mock('@/api/client', () => ({ errorMessage: (e: unknown) => String(e) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { clientId: 'demo-app' } }) }))
vi.mock('./detail-context', () => ({
  reloadClientSummaryKey: Symbol('reload'),
}))

import ClientMembers from './ClientMembers.vue'

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

const memberWithGrant: AppMember = {
  userId: 'u1',
  username: 'zhangsan',
  displayName: '张三',
  deptId: 'd1',
  deptName: '研发部',
  status: 1,
  joinedAt: '2026-10-20T10:00:00Z',
  roleCount: 2,
  isAdmin: false,
  orgGrants: [{ orgId: 'd1', deptName: '研发部', includeSubDepts: true }],
}

const memberManualOnly: AppMember = {
  ...memberWithGrant,
  userId: 'u2',
  username: 'lisi',
  displayName: '李四',
  orgGrants: [],
}

/** 命中 3 条组织授权：来源列必须折叠成「手动 + 3 个组织」（FR-016） */
const memberManyGrants: AppMember = {
  ...memberWithGrant,
  userId: 'u3',
  username: 'wangwu',
  displayName: '王五',
  roleCount: 0,
  orgGrants: [
    { orgId: 'd1', deptName: '研发部', includeSubDepts: true },
    { orgId: 'd2', deptName: '市场部', includeSubDepts: true },
    { orgId: 'd3', deptName: '财务部', includeSubDepts: false },
  ],
}

const orgTree: OrgNode[] = [
  {
    id: 'root',
    name: '总公司',
    parentId: null,
    path: '/root/',
    userCount: 5,
    children: [
      { id: 'd1', name: '研发部', parentId: 'root', path: '/root/d1/', userCount: 10, children: [] },
    ],
  },
]

const grant: OrgGrant = {
  orgId: 'd1',
  deptName: '研发部',
  deptPath: '/root/d1/',
  includeSubDepts: true,
  coveredUsers: 42,
  coveredDisabledUsers: 3,
  uniqueUsers: 37,
  grantedBy: 'admin',
  grantedAt: '2026-11-01T10:00:00Z',
  updatedAt: '2026-11-01T10:00:00Z',
}

function page(list: AppMember[]): MemberPage {
  return { list, total: list.length, page: 1, pageSize: 20, visibleTotal: 40, orgGrantHints: [] }
}

async function mountPage() {
  const wrapper = mount(ClientMembers, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

async function clickButton(wrapper: ReturnType<typeof mount>, label: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`未找到按钮：${label}`)
  await btn.trigger('click')
  await flushPromises()
}

/** 触发成员表的 @select（表格勾选的回调），等价于点复选框 */
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

describe('应用成员页 · 组织授权（010）', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((fn) => fn.mockReset())
    mocks.listAppMembers.mockResolvedValue(page([memberWithGrant, memberManualOnly]))
    mocks.getOrgTree.mockResolvedValue([])
    mocks.listOrgGrants.mockResolvedValue({ list: [grant], total: 1 })
    mocks.listAppMemberIds.mockResolvedValue({ userIds: [] })
  })

  it('渲染组织授权的部门与两个口径的人数', async () => {
    const wrapper = await mountPage()
    const text = wrapper.text()

    expect(mocks.listOrgGrants).toHaveBeenCalledWith('demo-app')
    expect(text).toContain('研发部')
    expect(text).toContain('在职启用账号数')
    expect(text).toContain('42')
    expect(text).toContain('37')
  })

  it('成员来源列区分手动与组织授权', async () => {
    const wrapper = await mountPage()
    const text = wrapper.text()

    expect(text).toContain('手动 + 研发部')
    expect(text).toContain('手动')
  })

  it('页头同时给出手动成员数与实际可见人数', async () => {
    const wrapper = await mountPage()

    expect(wrapper.text()).toContain('手动成员')
    expect(wrapper.text()).toContain('实际可见')
    expect(wrapper.text()).toContain('40')
  })

  it('已下线「按部门加入」入口（FR-027）', async () => {
    const wrapper = await mountPage()

    expect(wrapper.text()).not.toContain('按部门加入')
    expect(wrapper.text()).toContain('添加组织授权')
  })

  it('移除成员时提示仍可通过组织授权访问（FR-017）', async () => {
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountPage()

    selectMembers(wrapper, [memberWithGrant])
    await flushPromises()
    await clickButton(wrapper, '移除选中')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(String(confirmMock.mock.calls[0][0])).toContain(
      '其中 1 人仍可通过 1 个组织授权继续访问该应用',
    )
    expect(mocks.removeAppMembers).not.toHaveBeenCalled()
  })

  it('只选纯手动成员时不给组织授权提示（FR-017）', async () => {
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountPage()

    selectMembers(wrapper, [memberManualOnly])
    await flushPromises()
    await clickButton(wrapper, '移除选中')

    expect(String(confirmMock.mock.calls[0][0])).not.toContain('仍可通过')
  })

  it('授予组织树顶层节点需二次确认，取消则不提交（FR-006）', async () => {
    mocks.getOrgTree.mockResolvedValue(orgTree)
    confirmMock.mockRejectedValueOnce(new Error('cancel'))
    const wrapper = await mountPage()

    await clickButton(wrapper, '添加组织授权')
    await pickDepartments(wrapper, ['root'])
    await clickButton(wrapper, '授权')

    expect(confirmMock).toHaveBeenCalledTimes(1)
    const message = String(confirmMock.mock.calls[0][0])
    expect(message).toContain('是组织树顶层节点')
    // 子树人数 = 5 + 10
    expect(message).toContain('15')
    expect(mocks.addOrgGrants).not.toHaveBeenCalled()
  })

  it('非根节点直接提交，请求体带 includeSubDepts=true（FR-002）', async () => {
    mocks.getOrgTree.mockResolvedValue(orgTree)
    mocks.addOrgGrants.mockResolvedValue({ added: 1, updated: 0, unchanged: 0, coveredUsers: 10 })
    const wrapper = await mountPage()

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
    const wrapper = await mountPage()

    await clickButton(wrapper, '撤销')

    const message = String(confirmMock.mock.calls[0][0])
    expect(message).toContain('撤销后 37 人将不再看到该应用')
    // others = coveredUsers(42) - uniqueUsers(37)
    expect(message).toContain('另有 5 人')
    expect(message).toContain('3 个停用账号')
    expect(mocks.removeOrgGrants).not.toHaveBeenCalled()
  })

  it('成员命中 3 条组织授权时来源列折叠（FR-016）', async () => {
    mocks.listAppMembers.mockResolvedValue(page([memberManyGrants]))
    const wrapper = await mountPage()

    expect(wrapper.text()).toContain('手动 + 3 个组织')
  })

  it('迁移提示只读：关闭不发起任何写请求（FR-031）', async () => {
    mocks.listAppMembers.mockResolvedValue({
      ...page([memberWithGrant]),
      orgGrantHints: [{ orgId: 'd1', deptName: '研发部', memberCount: 118 }],
    })
    const wrapper = await mountPage()

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
      mocks.setAppMemberAdmin,
    ]) {
      expect(fn).not.toHaveBeenCalled()
    }
  })
})
