<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getCaptcha, login, resetPassword, sendCode, verifyMfa } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import { ADMIN_HOME } from '@/config'
import { useSessionStore } from '@/stores/session'
import { nextLabel, resolveNext } from '@/utils/redirect'
import type { LoginType, VerifyChannel } from '@/api/types'

type Step = 'credentials' | 'mfa' | 'forgot'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const step = ref<Step>('credentials')
const submitting = ref(false)
const banner = ref('')
/** 轻提示：只停留几秒自动消失，不占据表单版面（常驻提示会挤压表单、加剧滚动与干扰） */
const toast = ref('')
const showPassword = ref(false)
const cooldown = ref(0)

const passwordInput = ref<HTMLInputElement | null>(null)
const loginCodeInput = ref<HTMLInputElement | null>(null)
const codeInput = ref<HTMLInputElement | null>(null)

let cooldownTimer: number | undefined
let codeTimer: number | undefined
let toastTimer: number | undefined

const TOAST_DURATION = 3200

function showToast(message: string, duration = TOAST_DURATION) {
  window.clearTimeout(toastTimer)
  toast.value = message
  toastTimer = window.setTimeout(() => {
    toast.value = ''
  }, duration)
}

function dismissToast() {
  window.clearTimeout(toastTimer)
  toast.value = ''
}

/** 登录方式：密码 / 邮箱验证码 / 手机验证码（FR-008） */
const loginType = ref<LoginType>('password')
const modes: { value: LoginType; label: string; targetLabel: string; placeholder: string }[] = [
  { value: 'password', label: '密码登录', targetLabel: '用户名 / 邮箱 / 手机号', placeholder: '请输入用户名、邮箱或手机号' },
  { value: 'email_code', label: '邮箱验证码', targetLabel: '邮箱', placeholder: '请输入邮箱' },
  { value: 'sms_code', label: '手机验证码', targetLabel: '手机号', placeholder: '请输入 11 位手机号' },
]

const codeCooldown = ref(0)
const sendingCode = ref(false)

const form = reactive({ username: '', password: '', rememberMe: false })
const loginCode = ref('')
const errors = reactive({ username: '', password: '', code: '', captcha: '' })
/** 图形验证码：未登录的验证码登录与找回密码必须先过图形码（2 分钟有效、一次有效） */
const captcha = reactive({ id: '', image: '', code: '', loading: false })
const mfa = reactive({ ticket: '', code: '', expiresIn: 0 })
const forgot = reactive({
  channel: 'email' as VerifyChannel,
  target: '',
  code: '',
  newPassword: '',
  confirmPassword: '',
})
const forgotErrors = reactive({ target: '', code: '', newPassword: '', confirmPassword: '' })
const forgotChannels: { value: VerifyChannel; label: string }[] = [
  { value: 'email', label: '邮箱验证码' },
  { value: 'sms', label: '手机验证码' },
]

const activeMode = computed(() => modes.find((m) => m.value === loginType.value) ?? modes[0]!)

// 四域下 admin 传来的 next 是绝对地址（跨域），站内相对路径同样支持；白名单外的目标丢弃（防开放重定向）。
const nextTarget = computed(() => resolveNext(route.query.next))
const nextText = computed(() => nextLabel(nextTarget.value))

const subtitle = computed(() =>
  nextText.value ? `登录后返回 ${nextText.value}` : '登录后返回统一门户',
)

const submitLabel = computed(() => {
  if (cooldown.value > 0) return `请稍候 ${cooldown.value}s`
  if (step.value === 'mfa') return '验证'
  if (step.value === 'forgot') return '重置密码'
  return '登录'
})

onMounted(() => {
  if (route.query.reason === 'password-changed') {
    showToast('密码已修改，请使用新密码重新登录')
  }
  // FR-023：登出统一落回门户登录页并提示"已退出"；提示后立即清除参数，
  // 否则刷新/前进后退会重复弹提示，且参数会被继续带进后续回跳地址。
  if (route.query.loggedOut === '1') {
    showToast('已退出登录')
    const cleared = { ...route.query }
    delete cleared.loggedOut
    router.replace({ path: '/login', query: cleared })
  }
})

