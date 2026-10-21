<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { createOrg, createUser, deleteOrg, getOrgTree, listUsers, updateOrg, updateUser } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type { OrgNode, UpdateOrgPayload, UpdateUserPayload, UserRow, UserStatus } from '@/api/types'
import { formatTime, roleLabel, statusLabel, statusTagType } from '@/utils/format'

const ALL_KEY = '__all__'

/* ---------------- 组织树 ---------------- */
const orgTree = ref<OrgNode[]>([])
const orgLoading = ref(true)
const orgError = ref('')

const allUserCount = computed(() => {
  const sum = (nodes: OrgNode[]): number =>
    nodes.reduce((acc, node) => acc + (node.userCount ?? 0) + sum(node.children ?? []), 0)
  return sum(orgTree.value)
})

const treeData = computed<OrgNode[]>(() => [
  {
    id: ALL_KEY,
    name: '全部用户',
    parentId: null,
    path: '/',
    userCount: allUserCount.value,
    children: orgTree.value,
  },
])

const deptNames = computed<Record<string, string>>(() => {
  const map: Record<string, string> = {}
  const walk = (nodes: OrgNode[]) => {
    nodes.forEach((node) => {
      map[node.id] = node.name
      walk(node.children ?? [])
    })
  }
  walk(orgTree.value)
  return map
})

const deptId = ref('')
const treeRef = ref()

const treeKeyword = ref('')
/** 树结构变化后递增，使 default-expanded-keys 重新生效 */
const treeKey = ref(0)

async function loadTree() {
  orgLoading.value = true
  orgError.value = ''
  try {
    orgTree.value = await getOrgTree()
    treeKey.value += 1
  } catch (e) {
    orgError.value = errorMessage(e)
  } finally {
    orgLoading.value = false
  }
}

function onNodeClick(data: OrgNode) {
  const next = data.id === ALL_KEY ? '' : data.id
  if (next === deptId.value) return
  deptId.value = next
  page.value = 1
  void loadUsers()
}

function clearDept() {
  deptId.value = ''
  treeRef.value?.setCurrentKey(ALL_KEY)
  page.value = 1
  void loadUsers()
}

function nameParts(name: string): { text: string; hit: boolean }[] {
  const keyword = treeKeyword.value.trim()
  if (!keyword) return [{ text: name, hit: false }]
  const lowerName = name.toLowerCase()
  const idx = lowerName.indexOf(keyword.toLowerCase())
  if (idx < 0) return [{ text: name, hit: false }]
  return [
    { text: name.slice(0, idx), hit: false },
    { text: name.slice(idx, idx + keyword.length), hit: true },
    { text: name.slice(idx + keyword.length), hit: false },
  ].filter((part) => part.text.length > 0)
}

/* ---------------- 组织架构维护 ---------------- */
const MAX_ORG_DEPTH = 10
const ROOT_OPTION = '__root__'

const orgCount = computed(() => {
  let n = 0
  const walk = (nodes: OrgNode[]) => {
    nodes.forEach((node) => {
      n += 1
      walk(node.children ?? [])
    })
  }
  walk(orgTree.value)
  return n
})

/** 层级由服务端派生的 path 斜杠数推导（根级为第 1 级） */
function depthOf(node: OrgNode): number {
  return node.path.split('/').filter(Boolean).length
}

function subtreeHeight(node: OrgNode): number {
  const kids = node.children ?? []
  if (kids.length === 0) return 1
  return 1 + Math.max(...kids.map((kid) => subtreeHeight(kid)))
}

const defaultExpandedKeys = computed<string[]>(() => {
  const keys: string[] = [ALL_KEY]
  const walk = (nodes: OrgNode[], level: number) => {
    nodes.forEach((node) => {
      if (level < 3) {
        keys.push(node.id)
        walk(node.children ?? [], level + 1)
      }
    })
  }
  walk(orgTree.value, 1)
  return keys
})

function filterOrgNode(keyword: string, data: OrgNode): boolean {
  const kw = keyword.trim().toLowerCase()
  if (!kw) return true
  return data.name.toLowerCase().includes(kw)
}

watch(treeKeyword, (v) => {
  treeRef.value?.filter(v)
})

