<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import QRCode from 'qrcode'
import { RouterLink } from 'vue-router'
import PortalLayout from '@/layouts/PortalLayout.vue'
import ErrorState from '@/components/ErrorState.vue'
import ImageUpload from '@/components/ImageUpload.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useSessionStore } from '@/stores/session'
import {
  bindEmail,
  bindPhone,
  clearMyAvatar,
  confirmMfaBind,
  sendCode,
  startMfaBind,
  unbindMfa,
  updateMyAvatar,
} from '@/api'
import { errorMessage } from '@/api/client'
import { useToast } from '@/composables/useToast'
import { initialOf, orDash, roleLabel, formatTime } from '@/utils/format'
import type { MfaBindStart, VerifyChannel } from '@/api/types'

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

/* ---------------- 联系方式绑定 / 修改 ---------------- */

const contactKind = ref<'phone' | 'email' | null>(null)
const contactValue = ref('')
const contactCode = ref('')
const contactError = ref('')
const contactBusy = ref(false)
const contactSending = ref(false)
const contactCooldown = ref(0)
let contactTimer: number | undefined

function openContact(kind: 'phone' | 'email') {
  contactKind.value = kind
  contactValue.value = ''
  contactCode.value = ''
  contactError.value = ''
  contactCooldown.value = 0
}

function closeContact() {
  contactKind.value = null
  window.clearInterval(contactTimer)
}

function contactChannel(): VerifyChannel {
  return contactKind.value === 'phone' ? 'sms' : 'email'
}

function startContactCooldown(seconds: number) {
  contactCooldown.value = seconds
  window.clearInterval(contactTimer)
  contactTimer = window.setInterval(() => {
    contactCooldown.value -= 1
    if (contactCooldown.value <= 0) window.clearInterval(contactTimer)
  }, 1000)
}

async function sendContactCode() {
  contactError.value = ''
  const target = contactValue.value.trim()
  if (contactKind.value === 'phone' && !/^1\d{10}$/.test(target)) {
    contactError.value = '请输入 11 位手机号'
    return
  }
  if (contactKind.value === 'email' && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(target)) {
    contactError.value = '请输入正确的邮箱地址'
    return
  }
  if (contactSending.value || contactCooldown.value > 0) return
  contactSending.value = true
  try {
    const res = await sendCode({
      scene: contactKind.value === 'phone' ? 'bind_phone' : 'bind_email',
      channel: contactChannel(),
      target,
    })
    startContactCooldown(res.resendAfter || 60)
    toast.success('验证码已发送')
  } catch (e) {
    contactError.value = errorMessage(e)
  } finally {
    contactSending.value = false
  }
}

async function submitContact() {
  contactError.value = ''
  const target = contactValue.value.trim()
  if (!/^\d{6}$/.test(contactCode.value)) {
    contactError.value = '请输入 6 位验证码'
    return
  }
  if (contactBusy.value) return
  contactBusy.value = true
  try {
    if (contactKind.value === 'phone') {
      await bindPhone({ phone: target, code: contactCode.value })
    } else {
      await bindEmail({ email: target, code: contactCode.value })
    }
    toast.success(contactKind.value === 'phone' ? '手机号已更新' : '邮箱已更新')
    closeContact()
    await session.fetchMe({ force: true })
  } catch (e) {
    contactError.value = errorMessage(e)
  } finally {
    contactBusy.value = false
  }
}

/* ---------------- MFA 绑定 / 解绑 ---------------- */

const mfaBindDialog = ref(false)
const mfaBind = ref<MfaBindStart | null>(null)
const mfaQr = ref('')
const mfaBindCode = ref('')
const mfaBindError = ref('')
const mfaBindBusy = ref(false)

const unbindDialog = ref(false)
const unbindChannel = ref<VerifyChannel>('sms')
const unbindCode = ref('')
const unbindError = ref('')
const unbindBusy = ref(false)
const unbindSending = ref(false)
const unbindCooldown = ref(0)
let unbindTimer: number | undefined

function mfaSecret(): string {
  const url = mfaBind.value?.otpauthUrl ?? ''
  const match = /[?&]secret=([^&]+)/.exec(url)
  return match?.[1] ?? ''
}

