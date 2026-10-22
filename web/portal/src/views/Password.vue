<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import PortalLayout from '@/layouts/PortalLayout.vue'
import { changePassword, resetPasswordWithCode, sendCode } from '@/api'
import type { VerifyChannel } from '@/api/types'
import { errorCode, errorMessage } from '@/api/client'
import { useSessionStore } from '@/stores/session'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const session = useSessionStore()
const toast = useToast()

const me = computed(() => session.user)

/** 两种修改方式：原密码校验 / 已绑定的联系方式验证码校验 */
const mode = ref<'password' | 'code'>('password')

const MSG = {
  oldRequired: '请输入原密码',
  oldWrong: '原密码错误',
  codeRequired: '请输入 6 位验证码',
  weak: '密码需 8–128 位，且同时包含字母与数字',
  newRequired: '请输入新密码',
  same: '新密码不能与原密码相同',
  confirmRequired: '请再次输入新密码',
  mismatch: '两次输入的新密码不一致',
}

const form = reactive({
  oldPassword: '',
  channel: 'sms' as VerifyChannel,
  code: '',
  newPassword: '',
  confirmPassword: '',
})
const errors = reactive({ oldPassword: '', code: '', newPassword: '', confirmPassword: '' })
const submitting = ref(false)
const sendingCode = ref(false)
const cooldown = ref(0)
let cooldownTimer: number | undefined

const channelTarget = computed(() =>
  form.channel === 'sms' ? (me.value?.phone ?? '') : (me.value?.email ?? ''),
)

/** 未绑定任何联系方式时，验证码方式不可用 */
const canUseCode = computed(() => Boolean(me.value?.phone || me.value?.email))

/** 提示里只展示脱敏后的接收方 */
const maskedTarget = computed(() => {
  const target = channelTarget.value
  if (!target) return ''
  if (form.channel === 'sms') return target.replace(/^(\d{3})\d{4}(\d{4})$/, '$1****$2')
  const [name, domain] = target.split('@')
  return domain ? `${name.slice(0, 1)}***@${domain}` : target
})

onMounted(() => {
  if (!me.value?.phone) form.channel = 'email'
})

onUnmounted(() => window.clearInterval(cooldownTimer))

function switchMode(next: 'password' | 'code') {
  mode.value = next
  clearErrors()
}

/** 纯前端提示，不拦截提交（PRD §8：校验以服务端 WEAK_PASSWORD 为准） */
const strength = computed(() => {
  const value = form.newPassword
  if (!value) return null
  const hasLetter = /[a-zA-Z]/.test(value)
  const hasDigit = /\d/.test(value)
  if (value.length < 8 || !hasLetter || !hasDigit) {
    return { level: 'weak', label: '弱', hint: '需 8 位以上，且同时包含字母与数字' }
  }
  if (value.length >= 12 && /[^a-zA-Z0-9]/.test(value)) {
    return { level: 'strong', label: '强', hint: '强度良好' }
  }
  return { level: 'medium', label: '中', hint: '满足最低要求，建议增加到 12 位并包含符号' }
})

function clearErrors() {
  errors.oldPassword = ''
  errors.code = ''
  errors.newPassword = ''
  errors.confirmPassword = ''
}

function startCooldown(seconds: number) {
  cooldown.value = seconds
  window.clearInterval(cooldownTimer)
  cooldownTimer = window.setInterval(() => {
    cooldown.value -= 1
    if (cooldown.value <= 0) window.clearInterval(cooldownTimer)
  }, 1000)
}

async function sendVerifyCode() {
  errors.code = ''
  if (!channelTarget.value) {
    errors.code = '当前账号未绑定该联系方式'
    return
  }
  if (sendingCode.value || cooldown.value > 0) return
  sendingCode.value = true
  try {
    const res = await sendCode({
      scene: 'find_password',
      channel: form.channel,
      target: channelTarget.value,
    })
    startCooldown(res.resendAfter || 60)
    toast.success('验证码已发送')
  } catch (e) {
    errors.code = errorMessage(e)
  } finally {
    sendingCode.value = false
  }
}