/** 拉取新的图形验证码（一次性使用，提交失败后必须刷新） */
async function refreshCaptcha() {
  captcha.loading = true
  try {
    const res = await getCaptcha()
    captcha.id = res.captchaId
    captcha.image = res.image
    captcha.code = ''
  } catch (e) {
    banner.value = errorMessage(e)
  } finally {
    captcha.loading = false
  }
}

/** 需要图形验证码的场景（验证码登录 / 找回密码）按需首次拉取 */
async function ensureCaptcha() {
  if (captcha.id && captcha.image) return
  await refreshCaptcha()
}

onBeforeUnmount(() => {
  window.clearInterval(cooldownTimer)
  window.clearInterval(codeTimer)
  window.clearTimeout(toastTimer)
})

function clearErrors() {
  errors.username = ''
  errors.password = ''
  errors.code = ''
  errors.captcha = ''
  forgotErrors.target = ''
  forgotErrors.code = ''
  forgotErrors.newPassword = ''
  forgotErrors.confirmPassword = ''
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

  const target = nextTarget.value
  if (target) {
    // 跨域（admin 域）必须整页跳转；站内相对路径走路由，避免整页刷新
    if (target.kind === 'relative') {
      await router.replace(target.path)
    } else {
      window.location.assign(target.url)
    }
    return
  }
  if (me?.roles.includes('admin')) {
    window.location.assign(ADMIN_HOME)
    return
  }
  await router.replace('/account/profile')
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const MOBILE_RE = /^1\d{10}$/

function isEmail(value: string): boolean {
  return EMAIL_RE.test(value.trim())
}

function isMobile(value: string): boolean {
  return MOBILE_RE.test(value.trim())
}

function switchMode(mode: LoginType) {
  if (loginType.value === mode) return
  loginType.value = mode
  banner.value = ''
  clearErrors()
  dismissToast()
  loginCode.value = ''
  form.password = ''
  // 账号输入框在不同方式下语义不同：切到邮箱/手机验证码时，若原值不符合该格式
  // 就清空，避免"用户名 张三"留在邮箱框里被误当成邮箱提交。
  form.username = keepIfCompatible(mode, form.username)
  if (mode !== 'password') void ensureCaptcha()
}

/** 切换登录方式时，只保留与新方式格式兼容的已填账号 */
function keepIfCompatible(mode: LoginType, value: string): string {
  const v = value.trim()
  if (!v) return ''
  if (mode === 'email_code') return isEmail(v) ? v : ''
  if (mode === 'sms_code') return isMobile(v) ? v : ''
  return v
}

/** 联系方式脱敏，用于提示"码发到哪了"，避免用户对着明文号码反复确认 */
function maskTarget(target: string): string {
  const v = target.trim()
  if (!v) return ''
  if (isEmail(v)) {
    const [name, domain] = v.split('@')
    const head = name!.slice(0, 1)
    return `${head}***@${domain}`
  }
  if (isMobile(v)) return `${v.slice(0, 3)}****${v.slice(7)}`
  return v
}

/** 用户开始修改某个字段时立即消掉它的旧错误，避免"已改正但红字还在" */
function clearError(field: keyof typeof errors) {
  if (errors[field]) errors[field] = ''
}

/** 账号/密码/验证码输入：纠正输入内容并即时清除对应错误 */
function onIdentifierInput() {
  clearError('username')
  if (banner.value) banner.value = ''
}

function onPasswordInput() {
  clearError('password')
}

function onCaptchaInput(event: Event) {
  captcha.code = (event.target as HTMLInputElement).value.replace(/[^0-9A-Za-z]/g, '').slice(0, 4)
  clearError('captcha')
}

function onLoginCodeInput(event: Event) {
  loginCode.value = (event.target as HTMLInputElement).value.replace(/\D/g, '').slice(0, 6)
  clearError('code')
}

function onForgotCodeInput(event: Event) {
  forgot.code = (event.target as HTMLInputElement).value.replace(/\D/g, '').slice(0, 6)
  if (forgotErrors.code) forgotErrors.code = ''
}

/** 校验登录标识：密码登录允许三种标识，验证码登录要求对应格式 */
function validateIdentifier(value: string): string {
  if (!value) {
    if (loginType.value === 'email_code') return '请输入邮箱'
    if (loginType.value === 'sms_code') return '请输入手机号'
    return '请输入账号'
  }
  if (loginType.value === 'email_code') {
    return isEmail(value) ? '' : '请输入正确的邮箱地址'
  }
  if (loginType.value === 'sms_code') {
    return isMobile(value) ? '' : '请输入 11 位手机号'
  }
  if (value.length > 128) return '账号长度不能超过 128 个字符'
  return ''
}

async function submitCredentials() {
  banner.value = ''
  dismissToast()
  clearErrors()

  const identifier = form.username.trim()
  errors.username = validateIdentifier(identifier)

  if (loginType.value === 'password') {
    if (!form.password) {
      errors.password = `请输入密码`
    } else if (form.password.length < 8 || form.password.length > 128) {
      errors.password = `密码长度需为 8–128 位`
    }
  } else {
    if (!/^\d{6}$/.test(loginCode.value)) errors.code = '请输入 6 位验证码'
    if (!captcha.code.trim()) errors.captcha = '请输入图形验证码'
  }

  // 一次性展示所有字段问题，避免逐个试错
  if (errors.username || errors.password || errors.code || errors.captcha) return
  if (submitting.value || cooldown.value > 0) return

  submitting.value = true
  try {
    const res =
      loginType.value === 'password'
        ? await login({
            loginType: 'password',
            identifier,
            username: identifier,
            password: form.password,
            clientId: 'portal',
            rememberMe: form.rememberMe,
          })
        : await login({
            loginType: loginType.value,
            identifier,
            code: loginCode.value,
            captchaId: captcha.id,
            captchaCode: captcha.code,
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

function startCodeCooldown(seconds: number) {
  codeCooldown.value = seconds
  window.clearInterval(codeTimer)
  codeTimer = window.setInterval(() => {
    codeCooldown.value -= 1
    if (codeCooldown.value <= 0) window.clearInterval(codeTimer)
  }, 1000)
}

/** 登录页获取邮箱/手机验证码（scene=login） */
async function sendLoginCode() {
  banner.value = ''
  errors.username = ''
  errors.code = ''
  const target = form.username.trim()
  const msg = validateIdentifier(target)
  if (msg) {
    errors.username = msg
    return
  }
  if (!captcha.code.trim()) {
    errors.captcha = '请先输入图形验证码'
    return
  }
  if (sendingCode.value || codeCooldown.value > 0) return
  sendingCode.value = true
  try {
    const channel: VerifyChannel = loginType.value === 'sms_code' ? 'sms' : 'email'
    const res = await sendCode({
      scene: 'login',
      channel,
      target,
      captchaId: captcha.id,
      captchaCode: captcha.code,
    })
    startCodeCooldown(res.resendAfter || 60)
    // 图形码已在服务端消费，重发前必须先换一张
    void refreshCaptcha()
    const minutes = Math.ceil((res.expiresIn || 300) / 60)
    showToast(`验证码已发送至 ${maskTarget(target)}，${minutes} 分钟内有效`)
    // 发送成功后把焦点送到验证码框，省掉一次点击
    window.setTimeout(() => loginCodeInput.value?.focus(), 0)
  } catch (e) {
    // 服务端只要走到图形码校验就会一次性消费它（GetDel），任何失败都必须换一张，
    // 否则用户重试的第一次请求必然先返回 AUTH_CAPTCHA_INVALID，把真正原因盖掉。
    if (errorCode(e) === 'AUTH_CAPTCHA_INVALID') {
      handleCaptchaError()
      return
    }
    void refreshCaptcha()
    // VERIFY_TARGET_NOT_FOUND：该邮箱/手机号未绑定任何账号（服务端文案已含处理建议）。
    banner.value = errorMessage(e)
  } finally {
    sendingCode.value = false
  }
}

/** 图形验证码错误：提示并换一张（一次性使用，必须重新获取） */
function handleCaptchaError() {
  errors.captcha = '图形验证码错误或已过期，请重新输入'
  void refreshCaptcha()
}

function setForgotChannel(channel: VerifyChannel) {
  forgot.channel = channel
  forgot.code = ''
  clearErrors()
}

function openForgot() {
  step.value = 'forgot'
  banner.value = ''
  dismissToast()
  clearErrors()
  forgot.channel = loginType.value === 'sms_code' ? 'sms' : 'email'
  // 只在已填内容与找回渠道匹配时带入，避免把用户名塞进"邮箱"输入框
  const typed = form.username.trim()
  forgot.target = loginType.value === 'password' ? '' : keepIfCompatible(loginType.value, typed)
  forgot.code = ''
  forgot.newPassword = ''
  forgot.confirmPassword = ''
  void ensureCaptcha()
}

function backToCredentials(message = '') {
  step.value = 'credentials'
  mfa.ticket = ''
  mfa.code = ''
  mfa.expiresIn = 0
  clearErrors()
  banner.value = message
}

/** 找回密码：获取验证码（scene=find_password） */
async function sendForgotCode() {
  banner.value = ''
  forgotErrors.target = ''
  const target = forgot.target.trim()
  if (forgot.channel === 'email') {
    forgotErrors.target = isEmail(target) ? '' : '请输入正确的邮箱地址'
  } else {
    forgotErrors.target = isMobile(target) ? '' : '请输入 11 位手机号'
  }
  if (forgotErrors.target) return
  if (!captcha.code.trim()) {
    errors.captcha = '请先输入图形验证码'
    return
  }
  if (sendingCode.value || codeCooldown.value > 0) return
  sendingCode.value = true
  try {
    const res = await sendCode({
      scene: 'find_password',
      channel: forgot.channel,
      target,
      captchaId: captcha.id,
      captchaCode: captcha.code,
    })
    startCodeCooldown(res.resendAfter || 60)
    void refreshCaptcha()
    showToast('如该联系方式已绑定账号，验证码将发送到该地址')
  } catch (e) {
    if (errorCode(e) === 'AUTH_CAPTCHA_INVALID') {
      handleCaptchaError()
      return
    }
    void refreshCaptcha()
    banner.value = errorMessage(e)
  } finally {
    sendingCode.value = false
  }
}

/** 找回密码：提交新密码（成功后不自动登录，澄清 Q1） */
async function submitForgot() {
  banner.value = ''
  dismissToast()
  clearErrors()
  const target = forgot.target.trim()
  if (forgot.channel === 'email') {
    forgotErrors.target = isEmail(target) ? '' : '请输入正确的邮箱地址'
  } else {
    forgotErrors.target = isMobile(target) ? '' : '请输入 11 位手机号'
  }
  if (!/^\d{6}$/.test(forgot.code)) forgotErrors.code = '请输入 6 位验证码'
  if (forgot.newPassword.length < 8 || forgot.newPassword.length > 128) {
    forgotErrors.newPassword = `密码长度需为 8–128 位`
  } else if (!/[A-Za-z]/.test(forgot.newPassword) || !/\d/.test(forgot.newPassword)) {
    forgotErrors.newPassword = `密码需同时包含字母与数字`
  }
  if (forgot.newPassword !== forgot.confirmPassword) {
    forgotErrors.confirmPassword = `两次输入的密码不一致`
  }
  if (forgotErrors.target || forgotErrors.code || forgotErrors.newPassword || forgotErrors.confirmPassword) {
    return
  }
  if (submitting.value) return

  submitting.value = true
  try {
    await resetPassword({
      channel: forgot.channel,
      target,
      code: forgot.code,
      newPassword: forgot.newPassword,
    })
    backToCredentials()
    showToast('密码已重置，请使用新密码登录')
  } catch (e) {
    if (errorCode(e) === 'AUTH_CODE_INVALID') {
      forgotErrors.code = '验证码错误或已过期'
    } else {
      banner.value = errorMessage(e)
    }
  } finally {
    submitting.value = false
  }
}

function handleLoginError(e: unknown) {
  // 验证码登录：服务端对图形码是一次性消费（GetDel），本次提交无论成败都已用掉，
  // 必须换一张，否则用户重试的第一次请求必然先返回 AUTH_CAPTCHA_INVALID。
  if (loginType.value !== 'password') void refreshCaptcha()

  switch (errorCode(e)) {
    case 'AUTH_INVALID_CREDENTIALS':
      // 验证码登录：账号未绑定对应联系方式时同样落到此处（服务端不区分，避免枚举），
      // 故提示统一给出「绑定指引」而不是「密码错误」。
      if (loginType.value === 'password') {
        banner.value = '用户名或密码错误'
        form.password = ''
        passwordInput.value?.focus()
      } else {
        banner.value =
          '验证码登录失败，请确认验证码是否正确；如账号未绑定手机号或邮箱，请改用密码登录后到账号中心绑定'
      }
      break
    case 'AUTH_CAPTCHA_INVALID':
      // 图片已在函数入口刷新，这里只提示
      errors.captcha = '图形验证码错误或已过期，请重新输入'
      break
    case 'AUTH_ACCOUNT_LOCKED':
      banner.value = errorMessage(e) || '账号已锁定，请 15 分钟后再试或联系管理员'
      startCooldown(10)
      break
    case 'AUTH_ACCOUNT_DISABLED':
      banner.value = '账号已停用，请联系管理员'
      break
    case 'AUTH_CODE_INVALID':
      errors.code = '验证码错误或已过期'
      loginCode.value = ''
      break
    case 'RATE_LIMITED':
      banner.value = '操作过于频繁，请稍后再试'
      startCooldown(10)
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
            {{
              step === 'credentials' ? '统一身份认证' : step === 'mfa' ? '两步验证' : '找回密码'
            }}
          </h1>
          <p class="mt-2 text-sm text-ink-400">
            {{
              step === 'credentials'
                ? subtitle
                : step === 'mfa'
                  ? '请输入身份验证器中的 6 位动态码'
                  : '通过已绑定的邮箱或手机号验证身份后设置新密码'
            }}
          </p>

          <!-- 轻提示：浮在页面顶部，几秒后自动消失，不占表单版面 -->
          <div class="pointer-events-none fixed inset-x-0 top-6 z-50 flex justify-center px-4">
            <Transition name="toast">
              <div
                v-if="toast"
                class="rounded-lg bg-ink-800 px-4 py-2.5 text-sm text-white shadow-lg"
                role="status"
                aria-live="polite"
              >
                {{ toast }}
              </div>
            </Transition>
          </div>

          <div
            v-if="banner"
            class="mt-5 rounded-lg bg-rose-50 px-3.5 py-2.5 text-sm text-rose-700"
            role="alert"
          >
            {{ banner }}
          </div>

          <!-- 第一步：多方式登录（密码 / 邮箱验证码 / 手机验证码） -->
          <form v-if="step === 'credentials'" class="mt-6 space-y-4" novalidate @submit.prevent="submitCredentials">
            <div class="flex rounded-lg bg-ink-50 p-1 text-xs font-medium text-ink-500">
              <button
                v-for="m in modes"
                :key="m.value"
                type="button"
                class="flex-1 rounded-md px-2 py-2 transition"
                :class="loginType === m.value ? 'bg-white text-brand-700 shadow-sm' : 'hover:text-ink-700'"
                @click="switchMode(m.value)"
              >
                {{ m.label }}
              </button>
            </div>

            <div>
              <label for="username" class="mb-1.5 block text-sm font-medium text-ink-600">
                {{ activeMode.targetLabel }}
              </label>
              <input
                id="username"
                v-model="form.username"
                type="text"
                name="username"
                autocomplete="username"
                :placeholder="activeMode.placeholder"
                class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
                :class="
                  errors.username
                    ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                    : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                "
                @input="onIdentifierInput"
              />
              <p v-if="errors.username" class="mt-1.5 text-xs text-rose-600">{{ errors.username }}</p>
            </div>

            <div v-if="loginType === 'password'">
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
                  @input="onPasswordInput"
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

            <div v-else class="space-y-4">
              <div>
                <label for="login-captcha" class="mb-1.5 block text-sm font-medium text-ink-600">
                  图形验证码
                </label>
                <div class="flex gap-2">
                  <input
                    id="login-captcha"
                    v-model="captcha.code"
                    type="text"
                    autocomplete="off"
                    maxlength="4"
                    placeholder="4 位字符"
                    class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm tracking-[0.3em] text-ink-800 uppercase outline-none transition placeholder:tracking-normal placeholder:text-ink-400/70 focus:ring-4"
                    :class="
                      errors.captcha
                        ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                        : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                    "
                    @input="onCaptchaInput"
                  />
                  <button
                    type="button"
                    class="h-[44px] w-[132px] shrink-0 overflow-hidden rounded-lg border border-ink-200 bg-white transition hover:border-brand-300 disabled:opacity-60"
                    title="看不清？点击换一张"
                    :disabled="captcha.loading"
                    @click="refreshCaptcha"
                  >
                    <img
                      v-if="captcha.image"
                      :src="captcha.image"
                      alt="图形验证码"
                      class="h-full w-full object-contain"
                    />
                    <span v-else class="text-xs text-ink-400">加载中…</span>
                  </button>
                </div>
                <p v-if="errors.captcha" class="mt-1.5 text-xs text-rose-600">{{ errors.captcha }}</p>
              </div>

              <div>
                <label for="login-code" class="mb-1.5 block text-sm font-medium text-ink-600">
                  验证码
                </label>
                <div class="flex gap-2">
                  <input
                    id="login-code"
                    ref="loginCodeInput"
                    v-model="loginCode"
                    type="text"
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    maxlength="6"
                    placeholder="6 位数字"
                    class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm tracking-[0.3em] text-ink-800 outline-none transition placeholder:tracking-normal placeholder:text-ink-400/70 focus:ring-4"
                    :class="
                      errors.code
                        ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                        : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                    "
                    @input="onLoginCodeInput"
                  />
                  <button
                    type="button"
                    class="shrink-0 rounded-lg border border-ink-200 px-3 text-xs text-ink-600 transition hover:border-brand-300 hover:text-brand-700 disabled:cursor-not-allowed disabled:opacity-50"
                    :disabled="sendingCode || codeCooldown > 0"
                    @click="sendLoginCode"
                  >
                    {{ codeCooldown > 0 ? `${codeCooldown}s 后重发` : '获取验证码' }}
                  </button>
                </div>
                <p v-if="errors.code" class="mt-1.5 text-xs text-rose-600">{{ errors.code }}</p>
              </div>
            </div>

            <div class="flex items-center justify-between">
              <label class="flex cursor-pointer select-none items-center gap-2 text-sm text-ink-600">
                <input
                  v-model="form.rememberMe"
                  type="checkbox"
                  class="h-4 w-4 rounded border-ink-200 text-brand-600 focus:ring-brand-200"
                />
                7 天内免登录
              </label>
              <button
                type="button"
                class="text-sm text-ink-400 transition hover:text-brand-700"
                @click="openForgot"
              >
                忘记密码？
              </button>
            </div>

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

          <!-- 找回密码 -->
          <form
            v-else-if="step === 'forgot'"
            class="mt-6 space-y-4"
            novalidate
            @submit.prevent="submitForgot"
          >
            <div class="flex rounded-lg bg-ink-50 p-1 text-xs font-medium text-ink-500">
              <button
                v-for="c in forgotChannels"
                :key="c.value"
                type="button"
                class="flex-1 rounded-md px-2 py-2 transition"
                :class="forgot.channel === c.value ? 'bg-white text-brand-700 shadow-sm' : 'hover:text-ink-700'"
                @click="setForgotChannel(c.value)"
              >
                {{ c.label }}
              </button>
            </div>

            <div>
              <label for="forgot-target" class="mb-1.5 block text-sm font-medium text-ink-600">
                {{ forgot.channel === 'email' ? '邮箱' : '手机号' }}
              </label>
              <input
                id="forgot-target"
                v-model="forgot.target"
                type="text"
                :placeholder="forgot.channel === 'email' ? '请输入已绑定的邮箱' : '请输入已绑定的手机号'"
                class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
                :class="
                  forgotErrors.target
                    ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                    : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                "
              />
              <p v-if="forgotErrors.target" class="mt-1.5 text-xs text-rose-600">
                {{ forgotErrors.target }}
              </p>
            </div>

            <div>
              <label for="forgot-captcha" class="mb-1.5 block text-sm font-medium text-ink-600">
                图形验证码
              </label>
              <div class="flex gap-2">
                <input
                  id="forgot-captcha"
                  v-model="captcha.code"
                  type="text"
                  autocomplete="off"
                  maxlength="4"
                  placeholder="4 位字符"
                  class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm uppercase tracking-[0.3em] text-ink-800 outline-none transition placeholder:tracking-normal placeholder:text-ink-400/70 focus:ring-4"
                  :class="
                    errors.captcha
                      ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                      : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                  "
                  @input="onCaptchaInput"
                />
                <button
                  type="button"
                  class="h-[44px] w-[132px] shrink-0 overflow-hidden rounded-lg border border-ink-200 bg-white transition hover:border-brand-300 disabled:opacity-60"
                  title="看不清？点击换一张"
                  :disabled="captcha.loading"
                  @click="refreshCaptcha"
                >
                  <img
                    v-if="captcha.image"
                    :src="captcha.image"
                    alt="图形验证码"
                    class="h-full w-full object-contain"
                  />
                  <span v-else class="text-xs text-ink-400">加载中…</span>
                </button>
              </div>
              <p v-if="errors.captcha" class="mt-1.5 text-xs text-rose-600">{{ errors.captcha }}</p>
            </div>

            <div>
              <label for="forgot-code" class="mb-1.5 block text-sm font-medium text-ink-600">
                验证码
              </label>
              <div class="flex gap-2">
                <input
                  id="forgot-code"
                  v-model="forgot.code"
                  type="text"
                  inputmode="numeric"
                  maxlength="6"
                  placeholder="6 位数字"
                  class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm tracking-[0.3em] text-ink-800 outline-none transition placeholder:tracking-normal placeholder:text-ink-400/70 focus:ring-4"
                  :class="
                    forgotErrors.code
                      ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                      : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                  "
                  @input="onForgotCodeInput"
                />
                <button
                  type="button"
                  class="shrink-0 rounded-lg border border-ink-200 px-3 text-xs text-ink-600 transition hover:border-brand-300 hover:text-brand-700 disabled:cursor-not-allowed disabled:opacity-50"
                  :disabled="sendingCode || codeCooldown > 0"
                  @click="sendForgotCode"
                >
                  {{ codeCooldown > 0 ? `${codeCooldown}s 后重发` : '获取验证码' }}
                </button>
              </div>
              <p v-if="forgotErrors.code" class="mt-1.5 text-xs text-rose-600">
                {{ forgotErrors.code }}
              </p>
            </div>

            <div>
              <label for="forgot-password" class="mb-1.5 block text-sm font-medium text-ink-600">
                新密码
              </label>
              <input
                id="forgot-password"
                v-model="forgot.newPassword"
                type="password"
                autocomplete="new-password"
                placeholder="8–128 位，需含字母与数字"
                class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
                :class="
                  forgotErrors.newPassword
                    ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                    : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                "
              />
              <p v-if="forgotErrors.newPassword" class="mt-1.5 text-xs text-rose-600">
                {{ forgotErrors.newPassword }}
              </p>
            </div>

            <div>
              <label for="forgot-confirm" class="mb-1.5 block text-sm font-medium text-ink-600">
                确认新密码
              </label>
              <input
                id="forgot-confirm"
                v-model="forgot.confirmPassword"
                type="password"
                autocomplete="new-password"
                placeholder="请再次输入新密码"
                class="w-full rounded-lg border bg-white px-3.5 py-2.5 text-sm text-ink-800 outline-none transition placeholder:text-ink-400/70 focus:ring-4"
                :class="
                  forgotErrors.confirmPassword
                    ? 'border-rose-300 focus:border-rose-400 focus:ring-rose-100'
                    : 'border-ink-200 focus:border-brand-400 focus:ring-brand-50'
                "
              />
              <p v-if="forgotErrors.confirmPassword" class="mt-1.5 text-xs text-rose-600">
                {{ forgotErrors.confirmPassword }}
              </p>
            </div>

            <button
              type="submit"
              class="mt-2 flex w-full items-center justify-center gap-2 rounded-lg bg-brand-600 px-4 py-2.5 text-sm font-medium text-white transition hover:bg-brand-700 focus:outline-none focus-visible:ring-4 focus-visible:ring-brand-100 disabled:cursor-not-allowed disabled:opacity-60"
              :disabled="submitting"
            >
              <span
                v-if="submitting"
                class="h-4 w-4 animate-spin rounded-full border-2 border-white/40 border-t-white"
                aria-hidden="true"
              />
              {{ submitLabel }}
            </button>

            <button
              type="button"
              class="w-full text-center text-sm text-ink-400 transition hover:text-brand-700"
              @click="backToCredentials()"
            >
              返回登录
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

<style scoped>
/* 轻提示的进出场：只做淡入淡出 + 轻微位移，不打断视线 */
.toast-enter-active,
.toast-leave-active {
  transition:
    opacity 0.18s ease,
    transform 0.18s ease;
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
