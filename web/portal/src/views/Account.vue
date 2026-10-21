<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import PortalLayout from '@/layouts/PortalLayout.vue'
import ErrorState from '@/components/ErrorState.vue'
import ImageUpload from '@/components/ImageUpload.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useSessionStore } from '@/stores/session'
import { clearMyAvatar, updateMyAvatar } from '@/api'
import { errorMessage } from '@/api/client'
import { useToast } from '@/composables/useToast'
import { initialOf, orDash, roleLabel, formatTime } from '@/utils/format'

const session = useSessionStore()
const toast = useToast()

const loading = ref(false)
const error = ref('')

const me = computed(() => session.user)

const avatarBroken = ref(false)

watch(
  () => me.value?.avatarUrl,
  () => {
    avatarBroken.value = false
  },
)

const roleTags = computed(() =>
  (me.value?.roles ?? []).map((role) => ({
    key: role,
    label: roleLabel(role),
    primary: role === 'admin',
  })),
)

const deptText = computed(() => {
  const path = me.value?.deptPath ?? []
  return path.length ? path.join(' / ') : '-'
})

const mfaText = computed(() => {
  if (!me.value) return '-'
  return me.value.mfaEnabled ? '已启用' : '未启用'
})

async function load(force = false) {
  if (!force && session.user) return
  loading.value = true
  error.value = ''
  const user = await session.fetchMe({ force, silent: false })
  if (!user) error.value = '无法获取个人信息，请稍后重试'
  loading.value = false
}

async function confirmAvatar(key: string, format: string, size: number) {
  const file = await updateMyAvatar({ key, format, size })
  return { url: file.url }
}

async function onAvatarSuccess() {
  await session.fetchMe({ force: true })
}

function onAvatarError(message: string) {
  toast.error(message)
}

const clearConfirm = ref(false)
const clearingAvatar = ref(false)

function askClearAvatar() {
  clearConfirm.value = true
}

async function confirmClearAvatar() {
  clearingAvatar.value = true
  try {
    await clearMyAvatar()
    toast.success('头像已清除')
    clearConfirm.value = false
    await session.fetchMe({ force: true })
  } catch (e) {
    toast.error(errorMessage(e))
  } finally {
    clearingAvatar.value = false
  }
}

onMounted(() => load())
</script>

