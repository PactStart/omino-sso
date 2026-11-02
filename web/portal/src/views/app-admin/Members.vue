<script setup lang="ts">
import { computed, inject, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  ElAlert,
  ElButton,
  ElDialog,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElLoading,
  ElMessage,
  ElMessageBox,
  ElPagination,
  ElSwitch,
  ElTable,
  ElTableColumn,
  ElTag,
  ElTreeSelect,
} from './ep'
import {
  addAppMembers,
  addOrgGrants,
  listOrgTree,
  listAppMemberIds,
  listAppMembers,
  listCandidateIds,
  listMemberCandidates,
  listOrgGrants,
  removeAppMembers,
  removeOrgGrants,
} from '@/api/app'
import { errorMessage } from '@/api/client'
import type { AppCandidate, AppMember, OrgGrant, OrgGrantHint, OrgNode } from '@/api/types'
import { formatTime, orDash } from '@/utils/format'
import { reloadClientSummaryKey } from './detail-context'

const reloadSummary = inject(reloadClientSummaryKey, async () => {})

// v-loading 是 Element Plus 的指令，局部引入时需显式注册（portal 未全局安装 EP）。
const vLoading = ElLoading.directive

const CHUNK = 500
const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

/* ---------------- 组织树（筛选与组织授权共用） ---------------- */

const orgTree = ref<OrgNode[]>([])
const orgTreeError = ref('')
const orgProps = { label: 'name', children: 'children' } as const

/** 组织树读取失败必须显式提示（010 FR-022），不得静默降级为空树。 */
async function loadOrgTree() {
  orgTreeError.value = ''
  try {
    orgTree.value = await listOrgTree()
  } catch (e) {
    orgTree.value = []
    orgTreeError.value = errorMessage(e)
  }
}

/* ---------------- 成员列表 ---------------- */

