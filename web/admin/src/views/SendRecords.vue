<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { listSendRecords } from '@/api'
import { errorMessage } from '@/api/client'
import type { SendRecordQuery, SendRecordRow } from '@/api/types'

const DAY = 24 * 60 * 60 * 1000

function defaultRange(): [Date, Date] {
  const now = new Date()
  return [new Date(now.getTime() - 7 * DAY), now]
}

const CHANNEL_LABEL: Record<string, string> = { sms: '短信', email: '邮件' }
const SCENE_LABEL: Record<string, string> = {
  bind_phone: '绑定手机号',
  bind_email: '绑定邮箱',
  login: '验证码登录',
  'find_password': '找回密码',
  mfa_unbind: '解绑 MFA',
}
const RESULT_LABEL: Record<string, string> = { success: '成功', failed: '失败' }
const PROVIDER_LABEL: Record<string, string> = {
  chuanglan: '创蓝',
  smtp: 'SMTP',
  mock: '模拟发送',
}

const filters = reactive({ channel: '', scene: '', result: '' })
const range = ref<[Date, Date]>(defaultRange())

const rows = ref<SendRecordRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(true)
const error = ref('')

function buildQuery(): SendRecordQuery {
  const query: SendRecordQuery = { page: page.value, pageSize: pageSize.value }
  if (filters.channel) query.channel = filters.channel
  if (filters.scene) query.scene = filters.scene
  if (filters.result) query.result = filters.result
  if (range.value && range.value.length === 2) {
    query.startTime = String(range.value[0].getTime())
    query.endTime = String(range.value[1].getTime())
  }
  return query
}

async function load() {
  if (range.value && range.value.length === 2 && range.value[0] > range.value[1]) {
    ElMessage.error('开始时间不能晚于结束时间')
    return
  }
  loading.value = true
  error.value = ''
  try {
    const data = await listSendRecords(buildQuery())
    rows.value = data.items
    total.value = data.total
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function onSearch() {
  page.value = 1
  void load()
}

function onReset() {
  filters.channel = ''
  filters.scene = ''
  filters.result = ''
  range.value = defaultRange()
  page.value = 1
  void load()
}

function onPageChange(next: number) {
  page.value = next
  void load()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void load()
}

function formatMillis(ms: number): string {
  if (!ms) return '-'
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

onMounted(load)
</script>

<template>
  <AdminLayout>
    <div class="page-head">
      <h1 class="page-title">发送记录</h1>
      <p class="page-desc">短信与邮箱验证码发送流水，接收方已脱敏，默认展示最近 7 天</p>
    </div>

    <section class="panel">
      <div class="filters">
        <el-select v-model="filters.channel" placeholder="全部渠道" clearable style="width: 140px">
          <el-option label="短信" value="sms" />
          <el-option label="邮件" value="email" />
        </el-select>
        <el-select v-model="filters.scene" placeholder="全部场景" clearable style="width: 170px">
          <el-option v-for="(label, value) in SCENE_LABEL" :key="value" :label="label" :value="value" />
        </el-select>
        <el-select v-model="filters.result" placeholder="全部结果" clearable style="width: 130px">
          <el-option label="成功" value="success" />
          <el-option label="失败" value="failed" />
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
          <el-button size="small" @click="load">重试</el-button>
        </template>
      </el-alert>

      <el-table v-loading="loading" :data="rows" class="record-table" row-key="id">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatMillis(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="渠道" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.channel === 'sms' ? 'warning' : 'primary'" effect="plain">
              {{ CHANNEL_LABEL[row.channel] || row.channel }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="场景" min-width="120">
          <template #default="{ row }">{{ SCENE_LABEL[row.scene] || row.scene }}</template>
        </el-table-column>
        <el-table-column label="接收方" min-width="170">
          <template #default="{ row }">
            <span class="mono">{{ row.target }}</span>
          </template>
        </el-table-column>
        <el-table-column label="供应商" width="110">
          <template #default="{ row }">{{ PROVIDER_LABEL[row.provider] || row.provider }}</template>
        </el-table-column>
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.result === 'success' ? 'success' : 'danger'">
              {{ RESULT_LABEL[row.result] || row.result }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="失败原因" min-width="200">
          <template #default="{ row }">
            <span class="error-text" :title="row.error || ''">{{ row.error || '-' }}</span>
          </template>
        </el-table-column>

        <template #empty>
          <div v-if="error" class="empty-block">
            <p class="empty-text">{{ error }}</p>
            <el-button size="small" @click="load">重试</el-button>
          </div>
          <el-empty v-else description="该条件下没有发送记录" />
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

.record-table {
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

.error-text {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--app-text-secondary);
}
</style>