<template>
  <PortalLayout>
    <div class="mb-6 flex items-end justify-between gap-4">
      <div>
        <h1 class="text-xl font-semibold text-ink-800">个人信息</h1>
        <p class="mt-1 text-sm text-ink-400">你的账号资料由管理员统一维护</p>
      </div>
      <button
        type="button"
        class="rounded-lg border border-ink-200 bg-white px-3 py-1.5 text-sm text-ink-600 transition hover:border-brand-300 hover:text-brand-700"
        @click="load(true)"
      >
        刷新
      </button>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="grid gap-5 lg:grid-cols-[280px_1fr]">
      <div class="rounded-card border border-ink-200 bg-white p-6">
        <div class="skeleton mx-auto h-20 w-20 rounded-full" />
        <div class="skeleton mx-auto mt-4 h-5 w-24" />
        <div class="skeleton mx-auto mt-2 h-4 w-32" />
        <div class="skeleton mx-auto mt-4 h-6 w-20" />
      </div>
      <div class="space-y-5">
        <div class="rounded-card border border-ink-200 bg-white p-6">
          <div class="skeleton h-4 w-24" />
          <div class="mt-5 grid grid-cols-2 gap-4">
            <div v-for="i in 6" :key="i" class="space-y-2">
              <div class="skeleton h-3 w-16" />
              <div class="skeleton h-4 w-28" />
            </div>
          </div>
        </div>
        <div class="rounded-card border border-ink-200 bg-white p-6">
          <div class="skeleton h-4 w-24" />
          <div class="mt-5 space-y-3">
            <div class="skeleton h-4 w-56" />
            <div class="skeleton h-4 w-48" />
          </div>
        </div>
      </div>
    </div>

    <!-- 错误 -->
    <div v-else-if="error" class="rounded-card border border-ink-200 bg-white">
      <ErrorState :message="error" @retry="load(true)" />
    </div>

    <!-- 正常 -->
    <div v-else-if="me" class="grid items-start gap-5 lg:grid-cols-[280px_1fr]">
      <!-- 左：身份卡片 -->
      <section class="rounded-card border border-ink-200 bg-white p-6 text-center">
        <img
          v-if="me.avatarUrl && !avatarBroken"
          :src="me.avatarUrl"
          :alt="me.displayName"
          class="mx-auto h-20 w-20 rounded-full object-cover ring-1 ring-ink-200"
          @error="avatarBroken = true"
        />
        <div
          v-else
          class="mx-auto flex h-20 w-20 items-center justify-center rounded-full bg-brand-50 text-2xl font-semibold text-brand-700 ring-1 ring-brand-100"
          aria-hidden="true"
        >
          {{ initialOf(me.displayName) }}
        </div>

        <div class="mt-4 flex justify-center">
          <ImageUpload
            scene="user_avatar"
            :enable-crop="true"
            :confirm="confirmAvatar"
            @success="onAvatarSuccess"
            @error="onAvatarError"
          />
        </div>

        <button
          v-if="me.avatarUrl"
          type="button"
          class="mt-2 text-xs text-ink-400 transition hover:text-rose-600"
          @click="askClearAvatar"
        >
          清除头像
        </button>

        <h2 class="mt-4 text-lg font-semibold text-ink-800">{{ me.displayName }}</h2>
        <p class="mt-0.5 font-mono text-sm text-ink-400">{{ me.username }}</p>

        <div class="mt-3 flex flex-wrap items-center justify-center gap-2">
          <span
            v-for="tag in roleTags"
            :key="tag.key"
            class="rounded-full px-2.5 py-0.5 text-xs font-medium ring-1"
            :class="
              tag.primary
                ? 'bg-brand-50 text-brand-700 ring-brand-200'
                : 'bg-ink-50 text-ink-600 ring-ink-200'
            "
          >
            {{ tag.label }}
          </span>
        </div>

        <div class="mt-6 space-y-2">
          <RouterLink
            to="/account/password"
            class="block rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-brand-700"
          >
            修改密码
          </RouterLink>
          <RouterLink
            to="/account/sessions"
            class="block rounded-lg border border-ink-200 px-4 py-2 text-sm text-ink-600 transition hover:border-brand-300 hover:text-brand-700"
          >
            登录设备
          </RouterLink>
        </div>
      </section>

      <!-- 右：信息分组 -->
      <div class="space-y-5">
        <section class="rounded-card border border-ink-200 bg-white p-6">
          <h3 class="text-sm font-semibold text-ink-800">基本信息</h3>
          <dl class="mt-4 grid gap-x-8 gap-y-4 sm:grid-cols-2">
            <div>
              <dt class="text-xs text-ink-400">邮箱</dt>
              <dd class="mt-1 text-sm break-all text-ink-800">
                {{ me.email ? me.email : '未绑定' }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-ink-400">手机号</dt>
              <dd class="mt-1 text-sm text-ink-800">{{ me.phone ? me.phone : '未绑定' }}</dd>
            </div>
            <div>
              <dt class="text-xs text-ink-400">部门</dt>
              <dd class="mt-1 text-sm text-ink-800">{{ deptText }}</dd>
            </div>
            <div>
              <dt class="text-xs text-ink-400">租户</dt>
              <dd class="mt-1 text-sm text-ink-800">{{ orDash(me.tenantId) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-ink-400">用户 ID</dt>
              <dd class="mt-1 font-mono text-xs text-ink-600">{{ me.id }}</dd>
            </div>
            <div>
              <dt class="text-xs text-ink-400">部门 ID</dt>
              <dd class="mt-1 font-mono text-xs text-ink-600">{{ orDash(me.deptId) }}</dd>
            </div>
          </dl>
        </section>

        <section class="rounded-card border border-ink-200 bg-white p-6">
          <h3 class="text-sm font-semibold text-ink-800">安全状态</h3>
          <dl class="mt-4 space-y-4">
            <div class="flex items-center justify-between gap-4">
              <dt class="text-sm text-ink-400">两步验证（MFA）</dt>
              <dd class="flex items-center gap-2">
                <span
                  class="rounded-full px-2.5 py-0.5 text-xs font-medium ring-1"
                  :class="
                    me.mfaEnabled
                      ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                      : 'bg-ink-50 text-ink-600 ring-ink-200'
                  "
                >
                  {{ mfaText }}
                </span>
                <span
                  v-if="me.mfaRequired"
                  class="rounded-full bg-amber-50 px-2.5 py-0.5 text-xs font-medium text-amber-700 ring-1 ring-amber-200"
                >
                  管理员强制
                </span>
              </dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="text-sm text-ink-400">最近登录</dt>
              <dd class="text-sm text-ink-800">{{ formatTime(me.lastLoginAt) }}</dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="text-sm text-ink-400">账号创建时间</dt>
              <dd class="text-sm text-ink-800">{{ formatTime(me.createdAt) }}</dd>
            </div>
          </dl>
        </section>
      </div>
    </div>

    <ConfirmDialog
      :open="clearConfirm"
      title="清除头像？"
      message="清除后将回退为姓名首字占位，可随时重新上传。"
      confirm-text="清除"
      danger
      :loading="clearingAvatar"
      @confirm="confirmClearAvatar"
      @cancel="clearConfirm = false"
    />
  </PortalLayout>
</template>
