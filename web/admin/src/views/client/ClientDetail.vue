<script setup lang="ts">
import { computed, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { getClient } from '@/api'
import { errorMessage } from '@/api/client'
import type { ClientDetail } from '@/api/types'
import { orDash } from '@/utils/format'
import { reloadClientSummaryKey } from './detail-context'

const route = useRoute()
const router = useRouter()

const clientId = computed(() => String(route.params.clientId ?? ''))
const activeTab = computed(() => String(route.params.tab ?? route.path.split('/').pop() ?? 'overview'))

/** 五项菜单顺序固定，不可调整（FR-001） */
const menus = [
  { key: 'overview', label: '应用详情' },
  { key: 'members', label: '应用成员' },
  { key: 'roles', label: '应用角色' },
  { key: 'permissions', label: '应用权限' },
  { key: 'grants', label: '授权管理' },
] as const

const activeMenu = computed(() => {
  const tab = activeTab.value
  return menus.some((m) => m.key === tab) ? `/clients/${clientId.value}/${tab}` : `/clients/${clientId.value}/overview`
})

const detail = ref<ClientDetail | null>(null)
const loadError = ref('')
const loading = ref(true)

async function loadDetail() {
  loading.value = true
  loadError.value = ''
  try {
    detail.value = await getClient(clientId.value)
  } catch (e) {
    loadError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function backToList() {
  void router.push({ name: 'clients' })
}

// 详情页保存后刷新头部摘要；成员页增删成员后同理
provide(reloadClientSummaryKey, async () => {
  await loadDetail()
})

watch(clientId, () => void loadDetail(), { immediate: true })
</script>

<template>
  <AdminLayout>
    <div class="sub-head">
      <div class="sub-head-main">
        <el-button link type="primary" class="back-link" @click="backToList">← 返回应用列表</el-button>
        <h1 class="sub-title">{{ orDash(detail?.clientName || clientId) }}</h1>
        <span class="sub-id mono">{{ clientId }}</span>
        <el-tag v-if="detail" size="small" :type="detail.status === 1 ? 'success' : 'info'">
          {{ detail.status === 1 ? '已启用' : '已停用' }}
        </el-tag>
      </div>
      <div v-if="detail" class="sub-meta">
        <span>成员 {{ detail.memberCount }} 人</span>
      </div>
    </div>

    <el-alert
      v-if="loadError"
      class="load-alert"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
    >
      <template #default>
        <el-button size="small" @click="loadDetail">重试</el-button>
        <el-button size="small" @click="backToList">返回列表</el-button>
      </template>
    </el-alert>

    <div v-else v-loading="loading" class="sub-body">
      <aside class="sub-aside">
        <el-menu :default-active="activeMenu" router class="sub-menu">
          <el-menu-item v-for="m in menus" :key="m.key" :index="`/clients/${clientId}/${m.key}`">
            {{ m.label }}
          </el-menu-item>
        </el-menu>
      </aside>
      <section class="sub-content">
        <router-view v-if="detail" />
      </section>
    </div>
  </AdminLayout>
</template>

<style scoped>
.sub-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}

.sub-head-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.back-link {
  flex-shrink: 0;
}

.sub-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--app-text);
}

.sub-id {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.sub-meta {
  margin-left: auto;
  font-size: 13px;
  color: var(--app-text-secondary);
}

.load-alert {
  margin-bottom: 14px;
}

.sub-body {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  min-height: 320px;
}

.sub-aside {
  width: 168px;
  flex-shrink: 0;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
  overflow: hidden;
}

.sub-menu {
  border-right: none;
}

.sub-menu :deep(.el-menu-item) {
  height: 44px;
  line-height: 44px;
  font-size: 14px;
}

.sub-content {
  flex: 1;
  min-width: 0;
}

.mono {
  font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
</style>
