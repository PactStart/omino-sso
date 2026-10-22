<script setup lang="ts">
/**
 * 门户应用管理壳（006-app-admin-role）。
 *
 * - 独立地址、可收藏：/account/apps/:clientId/manage/{overview|members|roles|permissions|grants}
 * - 首次进入先取摘要（GET /me/apps/:id）：403 → 页内「无权限」提示，不空白、不无限加载（FR-016）
 * - 「应用信息」参考 admin 端详情，但可编辑字段缩减为名称 / 描述 / Logo / 回调地址；
 *   密钥与「设为/取消应用管理员」仍只在 admin 端
 * - 本组件刻意不引入 Element Plus（EP 只出现在子页里），保证管理壳轻量
 */
import { computed, onMounted, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getManageContext } from '@/api/app'
import { errorMessage } from '@/api/client'
import type { AppManageContext } from '@/api/types'
import { reloadClientSummaryKey } from './detail-context'

const route = useRoute()
const router = useRouter()

const clientId = computed(() => String(route.params.clientId ?? ''))
const ctx = ref<AppManageContext | null>(null)
const loading = ref(true)
const denied = ref(false)
const error = ref('')

const tabs = [
  { key: 'overview', label: '应用信息' },
  { key: 'members', label: '成员' },
  { key: 'roles', label: '角色' },
  { key: 'permissions', label: '权限' },
  { key: 'grants', label: '授权' },
] as const

const activeTab = computed(() => {
  const seg = String(route.path.split('/').pop() ?? '')
  return tabs.some((t) => t.key === seg) ? seg : 'overview'
})

async function loadContext() {
  loading.value = true
  denied.value = false
  error.value = ''
  try {
    ctx.value = await getManageContext(clientId.value)
  } catch (e) {
    // 403 = 不是该应用的应用管理员 / 应用不存在 / 应用已停用，统一按「无权限」呈现。
    const code = (e as { code?: string })?.code
    if (code === 'FORBIDDEN') {
      denied.value = true
    } else {
      error.value = errorMessage(e)
    }
  } finally {
    loading.value = false
  }
}

function switchTab(key: string) {
  void router.replace(`/account/apps/${encodeURIComponent(clientId.value)}/manage/${key}`)
}

function backToApps() {
  void router.push('/account/apps')
}

provide(reloadClientSummaryKey, loadContext)

onMounted(loadContext)
watch(clientId, loadContext)
</script>

<template>
  <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
    <button class="mb-4 text-sm text-brand-700 hover:underline" @click="backToApps">
      ← 返回我的应用
    </button>

    <div v-if="loading" class="skeleton h-24 w-full rounded-[var(--radius-card)]" />

    <!-- 无权限：明确提示 + 返回入口，不空白、不反复加载（FR-016） -->
    <div
      v-else-if="denied"
      class="rounded-[var(--radius-card)] border border-ink-100 bg-white p-10 text-center"
    >
      <p class="text-lg font-semibold text-ink-800">无权限管理该应用</p>
      <p class="mt-2 text-sm text-ink-500">
        你当前不是该应用的应用管理员。如需管理权限，请联系平台管理员在管理后台授予。
      </p>
      <button
        class="mt-6 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700"
        @click="backToApps"
      >
        返回我的应用
      </button>
    </div>

    <div
      v-else-if="error"
      class="rounded-[var(--radius-card)] border border-ink-100 bg-white p-10 text-center"
    >
      <p class="text-sm text-ink-600">{{ error }}</p>
      <button class="mt-4 text-sm text-brand-700 hover:underline" @click="loadContext">重试</button>
    </div>

    <template v-else-if="ctx">
      <header class="flex items-center gap-4 rounded-[var(--radius-card)] border border-ink-100 bg-white p-5">
        <img
          v-if="ctx.logoUrl"
          :src="ctx.logoUrl"
          alt=""
          class="h-12 w-12 rounded-lg object-cover"
        />
        <div v-else class="flex h-12 w-12 items-center justify-center rounded-lg bg-brand-50 text-brand-700">
          {{ ctx.name.slice(0, 1) }}
        </div>
        <div class="min-w-0">
          <h1 class="truncate text-lg font-semibold text-ink-900">{{ ctx.name }}</h1>
          <p class="truncate text-sm text-ink-500">
            {{ ctx.description || '暂无描述' }} · 共 {{ ctx.memberCount }} 名成员
          </p>
        </div>
      </header>

      <nav class="mt-4 flex gap-1 border-b border-ink-100">
        <button
          v-for="t in tabs"
          :key="t.key"
          class="-mb-px border-b-2 px-4 py-2 text-sm transition"
          :class="
            activeTab === t.key
              ? 'border-brand-600 font-medium text-brand-700'
              : 'border-transparent text-ink-500 hover:text-ink-800'
          "
          @click="switchTab(t.key)"
        >
          {{ t.label }}
        </button>
      </nav>

      <div class="mt-4">
        <router-view />
      </div>
    </template>
  </div>
</template>
