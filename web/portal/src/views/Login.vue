<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login, verifyMfa } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import { ADMIN_HOME } from '@/config'
import { useSessionStore } from '@/stores/session'

type Step = 'credentials' | 'mfa'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const step = ref<Step>('credentials')
const submitting = ref(false)
const banner = ref('')
const notice = ref('')
const showPassword = ref(false)
const cooldown = ref(0)

const passwordInput = ref<HTMLInputElement | null>(null)
const codeInput = ref<HTMLInputElement | null>(null)

let cooldownTimer: number | undefined

const form = reactive({ username: '', password: '', rememberMe: false })
const errors = reactive({ username: '', password: '', code: '' })
const mfa = reactive({ ticket: '', code: '', expiresIn: 0 })

const nextPath = computed(() => {
  const raw = route.query.next
  return typeof raw === 'string' && raw.startsWith('/') ? raw : ''
})

const subtitle = computed(() =>
  nextPath.value ? `登录后返回 ${nextPath.value}` : '登录后返回统一门户',
)

const submitLabel = computed(() => {
  if (cooldown.value > 0) return `请稍候 ${cooldown.value}s`
  return step.value === 'credentials' ? '登录' : '验证'
})

onMounted(() => {
  if (route.query.reason === 'password-changed') {
    notice.value = '密码已修改，请使用新密码重新登录'
  }
})

onBeforeUnmount(() => {
  window.clearInterval(cooldownTimer)
})

function clearErrors() {
  errors.username = ''
  errors.password = ''
  errors.code = ''
}

function startCooldown(seconds: number) {
  cooldown.value = seconds
  window.clearInterval(cooldownTimer)
  cooldownTimer = window.setInterval(() => {
    cooldown.value -= 1
    if (cooldown.value <= 0) window.clearInterval(cooldownTimer)
  }, 1000)
}

async function goAfterAuth() {
  // 登录页进入时探测过会话（结果为 null），成功后必须强制刷新，否则守卫会拿到旧缓存再弹回登录页
  const me = await session.fetchMe({ force: true, silent: true })

  if (nextPath.value) {
    await router.replace(nextPath.value)
    return
  }
  if (me?.roles.includes('admin')) {
    window.location.assign(ADMIN_HOME)
    return
  }
  await router.replace('/account/profile')
}

async function submitCredentials() {
  banner.value = ''
  notice.value = ''
  clearErrors()

  const username = form.username.trim()
  if (!username) {
    errors.username = '请输入用户名'
  } else if (username.length > 64) {
    errors.username = '用户名长度不能超过 64 个字符'
  }

  if (!form.password) {
    errors.password = '请输入密码'
  } else if (form.password.length < 8 || form.password.length > 128) {
    errors.password = '密码长度需为 8–128 位'
  }

  // 一次性展示所有字段问题，避免逐个试错
  if (errors.username || errors.password) return

  if (submitting.value || cooldown.value > 0) return

  submitting.value = true
  try {
    const res = await login({
      username,
      password: form.password,
      clientId: 'portal',
      rememberMe: form.rememberMe,
    })

    if (res.mfaRequired) {
      mfa.ticket = res.mfaTicket ?? ''
      mfa.code = ''
      mfa.expiresIn = res.expiresIn
      step.value = 'mfa'
      window.setTimeout(() => codeInput.value?.focus(), 0)
      return
    }
    await goAfterAuth()
  } catch (e) {
    handleLoginError(e)
  } finally {
    submitting.value = false
  }
}

function handleLoginError(e: unknown) {
  switch (errorCode(e)) {
    case 'AUTH_INVALID_CREDENTIALS':
      banner.value = '用户名或密码错误'
      form.password = ''
      passwordInput.value?.focus()
      break
    case 'AUTH_ACCOUNT_LOCKED':
      banner.value = errorMessage(e) || '账号已锁定，请 15 分钟后再试或联系管理员'
      startCooldown(10)
      break
    case 'AUTH_ACCOUNT_DISABLED':
      banner.value = '账号已停用，请联系管理员'
      break
    case 'RATE_LIMITED':
      banner.value = '操作过于频繁，请稍后再试'
      break
    default:
      banner.value = errorMessage(e)
  }
}

