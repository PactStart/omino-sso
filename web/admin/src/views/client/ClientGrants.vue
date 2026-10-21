<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { ElTable, ElTree } from 'element-plus'
import {
  getAppRole,
  listAppMemberIds,
  listAppMembers,
  listAppPermissions,
  listAppRoles,
  putRoleMembers,
  putRolePermissions,
} from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type { AppMember, AppPermission, AppRole, RoleDetail } from '@/api/types'
import { orDash } from '@/utils/format'

const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

/* ---------------- 基础数据 ---------------- */

const roles = ref<AppRole[]>([])
const permissions = ref<AppPermission[]>([])
const loading = ref(true)
const loadError = ref('')

interface PermissionNode extends AppPermission {
  children: PermissionNode[]
}

const permissionTree = computed<PermissionNode[]>(() => {
  const nodes = new Map<string, PermissionNode>()
  for (const p of permissions.value) nodes.set(p.id, { ...p, children: [] })
  const roots: PermissionNode[] = []
  for (const node of nodes.values()) {
    const parent = node.parentId ? nodes.get(node.parentId) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  return roots
})

const roleKeyword = ref('')
const filteredRoles = computed(() => {
  const kw = roleKeyword.value.trim().toLowerCase()
  if (!kw) return roles.value
  return roles.value.filter((r) => r.name.toLowerCase().includes(kw))
})

async function loadBase() {
  loading.value = true
  loadError.value = ''
  try {
    const [rolePage, permPage] = await Promise.all([
      listAppRoles(clientId.value),
      listAppPermissions(clientId.value),
    ])
    roles.value = rolePage.list
    permissions.value = permPage.list
    if (!currentRole.value && roles.value.length > 0) {
      await selectRole(roles.value[0]!)
    }
  } catch (e) {
    loadError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

/* ---------------- 选中角色 ---------------- */

const currentRole = ref<AppRole | null>(null)
const roleDetail = ref<RoleDetail | null>(null)
const roleLoading = ref(false)
const activePanel = ref<'permissions' | 'members'>('permissions')

const treeRef = ref<InstanceType<typeof ElTree>>()
const memberTableRef = ref<InstanceType<typeof ElTable>>()

async function selectRole(role: AppRole) {
  currentRole.value = role
  roleLoading.value = true
  try {
    const detail = await getAppRole(clientId.value, role.id)
    roleDetail.value = detail
    await applyRoleState(detail)
  } catch (e) {
    ElMessage.error(errorMessage(e))
  } finally {
    roleLoading.value = false
  }
}

async function applyRoleState(detail: RoleDetail) {
  await nextTick()
  treeRef.value?.setCheckedKeys(detail.permissionIds)
  selectedMemberIds.value = [...detail.memberIds]
  await loadMembers()
}

/* ---------------- 权限勾选面板 ---------------- */

const treeProps = { label: 'name', children: 'children' } as const

function checkedPermissionIds(): string[] {
  const tree = treeRef.value
  if (!tree) return []
  const checked = tree.getCheckedKeys(false) as string[]
  const half = tree.getHalfCheckedKeys() as string[]
  return [...new Set([...checked, ...half])]
}

const permissionCount = computed(() => {
  if (roleDetail.value) return roleDetail.value.permissionIds.length
  return currentRole.value?.permissionCount ?? 0
})

/* ---------------- 成员勾选面板 ---------------- */

const members = ref<AppMember[]>([])
const memberTotal = ref(0)
const memberPage = ref(1)
const memberPageSize = ref(20)
const memberKeyword = ref('')
const memberLoading = ref(false)
const selectedMemberIds = ref<string[]>([])

async function loadMembers() {
  memberLoading.value = true
  try {
    const data = await listAppMembers(clientId.value, {
      keyword: memberKeyword.value.trim() || undefined,
      page: memberPage.value,
      pageSize: memberPageSize.value,
    })
    members.value = data.list
    memberTotal.value = data.total
    await syncMemberSelection()
  } catch (e) {
    ElMessage.error(errorMessage(e))
  } finally {
    memberLoading.value = false
  }
}

/** 只监听用户动作（@select / @select-all），程序化勾选不会覆盖已选集合 */
async function syncMemberSelection() {
  await nextTick()
  const table = memberTableRef.value
  if (!table) return
  table.clearSelection()
  for (const row of members.value) {
    if (selectedMemberIds.value.includes(row.userId)) table.toggleRowSelection(row, true)
  }
}

function mergeMemberSelection(selection: AppMember[]) {
  const pageIds = new Set(members.value.map((m) => m.userId))
  const kept = selectedMemberIds.value.filter((id) => !pageIds.has(id))
  selectedMemberIds.value = [...kept, ...selection.map((r) => r.userId)]
}

/** 成员面板是懒挂载的：切到该页签时再回写勾选态 */
async function onTabChange(name: string | number) {
  if (name !== 'members') return
  if (members.value.length === 0 && memberTotal.value === 0) await loadMembers()
  else await syncMemberSelection()
}

async function selectAllMembers() {
  try {
    const res = await listAppMemberIds(clientId.value, {
      keyword: memberKeyword.value.trim() || undefined,
    })
    selectedMemberIds.value = res.userIds
    await syncMemberSelection()
    ElMessage.success(`已选择全部 ${res.userIds.length} 名成员`)
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

function onMemberPageChange(next: number) {
  memberPage.value = next
  void loadMembers()
}

function onMemberSearch() {
  memberPage.value = 1
  void loadMembers()
}

/* ---------------- 提交 ---------------- */

const savingPermissions = ref(false)
const savingMembers = ref(false)
const grantResult = ref('')

async function savePermissions() {
  const role = currentRole.value
  if (!role) return
  savingPermissions.value = true
  grantResult.value = ''
  try {
    const res = await putRolePermissions(clientId.value, role.id, {
      permissionIds: checkedPermissionIds(),
      updatedAt: role.updatedAt,
    })
    grantResult.value = `权限已保存：新增 ${res.added} 项，移除 ${res.removed} 项，当前 ${res.permissionIds.length} 项`
    ElMessage.success('权限已保存')
    await refreshAfterGrant()
  } catch (e) {
    await handleGrantError(e)
  } finally {
    savingPermissions.value = false
  }
}

async function saveMembers() {
  const role = currentRole.value
  if (!role) return
  savingMembers.value = true
  grantResult.value = ''
  try {
    const res = await putRoleMembers(clientId.value, role.id, {
      userIds: selectedMemberIds.value,
      updatedAt: role.updatedAt,
    })
    const auto = res.autoJoinedMembers > 0 ? `，并自动加入 ${res.autoJoinedMembers} 名应用成员` : ''
    grantResult.value = `成员已保存：新增 ${res.added} 人，移除 ${res.removed} 人${auto}，当前 ${res.memberIds.length} 人`
    ElMessage.success('成员已保存')
    await refreshAfterGrant()
  } catch (e) {
    await handleGrantError(e)
  } finally {
    savingMembers.value = false
  }
}

async function handleGrantError(e: unknown) {
  if (errorCode(e) === 'APP_RESOURCE_STALE') {
    ElMessage.warning('该角色已被他人修改，请刷新后重试')
    await refreshAfterGrant()
    return
  }
  ElMessage.error(errorMessage(e))
}

async function refreshAfterGrant() {
  const roleId = currentRole.value?.id
  if (!roleId) return
  try {
    const [rolePage, detail] = await Promise.all([
      listAppRoles(clientId.value),
      getAppRole(clientId.value, roleId),
    ])
    roles.value = rolePage.list
    currentRole.value = rolePage.list.find((r) => r.id === roleId) ?? currentRole.value
    roleDetail.value = detail
    await applyRoleState(detail)
  } catch {
    // 刷新失败不覆盖用户的成功提示
  }
}

onMounted(loadBase)
</script>

<template>
  <div class="grants">
    <el-alert
      v-if="loadError"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
      class="block-alert"
    >
      <template #default>
        <el-button size="small" @click="loadBase">重试</el-button>
      </template>
    </el-alert>

    <div v-else v-loading="loading" class="grant-layout">
      <!-- 角色列 -->
      <aside class="role-pane">
        <div class="role-pane-head">
          <el-input
            v-model="roleKeyword"
            placeholder="搜索角色名"
            clearable
            size="small"
          />
        </div>
        <ul v-if="filteredRoles.length > 0" class="role-list">
          <li
            v-for="role in filteredRoles"
            :key="role.id"
            class="role-item"
            :class="{ active: currentRole?.id === role.id }"
            @click="selectRole(role)"
          >
            <div class="role-name mono">{{ role.name }}</div>
            <div class="role-desc">{{ orDash(role.description) }}</div>
            <div class="role-counts">
              {{ role.permissionCount }} 个权限 / {{ role.memberCount }} 名成员
            </div>
          </li>
        </ul>
        <el-empty v-else description="没有匹配的角色" :image-size="72" />
      </aside>

      <!-- 授权面板 -->
      <section class="panel">
        <template v-if="currentRole">
          <div class="panel-head">
            <div>
              <h2 class="panel-title mono">{{ currentRole.name }}</h2>
              <p class="panel-desc">
                {{ permissionCount }} 个权限 / {{ currentRole.memberCount }} 名成员 · 覆盖式保存（提交最终态）
              </p>
            </div>
          </div>

          <el-alert
            v-if="grantResult"
            type="success"
            show-icon
            :closable="false"
            :title="grantResult"
            class="block-alert"
          />

          <el-tabs v-model="activePanel" @tab-change="onTabChange">
            <el-tab-pane label="分配权限" name="permissions">
              <div v-loading="roleLoading" class="tab-body">
                <el-tree
                  ref="treeRef"
                  :data="permissionTree"
                  :props="treeProps"
                  node-key="id"
                  show-checkbox
                  check-strictly
                  default-expand-all
                  class="grant-tree"
                >
                  <template #default="{ data }">
                    <span class="tree-row">
                      <span>{{ data.name }}</span>
                      <span class="mono tree-code">{{ data.code }}</span>
                    </span>
                  </template>
                </el-tree>
                <el-empty
                  v-if="permissionTree.length === 0"
                  description="该应用还没有权限点，请先在「应用权限」创建"
                  :image-size="72"
                />
                <div class="tab-footer">
                  <span class="field-hint">
                    勾选即授予；父级与子级相互独立，不会自动级联
                  </span>
                  <el-button type="primary" :loading="savingPermissions" @click="savePermissions">
                    保存权限
                  </el-button>
                </div>
              </div>
            </el-tab-pane>

            <el-tab-pane label="分配成员" name="members">
              <div class="tab-body">
                <div class="member-toolbar">
                  <el-input
                    v-model="memberKeyword"
                    placeholder="用户名前缀或姓名关键字"
                    clearable
                    size="small"
                    style="width: 220px"
                    @keyup.enter="onMemberSearch"
                    @clear="onMemberSearch"
                  />
                  <el-button size="small" @click="onMemberSearch">筛选</el-button>
                  <el-button link type="primary" size="small" @click="selectAllMembers">
                    全选全部成员
                  </el-button>
                  <span class="member-selected">已选 {{ selectedMemberIds.length }} 人</span>
                </div>

                <el-table
                  ref="memberTableRef"
                  v-loading="memberLoading"
                  :data="members"
                  row-key="userId"
                  height="360"
                  @select="mergeMemberSelection"
                  @select-all="mergeMemberSelection"
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
                  <el-table-column label="状态" width="90">
                    <template #default="{ row }">
                      <el-tag v-if="row.status === 1" size="small" type="success">正常</el-tag>
                      <el-tag v-else size="small" type="info">已停用</el-tag>
                    </template>
                  </el-table-column>
                  <template #empty>
                    <el-empty description="该应用还没有成员" :image-size="72" />
                  </template>
                </el-table>

                <div class="pager">
                  <el-pagination
                    v-model:current-page="memberPage"
                    :page-size="memberPageSize"
                    :total="memberTotal"
                    layout="total, prev, pager, next"
                    background
                    size="small"
                    @current-change="onMemberPageChange"
                  />
                </div>

                <div class="tab-footer">
                  <span class="field-hint">
                    选中非成员时服务端会自动将其加入应用（FR-025）；停用账号会被跳过
                  </span>
                  <el-button type="primary" :loading="savingMembers" @click="saveMembers">
                    保存成员
                  </el-button>
                </div>
              </div>
            </el-tab-pane>
          </el-tabs>
        </template>

        <el-empty v-else description="请先在「应用角色」创建角色，再回到这里分配权限与成员" />
      </section>
    </div>
  </div>
</template>

<style scoped>
.grant-layout {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.role-pane {
  width: 280px;
  flex-shrink: 0;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
  overflow: hidden;
}

.role-pane-head {
  padding: 12px;
  border-bottom: 1px solid var(--app-border);
}

.role-list {
  margin: 0;
  padding: 0;
  list-style: none;
  max-height: 620px;
  overflow-y: auto;
}

.role-item {
  padding: 10px 12px;
  border-bottom: 1px solid #f2f3f5;
  cursor: pointer;
}

.role-item:hover {
  background: #fafbfc;
}

.role-item.active {
  background: var(--el-color-primary-light-9);
  border-left: 3px solid var(--el-color-primary);
}

.role-name {
  font-size: 14px;
  color: var(--app-text);
}

.role-desc {
  margin-top: 2px;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.role-counts {
  margin-top: 4px;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.panel {
  flex: 1;
  min-width: 0;
  padding: 16px;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
}

.panel-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--app-text);
}

.panel-desc {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.block-alert {
  margin: 12px 0;
}

.tab-body {
  padding-top: 6px;
}

.grant-tree {
  max-height: 420px;
  overflow-y: auto;
  padding: 4px 0;
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tree-code {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.tab-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--app-border);
}

.member-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.member-selected {
  margin-left: auto;
  font-size: 13px;
  color: var(--app-text-secondary);
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 10px;
}

.field-hint {
  font-size: 12px;
  color: var(--app-text-secondary);
  line-height: 1.5;
}

.mono {
  font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
</style>
