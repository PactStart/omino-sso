<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import PortalLayout from '@/layouts/PortalLayout.vue'
import { changePassword } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import { useSessionStore } from '@/stores/session'
import { useToast } from '@/composables/useToast'

const router = useRouter()
const session = useSessionStore()
const toast = useToast()

const form = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })
const errors = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })
const submitting = ref(false)

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
  errors.newPassword = ''
  errors.confirmPassword = ''
}

async function submit() {
  clearErrors()

  if (!form.oldPassword) {
    errors.oldPassword = '请输入原密码'
  }
  if (!form.newPassword) {
    errors.newPassword = '请输入新密码'
  } else if (form.newPassword === form.oldPassword) {
    errors.newPassword = '新密码不能与原密码相同'
  } else if (!form.confirmPassword) {
    errors.confirmPassword = '请再次输入新密码'
  } else if (form.newPassword !== form.confirmPassword) {
    errors.confirmPassword = '两次输入的新密码不一致'
  }

  // 一次性展示所有字段问题
  if (errors.oldPassword || errors.newPassword || errors.confirmPassword) return

  if (submitting.value) return

  submitting.value = true
  try {
    await changePassword({ oldPassword: form.oldPassword, newPassword: form.newPassword })
    session.reset()
    toast.success('密码已修改，请重新登录')
    await router.replace({ path: '/login', query: { reason: 'password-changed' } })
  } catch (e) {
    const code = errorCode(e)
    if (code === 'AUTH_INVALID_CREDENTIALS') {
      errors.oldPassword = '原密码错误'
    } else if (code === 'WEAK_PASSWORD') {
      errors.newPassword = '密码需 8–128 位，且同时包含字母与数字'
    } else if (code === 'VALIDATION_FAILED') {
      errors.newPassword = errorMessage(e)
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

      <form class="mt-6 space-y-4" novalidate @submit.prevent="submit">
        <div>
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