async function openMfaBind() {
  mfaBindError.value = ''
  mfaBindCode.value = ''
  mfaBind.value = null
  mfaQr.value = ''
  mfaBindBusy.value = true
  try {
    mfaBind.value = await startMfaBind()
    // otpauth:// 链接由服务端生成，前端只做二维码渲染（不二次加工）
    mfaQr.value = await QRCode.toDataURL(mfaBind.value.otpauthUrl, {
      margin: 1,
      width: 220,
      errorCorrectionLevel: 'M',
    })
    mfaBindDialog.value = true
  } catch (e) {
    toast.error(errorMessage(e))
  } finally {
    mfaBindBusy.value = false
  }
}

async function submitMfaBind() {
  mfaBindError.value = ''
  if (!/^\d{6}$/.test(mfaBindCode.value)) {
    mfaBindError.value = '请输入 6 位动态验证码'
    return
  }
  if (!mfaBind.value || mfaBindBusy.value) return
  mfaBindBusy.value = true
  try {
    await confirmMfaBind({ mfaSetupTicket: mfaBind.value.mfaSetupTicket, code: mfaBindCode.value })
    mfaBindDialog.value = false
    toast.success('两步验证已启用')
    await session.fetchMe({ force: true })
  } catch (e) {
    mfaBindError.value = errorMessage(e)
  } finally {
    mfaBindBusy.value = false
  }
}

function openUnbind() {
  unbindChannel.value = me.value?.phone ? 'sms' : 'email'
  unbindCode.value = ''
  unbindError.value = ''
  unbindCooldown.value = 0
  unbindDialog.value = true
}

function startUnbindCooldown(seconds: number) {
  unbindCooldown.value = seconds
  window.clearInterval(unbindTimer)
  unbindTimer = window.setInterval(() => {
    unbindCooldown.value -= 1
    if (unbindCooldown.value <= 0) window.clearInterval(unbindTimer)
  }, 1000)
}

async function sendUnbindCode() {
  unbindError.value = ''
  const target = unbindChannel.value === 'sms' ? (me.value?.phone ?? '') : (me.value?.email ?? '')
  if (!target) {
    unbindError.value = '当前账号未绑定该联系方式'
    return
  }
  if (unbindSending.value || unbindCooldown.value > 0) return
  unbindSending.value = true
  try {
    const res = await sendCode({ scene: 'mfa_unbind', channel: unbindChannel.value, target })
    startUnbindCooldown(res.resendAfter || 60)
    toast.success('验证码已发送')
  } catch (e) {
    unbindError.value = errorMessage(e)
  } finally {
    unbindSending.value = false
  }
}

