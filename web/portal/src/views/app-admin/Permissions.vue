<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  ElAlert,
  ElButton,
  ElDialog,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElLoading,
  ElMessage,
  ElMessageBox,
  ElRadioButton,
  ElRadioGroup,
  ElTable,
  ElTableColumn,
  ElTree,
  ElTreeSelect,
  type FormInstance,
  type FormRules,
} from './ep'
import {
  createAppPermission,
  deleteAppPermission,
  listAppPermissions,
  updateAppPermission,
} from '@/api/app'
import { errorCode, errorMessage } from '@/api/client'
import type { AppPermission } from '@/api/types'
import { formatTime, orDash } from '@/utils/format'

const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

// v-loading 指令需局部注册（portal 未全局安装 Element Plus）。
const vLoading = ElLoading.directive

interface PermissionNode extends AppPermission {
  children: PermissionNode[]
}

interface SelectNode {
  id: string
  name: string
  code: string
  disabled?: boolean
  children: SelectNode[]
}

const permissions = ref<AppPermission[]>([])
const loading = ref(true)
const listError = ref('')

const VIEW_KEY = 'portal:app-permissions-view'
const viewMode = ref<'tree' | 'list'>(
  localStorage.getItem(VIEW_KEY) === 'list' ? 'list' : 'tree',
)

function switchView(mode: 'tree' | 'list') {
  viewMode.value = mode
  localStorage.setItem(VIEW_KEY, mode)
}