async function submit() {
  clearErrors()

  if (mode.value === 'password') {
    if (!form.oldPassword) errors.oldPassword = MSG.oldRequired
  } else if (!/^\d{6}$/.test(form.code)) {
    errors.code = MSG.codeRequired
  }

  if (!form.newPassword) {
    errors.newPassword = MSG.newRequired
  } else if (form.newPassword.length < 8 || form.newPassword.length > 128) {
    errors.newPassword = MSG.weak
  } else if (!/[A-Za-z]/.test(form.newPassword) || !/\d/.test(form.newPassword)) {
    errors.newPassword = MSG.weak
  } else if (mode.value === 'password' && form.newPassword === form.oldPassword) {
    errors.newPassword = MSG.same
  } else if (!form.confirmPassword) {
    errors.confirmPassword = MSG.confirmRequired
  } else if (form.newPassword !== form.confirmPassword) {
    errors.confirmPassword = MSG.mismatch
  }

  // 一次性展示所有字段问题
  if (errors.oldPassword || errors.code || errors.newPassword || errors.confirmPassword) return

  if (submitting.value) return

  submitting.value = true
  try {
    if (mode.value === 'code') {
      await resetPasswordWithCode({
        channel: form.channel,
        code: form.code,
        newPassword: form.newPassword,
      })
    } else {
      await changePassword({ oldPassword: form.oldPassword, newPassword: form.newPassword })
    }
    session.reset()
    toast.success(mode.value === 'code' ? '密码已重置，请重新登录' : '密码已修改，请重新登录')
    await router.replace({ path: '/login', query: { reason: 'password-changed' } })
  } catch (e) {
    const code = errorCode(e)
    if (code === 'WEAK_PASSWORD') {
      errors.newPassword = MSG.weak
    } else if (code === 'VALIDATION_FAILED') {
      errors.newPassword = errorMessage(e)
    } else if (code === 'AUTH_INVALID_CREDENTIALS' && mode.value === 'password') {
      errors.oldPassword = MSG.oldWrong
    } else if (code === 'AUTH_CODE_INVALID' && mode.value === 'code') {
      errors.code = '验证码错误或已过期'
    } else {
      toast.error(errorMessage(e))
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <PortalLayout>
    <div class="mb-6">
      <h1 class="text-xl font-semibold text-ink-800">修改密码</h1>
      <p class="mt-1 text-sm text-ink-400">定期更换密码有助于保护账号安全</p>
    </div>

    <div class="max-w-xl rounded-card border border-ink-200 bg-white p-6">
      <div class="rounded-lg bg-amber-50 px-3.5 py-2.5 text-sm text-amber-800 ring-1 ring-amber-100">
        修改成功后需要使用新密码重新登录，所有设备将退出
      </div>

      <!-- 修改方式：原密码 / 验证码 -->
      <div class="mt-6 grid grid-cols-2 gap-1 rounded-lg bg-ink-50 p-1 text-sm font-medium text-ink-500">
        <button
          type="button"
          class="rounded-md px-3 py-2 transition"
          :class="mode === 'password' ? 'bg-white text-brand-700 shadow-sm' : ''"
          @click="switchMode('password')"
        >
          原密码修改
        </button>
        <button
          type="button"
          class="rounded-md px-3 py-2 transition disabled:cursor-not-allowed disabled:opacity-50"
          :class="mode === 'code' ? 'bg-white text-brand-700 shadow-sm' : ''"
          :disabled="!canUseCode"
          @click="switchMode('code')"
        >
          验证码修改
        </button>
      </div>
      <p v-if="!canUseCode" class="mt-2 text-xs text-ink-400">
        账号未绑定手机号或邮箱，暂不能使用验证码修改
      </p>

      <form class="mt-5 space-y-4" novalidate @submit.prevent="submit">
        <div v-if="mode === 'password'">
          <label for="old-password" class="mb-1.5 block text-sm font-medium text-ink-600">
            原密码
          </label>
          <input
            id="old-password"
            v-model="form.oldPassword"
            type="password"
            autocomplete="current-password"
            placeholder="请输入当前密码"
            class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
            :class="
              errors.oldPassword
                ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
            "
          />
          <p v-if="errors.oldPassword" class="mt-1.5 text-xs text-rose-600">
            {{ errors.oldPassword }}
          </p>
        </div>

        <div v-else>
          <span class="mb-1.5 block text-sm font-medium text-ink-600">验证方式</span>
          <div class="flex rounded-lg bg-ink-50 p-1 text-xs font-medium text-ink-500">
            <button
              type="button"
              class="flex-1 rounded-md px-2 py-1.5 transition disabled:cursor-not-allowed disabled:opacity-50"
              :class="form.channel === 'sms' ? 'bg-white text-brand-700 shadow-sm' : ''"
              :disabled="!me?.phone"
              @click="form.channel = 'sms'"
            >
              手机验证码
            </button>
            <button
              type="button"
              class="flex-1 rounded-md px-2 py-1.5 transition disabled:cursor-not-allowed disabled:opacity-50"
              :class="form.channel === 'email' ? 'bg-white text-brand-700 shadow-sm' : ''"
              :disabled="!me?.email"
              @click="form.channel = 'email'"
            >
              邮箱验证码
            </button>
          </div>

          <div class="mt-3 flex gap-2">
            <input
              v-model="form.code"
              type="text"
              inputmode="numeric"
              maxlength="6"
              placeholder="6 位验证码"
              class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm tracking-[0.3em] text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
              :class="
                errors.code
                  ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                  : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
              "
              @input="form.code = form.code.replace(/\D/g, '').slice(0, 6)"
            />
            <button
              type="button"
              class="shrink-0 rounded-lg border border-ink-200 px-3.5 text-xs text-ink-600 transition hover:border-brand-300 hover:text-brand-700 disabled:opacity-50"
              :disabled="sendingCode || cooldown > 0"
              @click="sendVerifyCode"
            >
              {{ cooldown > 0 ? `${cooldown}s` : '获取验证码' }}
            </button>
          </div>
          <p v-if="errors.code" class="mt-1.5 text-xs text-rose-600">{{ errors.code }}</p>
          <p v-else class="mt-1.5 text-xs text-ink-400">验证码将发送至 {{ maskedTarget }}</p>
        </div>

        <div>
          <label for="new-password" class="mb-1.5 block text-sm font-medium text-ink-600">
            新密码
          </label>
          <input
            id="new-password"
            v-model="form.newPassword"
            type="password"
            autocomplete="new-password"
            placeholder="8–128 位，需包含字母与数字"
            class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
            :class="
              errors.newPassword
                ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
            "
          />
          <p v-if="errors.newPassword" class="mt-1.5 text-xs text-rose-600">
            {{ errors.newPassword }}
          </p>
          <p v-else-if="strength" class="mt-1.5 flex items-center gap-2 text-xs text-ink-400">
            强度：
            <span
              class="rounded px-1.5 py-0.5 font-medium"
              :class="{
                'bg-rose-50 text-rose-600': strength.level === 'weak',
                'bg-amber-50 text-amber-700': strength.level === 'medium',
                'bg-emerald-50 text-emerald-700': strength.level === 'strong',
              }"
            >
              {{ strength.label }}
            </span>
            {{ strength.hint }}
          </p>
        </div>

        <div>
          <label for="confirm-password" class="mb-1.5 block text-sm font-medium text-ink-600">
            确认新密码
          </label>
          <input
            id="confirm-password"
            v-model="form.confirmPassword"
            type="password"
            autocomplete="new-password"
            placeholder="请再次输入新密码"
            class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
            :class="
              errors.confirmPassword
                ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
            "
          />
          <p v-if="errors.confirmPassword" class="mt-1.5 text-xs text-rose-600">
            {{ errors.confirmPassword }}
          </p>
        </div>

        <div class="flex items-center gap-3 pt-2">
          <button
            type="submit"
            class="flex items-center gap-2 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-brand-700 disabled:cursor-not-allowed disabled:opacity-60"
            :disabled="submitting"
          >
            <span
              v-if="submitting"
              class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"
              aria-hidden="true"
            />
            确认修改
          </button>
          <button
            type="button"
            class="rounded-lg border border-ink-200 px-4 py-2 text-sm text-ink-600 transition hover:bg-ink-50"
            :disabled="submitting"
            @click="router.push('/account/profile')"
          >
            取消
          </button>
        </div>
      </form>
    </div>
  </PortalLayout>
</template>