async function submitUnbind() {
  unbindError.value = ''
  if (!/^\d{6}$/.test(unbindCode.value)) {
    unbindError.value = '请输入 6 位验证码'
    return
  }
  if (unbindBusy.value) return
  unbindBusy.value = true
  try {
    await unbindMfa({ channel: unbindChannel.value, code: unbindCode.value })
    unbindDialog.value = false
    toast.success('两步验证已解绑')
    await session.fetchMe({ force: true })
  } catch (e) {
    unbindError.value = errorMessage(e)
  } finally {
    unbindBusy.value = false
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
              <dd class="mt-1 flex items-center gap-2 text-sm break-all text-ink-800">
                <span>{{ me.email ? me.email : '未绑定' }}</span>
                <button
                  type="button"
                  class="shrink-0 text-xs text-brand-600 transition hover:text-brand-700"
                  @click="openContact('email')"
                >
                  {{ me.email ? '修改' : '绑定' }}
                </button>
              </dd>
            </div>
            <div>
              <dt class="text-xs text-ink-400">手机号</dt>
              <dd class="mt-1 flex items-center gap-2 text-sm text-ink-800">
                <span>{{ me.phone ? me.phone : '未绑定' }}</span>
                <button
                  type="button"
                  class="shrink-0 text-xs text-brand-600 transition hover:text-brand-700"
                  @click="openContact('phone')"
                >
                  {{ me.phone ? '修改' : '绑定' }}
                </button>
              </dd>
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
            <div class="flex flex-wrap items-center justify-between gap-3">
              <dt class="flex items-center gap-2.5 text-sm text-ink-400">
                <span>两步验证（MFA）</span>
                <button
                  v-if="!me.mfaEnabled"
                  type="button"
                  class="rounded-lg bg-brand-600 px-2.5 py-1 text-xs font-medium text-white transition hover:bg-brand-700 disabled:opacity-60"
                  :disabled="mfaBindBusy"
                  @click="openMfaBind"
                >
                  启用
                </button>
                <button
                  v-else
                  type="button"
                  class="rounded-lg border border-ink-200 px-2.5 py-1 text-xs text-ink-600 transition hover:border-rose-300 hover:text-rose-600"
                  @click="openUnbind"
                >
                  解绑
                </button>
              </dt>
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

    <!-- 绑定/修改联系方式 -->
    <div
      v-if="contactKind"
      class="fixed inset-0 z-50 flex items-center justify-center bg-ink-900/40 px-4"
      role="dialog"
      aria-modal="true"
    >
      <div class="w-full max-w-sm rounded-card border border-ink-200 bg-white p-6 shadow-xl">
        <h3 class="text-base font-semibold text-ink-800">
          {{ contactKind === 'phone' ? '绑定 / 修改手机号' : '绑定 / 修改邮箱' }}
        </h3>
        <p class="mt-1 text-xs text-ink-400">验证码将发送到新填写的{{ contactKind === 'phone' ? '手机号' : '邮箱' }}</p>

        <div class="mt-4 space-y-3">
          <input
            v-model="contactValue"
            type="text"
            :placeholder="contactKind === 'phone' ? '11 位手机号' : '邮箱地址'"
            class="w-full rounded-lg border border-ink-200 px-3.5 py-2.5 text-sm text-ink-800 outline-none transition focus:border-brand-400 focus:ring-4 focus:ring-brand-50"
          />
          <div class="flex gap-2">
            <input
              v-model="contactCode"
              type="text"
              inputmode="numeric"
              maxlength="6"
              placeholder="6 位验证码"
              class="w-full rounded-lg border border-ink-200 px-3.5 py-2.5 text-sm tracking-[0.3em] text-ink-800 outline-none transition focus:border-brand-400 focus:ring-4 focus:ring-brand-50"
              @input="contactCode = contactCode.replace(/\D/g, '').slice(0, 6)"
            />
            <button
              type="button"
              class="shrink-0 rounded-lg border border-ink-200 px-3 text-xs text-ink-600 transition hover:border-brand-300 hover:text-brand-700 disabled:opacity-50"
              :disabled="contactSending || contactCooldown > 0"
              @click="sendContactCode"
            >
              {{ contactCooldown > 0 ? `${contactCooldown}s` : '获取验证码' }}
            </button>
          </div>
          <p v-if="contactError" class="text-xs text-rose-600">{{ contactError }}</p>
        </div>

        <div class="mt-5 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg border border-ink-200 px-3.5 py-2 text-sm text-ink-600 transition hover:text-ink-800"
            @click="closeContact"
          >
            取消
          </button>
          <button
            type="button"
            class="rounded-lg bg-brand-600 px-3.5 py-2 text-sm font-medium text-white transition hover:bg-brand-700 disabled:opacity-60"
            :disabled="contactBusy"
            @click="submitContact"
          >
            提交
          </button>
        </div>
      </div>
    </div>

    <!-- MFA 绑定 -->
    <div
      v-if="mfaBindDialog"
      class="fixed inset-0 z-50 flex items-center justify-center bg-ink-900/40 px-4"
      role="dialog"
      aria-modal="true"
    >
      <div class="w-full max-w-md rounded-card border border-ink-200 bg-white p-6 shadow-xl">
        <h3 class="text-base font-semibold text-ink-800">启用两步验证</h3>
        <p class="mt-1 text-xs text-ink-400">
          用身份验证器（Google / Microsoft Authenticator 等）扫描二维码，或手动输入下方密钥：
        </p>

        <div class="mt-4 flex justify-center">
          <img
            v-if="mfaQr"
            :src="mfaQr"
            alt="两步验证绑定二维码"
            class="h-[220px] w-[220px] rounded-lg border border-ink-200 bg-white p-2"
          />
          <div v-else class="skeleton h-[220px] w-[220px] rounded-lg" />
        </div>

        <div class="mt-4 space-y-3">
          <div>
            <p class="text-xs text-ink-400">密钥（手动录入时使用）</p>
            <p class="mt-1 rounded-lg bg-ink-50 px-3 py-2 font-mono text-xs break-all text-ink-700">
              {{ mfaSecret() || '-' }}
            </p>
          </div>
          <details class="text-xs text-ink-400">
            <summary class="cursor-pointer select-none">无法扫码？展开 otpauth 链接</summary>
            <p class="mt-1 max-h-24 overflow-auto rounded-lg bg-ink-50 px-3 py-2 font-mono break-all text-ink-600">
              {{ mfaBind?.otpauthUrl }}
            </p>
          </details>
          <input
            v-model="mfaBindCode"
            type="text"
            inputmode="numeric"
            maxlength="6"
            placeholder="填写 6 位动态码"
            class="w-full rounded-lg border border-ink-200 px-3.5 py-2.5 text-center font-mono text-sm tracking-[0.3em] text-ink-800 outline-none transition focus:border-brand-400 focus:ring-4 focus:ring-brand-50"
            @input="mfaBindCode = mfaBindCode.replace(/\D/g, '').slice(0, 6)"
          />
          <p v-if="mfaBindError" class="text-xs text-rose-600">{{ mfaBindError }}</p>
        </div>

        <div class="mt-5 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg border border-ink-200 px-3.5 py-2 text-sm text-ink-600"
            @click="mfaBindDialog = false"
          >
            取消
          </button>
          <button
            type="button"
            class="rounded-lg bg-brand-600 px-3.5 py-2 text-sm font-medium text-white transition hover:bg-brand-700 disabled:opacity-60"
            :disabled="mfaBindBusy"
            @click="submitMfaBind"
          >
            确认启用
          </button>
        </div>
      </div>
    </div>

    <!-- MFA 解绑 -->
    <div
      v-if="unbindDialog"
      class="fixed inset-0 z-50 flex items-center justify-center bg-ink-900/40 px-4"
      role="dialog"
      aria-modal="true"
    >
      <div class="w-full max-w-sm rounded-card border border-ink-200 bg-white p-6 shadow-xl">
        <h3 class="text-base font-semibold text-ink-800">解绑两步验证</h3>
        <p class="mt-1 text-xs text-ink-400">需通过已绑定联系方式的验证码验证身份</p>

        <div class="mt-4 space-y-3">
          <div class="flex rounded-lg bg-ink-50 p-1 text-xs font-medium text-ink-500">
            <button
              type="button"
              class="flex-1 rounded-md px-2 py-1.5 transition"
              :class="unbindChannel === 'sms' ? 'bg-white text-brand-700 shadow-sm' : ''"
              :disabled="!me?.phone"
              @click="unbindChannel = 'sms'"
            >
              手机验证码
            </button>
            <button
              type="button"
              class="flex-1 rounded-md px-2 py-1.5 transition"
              :class="unbindChannel === 'email' ? 'bg-white text-brand-700 shadow-sm' : ''"
              :disabled="!me?.email"
              @click="unbindChannel = 'email'"
            >
              邮箱验证码
            </button>
          </div>
          <div class="flex gap-2">
            <input
              v-model="unbindCode"
              type="text"
              inputmode="numeric"
              maxlength="6"
              placeholder="6 位验证码"
              class="w-full rounded-lg border border-ink-200 px-3.5 py-2.5 text-sm tracking-[0.3em] text-ink-800 outline-none transition focus:border-brand-400 focus:ring-4 focus:ring-brand-50"
              @input="unbindCode = unbindCode.replace(/\D/g, '').slice(0, 6)"
            />
            <button
              type="button"
              class="shrink-0 rounded-lg border border-ink-200 px-3 text-xs text-ink-600 transition hover:border-brand-300 disabled:opacity-50"
              :disabled="unbindSending || unbindCooldown > 0"
              @click="sendUnbindCode"
            >
              {{ unbindCooldown > 0 ? `${unbindCooldown}s` : '获取验证码' }}
            </button>
          </div>
          <p v-if="unbindError" class="text-xs text-rose-600">{{ unbindError }}</p>
        </div>

        <div class="mt-5 flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg border border-ink-200 px-3.5 py-2 text-sm text-ink-600"
            @click="unbindDialog = false"
          >
            取消
          </button>
          <button
            type="button"
            class="rounded-lg bg-rose-600 px-3.5 py-2 text-sm font-medium text-white transition hover:bg-rose-700 disabled:opacity-60"
            :disabled="unbindBusy"
            @click="submitUnbind"
          >
            确认解绑
          </button>
        </div>
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