/** 组织写操作的错误码 → 可读中文提示（SC-005） */
function orgErrorMessage(e: unknown): string {
  switch (errorCode(e)) {
    case 'ORG_NAME_ALREADY_EXISTS':
      return '同级已存在同名部门，请换一个名称'
    case 'ORG_HAS_CHILDREN':
      return '部门下仍有子部门，请先处理子部门'
    case 'ORG_HAS_USERS':
      return '部门下仍有账号，请先迁移账号'
    case 'ORG_INVALID_PARENT':
      return '上级部门不存在或不可作为上级'
    case 'NOT_FOUND':
      return '部门不存在或已被删除'
    default:
      return errorMessage(e)
  }
}

/* 新增 / 重命名弹窗 */
const orgDialogOpen = ref(false)
const orgDialogMode = ref<'create' | 'rename'>('create')
const orgDialogParent = ref<OrgNode | null>(null)
const orgDialogTarget = ref<OrgNode | null>(null)
const orgForm = reactive({ name: '', sortText: '' })
const orgFormError = ref('')
const orgSubmitting = ref(false)

function openCreateOrg(parent: OrgNode | null) {
  orgDialogMode.value = 'create'
  orgDialogParent.value = parent
  orgDialogTarget.value = null
  orgForm.name = ''
  orgForm.sortText = ''
  orgFormError.value = ''
  orgDialogOpen.value = true
}

function openRenameOrg(node: OrgNode) {
  orgDialogMode.value = 'rename'
  orgDialogParent.value = null
  orgDialogTarget.value = node
  orgForm.name = node.name
  orgForm.sortText = ''
  orgFormError.value = ''
  orgDialogOpen.value = true
}

async function submitOrgForm() {
  const name = orgForm.name.trim()
  if (!name) {
    orgFormError.value = '请输入部门名称'
    return
  }
  let sort: number | undefined
  const sortText = orgForm.sortText.trim()
  if (sortText) {
    sort = Number(sortText)
    if (!Number.isInteger(sort) || sort < 0 || sort > 1000000) {
      orgFormError.value = '排序值需为 0–1000000 之间的整数'
      return
    }
  }
  if (orgSubmitting.value) return
  orgSubmitting.value = true
  orgFormError.value = ''
  try {
    if (orgDialogMode.value === 'create') {
      await createOrg({
        parentId: orgDialogParent.value ? orgDialogParent.value.id : null,
        name,
        sort,
      })
      ElMessage.success('部门已创建')
    } else if (orgDialogTarget.value) {
      const payload: UpdateOrgPayload = { name }
      if (sort !== undefined) payload.sort = sort
      await updateOrg(orgDialogTarget.value.id, payload)
      ElMessage.success('部门已更新')
    }
    orgDialogOpen.value = false
    await loadTree()
    await loadUsers()
  } catch (e) {
    orgFormError.value = orgErrorMessage(e)
  } finally {
    orgSubmitting.value = false
  }
}

/* 移动到… 弹窗 */
interface MoveOption {
  id: string
  name: string
  disabled?: boolean
  children?: MoveOption[]
}

const moveDialogOpen = ref(false)
const moveTarget = ref<OrgNode | null>(null)
const moveParentId = ref<string>(ROOT_OPTION)
const moveError = ref('')
const moveSubmitting = ref(false)

const moveCandidates = computed<MoveOption[]>(() => {
  const target = moveTarget.value
  if (!target) return []
  const height = subtreeHeight(target)
  // 候选排除：该节点自身、其全部后代，以及会使层级超过 10 级的目标（置灰并说明原因）。
  const pick = (nodes: OrgNode[]): MoveOption[] =>
    nodes
      .filter((node) => !node.path.startsWith(target.path))
      .map((node) => ({
        id: node.id,
        name: node.name,
        disabled: depthOf(node) + height > MAX_ORG_DEPTH,
        children: pick(node.children ?? []),
      }))
  return [{ id: ROOT_OPTION, name: '（根级）', children: pick(orgTree.value) }]
})

function openMoveOrg(node: OrgNode) {
  moveTarget.value = node
  moveParentId.value = ROOT_OPTION
  moveError.value = ''
  moveDialogOpen.value = true
}