async function loadPermissions() {
  loading.value = true
  listError.value = ''
  try {
    const data = await listAppPermissions(clientId.value)
    permissions.value = data.list
  } catch (e) {
    listError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

const codeMap = computed(() => {
  const map = new Map<string, string>()
  for (const p of permissions.value) map.set(p.id, p.code)
  return map
})

const treeData = computed<PermissionNode[]>(() => {
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

/** 默认展开前 3 层，超过 3 层的节点默认折叠（spec 边界场景） */
const defaultExpandedKeys = computed(() => {
  const keys: string[] = []
  const walk = (nodes: PermissionNode[], depth: number) => {
    for (const n of nodes) {
      if (depth < 3 && n.children.length > 0) {
        keys.push(n.id)
        walk(n.children, depth + 1)
      }
    }
  }
  walk(treeData.value, 1)
  return keys
})

/* ---------------- 新建 / 编辑 ---------------- */

const dialogOpen = ref(false)
const submitting = ref(false)
const formError = ref('')
const formRef = ref<FormInstance>()

const form = reactive({
  id: '',
  updatedAt: 0,
  code: '',
  name: '',
  description: '',
  parentId: '' as string,
  sort: 0,
})

/** 父级选择器候选：排除自身与全部后代，避免成环（FR-023） */
/**
 * el-tree-select 默认按节点的 `label` 字段显示文本；权限节点用的字段是 `name`
 * （值为「权限名称（权限编码）」）。不显式映射会回落到显示 value（ULID），
 * 下拉里全是不可读的 ID。
 */
const parentSelectProps = { label: 'name', children: 'children' }

const parentOptions = computed<SelectNode[]>(() => {
  const blocked = new Set<string>()
  if (form.id) {
    const childrenOf = new Map<string, string[]>()
    for (const p of permissions.value) {
      if (!p.parentId) continue
      const list = childrenOf.get(p.parentId) ?? []
      list.push(p.id)
      childrenOf.set(p.parentId, list)
    }
    const stack = [form.id]
    while (stack.length > 0) {
      const cur = stack.pop()!
      if (blocked.has(cur)) continue
      blocked.add(cur)
      for (const child of childrenOf.get(cur) ?? []) stack.push(child)
    }
  }

  const build = (nodes: PermissionNode[]): SelectNode[] =>
    nodes.map((n) => ({
      id: n.id,
      name: `${n.name}（${n.code}）`,
      code: n.code,
      disabled: blocked.has(n.id),
      children: build(n.children),
    }))
  return build(treeData.value)
})

const rules: FormRules = {
  code: [
    { required: true, message: '请输入权限编码', trigger: 'blur' },
    {
      pattern: /^[A-Za-z0-9_.:-]{1,64}$/,
      message: '编码只能含字母、数字与 _ - . :，长度 1–64',
      trigger: 'blur',
    },
  ],
  name: [
    { required: true, message: '请输入权限名称', trigger: 'blur' },
    { max: 64, message: '名称最长 64 个字符', trigger: 'blur' },
  ],
  description: [{ max: 255, message: '描述最长 255 个字符', trigger: 'blur' }],
}

function openCreate(parentId?: string) {
  form.id = ''
  form.updatedAt = 0
  form.code = ''
  form.name = ''
  form.description = ''
  form.parentId = parentId ?? ''
  form.sort = 0
  formError.value = ''
  formRef.value?.clearValidate()
  dialogOpen.value = true
}

function openEdit(row: AppPermission) {
  form.id = row.id
  form.updatedAt = row.updatedAt
  form.code = row.code
  form.name = row.name
  form.description = row.description ?? ''
  form.parentId = row.parentId ?? ''
  form.sort = row.sort
  formError.value = ''
  formRef.value?.clearValidate()
  dialogOpen.value = true
}

async function submit() {
  formError.value = ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  if (submitting.value) return

  submitting.value = true
  const parentId = form.parentId || null
  try {
    if (form.id) {
      await updateAppPermission(clientId.value, form.id, {
        code: form.code.trim(),
        name: form.name.trim(),
        description: form.description.trim(),
        parentId,
        sort: form.sort,
        updatedAt: form.updatedAt,
      })
      ElMessage.success('权限已更新')
    } else {
      await createAppPermission(clientId.value, {
        code: form.code.trim(),
        name: form.name.trim(),
        description: form.description.trim() || undefined,
        parentId,
        sort: form.sort,
      })
      ElMessage.success('权限已创建')
    }
    dialogOpen.value = false
    await loadPermissions()
  } catch (e) {
    switch (errorCode(e)) {
      case 'APP_PERMISSION_CODE_EXISTS':
        formError.value = '权限编码已存在（应用内唯一，区分大小写）'
        break
      case 'APP_PERMISSION_INVALID_PARENT':
        formError.value = '父级不合法：不能选择自身或其后代'
        break
      case 'APP_RESOURCE_STALE':
        formError.value = '该权限已被他人修改，请刷新后重试'
        await loadPermissions()
        break
      default:
        formError.value = errorMessage(e)
    }
  } finally {
    submitting.value = false
  }
}

/* ---------------- 删除 ---------------- */

async function remove(row: AppPermission) {
  if (row.childCount > 0) {
    ElMessage.error(`该权限仍有 ${row.childCount} 个子权限，请先删除或移走子节点`)
    return
  }
  const refText =
    row.roleCount > 0 ? `，并被 ${row.roleCount} 个角色解除引用` : ''
  try {
    await ElMessageBox.confirm(
      `删除后不可恢复${refText}。确定删除「${row.name}（${row.code}）」？`,
      '删除权限',
      { type: 'warning', confirmButtonText: '确定删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await deleteAppPermission(clientId.value, row.id)
    ElMessage.success('权限已删除')
    await loadPermissions()
  } catch (e) {
    if (errorCode(e) === 'APP_PERMISSION_HAS_CHILDREN') {
      ElMessage.error(errorMessage(e))
      await loadPermissions()
      return
    }
    ElMessage.error(errorMessage(e))
  }
}

onMounted(loadPermissions)
</script>

<template>
  <div class="permissions">
    <section class="panel">
      <div class="panel-head">
        <div>
          <h2 class="panel-title">应用权限</h2>
          <p class="panel-desc">
            权限点是接入方做功能级鉴权的原子单位，通过角色授予成员；与 OIDC 的 scope 是两套概念
          </p>
        </div>
        <div class="head-actions">
          <el-radio-group :model-value="viewMode" size="small" @change="switchView($event as 'tree' | 'list')">
            <el-radio-button value="tree">树形</el-radio-button>
            <el-radio-button value="list">列表</el-radio-button>
          </el-radio-group>
          <el-button type="primary" @click="openCreate()">新建权限</el-button>
        </div>
      </div>

      <el-alert
        v-if="listError"
        type="error"
        :title="listError"
        show-icon
        :closable="false"
        class="block-alert"
      >
        <template #default>
          <el-button size="small" @click="loadPermissions">重试</el-button>
        </template>
      </el-alert>

      <!-- 树形视图 -->
      <div v-if="viewMode === 'tree'" v-loading="loading" class="tree-wrap">
        <el-tree
          v-if="treeData.length > 0"
          :data="treeData"
          node-key="id"
          :default-expanded-keys="defaultExpandedKeys"
          :props="{ label: 'name', children: 'children' }"
          class="perm-tree"
        >
          <template #default="{ data }">
            <div class="tree-row">
              <span class="tree-name">{{ data.name }}</span>
              <span class="mono tree-code">{{ data.code }}</span>
              <span v-if="data.childCount > 0" class="tree-count">子权限 {{ data.childCount }}</span>
              <span v-if="data.roleCount > 0" class="tree-count">被 {{ data.roleCount }} 个角色引用</span>
              <span class="tree-actions">
                <el-button link type="primary" size="small" @click.stop="openCreate(data.id)">
                  加子权限
                </el-button>
                <el-button link type="primary" size="small" @click.stop="openEdit(data)">编辑</el-button>
                <el-button link type="danger" size="small" @click.stop="remove(data)">删除</el-button>
              </span>
            </div>
          </template>
        </el-tree>
        <el-empty v-else description="还没有权限点" />
      </div>

      <!-- 列表视图 -->
      <el-table v-else v-loading="loading" :data="permissions" class="perm-table" stripe>
        <el-table-column label="编码" min-width="170">
          <template #default="{ row }">
            <span class="mono">{{ row.code }}</span>
          </template>
        </el-table-column>
        <el-table-column label="名称" min-width="140">
          <template #default="{ row }">{{ orDash(row.name) }}</template>
        </el-table-column>
        <el-table-column label="父级" min-width="150">
          <template #default="{ row }">
            <span class="mono">{{ row.parentId ? orDash(codeMap.get(row.parentId)) : '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="描述" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ orDash(row.description) }}</template>
        </el-table-column>
        <el-table-column label="排序" width="80">
          <template #default="{ row }">{{ row.sort }}</template>
        </el-table-column>
        <el-table-column label="子权限" width="90">
          <template #default="{ row }">{{ row.childCount }}</template>
        </el-table-column>
        <el-table-column label="引用角色" width="96">
          <template #default="{ row }">{{ row.roleCount }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="还没有权限点" />
        </template>
      </el-table>
    </section>

    <el-dialog
      v-model="dialogOpen"
      :title="form.id ? '编辑权限' : '新建权限'"
      width="520px"
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="formError"
        type="error"
        :title="formError"
        show-icon
        :closable="false"
        class="block-alert"
      />
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="权限编码" prop="code">
          <el-input v-model="form.code" maxlength="64" placeholder="如：order:read" class="mono" />
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" maxlength="64" placeholder="如：订单查询" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            maxlength="255"
            show-word-limit
            placeholder="选填"
          />
        </el-form-item>
        <el-form-item label="父级">
          <el-tree-select
            v-model="form.parentId"
            :data="parentOptions"
            :props="parentSelectProps"
            node-key="id"
            check-strictly
            clearable
            placeholder="留空表示根权限"
            style="width: 100%"
          />
          <p class="field-hint">不能选择自身或自己的后代</p>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" :max="9999" />
          <p class="field-hint">同级按排序值升序展示</p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">保存</el-button>
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

.panel-head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.head-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 12px;
}

.panel-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--app-text);
}

.panel-desc {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-text-secondary);
  max-width: 620px;
  line-height: 1.6;
}

.block-alert {
  margin: 12px 0;
}

.tree-wrap {
  margin-top: 12px;
  min-height: 120px;
}

.perm-tree {
  --el-tree-node-content-height: 34px;
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
}

.tree-name {
  font-size: 14px;
  color: var(--app-text);
}

.tree-code {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.tree-count {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.tree-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  opacity: 0;
  transition: opacity 0.15s;
}

.tree-row:hover .tree-actions {
  opacity: 1;
}

.perm-table {
  margin-top: 12px;
  width: 100%;
}

.field-hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.mono {
  font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
</style>
