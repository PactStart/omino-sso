<script setup lang="ts">
import { computed, inject, nextTick, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { ElTable } from 'element-plus'
import {
  addAppMembers,
  addAppMembersByDept,
  getOrgTree,
  listAppMemberIds,
  listAppMembers,
  listCandidateIds,
  listMemberCandidates,
  removeAppMembers,
} from '@/api'
import { errorMessage } from '@/api/client'
import type { AppCandidate, AppMember, OrgNode } from '@/api/types'
import { formatTime, orDash } from '@/utils/format'
import { reloadClientSummaryKey } from './detail-context'

const reloadSummary = inject(reloadClientSummaryKey, async () => {})

const CHUNK = 500
const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

/* ---------------- 组织树（筛选与按部门加入共用） ---------------- */

const orgTree = ref<OrgNode[]>([])
const orgProps = { label: 'name', children: 'children' } as const

async function loadOrgTree() {
  try {
    orgTree.value = await getOrgTree()
  } catch {
    orgTree.value = []
  }
}

/* ---------------- 成员列表 ---------------- */

const members = ref<AppMember[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const listError = ref('')

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

  try {
    await ElMessageBox.confirm(
      `将移除 ${count} 名成员，${bindingText}。该操作不可撤销，确定继续？`,
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

/* ---------------- 按部门加入（FR-033） ---------------- */

const deptOpen = ref(false)
const deptForm = reactive({ deptId: '', includeSubDepts: true })
const deptSubmitting = ref(false)
const deptResult = ref<{ added: number; existing: number; disabled: number } | null>(null)

function openDeptAdd() {
  deptForm.deptId = ''
  deptForm.includeSubDepts = true
  deptResult.value = null
  deptOpen.value = true
}

async function submitDeptAdd() {
  if (!deptForm.deptId) {
    ElMessage.warning('请选择部门')
    return
  }
  if (deptSubmitting.value) return
  deptSubmitting.value = true
  try {
    const res = await addAppMembersByDept(clientId.value, {
      deptId: deptForm.deptId,
      includeSubDepts: deptForm.includeSubDepts,
    })
    deptResult.value = {
      added: res.added ?? 0,
      existing: res.existing ?? 0,
      disabled: res.disabled ?? 0,
    }
    clearSelection()
    await loadMembers()
    await reloadSummary()
  } catch (e) {
    ElMessage.error(errorMessage(e))
  } finally {
    deptSubmitting.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadOrgTree(), loadMembers()])
})
</script>

<template>
  <div class="members">
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
          <el-button @click="openDeptAdd">按部门加入</el-button>
          <el-button type="primary" @click="openAdd">添加成员</el-button>
        </div>
      </div>

      <el-alert
        v-if="!loading && total === 0 && !listError"
        class="empty-hint"
        type="warning"
        show-icon
        :closable="false"
        title="当前无成员，员工在门户将看不到该应用"
      >
        <template #default>
          <el-button size="small" type="primary" @click="openAdd">立即添加成员</el-button>
        </template>
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
        <el-table-column label="角色数" width="90">
          <template #default="{ row }">{{ row.roleCount }}</template>
        </el-table-column>
        <el-table-column label="状态" width="96">
          <template #default="{ row }">
            <el-tag v-if="row.status === 1" size="small" type="success">正常</el-tag>
            <el-tag v-else size="small" type="info">已停用</el-tag>
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

    <!-- 按部门加入 -->
    <el-dialog v-model="deptOpen" title="按部门加入" width="520px" :close-on-click-modal="false">
      <el-form label-width="100px">
        <el-form-item label="部门">
          <el-tree-select
            v-model="deptForm.deptId"
            :data="orgTree"
            :props="orgProps"
            node-key="id"
            check-strictly
            placeholder="选择部门"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="包含子部门">
          <el-switch v-model="deptForm.includeSubDepts" />
          <span class="field-hint inline">默认包含全部下级部门</span>
        </el-form-item>
      </el-form>

      <el-alert
        v-if="deptResult"
        type="success"
        show-icon
        :closable="false"
        :title="`新增 ${deptResult.added} 人，已存在 ${deptResult.existing} 人，已停用跳过 ${deptResult.disabled} 人`"
      />

      <template #footer>
        <el-button @click="deptOpen = false">关闭</el-button>
        <el-button type="primary" :loading="deptSubmitting" @click="submitDeptAdd">加入</el-button>
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
</style>
