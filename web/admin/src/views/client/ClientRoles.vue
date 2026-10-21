<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import {
  createAppRole,
  deleteAppRole,
  listAppRoles,
  updateAppRole,
} from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type { AppRole } from '@/api/types'
import { formatTime, orDash } from '@/utils/format'

const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

const roles = ref<AppRole[]>([])
const loading = ref(true)
const listError = ref('')

async function loadRoles() {
  loading.value = true
  listError.value = ''
  try {
    const data = await listAppRoles(clientId.value)
    roles.value = data.list
  } catch (e) {
    listError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

/* ---------------- 新建 / 编辑 ---------------- */

const dialogOpen = ref(false)
const editing = ref<AppRole | null>(null)
const submitting = ref(false)
const formError = ref('')
const formRef = ref<FormInstance>()
const form = reactive({ name: '', description: '' })

const rules: FormRules = {
  name: [
    { required: true, message: '请输入角色名', trigger: 'blur' },
    {
      pattern: /^[A-Za-z0-9]{1,32}$/,
      message: '角色名只能由字母与数字组成，长度 1–32',
      trigger: 'blur',
    },
  ],
  description: [{ max: 100, message: '描述最长 100 个字符', trigger: 'blur' }],
}

function openCreate() {
  editing.value = null
  form.name = ''
  form.description = ''
  formError.value = ''
  formRef.value?.clearValidate()
  dialogOpen.value = true
}

function openEdit(row: AppRole) {
  editing.value = row
  form.name = row.name
  form.description = row.description ?? ''
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
  try {
    if (editing.value) {
      await updateAppRole(clientId.value, editing.value.id, {
        name: form.name.trim(),
        description: form.description.trim(),
        updatedAt: editing.value.updatedAt,
      })
      ElMessage.success('角色已更新')
    } else {
      await createAppRole(clientId.value, {
        name: form.name.trim(),
        description: form.description.trim() || undefined,
      })
      ElMessage.success('角色已创建')
    }
    dialogOpen.value = false
    await loadRoles()
  } catch (e) {
    switch (errorCode(e)) {
      case 'APP_ROLE_NAME_EXISTS':
        formError.value = '角色名已存在（应用内唯一，区分大小写）'
        break
      case 'APP_RESOURCE_STALE':
        formError.value = '该角色已被他人修改，请刷新后重试'
        await loadRoles()
        break
      case 'VALIDATION_FAILED':
        formError.value = errorMessage(e) || '输入不合法，请检查角色名与描述'
        break
      default:
        formError.value = errorMessage(e)
    }
  } finally {
    submitting.value = false
  }
}

/* ---------------- 删除 ---------------- */

async function remove(row: AppRole) {
  try {
    await ElMessageBox.confirm(
      `删除后不可恢复。将解除 ${row.permissionCount} 项权限绑定与 ${row.memberCount} 名成员绑定（成员本身不会被移出应用）。确定删除「${row.name}」？`,
      '删除角色',
      { type: 'warning', confirmButtonText: '确定删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await deleteAppRole(clientId.value, row.id)
    ElMessage.success('角色已删除')
    await loadRoles()
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

onMounted(loadRoles)
</script>

<template>
  <div class="roles">
    <section class="panel">
      <div class="panel-head">
        <div>
          <h2 class="panel-title">应用内角色</h2>
          <p class="panel-desc">
            应用内角色仅在本应用生效，与平台角色（roles）无关
          </p>
        </div>
        <el-button type="primary" @click="openCreate">新建角色</el-button>
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
          <el-button size="small" @click="loadRoles">重试</el-button>
        </template>
      </el-alert>

      <el-table v-loading="loading" :data="roles" class="role-table" stripe>
        <el-table-column label="角色名" min-width="150">
          <template #default="{ row }">
            <span class="mono">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="描述" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">{{ orDash(row.description) }}</template>
        </el-table-column>
        <el-table-column label="权限数" width="100">
          <template #default="{ row }">{{ row.permissionCount }}</template>
        </el-table-column>
        <el-table-column label="成员数" width="100">
          <template #default="{ row }">{{ row.memberCount }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="还没有角色。角色把权限打包后授予成员，建议先建 1 个角色" />
        </template>
      </el-table>
    </section>

    <el-dialog
      v-model="dialogOpen"
      :title="editing ? '编辑角色' : '新建角色'"
      width="480px"
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
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="角色名" prop="name">
          <el-input v-model="form.name" maxlength="32" placeholder="如：opsAdmin（仅字母数字）" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            maxlength="100"
            show-word-limit
            placeholder="选填，最长 100 字"
          />
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

.panel-head .el-button {
  margin-left: auto;
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
}

.block-alert {
  margin: 12px 0;
}

.role-table {
  margin-top: 12px;
  width: 100%;
}

.mono {
  font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
</style>