const members = ref<AppMember[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const listError = ref('')
/** 实际可见人数（手动成员 ∪ 组织授权覆盖，去重）；仅首屏返回，取不到时显示「—」 */
const visibleTotal = ref<number | null>(null)
/** 存量迁移提示（FR-031）：只读 */
const grantHints = ref<OrgGrantHint[]>([])
const hintsDismissed = ref<string[]>([])

const activeHints = computed(() => grantHints.value.filter((h) => !hintsDismissed.value.includes(h.orgId)))

const filters = reactive({ keyword: '', deptId: '' })

const tableRef = ref<InstanceType<typeof ElTable>>()
const selectedIds = ref<string[]>([])

const selectedCount = computed(() => selectedIds.value.length)

const filtersForQuery = () => ({
  keyword: filters.keyword.trim() || undefined,
  deptId: filters.deptId || undefined,
})

async function loadMembers() {
  loading.value = true
  listError.value = ''
  try {
    const data = await listAppMembers(clientId.value, {
      ...filtersForQuery(),
      page: page.value,
      pageSize: pageSize.value,
    })
    members.value = data.list
    total.value = data.total
    // 可见总人数与迁移提示只在首屏计算（critique E9）；翻页沿用首屏值。
    if (page.value <= 1) {
      visibleTotal.value = data.visibleTotal ?? null
      grantHints.value = data.orgGrantHints ?? []
    }
    await syncSelectionToTable()
  } catch (e) {
    listError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

/**
 * 把 selectedIds 回写到表格勾选态。
 * 只监听 @select / @select-all（用户动作），因此程序化 toggleRowSelection
 * 不会反过来覆盖 selectedIds —— 这是跨页全选能保住「非当前页」ID 的前提。
 */
async function syncSelectionToTable() {
  await nextTick()
  const table = tableRef.value
  if (!table) return
  table.clearSelection()
  for (const row of members.value) {
    if (selectedIds.value.includes(row.userId)) table.toggleRowSelection(row, true)
  }
}

function mergeSelection(selection: AppMember[]) {
  const pageIds = new Set(members.value.map((m) => m.userId))
  const kept = selectedIds.value.filter((id) => !pageIds.has(id))
  selectedIds.value = [...kept, ...selection.map((r) => r.userId)]
}

function clearSelection() {
  selectedIds.value = []
  tableRef.value?.clearSelection()
}

/**
 * 应用管理员身份在本页只读（spec FR-007 / A1）：
 * 授予与取消只在 admin 端由平台管理员执行，门户不提供该入口。
 * 平台管理员在 admin 端改完后，应用管理员刷新本页即可看到身份变化。
 */

async function selectAllMembers() {
  try {
    const res = await listAppMemberIds(clientId.value, filtersForQuery())
    selectedIds.value = res.userIds
    await syncSelectionToTable()
    ElMessage.success(`已选择全部 ${res.userIds.length} 名成员`)
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

function onSearch() {
  page.value = 1
  clearSelection()
  void loadMembers()
}

function resetFilters() {
  filters.keyword = ''
  filters.deptId = ''
  onSearch()
}

function onPageChange(next: number) {
  page.value = next
  void loadMembers()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void loadMembers()
}

/* ---------------- 移除成员 ---------------- */

async function removeSelected() {
  const count = selectedIds.value.length
  if (count === 0) return

  // 只有当选中的 ID 全部在当前页时才拿得到精确的角色绑定数
  const knownRoleBindings = members.value
    .filter((m) => selectedIds.value.includes(m.userId))
    .reduce((sum, m) => sum + (m.roleCount || 0), 0)
  const precise = count === members.value.filter((m) => selectedIds.value.includes(m.userId)).length

  const bindingText = precise
    ? `并解除其在本应用的 ${knownRoleBindings} 项角色绑定`
    : '并解除其在本应用的全部角色绑定'

  // FR-017：移除只解除手动成员关系。仍被组织授权覆盖的人不会失去访问权，
  // 必须在确认框里说清楚，否则管理员会误以为「移除了这个人」。
  const covered = members.value.filter(
    (m) => selectedIds.value.includes(m.userId) && (m.orgGrants?.length ?? 0) > 0,
  )
  const coveredGrantCount = new Set(covered.flatMap((m) => m.orgGrants.map((g) => g.orgId))).size
  const grantText = covered.length
    ? `；其中 ${covered.length} 人仍可通过 ${coveredGrantCount} 个组织授权继续访问该应用`
    : ''

  try {
    await ElMessageBox.confirm(
      `将移除 ${count} 名成员，${bindingText}${grantText}。该操作不可撤销，确定继续？`,
      '移除成员',
      { type: 'warning', confirmButtonText: '确定移除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }

  let removed = 0
  let bindings = 0
  let notMember = 0
  try {
    for (const part of chunk(selectedIds.value, CHUNK)) {
      const res = await removeAppMembers(clientId.value, part)
      removed += res.removed
      bindings += res.roleBindingsRemoved
      notMember += res.notMember
    }
    ElMessage.success(`已移除 ${removed} 名成员，解除 ${bindings} 项角色绑定`)
    if (notMember > 0) ElMessage.info(`${notMember} 个用户已不是成员，已跳过`)
    clearSelection()
    await loadMembers()
    await reloadSummary()
  } catch (e) {
    ElMessage.error(errorMessage(e))
    await loadMembers()
  }
}

function chunk<T>(list: T[], size: number): T[][] {
  const out: T[][] = []
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size))
  return out
}

/* ---------------- 添加成员（候选弹窗） ---------------- */

const addOpen = ref(false)
const candidates = ref<AppCandidate[]>([])
const candTotal = ref(0)
const candPage = ref(1)
const candPageSize = ref(20)
const candLoading = ref(false)
const candError = ref('')
const candFilters = reactive({ keyword: '', deptId: '' })
const candSelection = ref<string[]>([])
const candTableRef = ref<InstanceType<typeof ElTable>>()
const candSubmitting = ref(false)

function openAdd() {
  addOpen.value = true
  candidates.value = []
  candTotal.value = 0
  candPage.value = 1
  candSelection.value = []
  candError.value = ''
  void loadCandidates()
}

async function loadCandidates() {
  candLoading.value = true
  candError.value = ''
  try {
    const data = await listMemberCandidates(clientId.value, {
      keyword: candFilters.keyword.trim() || undefined,
      deptId: candFilters.deptId || undefined,
      page: candPage.value,
      pageSize: candPageSize.value,
    })
    candidates.value = data.list
    candTotal.value = data.total
    await nextTick()
    candTableRef.value?.clearSelection()
  } catch (e) {
    candError.value = errorMessage(e)
  } finally {
    candLoading.value = false
  }
}

function onCandSearch() {
  candPage.value = 1
  candSelection.value = []
  void loadCandidates()
}

function onCandPageChange(next: number) {
  candPage.value = next
  void loadCandidates()
}

function onCandSelectionChange(rows: AppCandidate[]) {
  candSelection.value = rows.map((r) => r.userId)
}

async function selectAllCandidates() {
  try {
    const res = await listCandidateIds(clientId.value, {
      keyword: candFilters.keyword.trim() || undefined,
      deptId: candFilters.deptId || undefined,
    })
    ElMessage.info(
      `当前筛选下共 ${res.userIds.length} 名候选；请逐页勾选，或分多次确认添加（单次最多 ${CHUNK} 个）`,
    )
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

async function submitAdd() {
  if (candSelection.value.length === 0) {
    ElMessage.warning('请先勾选要添加的用户')
    return
  }
  if (candSubmitting.value) return
  candSubmitting.value = true
  let added = 0
  let skipped = 0
  try {
    for (const part of chunk(candSelection.value, CHUNK)) {
      const res = await addAppMembers(clientId.value, { userIds: part })
      added += res.added ?? 0
      skipped += res.skipped ?? 0
    }
    addOpen.value = false
    ElMessage.success(
      skipped > 0 ? `新增 ${added} 名成员，${skipped} 名已存在或已停用被跳过` : `新增 ${added} 名成员`,
    )
    clearSelection()
    await loadMembers()
    await reloadSummary()
  } catch (e) {
    ElMessage.error(errorMessage(e))
  } finally {
    candSubmitting.value = false
  }
}

/* ---------------- 组织授权（010，取代「按部门加入」） ---------------- */

const orgGrants = ref<OrgGrant[]>([])
const grantLoading = ref(true)
const grantError = ref('')

async function loadOrgGrants() {
  grantLoading.value = true
  grantError.value = ''
  try {
    const data = await listOrgGrants(clientId.value)
    orgGrants.value = data.list
  } catch (e) {
    // 降级（FR-030）：授权卡片单独报错，不影响成员列表。
    grantError.value = errorMessage(e)
  } finally {
    grantLoading.value = false
  }
}

const grantOpen = ref(false)
const grantSubmitting = ref(false)
const grantPicker = ref<string[]>([])
/** 待提交列表：每个部门一行，「含子部门」可逐行切换（FR-002） */
const grantDraft = ref<{ deptId: string; deptName: string; includeSubDepts: boolean }[]>([])

const rootIds = computed(() => new Set(orgTree.value.map((n) => n.id)))

function findNode(id: string, nodes: OrgNode[] = orgTree.value): OrgNode | undefined {
  for (const n of nodes) {
    if (n.id === id) return n
    const hit = n.children?.length ? findNode(id, n.children) : undefined
    if (hit) return hit
  }
  return undefined
}

function subtreeUsers(node: OrgNode): number {
  return (node.userCount || 0) + (node.children || []).reduce((s, c) => s + subtreeUsers(c), 0)
}

watch(grantPicker, (ids) => {
  const existing = new Map(grantDraft.value.map((g) => [g.deptId, g]))
  grantDraft.value = ids.map(
    (id) => existing.get(id) ?? { deptId: id, deptName: findNode(id)?.name ?? id, includeSubDepts: true },
  )
})

function openGrantAdd() {
  grantPicker.value = []
  grantDraft.value = []
  grantOpen.value = true
}

/** 授予根组织 = 对全员放开，必须二次确认并给出覆盖人数（FR-006）。 */
async function confirmRootGrants(): Promise<boolean> {
  const roots = grantDraft.value.filter((g) => rootIds.value.has(g.deptId))
  if (roots.length === 0) return true
  const node = findNode(roots[0].deptId)
  const total = node ? subtreeUsers(node) : 0
  try {
    await ElMessageBox.confirm(
      `「${roots.map((r) => r.deptName).join('、')}」是组织树顶层节点，授权后将覆盖其整棵子树，` +
        `涉及约 ${total} 名在职员工（以保存后的实际覆盖人数为准）。确定继续？`,
      '将向全员放开该应用',
      { type: 'warning', confirmButtonText: '确定授权', cancelButtonText: '返回' },
    )
    return true
  } catch {
    return false
  }
}

async function submitGrantAdd() {
  if (grantSubmitting.value) return
  if (grantDraft.value.length === 0) {
    ElMessage.warning('请选择部门')
    return
  }
  if (!(await confirmRootGrants())) return
  grantSubmitting.value = true
  try {
    const res = await addOrgGrants(clientId.value, {
      grants: grantDraft.value.map((g) => ({ deptId: g.deptId, includeSubDepts: g.includeSubDepts })),
    })
    ElMessage.success(
      `已新增 ${res.added} 条、调整 ${res.updated} 条授权，涉及部门合计覆盖 ${res.coveredUsers} 人`,
    )
    grantOpen.value = false
    await Promise.all([loadOrgGrants(), loadMembers(), reloadSummary()])
  } catch (e) {
    ElMessage.error(errorMessage(e))
  } finally {
    grantSubmitting.value = false
  }
}

async function removeGrant(row: OrgGrant) {
  const others = row.coveredUsers - row.uniqueUsers
  const parts = [
    `撤销后 ${row.uniqueUsers} 人将不再看到该应用`,
    others > 0 ? `另有 ${others} 人仍可通过手动成员或其它组织授权访问` : '',
    row.coveredDisabledUsers > 0 ? `该授权还覆盖 ${row.coveredDisabledUsers} 个停用账号（不影响可见性）` : '',
  ].filter(Boolean)
  try {
    await ElMessageBox.confirm(
      `${parts.join('；')}。撤销不会让已登录用户掉线，也不影响手动成员与角色绑定。`,
      `撤销「${row.deptName}」的组织授权`,
      { type: 'warning', confirmButtonText: '确定撤销', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    const res = await removeOrgGrants(clientId.value, [row.orgId])
    ElMessage.success(`已撤销 ${res.removed} 条组织授权，${res.impactUsers} 人不再可见`)
    await Promise.all([loadOrgGrants(), loadMembers(), reloadSummary()])
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

/** 成员「来源」列：手动 + 命中的组织授权（>2 个折叠）。 */
function sourceLabel(row: AppMember): string {
  const grants = row.orgGrants || []
  if (grants.length === 0) return '手动'
  if (grants.length > 2) return `手动 + ${grants.length} 个组织`
  return `手动 + ${grants.map((g) => g.deptName).join('、')}`
}

function dismissHint(orgId: string) {
  hintsDismissed.value = [...hintsDismissed.value, orgId]
}

onMounted(async () => {
  await Promise.all([loadOrgTree(), loadOrgGrants(), loadMembers()])
})
</script>

<template>
  <div class="members">
    <!-- 组织授权（010）：把门户可见性授予部门（可含子部门），取代「按部门加入」 -->
    <section class="panel">
      <div class="toolbar">
        <h3 class="panel-title">组织授权</h3>
        <span class="panel-sub">被授权部门（含子部门）内的在职员工会自动在门户看到该应用</span>
        <div class="toolbar-right">
          <el-button type="primary" @click="openGrantAdd">添加组织授权</el-button>
        </div>
      </div>

      <el-alert v-if="grantError" type="error" :closable="false" show-icon class="hint">
        组织授权加载失败：{{ grantError }}
        <el-button link type="primary" size="small" @click="loadOrgGrants">重试</el-button>
      </el-alert>

      <el-table v-loading="grantLoading" :data="orgGrants" row-key="orgId" stripe>
        <el-table-column label="部门" min-width="180">
          <template #default="{ row }">
            <span>{{ row.deptName }}</span>
            <span class="path">{{ row.deptPath }}</span>
          </template>
        </el-table-column>
        <el-table-column label="包含子部门" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.includeSubDepts ? 'success' : 'info'">
              {{ row.includeSubDepts ? '含子部门' : '仅本级' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="在职启用账号数" width="140">
          <template #default="{ row }">{{ row.coveredUsers }}</template>
        </el-table-column>
        <el-table-column label="撤销影响人数" width="130">
          <template #default="{ row }">{{ row.uniqueUsers }}</template>
        </el-table-column>
        <el-table-column label="授权时间" width="160">
          <template #default="{ row }">{{ formatTime(row.grantedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="removeGrant(row)">撤销</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="尚未按组织授权，该应用对所有人都不可见" />
        </template>
      </el-table>
    </section>

    <section class="panel">
      <div class="toolbar">
        <el-input
          v-model="filters.keyword"
          placeholder="用户名前缀或姓名关键字"
          clearable
          style="width: 220px"
          @keyup.enter="onSearch"
          @clear="onSearch"
        />
        <el-tree-select
          v-model="filters.deptId"
          :data="orgTree"
          :props="orgProps"
          node-key="id"
          check-strictly
          clearable
          placeholder="全部部门（含子部门）"
          style="width: 220px"
        />
        <el-button type="primary" @click="onSearch">查询</el-button>
        <el-button @click="resetFilters">重置</el-button>
        <div class="toolbar-right">
          <el-button type="primary" @click="openAdd">添加成员</el-button>
        </div>
      </div>

      <div class="stat-line">
        手动成员 <strong>{{ total }}</strong> 人
        <span class="stat-sep">/</span>
        实际可见 <strong>{{ visibleTotal === null ? '—' : visibleTotal }}</strong> 人
        <span class="stat-hint">（手动成员 ∪ 组织授权覆盖，去重，仅计在职启用账号）</span>
      </div>

      <el-alert
        v-for="h in activeHints"
        :key="h.orgId"
        class="hint"
        type="info"
        :closable="true"
        show-icon
        @close="dismissHint(h.orgId)"
      >
        「{{ h.deptName }}」在本应用中有 {{ h.memberCount }} 名成员来自历史「按部门加入」，建议改用组织授权，
        之后该部门的新增员工将自动可见。
      </el-alert>

      <el-alert v-if="orgTreeError" class="hint" type="error" :closable="false" show-icon>
        组织架构加载失败：{{ orgTreeError }}
        <el-button link type="primary" size="small" @click="loadOrgTree">重试</el-button>
      </el-alert>

      <div v-if="selectedCount > 0" class="selection-bar">
        <span>已选择 {{ selectedCount }} 名成员</span>
        <el-button link type="primary" @click="selectAllMembers">
          全选全部 {{ total }} 名成员
        </el-button>
        <el-button link @click="clearSelection">清空选择</el-button>
        <el-button type="danger" size="small" @click="removeSelected">移除选中</el-button>
      </div>

      <el-table
        ref="tableRef"
        v-loading="loading"
        :data="members"
        row-key="userId"
        class="member-table"
        stripe
        @select="mergeSelection"
        @select-all="mergeSelection"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column label="用户名" min-width="130">
          <template #default="{ row }">
            <span class="mono">{{ row.username }}</span>
          </template>
        </el-table-column>
        <el-table-column label="姓名" min-width="110">
          <template #default="{ row }">{{ orDash(row.displayName) }}</template>
        </el-table-column>
        <el-table-column label="部门" min-width="140">
          <template #default="{ row }">{{ orDash(row.deptName) }}</template>
        </el-table-column>
        <el-table-column label="来源" min-width="170">
          <template #default="{ row }">
            <span class="source">{{ sourceLabel(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="角色数" width="90">
          <template #default="{ row }">{{ row.roleCount }}</template>
        </el-table-column>
        <el-table-column label="状态" width="96">
          <template #default="{ row }">
            <el-tag v-if="row.status === 1" size="small" type="success">正常</el-tag>
            <el-tag v-else size="small" type="info">已停用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="身份" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.isAdmin" size="small" type="warning">应用管理员</el-tag>
            <el-tag v-else size="small" type="info">普通成员</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="加入时间" width="160">
          <template #default="{ row }">{{ formatTime(row.joinedAt) }}</template>
        </el-table-column>

        <template #empty>
          <div v-if="listError" class="empty-block">
            <p class="empty-text">{{ listError }}</p>
            <el-button size="small" @click="loadMembers">重试</el-button>
          </div>
          <el-empty v-else description="暂无成员" />
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          background
          @current-change="onPageChange"
          @size-change="onPageSizeChange"
        />
      </div>
    </section>

    <!-- 添加成员 -->
    <el-dialog v-model="addOpen" title="添加成员" width="720px" :close-on-click-modal="false">
      <div class="cand-toolbar">
        <el-input
          v-model="candFilters.keyword"
          placeholder="用户名前缀或姓名关键字"
          clearable
          style="width: 200px"
          @keyup.enter="onCandSearch"
          @clear="onCandSearch"
        />
        <el-tree-select
          v-model="candFilters.deptId"
          :data="orgTree"
          :props="orgProps"
          node-key="id"
          check-strictly
          clearable
          placeholder="全部部门"
          style="width: 200px"
        />
        <el-button @click="onCandSearch">筛选</el-button>
        <el-button link type="primary" @click="selectAllCandidates">查看候选总数</el-button>
      </div>

      <el-alert
        v-if="candError"
        type="error"
        :title="candError"
        show-icon
        :closable="false"
        class="block-alert"
      />

      <el-table
        ref="candTableRef"
        v-loading="candLoading"
        :data="candidates"
        row-key="userId"
        height="320"
        @selection-change="onCandSelectionChange"
      >
        <el-table-column type="selection" width="46" />
        <el-table-column label="用户名" min-width="130">
          <template #default="{ row }">
            <span class="mono">{{ row.username }}</span>
          </template>
        </el-table-column>
        <el-table-column label="姓名" min-width="110">
          <template #default="{ row }">{{ orDash(row.displayName) }}</template>
        </el-table-column>
        <el-table-column label="部门" min-width="140">
          <template #default="{ row }">{{ orDash(row.deptName) }}</template>
        </el-table-column>
        <el-table-column label="访问" width="150">
          <template #default="{ row }">
            <el-tag v-if="row.orgCovered" size="small" type="info">已可经组织授权访问</el-tag>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="没有可添加的用户（已是成员的用户不会出现在这里）" />
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="candPage"
          :page-size="candPageSize"
          :total="candTotal"
          layout="total, prev, pager, next"
          background
          @current-change="onCandPageChange"
        />
      </div>

      <template #footer>
        <span class="dialog-tip">已勾选 {{ candSelection.length }} 人</span>
        <el-button @click="addOpen = false">取消</el-button>
        <el-button type="primary" :loading="candSubmitting" @click="submitAdd">
          添加（单次最多 {{ CHUNK }} 个，超出自动分批）
        </el-button>
      </template>
    </el-dialog>

    <!-- 添加组织授权 -->
    <el-dialog v-model="grantOpen" title="添加组织授权" width="640px" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="部门">
          <el-tree-select
            v-model="grantPicker"
            :data="orgTree"
            :props="orgProps"
            node-key="id"
            multiple
            show-checkbox
            check-strictly
            placeholder="可多选部门"
            style="width: 100%"
          />
          <div class="field-hint">每个部门可单独设置是否包含下级部门，默认包含</div>
        </el-form-item>
      </el-form>

      <el-table v-if="grantDraft.length > 0" :data="grantDraft" size="small">
        <el-table-column label="部门" prop="deptName" min-width="200" />
        <el-table-column label="包含子部门" width="130">
          <template #default="{ row }">
            <el-switch v-model="row.includeSubDepts" />
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="请选择要授权的部门" :image-size="60" />

      <template #footer>
        <span class="dialog-tip">已选 {{ grantDraft.length }} 个部门</span>
        <el-button @click="grantOpen = false">取消</el-button>
        <el-button type="primary" :loading="grantSubmitting" @click="submitGrantAdd">授权</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.panel {
  padding: 16px;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
}

.toolbar-right {
  margin-left: auto;
  display: flex;
  gap: 10px;
}

.empty-hint {
  margin-top: 12px;
}

.selection-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
  padding: 8px 12px;
  font-size: 13px;
  background: var(--el-color-primary-light-9);
  border-radius: 8px;
}

.member-table {
  margin-top: 12px;
  width: 100%;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}

.empty-block {
  padding: 16px 0;
}

.empty-text {
  margin: 0 0 10px;
  font-size: 13px;
  color: #d14343;
}

.cand-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.block-alert {
  margin-bottom: 10px;
}

.dialog-tip {
  margin-right: auto;
  font-size: 13px;
  color: var(--app-text-secondary);
}

.field-hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.field-hint.inline {
  margin: 0 0 0 12px;
}

.mono {
  font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}

/* 010：组织授权卡片与成员卡片纵向排列 */
.members {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.panel-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

.panel-sub {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.stat-line {
  margin-top: 12px;
  font-size: 13px;
  color: var(--app-text-secondary);
}

.stat-line strong {
  color: var(--app-text-primary, #1f2329);
}

.stat-sep {
  margin: 0 8px;
}

.stat-hint {
  margin-left: 6px;
  font-size: 12px;
  opacity: 0.8;
}

.hint {
  margin-top: 12px;
}

.path {
  margin-left: 8px;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.source {
  font-size: 12px;
}

.muted {
  color: var(--app-text-secondary);
}
</style>
