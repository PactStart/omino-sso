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

interface DetailEntry {
  key: string
  text: string
  /** 对象/数组等结构化值按缩进 JSON 以代码块展示 */
  block: boolean
}

/**
 * detail 列是服务端存的 JSON 文本（如 {"message":"invalid password"}），不是对象；
 * 直接对它 v-for 会把字符串按字符迭代成"一字符一行"，必须先解析。
 */
function parseDetailRow(row: AuditRow): Record<string, unknown> | null {
  if (!row.detail) return null
  try {
    const value: unknown = JSON.parse(row.detail)
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return value as Record<string, unknown>
    }
  } catch {
    // 历史数据可能不是合法 JSON，按原文展示
  }
  return null
}

function detailValueText(value: unknown): { text: string; block: boolean } {
  if (value === null || value === undefined) return { text: '-', block: false }
  if (typeof value === 'string') return { text: value, block: false }
  if (typeof value === 'number' || typeof value === 'boolean') return { text: String(value), block: false }
  return { text: JSON.stringify(value, null, 2), block: true }
}

function detailEntries(row: AuditRow): DetailEntry[] {
  const parsed = parseDetailRow(row)
  if (!parsed) return []
  return Object.entries(parsed).map(([key, value]) => {
    const { text, block } = detailValueText(value)
    return { key, text, block }
  })
}

/** 折叠态单行预览：优先 message，其余拼成 key=value，超长由 CSS 截断 */
function detailPreview(row: AuditRow): string {
  const entries = detailEntries(row)
  if (entries.length === 0) return row.detail || '-'
  const message = entries.find((entry) => entry.key === 'message')
  if (message && !message.block) return message.text
  return entries
    .map((entry) => `${entry.key}=${entry.text.replace(/\s+/g, ' ')}`)
    .join('，')
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
              <template v-if="detailEntries(row).length">
                <div v-for="entry in detailEntries(row)" :key="entry.key" class="detail-line">
                  <span class="detail-key">{{ entry.key }}</span>
                  <pre v-if="entry.block" class="mono detail-value detail-json">{{ entry.text }}</pre>
                  <span v-else class="detail-value">{{ entry.text }}</span>
                </div>
              </template>
              <p v-else-if="row.detail" class="mono detail-raw">{{ row.detail }}</p>
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
            <span class="detail-preview" :title="detailPreview(row)">{{ detailPreview(row) }}</span>
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
  padding: 10px 48px 14px;
  font-size: 13px;
}

/* 键固定宽、值占满剩余宽度：flex 子项默认 min-width:auto 会被压到 1 字符宽 */
.detail-line {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 3px 0;
}

.detail-key {
  flex: 0 0 96px;
  padding-top: 1px;
  color: var(--app-text-secondary);
}

.detail-value {
  flex: 1 1 auto;
  min-width: 0;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.detail-json {
  margin: 0;
  padding: 6px 10px;
  font-size: 12px;
  background: #f7f8fc;
  border-radius: 6px;
}

.detail-raw {
  margin: 0 0 4px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
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