async function submitMfa() {
  banner.value = ''
  errors.code = ''
  const code = mfa.code.trim()
  if (!/^\d{6}$/.test(code)) {
    errors.code = '请输入 6 位动态验证码'
    return
  }
  if (submitting.value) return

  submitting.value = true
  try {
    await verifyMfa({ mfaTicket: mfa.ticket, code })
    await goAfterAuth()
  } catch (e) {
    if (errorCode(e) === 'AUTH_MFA_INVALID') {
      errors.code = '动态验证码错误'
      mfa.code = ''
      codeInput.value?.focus()
    } else if (errorCode(e) === 'AUTH_MFA_TICKET_EXPIRED') {
      backToCredentials('验证已超时，请重新登录')
    } else {
      banner.value = errorMessage(e)
    }
  } finally {
    submitting.value = false
  }
}

function backToCredentials(message = '') {
  step.value = 'credentials'
  mfa.ticket = ''
  mfa.code = ''
  mfa.expiresIn = 0
  clearErrors()
  banner.value = message
}

function onCodeInput(event: Event) {
  const target = event.target as HTMLInputElement
  mfa.code = target.value.replace(/\D/g, '').slice(0, 6)
}
</script>

<template>
  <div class="flex min-h-full items-stretch">
    <!-- 品牌区：≥1024px 显示 -->
    <aside
      class="relative hidden w-1/2 flex-col justify-between overflow-hidden bg-gradient-to-br from-brand-700 via-brand-600 to-brand-800 p-12 text-white lg:flex"
    >
      <div
        class="pointer-events-none absolute -right-24 -top-24 h-72 w-72 rounded-full bg-white/10"
        aria-hidden="true"
      />
      <div
        class="pointer-events-none absolute -bottom-32 -left-16 h-80 w-80 rounded-full bg-white/5"
        aria-hidden="true"
      />

      <div class="relative flex items-center gap-3">
        <span
          class="flex h-10 w-10 items-center justify-center rounded-xl bg-white/15 text-sm font-semibold"
          >SSO</span
        >
        <span class="text-base font-semibold">omino identity</span>
      </div>

      <div class="relative">
        <h2 class="text-3xl font-semibold leading-snug">
          一个账号<br />访问所有内部系统
        </h2>
        <p class="mt-4 max-w-sm text-sm leading-6 text-white/70">
          统一身份认证为企业内部系统提供单点登录、单点登出与集中账号管理。
        </p>
        <ul class="mt-8 space-y-3 text-sm text-white/80">
          <li class="flex items-center gap-2.5">
            <span class="h-1.5 w-1.5 rounded-full bg-white/60" aria-hidden="true" />
            一次登录，贯通全部接入系统
          </li>
          <li class="flex items-center gap-2.5">
            <span class="h-1.5 w-1.5 rounded-full bg-white/60" aria-hidden="true" />
            账号密码 + 动态口令双因子
          </li>
          <li class="flex items-center gap-2.5">
            <span class="h-1.5 w-1.5 rounded-full bg-white/60" aria-hidden="true" />
            全量认证审计可追溯
          </li>
        </ul>
      </div>

      <p class="relative text-xs text-white/40">omino-sso · 企业内部统一身份平台</p>
    </aside>

    <!-- 表单区 -->
    <main class="flex w-full items-center justify-center px-6 py-12 lg:w-1/2">
      <div class="w-full max-w-[380px]">
        <div class="rounded-card border border-ink-200 bg-white p-8 shadow-[0_1px_3px_rgba(16,24,40,0.06)]">
          <h1 class="text-[22px] font-semibold text-ink-800">
            {{ step === 'credentials' ? '统一身份认证' : '两步验证' }}
          </h1>
          <p class="mt-2 text-sm text-ink-400">
            {{
              step === 'credentials' ? subtitle : '请输入身份验证器中的 6 位动态码'
            }}
          </p>

          <div
            v-if="notice"
            class="mt-5 rounded-lg bg-brand-50 px-3.5 py-2.5 text-sm text-brand-700"
            role="status"
          >
            {{ notice }}
          </div>

          <div
            v-if="banner"
            class="mt-5 rounded-lg bg-rose-50 px-3.5 py-2.5 text-sm text-rose-700"
            role="alert"
          >
            {{ banner }}
          </div>

          <!-- 第一步：账号密码 -->
          <form v-if="step === 'credentials'" class="mt-6 space-y-4" novalidate @submit.prevent="submitCredentials">
            <div>
              <label for="username" class="mb-1.5 block text-sm font-medium text-ink-600">用户名</label>
              <input
                id="username"
                v-model="form.username"
                type="text"
                name="username"
                autocomplete="username"
                placeholder="请输入用户名"
                class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
                :class="
                  errors.username
                    ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                    : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                "
              />
              <p v-if="errors.username" class="mt-1.5 text-xs text-rose-600">{{ errors.username }}</p>
            </div>

            <div>
              <label for="password" class="mb-1.5 block text-sm font-medium text-ink-600">密码</label>
              <div class="relative">
                <input
                  id="password"
                  ref="passwordInput"
                  v-model="form.password"
                  :type="showPassword ? 'text' : 'password'"
                  name="password"
                  autocomplete="current-password"
                  placeholder="请输入密码"
                  class="w-full rounded-lg border bg-white py-2.5 pl-3.5 pr-14 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
                  :class="
                    errors.password
                      ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                      : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                  "
                />
                <button
                  type="button"
                  class="absolute inset-y-0 right-0 px-3 text-xs text-ink-400 transition hover:text-brand-700"
                  @click="showPassword = !showPassword"
                >
                  {{ showPassword ? '隐藏' : '显示' }}
                </button>
              </div>
              <p v-if="errors.password" class="mt-1.5 text-xs text-rose-600">{{ errors.password }}</p>
            </div>

            <label class="flex cursor-pointer select-none items-center gap-2 text-sm text-ink-600">
              <input
                v-model="form.rememberMe"
                type="checkbox"
                class="h-4 w-4 rounded border-ink-200 text-brand-600 focus:ring-brand-200"
              />
              7 天内免登录
            </label>

            <button
              type="submit"
              class="mt-2 flex w-full items-center justify-center gap-2 rounded-lg bg-brand-600 px-4 py-2.5 text-sm font-medium text-white transition hover:bg-brand-700 focus:outline-none focus-visible:ring-4 focus-visible:ring-brand-100 disabled:cursor-not-allowed disabled:opacity-60"
              :disabled="submitting || cooldown > 0"
            >
              <span
                v-if="submitting"
                class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"
                aria-hidden="true"
              />
              {{ submitLabel }}
            </button>
          </form>

          <!-- 第二步：MFA -->
          <form v-else class="mt-6 space-y-4" novalidate @submit.prevent="submitMfa">
            <div>
              <label for="mfa-code" class="mb-1.5 block text-sm font-medium text-ink-600">
                动态验证码
              </label>
              <input
                id="mfa-code"
                ref="codeInput"
                :value="mfa.code"
                type="text"
                inputmode="numeric"
                autocomplete="one-time-code"
                maxlength="6"
                placeholder="6 位数字"
                class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-center font-mono text-lg tracking-[0.5em] text-ink-800 outline-none transition placeholder:tracking-normal placeholder:text-sm placeholder:text-ink-400/70 focus:ring-4"
                :class="
                  errors.code
                    ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                    : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                "
                @input="onCodeInput"
              />
              <p v-if="errors.code" class="mt-1.5 text-xs text-rose-600">{{ errors.code }}</p>
              <p v-else class="mt-1.5 text-xs text-ink-400">验证码 5 分钟内有效</p>
            </div>

            <button
              type="submit"
              class="flex w-full items-center justify-center gap-2 rounded-lg bg-brand-600 px-4 py-2.5 text-sm font-medium text-white transition hover:bg-brand-700 focus:outline-none focus-visible:ring-4 focus-visible:ring-brand-100 disabled:cursor-not-allowed disabled:opacity-60"
              :disabled="submitting"
            >
              <span
                v-if="submitting"
                class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"
                aria-hidden="true"
              />
              验证
            </button>

            <button
              type="button"
              class="w-full text-center text-sm text-ink-400 transition hover:text-brand-700"
              @click="backToCredentials()"
            >
              返回
            </button>
          </form>
        </div>

        <p class="mt-6 text-center text-xs text-ink-400">
          账号由管理员统一开通，如需帮助请联系 IT 管理员
        </p>
      </div>
    </main>
  </div>
</template>
