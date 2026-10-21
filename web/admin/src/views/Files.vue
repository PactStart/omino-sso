<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { listFiles, listUsers, updateFileStatus } from '@/api'
import { errorMessage } from '@/api/client'
import type { FileRow, UserRow } from '@/api/types'
import { formatTime } from '@/utils/format'

const SCENE_LABEL: Record<string, string> = {
  app_logo: '应用 Logo',
  user_avatar: '用户头像',
}

const FILE_STATUS_LABEL: Record<string, string> = {
  active: '正常',
  disabled: '禁用',
  deleted: '已删除',
}

function sceneLabel(scene: string): string {
  return SCENE_LABEL[scene] ?? scene
}

function fileStatusLabel(status: string): string {
  return FILE_STATUS_LABEL[status] ?? status
}

function fileStatusTagType(status: string): 'success' | 'warning' | 'danger' | 'info' {
  if (status === 'active') return 'success'
  if (status === 'disabled') return 'warning'
  if (status === 'deleted') return 'danger'
  return 'info'
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}

const filters = reactive({ scene: '', status: '', creatorId: '' })
const rows = ref<FileRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const error = ref('')

// 创建人远程搜索（用户名/姓名），防抖后调用用户列表接口。
const userOptions = ref<UserRow[]>([])
const userLoading = ref(false)
let userSearchTimer: number | undefined

function searchUsers(query: string) {
  if (userSearchTimer) window.clearTimeout(userSearchTimer)
  userSearchTimer = window.setTimeout(() => {
    void fetchUsers(query)
  }, 300)
}

async function fetchUsers(query: string) {
  const kw = query.trim()
  if (!kw) {
    userOptions.value = []
    return
  }
  userLoading.value = true
  try {
    const data = await listUsers({ keyword: kw, page: 1, pageSize: 20 })
    userOptions.value = data.list
  } catch {
    userOptions.value = []
  } finally {
    userLoading.value = false
  }
}

async function loadFiles() {
  loading.value = true
  error.value = ''
  try {
    const data = await listFiles({
      scene: filters.scene || undefined,
      status: filters.status || undefined,
      creatorId: filters.creatorId || undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    rows.value = data.list
    total.value = data.total
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function onSearch() {
  page.value = 1
  void loadFiles()
}

function onReset() {
  filters.scene = ''
  filters.status = ''
  filters.creatorId = ''
  page.value = 1
  void loadFiles()
}

function onPageChange(next: number) {
  page.value = next
  void loadFiles()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void loadFiles()
}

async function setStatus(row: FileRow, status: 'active' | 'disabled' | 'deleted') {
  if (status === 'deleted') {
    try {
      await ElMessageBox.confirm(
        '删除后业务字段回退（Logo / 头像不再显示），且不可恢复。确定删除？',
        '删除文件',
        { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  try {
    await updateFileStatus(row.id, status)
    ElMessage.success(status === 'deleted' ? '已删除' : status === 'disabled' ? '已禁用' : '已启用')
    await loadFiles()
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

onMounted(loadFiles)
onBeforeUnmount(() => {
  if (userSearchTimer) window.clearTimeout(userSearchTimer)
})
</script>

<template>
  <AdminLayout>
    <div class="page-head">
      <h1 class="page-title">文件管理</h1>
      <p class="page-desc">集中查看所有上传的 Logo 与头像，可禁用或删除文件记录</p>
    </div>

    <div class="panel">
      <div class="toolbar">
        <el-select v-model="filters.scene" placeholder="全部类型" clearable style="width: 150px" @change="onSearch">
          <el-option label="全部类型" value="" />
          <el-option label="应用 Logo" value="app_logo" />
          <el-option label="用户头像" value="user_avatar" />
        </el-select>
        <el-select v-model="filters.status" placeholder="全部状态" clearable style="width: 140px" @change="onSearch">
          <el-option label="全部状态" value="" />
          <el-option label="正常" value="active" />
          <el-option label="禁用" value="disabled" />
          <el-option label="已删除" value="deleted" />
        </el-select>
        <el-select
          v-model="filters.creatorId"
          filterable
          remote
          clearable
          :remote-method="searchUsers"
          :loading="userLoading"
          placeholder="搜索创建人（用户名/姓名）"
          style="width: 240px"
          @change="onSearch"
        >
          <el-option
            v-for="u in userOptions"
            :key="u.id"
            :label="`${u.displayName}（${u.username}）`"
            :value="u.id"
          />
        </el-select>
        <el-button type="primary" @click="onSearch">查询</el-button>
        <el-button @click="onReset">重置</el-button>
      </div>

      <el-alert
        v-if="error && rows.length > 0"
        class="list-alert"
        type="error"
        :title="error"
        show-icon
        :closable="false"
      >
        <template #default>
          <el-button size="small" @click="loadFiles">重试</el-button>
        </template>
      </el-alert>

      <el-table v-loading="loading" :data="rows" class="file-table" stripe>
        <el-table-column label="预览" width="72">
          <template #default="{ row }">
            <el-image
              v-if="row.status !== 'deleted'"
              :src="row.url"
              fit="cover"
              class="file-thumb"
              :preview-src-list="[row.url]"
              preview-teleported
            />
            <span v-else class="file-deleted">已删除</span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="110">
          <template #default="{ row }">{{ sceneLabel(row.scene) }}</template>
        </el-table-column>
        <el-table-column label="格式" width="80">
          <template #default="{ row }">{{ row.format }}</template>
        </el-table-column>
        <el-table-column label="大小" width="100">
          <template #default="{ row }">{{ formatSize(row.size) }}</template>
        </el-table-column>
        <el-table-column label="地址" min-width="220">
          <template #default="{ row }">
            <a v-if="row.status !== 'deleted'" class="file-url" :href="row.url" target="_blank" rel="noopener">
              {{ row.url }}
            </a>
            <span v-else class="file-deleted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="fileStatusTagType(row.status)">
              {{ fileStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建人" width="140">
          <template #default="{ row }">{{ row.creatorName || row.creator || '-' }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'active'"
              link
              type="warning"
              @click="setStatus(row, 'disabled')"
            >
              禁用
            </el-button>
            <el-button
              v-if="row.status === 'disabled'"
              link
              type="success"
              @click="setStatus(row, 'active')"
            >
              启用
            </el-button>
            <el-button
              v-if="row.status !== 'deleted'"
              link
              type="danger"
              @click="setStatus(row, 'deleted')"
            >
              删除
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div v-if="error" class="empty-block">
            <p class="empty-text">{{ error }}</p>
            <el-button size="small" @click="loadFiles">重试</el-button>
          </div>
          <el-empty v-else description="没有符合条件的文件" />
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
    </div>
  </AdminLayout>
</template>

<style scoped>
.page-head {
  margin-bottom: 16px;
}

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
  flex-wrap: wrap;
}

.list-alert {
  margin-top: 12px;
}

.file-table {
  margin-top: 12px;
  width: 100%;
}

.file-thumb {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  border: 1px solid var(--app-border);
}

.file-url {
  color: var(--el-color-primary);
  word-break: break-all;
}

.file-deleted {
  color: var(--app-text-secondary);
  font-size: 12px;
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
</style>