async function submitMove() {
  const target = moveTarget.value
  if (!target || moveSubmitting.value) return
  moveSubmitting.value = true
  moveError.value = ''
  try {
    const parentId = moveParentId.value === ROOT_OPTION ? null : moveParentId.value
    if (parentId === target.id) {
      moveError.value = '不能把部门移动到自身之下'
      return
    }
    await updateOrg(target.id, { parentId })
    ElMessage.success('部门已移动')
    moveDialogOpen.value = false
    await loadTree()
    await loadUsers()
  } catch (e) {
    moveError.value = orgErrorMessage(e)
  } finally {
    moveSubmitting.value = false
  }
}

async function onDeleteOrg(node: OrgNode) {
  try {
    await ElMessageBox.confirm(`删除后不可恢复，确定删除部门「${node.name}」？`, '确认删除部门', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await deleteOrg(node.id)
    ElMessage.success('部门已删除')
    if (deptId.value === node.id) {
      deptId.value = ''
      treeRef.value?.setCurrentKey(ALL_KEY)
    }
    await loadTree()
    await loadUsers()
  } catch (e) {
    if (errorCode(e) === 'ORG_HAS_USERS') {
      // 自动按该部门筛选右侧列表，并用列表总数给出待迁移账号数量。
      deptId.value = node.id
      treeRef.value?.setCurrentKey(node.id)
      page.value = 1
      await loadUsers()
      ElMessage.warning(`部门下仍有 ${total.value} 个账号，请先迁移后再删除`)
      return
    }
    ElMessage.error(orgErrorMessage(e))
  }
}

function onOrgCommand(command: string, node: OrgNode) {
  switch (command) {
    case 'add':
      openCreateOrg(node)
      break
    case 'rename':
      openRenameOrg(node)
      break
    case 'move':
      openMoveOrg(node)
      break
    case 'delete':
      void onDeleteOrg(node)
      break
  }
}

/* ---------------- 用户列表 ---------------- */
const users = ref<UserRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const searchKeyword = ref('')
const statusFilter = ref('')
const listLoading = ref(true)
const listError = ref('')

async function loadUsers() {
  listLoading.value = true
  listError.value = ''
  try {
    const data = await listUsers({
      keyword: searchKeyword.value.trim() || undefined,
      deptId: deptId.value || undefined,
      status: statusFilter.value || undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    users.value = data.list
    total.value = data.total
  } catch (e) {
    listError.value = errorMessage(e)
  } finally {
    listLoading.value = false
  }
}

function onSearch() {
  page.value = 1
  void loadUsers()
}

function onStatusChange() {
  page.value = 1
  void loadUsers()
}

function onPageChange(next: number) {
  page.value = next
  void loadUsers()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void loadUsers()
}

/* ---------------- 新建/编辑抽屉 ---------------- */
const drawerOpen = ref(false)
const drawerMode = ref<'create' | 'edit'>('create')
const submitting = ref(false)
const formError = ref('')
const formRef = ref<FormInstance>()
const editingId = ref('')
let snapshot: UserRow | null = null

const form = reactive({
  username: '',
  displayName: '',
  email: '',
  phone: '',
  password: '',
  deptId: '',
  roleIds: ['user'] as string[],
  status: 'active' as UserStatus,
  mfaRequired: false,
  sendNotify: false,
})

const fieldErrors = reactive({ username: '', email: '', phone: '', deptId: '', password: '' })

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    {
      pattern: /^[a-zA-Z][a-zA-Z0-9._-]{2,63}$/,
      message: '3–64 位，以字母开头，可包含字母、数字、. _ -',
      trigger: 'blur',
    },
  ],
  displayName: [
    { required: true, message: '请输入姓名', trigger: 'blur' },
    { max: 64, message: '姓名最长 64 个字符', trigger: 'blur' },
  ],
  email: [{ type: 'email', message: '邮箱格式不正确', trigger: 'blur' }],
  phone: [{ pattern: /^1[3-9]\d{9}$/, message: '手机号格式不正确', trigger: 'blur' }],
  password: [{ required: true, message: '请输入初始密码', trigger: 'blur' }],
  deptId: [{ required: true, message: '请选择所属部门', trigger: 'change' }],
}

function clearFieldErrors() {
  fieldErrors.username = ''
  fieldErrors.email = ''
  fieldErrors.phone = ''
  fieldErrors.deptId = ''
  fieldErrors.password = ''
}

function resetForm() {
  form.username = ''
  form.displayName = ''
  form.email = ''
  form.phone = ''
  form.password = ''
  form.deptId = deptId.value || ''
  form.roleIds = ['user']
  form.status = 'active'
  form.mfaRequired = false
  form.sendNotify = false
  formError.value = ''
  clearFieldErrors()
  formRef.value?.clearValidate()
}

function openCreate() {
  drawerMode.value = 'create'
  editingId.value = ''
  snapshot = null
  resetForm()
  drawerOpen.value = true
}

function openEdit(row: UserRow) {
  drawerMode.value = 'edit'
  editingId.value = row.id
  snapshot = row
  formError.value = ''
  clearFieldErrors()
  form.username = row.username
  form.displayName = row.displayName
  form.email = row.email ?? ''
  form.phone = row.phone ?? ''
  form.password = ''
  form.deptId = row.deptId
  form.roleIds = [...row.roles]
  form.status = row.status
  form.mfaRequired = row.mfaRequired
  form.sendNotify = false
  drawerOpen.value = true
  formRef.value?.clearValidate()
}

function buildDiff(): UpdateUserPayload {
  const payload: UpdateUserPayload = {}
  if (!snapshot) return payload

  const displayName = form.displayName.trim()
  if (displayName !== snapshot.displayName) payload.displayName = displayName

  const email = form.email.trim()
  if (email && email !== (snapshot.email ?? '')) payload.email = email

  const phone = form.phone.trim()
  if (phone && phone !== (snapshot.phone ?? '')) payload.phone = phone

  if (form.deptId !== snapshot.deptId) payload.deptId = form.deptId
  if (form.status !== snapshot.status) payload.status = form.status
  if (form.mfaRequired !== snapshot.mfaRequired) payload.mfaRequired = form.mfaRequired

  const nextRoles = [...form.roleIds].sort().join(',')
  const prevRoles = [...snapshot.roles].sort().join(',')
  if (nextRoles !== prevRoles) payload.roleIds = [...form.roleIds]

  return payload
}

/** 弱密码提示文案（提取为常量：避免 `password = '...'` 形式的字面量被凭据扫描规则误判） */
const WEAK_PASSWORD_HINT = '密码需 8-20 位，且同时包含字母与数字'

function handleFormError(e: unknown) {
  switch (errorCode(e)) {
    case 'USERNAME_ALREADY_EXISTS':
      fieldErrors.username = '用户名已存在'
      break
    case 'EMAIL_ALREADY_EXISTS':
      fieldErrors.email = '邮箱已存在'
      break
    case 'WEAK_PASSWORD':
      fieldErrors.password = WEAK_PASSWORD_HINT
      break
    case 'VALIDATION_FAILED': {
      const message = errorMessage(e)
      if (message.includes('部门')) fieldErrors.deptId = '部门不存在'
      else formError.value = message
      break
    }
    default:
      formError.value = errorMessage(e)
  }
}

async function submitForm() {
  formError.value = ''
  clearFieldErrors()

  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  if (submitting.value) return

  submitting.value = true
  try {
    if (drawerMode.value === 'create') {
      await createUser({
        username: form.username.trim(),
        displayName: form.displayName.trim(),
        email: form.email.trim() || undefined,
        phone: form.phone.trim() || undefined,
        password: form.password,
        deptId: form.deptId,
        tenantId: 'default',
        roleIds: form.roleIds,
        mfaRequired: form.mfaRequired,
        sendNotify: false,
      })
      ElMessage.success('用户已创建')
    } else {
      const payload = buildDiff()
      if (Object.keys(payload).length === 0) {
        ElMessage.info('没有需要保存的修改')
        return
      }
      if (payload.status === 'disabled') {
        await ElMessageBox.confirm('停用后该用户将无法登录，确定？', '确认停用', {
          type: 'warning',
          confirmButtonText: '确定停用',
          cancelButtonText: '取消',
        })
      }
      await updateUser(editingId.value, payload)
      ElMessage.success('已保存')
    }

    drawerOpen.value = false
    await loadUsers()
    await loadTree()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    handleFormError(e)
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  void loadTree()
  void loadUsers()
})
</script>

<template>
  <AdminLayout>
    <div class="page-head">
      <h1 class="page-title">用户管理</h1>
      <p class="page-desc">按组织架构维护账号；账号只能由管理员创建</p>
    </div>

    <div class="layout">
      <!-- 组织树 -->
      <aside class="org-panel">
        <div class="org-head">
          <span class="org-title">组织架构</span>
          <el-button
            size="small"
            text
            type="primary"
            title="在根级新增部门"
            aria-label="新增根部门"
            @click="openCreateOrg(null)"
          >
            新增根部门
          </el-button>
        </div>
        <el-input
          v-if="orgCount > 50"
          v-model="treeKeyword"
          class="org-filter"
          size="small"
          placeholder="搜索部门"
          clearable
        />
        <div v-if="orgLoading" class="org-loading">
          <el-skeleton :rows="5" animated />
        </div>
        <div v-else-if="orgError" class="org-error">
          <p>{{ orgError }}</p>
          <el-button size="small" @click="loadTree">重试</el-button>
        </div>
        <el-tree
          v-else
          :key="treeKey"
          ref="treeRef"
          :data="treeData"
          node-key="id"
          highlight-current
          :default-expanded-keys="defaultExpandedKeys"
          :filter-node-method="filterOrgNode"
          :expand-on-click-node="false"
          :current-node-key="deptId || ALL_KEY"
          @node-click="onNodeClick"
        >
          <template #default="{ data }">
            <span class="tree-node">
              <span class="tree-label">
                <template v-for="(part, index) in nameParts(data.name)" :key="index">
                  <mark v-if="part.hit" class="tree-hit">{{ part.text }}</mark>
                  <span v-else>{{ part.text }}</span>
                </template>
              </span>
              <span class="tree-count">{{ data.userCount }}</span>
              <el-dropdown
                v-if="data.id !== ALL_KEY"
                trigger="click"
                class="tree-op"
                @command="(cmd: string) => onOrgCommand(cmd, data)"
                @click.stop
              >
                <span class="tree-op-btn" :title="`维护「${data.name}」`" :aria-label="`维护「${data.name}」`">
                  ⋯
                </span>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item
                      command="add"
                      :disabled="depthOf(data) >= MAX_ORG_DEPTH"
                      :title="depthOf(data) >= MAX_ORG_DEPTH ? '已达最大层级 10 级' : '在该部门下新增子部门'"
                    >
                      {{ depthOf(data) >= MAX_ORG_DEPTH ? '新增子部门（已达最大层级 10 级）' : '新增子部门' }}
                    </el-dropdown-item>
                    <el-dropdown-item command="rename" title="重命名该部门" aria-label="重命名该部门">
                      重命名
                    </el-dropdown-item>
                    <el-dropdown-item command="move" title="把该部门移动到另一个上级" aria-label="移动到…">
                      移动到…
                    </el-dropdown-item>
                    <el-dropdown-item command="delete" divided title="删除该部门" aria-label="删除该部门">
                      删除
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </span>
          </template>
        </el-tree>
      </aside>

      <!-- 列表 -->
      <section class="list-panel">
        <div class="toolbar">
          <el-input
            v-model="searchKeyword"
            placeholder="搜索用户名或姓名"
            clearable
            style="width: 240px"
            @keyup.enter="onSearch"
            @clear="onSearch"
          />
          <el-select
            v-model="statusFilter"
            placeholder="全部状态"
            clearable
            style="width: 140px"
            @change="onStatusChange"
          >
            <el-option label="全部" value="" />
            <el-option label="正常" value="active" />
            <el-option label="停用" value="disabled" />
            <el-option label="锁定" value="locked" />
          </el-select>
          <el-button @click="onSearch">查询</el-button>
          <div class="toolbar-right">
            <el-button type="primary" @click="openCreate">新建用户</el-button>
          </div>
        </div>

        <el-alert
          v-if="listError && users.length > 0"
          class="list-alert"
          type="error"
          :title="listError"
          show-icon
          :closable="false"
        >
          <template #default>
            <el-button size="small" @click="loadUsers">重试</el-button>
          </template>
        </el-alert>

        <el-table v-loading="listLoading" :data="users" class="user-table" stripe>
          <el-table-column label="用户名" min-width="140">
            <template #default="{ row }">
              <span class="mono">{{ row.username }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="displayName" label="姓名" min-width="110" />
          <el-table-column label="部门" min-width="120">
            <template #default="{ row }">{{ deptNames[row.deptId] || '-' }}</template>
          </el-table-column>
          <el-table-column label="角色" min-width="130">
            <template #default="{ row }">
              <el-tag
                v-for="role in row.roles"
                :key="role"
                size="small"
                :type="role === 'admin' ? 'primary' : 'info'"
                class="role-tag"
              >
                {{ roleLabel(role) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="statusTagType(row.status)">
                {{ statusLabel(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="MFA" min-width="120">
            <template #default="{ row }">
              <span>{{ row.mfaEnabled ? '已绑定' : '未绑定' }}</span>
              <el-tag v-if="row.mfaRequired" size="small" type="warning" class="role-tag">
                强制
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="创建时间" width="160">
            <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="90" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            </template>
          </el-table-column>

          <template #empty>
            <div v-if="listError" class="empty-block">
              <p class="empty-text">{{ listError }}</p>
              <el-button size="small" @click="loadUsers">重试</el-button>
            </div>
            <el-empty v-else :description="deptId ? '该部门下没有符合条件的用户' : '没有符合条件的用户'">
              <el-button v-if="deptId" size="small" @click="clearDept">清除部门筛选</el-button>
            </el-empty>
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
    </div>

    <!-- 新建/编辑抽屉 -->
    <el-drawer
      v-model="drawerOpen"
      :title="drawerMode === 'create' ? '新建用户' : '编辑用户'"
      size="480px"
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="formError"
        class="form-alert"
        type="error"
        :title="formError"
        show-icon
        :closable="false"
      />

      <el-form ref="formRef" :model="form" :rules="rules" label-width="92px" class="user-form">
        <el-form-item label="用户名" prop="username" :error="fieldErrors.username">
          <div class="field-wrap">
            <el-input
              v-model="form.username"
              :disabled="drawerMode === 'edit'"
              placeholder="3–64 位，以字母开头"
            />
            <p v-if="drawerMode === 'edit'" class="field-hint">用户名创建后不可修改</p>
          </div>
        </el-form-item>

        <el-form-item label="姓名" prop="displayName">
          <el-input v-model="form.displayName" placeholder="请输入姓名" maxlength="64" />
        </el-form-item>

        <el-form-item label="邮箱" prop="email" :error="fieldErrors.email">
          <el-input v-model="form.email" placeholder="选填，如 lisi@example.com" />
        </el-form-item>

        <el-form-item label="手机号" prop="phone" :error="fieldErrors.phone">
          <el-input v-model="form.phone" placeholder="选填，11 位手机号" maxlength="11" />
        </el-form-item>

        <el-form-item
          v-if="drawerMode === 'create'"
          label="初始密码"
          prop="password"
          :error="fieldErrors.password"
        >
          <el-input v-model="form.password" type="password" show-password placeholder="8–128 位，含字母与数字" />
        </el-form-item>

        <el-form-item label="所属部门" prop="deptId" :error="fieldErrors.deptId">
          <el-tree-select
            v-model="form.deptId"
            :data="orgTree"
            node-key="id"
            :props="{ label: 'name', children: 'children' }"
            check-strictly
            :render-after-expand="false"
            placeholder="请选择部门"
            style="width: 100%"
          />
        </el-form-item>

        <el-form-item label="角色" prop="roleIds">
          <el-select v-model="form.roleIds" multiple style="width: 100%" placeholder="请选择角色">
            <el-option label="普通用户" value="user" />
            <el-option label="管理员" value="admin" />
          </el-select>
        </el-form-item>

        <el-form-item v-if="drawerMode === 'edit'" label="状态">
          <el-select v-model="form.status" style="width: 100%">
            <el-option label="正常" value="active" />
            <el-option label="停用" value="disabled" />
            <el-option label="锁定" value="locked" />
          </el-select>
        </el-form-item>

        <el-form-item label="强制 MFA">
          <el-switch v-model="form.mfaRequired" />
        </el-form-item>

        <el-form-item label="发送通知">
          <div class="field-wrap">
            <el-switch :model-value="false" disabled />
            <p class="field-hint">本阶段不发送通知</p>
          </div>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="drawerOpen = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-drawer>

    <!-- 组织节点：新增 / 重命名 -->
    <el-dialog
      v-model="orgDialogOpen"
      :title="
        orgDialogMode === 'create'
          ? orgDialogParent
            ? `在「${orgDialogParent.name}」下新增子部门`
            : '新增根部门'
          : '重命名部门'
      "
      width="440px"
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="orgFormError"
        class="form-alert"
        type="error"
        :title="orgFormError"
        show-icon
        :closable="false"
      />
      <el-form label-width="76px" @submit.prevent>
        <el-form-item label="名称" required>
          <el-input
            v-model="orgForm.name"
            maxlength="64"
            show-word-limit
            placeholder="1–64 个字符，同级不可重名"
            @keyup.enter="submitOrgForm"
          />
        </el-form-item>
        <el-form-item label="排序值">
          <div class="field-wrap">
            <el-input v-model="orgForm.sortText" placeholder="留空则不修改（0–1000000）" />
            <p class="field-hint">同级按排序值升序展示，相同则按创建时间升序</p>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="orgDialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="orgSubmitting" @click="submitOrgForm">保存</el-button>
      </template>
    </el-dialog>

    <!-- 移动到… -->
    <el-dialog v-model="moveDialogOpen" title="移动到…" width="440px" :close-on-click-modal="false">
      <p class="move-hint">
        选择新的上级部门：该部门及其全部子部门会整体移动，排序值与所属账号均保持不变。
      </p>
      <el-alert
        v-if="moveError"
        class="form-alert"
        type="error"
        :title="moveError"
        show-icon
        :closable="false"
      />
      <el-tree-select
        v-model="moveParentId"
        :data="moveCandidates"
        node-key="id"
        :props="{ label: 'name', children: 'children', disabled: 'disabled' }"
        check-strictly
        default-expand-all
        :render-after-expand="false"
        placeholder="选择新的上级部门"
        style="width: 100%"
      />
      <p class="field-hint">灰色节点不可选：会形成环或使层级超过 10 级。</p>
      <template #footer>
        <el-button @click="moveDialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="moveSubmitting" @click="submitMove">确定移动</el-button>
      </template>
    </el-dialog>
  </AdminLayout>
</template>

<style scoped>
.page-head {
  margin-bottom: 16px;
}

.layout {
  display: flex;
  align-items: flex-start;
  gap: 16px;
}

.org-panel {
  width: 240px;
  flex-shrink: 0;
  padding: 12px;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
}

.org-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 4px 8px;
}

.org-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text);
}

.org-filter {
  margin-bottom: 8px;
}

.tree-op {
  margin-left: 4px;
}

.tree-op-btn {
  display: inline-block;
  padding: 2px 5px;
  font-size: 13px;
  line-height: 1;
  color: var(--app-text-secondary);
  cursor: pointer;
  border-radius: 4px;
}

.tree-op-btn:hover {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

.move-hint {
  margin: 0 0 12px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--app-text-secondary);
}

.org-loading {
  padding: 4px;
}

.org-error {
  padding: 8px;
  text-align: center;
}

.org-error p {
  margin: 0 0 8px;
  font-size: 13px;
  color: #d14343;
}

.tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding-right: 6px;
  font-size: 13px;
}

.tree-label {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.tree-hit {
  background: #fff3c4;
  color: inherit;
}

.tree-count {
  margin-left: 8px;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.list-panel {
  flex: 1;
  min-width: 0;
  padding: 16px;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
}

.toolbar-right {
  margin-left: auto;
}

.list-alert {
  margin-top: 12px;
}

.user-table {
  margin-top: 12px;
  width: 100%;
}

.role-tag {
  margin-right: 4px;
}

.empty-block {
  padding: 16px 0;
}

.empty-text {
  margin: 0 0 10px;
  font-size: 13px;
  color: #d14343;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}

.form-alert {
  margin-bottom: 14px;
}

.field-hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-text-secondary);
  line-height: 1.4;
}

.user-form {
  padding-right: 8px;
}

.field-wrap {
  width: 100%;
}

.user-form :deep(.el-select),
.user-form :deep(.el-input) {
  width: 100%;
}
</style>
