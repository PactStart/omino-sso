<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { listAuditLogs, listClients } from '@/api'
import { errorMessage } from '@/api/client'
import type { AuditQuery, AuditRow } from '@/api/types'
import { EVENT_LABEL, eventLabel, formatTime } from '@/utils/format'

const DAY = 24 * 60 * 60 * 1000

function defaultRange(): [Date, Date] {
  const now = new Date()
  return [new Date(now.getTime() - 7 * DAY), now]
}

const filters = reactive({ userId: '', clientId: '', eventType: '', result: '' })
const range = ref<[Date, Date]>(defaultRange())

const rows = ref<AuditRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const error = ref('')

const eventOptions = Object.entries(EVENT_LABEL).map(([value, label]) => ({ value, label }))

const clientOptions = ref<{ clientId: string; clientName: string }[]>([])
const clientOptionsLoaded = ref(false)

async function loadClientOptions() {
  if (clientOptionsLoaded.value) return
  clientOptionsLoaded.value = true
  try {
    const data = await listClients({ page: 1, pageSize: 100 })
    clientOptions.value = data.list.map((item) => ({
      clientId: item.clientId,
      clientName: item.clientName || item.clientId,
    }))
  } catch (e) {
    clientOptionsLoaded.value = false
    ElMessage.error(errorMessage(e))
  }
}

function buildQuery(): AuditQuery {
  const query: AuditQuery = { page: page.value, pageSize: pageSize.value }
  if (filters.userId.trim()) query.userId = filters.userId.trim()
  if (filters.clientId) query.clientId = filters.clientId
  if (filters.eventType) query.eventType = filters.eventType
  if (filters.result) query.result = filters.result
  if (range.value && range.value.length === 2) {
    query.from = range.value[0].toISOString()
    query.to = range.value[1].toISOString()
  }
  return query
}

async function loadLogs() {
  if (range.value && range.value.length === 2 && range.value[0] > range.value[1]) {
    ElMessage.error('开始时间不能晚于结束时间')
    return
  }
  loading.value = true
  error.value = ''
  try {
    const data = await listAuditLogs(buildQuery())
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
  void loadLogs()
}

function onReset() {
  filters.userId = ''
  filters.clientId = ''
  filters.eventType = ''
  filters.result = ''
  range.value = defaultRange()
  page.value = 1
  void loadLogs()
}

function onPageChange(next: number) {
  page.value = next
  void loadLogs()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void loadLogs()
}

function userText(row: AuditRow): string {
  if (row.username) return row.username
  if (row.userId) return row.userId.slice(0, 8)
  return '-'
}

function detailText(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}

onMounted(loadLogs)
</script>

<template>
  <AdminLayout>
    <div class="page-head">
      <h1 class="page-title">审计日志</h1>
      <p class="page-desc">检索全量认证事件，默认展示最近 7 天</p>
    </div>

    <section class="panel">
      <div class="filters">
        <el-input
          v-model="filters.userId"
          placeholder="用户 ID（26 位 ULID，精确匹配）"
          clearable
          maxlength="26"
          style="width: 260px"
          @keyup.enter="onSearch"
        />
        <el-select
          v-model="filters.clientId"
          placeholder="全部应用"
          clearable
          filterable
          style="width: 180px"
          @visible-change="(visible: boolean) => visible && loadClientOptions()"
        >
          <el-option
            v-for="item in clientOptions"
            :key="item.clientId"
            :label="item.clientName"
            :value="item.clientId"
          />
        </el-select>
        <el-select v-model="filters.eventType" placeholder="全部事件" clearable style="width: 170px">
          <el-option
            v-for="item in eventOptions"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </el-select>
        <el-select v-model="filters.result" placeholder="全部结果" clearable style="width: 130px">
          <el-option label="成功" value="success" />
          <el-option label="失败" value="failure" />
        </el-select>
        <el-date-picker
          v-model="range"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          style="width: 380px"
        />
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
          <el-button size="small" @click="loadLogs">重试</el-button>
        </template>
      </el-alert>

      <el-table v-loading="loading" :data="rows" class="audit-table" row-key="id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="detail-box">
              <template v-if="row.detail && Object.keys(row.detail).length">
                <div v-for="(value, key) in row.detail" :key="key" class="detail-line">
                  <span class="detail-key">{{ key }}</span>
                  <span class="mono detail-value">{{ detailText(value) }}</span>
                </div>
              </template>
              <span v-else class="detail-empty">无附加详情</span>
              <div class="detail-line">
                <span class="detail-key">userAgent</span>
                <span class="mono detail-value">{{ row.userAgent || '-' }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="160">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="事件" min-width="130">
          <template #default="{ row }">{{ eventLabel(row.eventType) }}</template>
        </el-table-column>
        <el-table-column label="用户" min-width="120">
          <template #default="{ row }">
            <span :class="{ mono: !row.username }">{{ userText(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="应用" min-width="110">
          <template #default="{ row }">
            <span class="mono">{{ row.clientId || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="IP" width="130">
          <template #default="{ row }">
            <span class="mono">{{ row.ip || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.result === 'success' ? 'success' : 'danger'">
              {{ row.result === 'success' ? '成功' : '失败' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="详情" min-width="200">
          <template #default="{ row }">
            <span class="detail-preview">{{ row.detail ? detailText(row.detail) : '-' }}</span>
          </template>
        </el-table-column>

        <template #empty>
          <div v-if="error" class="empty-block">
            <p class="empty-text">{{ error }}</p>
            <el-button size="small" @click="loadLogs">重试</el-button>
          </div>
          <el-empty v-else description="该条件下没有审计事件" />
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

.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.list-alert {
  margin-top: 12px;
}

.audit-table {
  margin-top: 12px;
  width: 100%;
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

.detail-box {
  padding: 8px 48px 12px;
  font-size: 13px;
}

.detail-line {
  display: flex;
  gap: 12px;
  padding: 3px 0;
}

.detail-key {
  width: 96px;
  flex-shrink: 0;
  color: var(--app-text-secondary);
}

.detail-value {
  word-break: break-all;
}

.detail-empty {
  color: var(--app-text-secondary);
}

.detail-preview {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--app-text-secondary);
}
</style>
