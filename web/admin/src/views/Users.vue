<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { createUser, getOrgTree, listUsers, updateUser } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type { OrgNode, UpdateUserPayload, UserRow, UserStatus } from '@/api/types'
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

async function loadTree() {
  orgLoading.value = true
  orgError.value = ''
  try {
    orgTree.value = await getOrgTree()
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
  const keyword = searchKeyword.value.trim()
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

function handleFormError(e: unknown) {
  switch (errorCode(e)) {
    case 'USERNAME_ALREADY_EXISTS':
      fieldErrors.username = '用户名已存在'
      break
    case 'EMAIL_ALREADY_EXISTS':
      fieldErrors.email = '邮箱已存在'
      break
    case 'WEAK_PASSWORD':
      fieldErrors.password = '密码需 8–128 位，且同时包含字母与数字'
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
        <div class="org-title">组织架构</div>
        <div v-if="orgLoading" class="org-loading">
          <el-skeleton :rows="5" animated />
        </div>
        <div v-else-if="orgError" class="org-error">
          <p>{{ orgError }}</p>
          <el-button size="small" @click="loadTree">重试</el-button>
        </div>
        <el-tree
          v-else
          ref="treeRef"
          :data="treeData"
          node-key="id"
          highlight-current
          default-expand-all
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

.org-title {
  padding: 4px 8px 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--app-text);
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
