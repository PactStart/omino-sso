<script setup lang="ts">
/**
 * 应用信息（006 口径调整）：参考 admin 端应用详情，但可编辑字段缩减为
 * 名称 / 描述 / Logo / 回调地址；其余字段（应用标识、类型、授权方式、范围、
 * 登出地址、TTL、启用状态、创建时间、成员数）一律只读展示。
 */
import { computed, inject, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  ElAlert,
  ElButton,
  ElDescriptions,
  ElDescriptionsItem,
  ElForm,
  ElFormItem,
  ElInput,
  ElLoading,
  ElMessage,
  ElTag,
  type FormInstance,
  type FormRules,
} from './ep'
import { clearAppLogo, getAppDetail, setAppLogo, updateAppBasicInfo } from '@/api/app'
import { errorCode, errorMessage } from '@/api/client'
import type { AppDetail } from '@/api/types'
import ImageUpload from '@/components/ImageUpload.vue'
import { formatTime } from '@/utils/format'
import { reloadClientSummaryKey } from './detail-context'

const reloadSummary = inject(reloadClientSummaryKey, async () => {})

// v-loading 是 Element Plus 的指令，局部引入时需显式注册（portal 未全局安装 EP）。
const vLoading = ElLoading.directive

const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

const detail = ref<AppDetail | null>(null)
const loading = ref(true)
const loadError = ref('')

const form = reactive({
  clientName: '',
  description: '',
  logoUrl: '',
  redirectUris: [''] as string[],
})

const formRef = ref<FormInstance>()
const saving = ref(false)
const formError = ref('')
const logoBroken = ref(false)

/** 首页地址：由首个回调地址的 origin 推导（只读展示） */
const homeUrl = computed(() => {
  const first = form.redirectUris.map((u) => u.trim()).find(Boolean)
  if (!first) return ''
  try {
    const u = new URL(first)
    return `${u.protocol}//${u.host}`
  } catch {
    return ''
  }
})

/** 秒 → 人类可读时长（只读展示用） */
function formatTtl(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  const parts: string[] = []
  let rest = Math.floor(seconds)
  for (const [unit, label] of [
    [86400, '天'],
    [3600, '小时'],
    [60, '分钟'],
    [1, '秒'],
  ] as const) {
    if (rest >= unit) {
      const n = Math.floor(rest / unit)
      rest -= n * unit
      parts.push(`${n} ${label}`)
    }
  }
  return parts.join(' ')
}

function fill(d: AppDetail) {
  form.clientName = d.clientName
  form.description = d.description ?? ''
  form.logoUrl = d.logoUrl ?? ''
  form.redirectUris = d.redirectUris.length ? [...d.redirectUris] : ['']
  logoBroken.value = false
}

async function load() {
  loading.value = true
  loadError.value = ''
  formError.value = ''
  try {
    const d = await getAppDetail(clientId.value)
    detail.value = d
    fill(d)
    formRef.value?.clearValidate()
  } catch (e) {
    loadError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

/* ---------------- 校验 ---------------- */

function validateUriList() {
  return (_rule: unknown, value: string[], callback: (e?: Error) => void) => {
    const uris = (value ?? []).map((u) => u.trim()).filter(Boolean)
    if (uris.length < 1) {
      callback(new Error('至少填写 1 条回调地址'))
      return
    }
    if (uris.length > 10) {
      callback(new Error('最多 10 条回调地址'))
      return
    }
    const tooLong = uris.find((u) => u.length > 512)
    if (tooLong) {
      callback(new Error('单条地址最长 512 个字符'))
      return
    }
    const invalid = uris.find((u) => !/^https?:\/\/[^\s]+$/i.test(u))
    if (invalid) {
      callback(new Error(`必须是 http(s) 绝对地址：${invalid}`))
      return
    }
    if (new Set(uris).size !== uris.length) {
      callback(new Error('地址不能重复'))
      return
    }
    callback()
  }
}

const rules: FormRules = {
  clientName: [
    { required: true, message: '请输入应用名称', trigger: 'blur' },
    { max: 128, message: '应用名称最长 128 个字符', trigger: 'blur' },
  ],
  description: [{ max: 255, message: '描述最长 255 个字符', trigger: 'blur' }],
  logoUrl: [
    {
      validator: (_r: unknown, v: string, cb: (e?: Error) => void) => {
        const text = (v ?? '').trim()
        if (!text) return cb()
        if (text.length > 512) return cb(new Error('图标地址最长 512 个字符'))
        if (!/^https?:\/\/[^\s]+$/i.test(text)) return cb(new Error('请输入 http(s) 绝对地址'))
        cb()
      },
      trigger: 'blur',
    },
  ],
  redirectUris: [{ validator: validateUriList(), trigger: 'blur' }],
}

function addRedirect() {
  form.redirectUris.push('')
}

function removeRedirect(index: number) {
  if (form.redirectUris.length === 1) {
    form.redirectUris[0] = ''
    return
  }
  form.redirectUris.splice(index, 1)
}

/* ---------------- 保存 ---------------- */

async function save() {
  formError.value = ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  if (saving.value) return

  // el-form 对数组型自定义校验的失败可能不带 fields（被聚合为「通过」），
  // 这两条是提交前的硬性兜底，保证不会把空名称/空回调地址发出去。
  const clientName = form.clientName.trim()
  const redirectUris = form.redirectUris.map((u) => u.trim()).filter(Boolean)
  if (!clientName) {
    formError.value = '请输入应用名称'
    return
  }
  if (!redirectUris.length) {
    formError.value = '至少填写 1 条回调地址'
    return
  }

  saving.value = true
  try {
    const updated = await updateAppBasicInfo(clientId.value, {
      clientName,
      description: form.description.trim(),
      logoUrl: form.logoUrl.trim(),
      redirectUris,
      updatedAt: detail.value?.updatedAt,
    })
    detail.value = updated
    fill(updated)
    ElMessage.success('已保存')
    await reloadSummary()
  } catch (e) {
    if (errorCode(e) === 'APP_RESOURCE_STALE') {
      const conflict = '已被他人修改，请刷新后重试'
      ElMessage.warning(conflict)
      // load() 会把 formError 清空，刷新后再落一次，保证用户仍能看到冲突提示
      await load()
      formError.value = conflict
    } else {
      formError.value = errorMessage(e)
    }
  } finally {
    saving.value = false
  }
}

/* ---------------- Logo ---------------- */

/**
 * 上传/清除 Logo 会在服务端回写 logo_url 时刷新 clients.updated_at，
 * 页面持有的版本号随之过期；这里只同步版本号与 logoUrl，不动用户正在编辑的表单，
 * 避免「刚传完 Logo 点保存被乐观锁判为冲突、刚输入的名称/描述被回滚」。
 */
async function syncVersion() {
  try {
    const d = await getAppDetail(clientId.value)
    if (detail.value) {
      detail.value.updatedAt = d.updatedAt
      detail.value.logoUrl = d.logoUrl
    }
  } catch {
    // 拉取失败不阻断编辑；保存时仍会按服务端 409 兜底提示刷新
  }
}

async function confirmLogo(key: string, format: string, size: number) {
  const file = await setAppLogo(clientId.value, { key, format, size })
  return { url: file.url }
}

function onLogoSuccess(url: string) {
  form.logoUrl = url
  logoBroken.value = false
  void syncVersion()
  void reloadSummary()
  ElMessage.success('Logo 已更新')
}

function onLogoError(message: string) {
  ElMessage.error(message)
}

async function clearLogo() {
  try {
    await clearAppLogo(clientId.value)
    form.logoUrl = ''
    logoBroken.value = false
    await syncVersion()
    void reloadSummary()
    ElMessage.success('Logo 已清除')
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="space-y-4">
    <el-alert
      v-if="loadError"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
      class="rounded-[var(--radius-card)]"
    >
      <el-button size="small" @click="load">重试</el-button>
    </el-alert>

    <template v-else-if="detail">
      <section class="rounded-[var(--radius-card)] border border-ink-100 bg-white p-5">
        <h2 class="mb-3 text-base font-semibold text-ink-900">可编辑信息</h2>
        <p class="mb-3 text-xs text-ink-500">
          应用管理员可修改名称、描述、Logo 与回调地址；其余信息见下方「只读信息」。
        </p>
        <el-alert
          v-if="formError"
          type="error"
          :title="formError"
          show-icon
          :closable="false"
          class="mb-3"
        />

        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-width="120px"
          class="max-w-3xl"
        >
          <el-form-item label="应用名称" prop="clientName">
            <el-input v-model="form.clientName" maxlength="128" placeholder="如：订单中心" />
          </el-form-item>

          <el-form-item label="描述" prop="description">
            <el-input
              v-model="form.description"
              type="textarea"
              :rows="2"
              maxlength="255"
              show-word-limit
              placeholder="选填，简要说明应用用途"
            />
          </el-form-item>

          <el-form-item label="Logo" prop="logoUrl">
            <div class="flex w-full flex-col gap-2">
              <img
                v-if="form.logoUrl && !logoBroken"
                :src="form.logoUrl"
                class="h-16 w-16 rounded-lg border border-ink-100 object-cover"
                alt="应用 Logo"
                @error="logoBroken = true"
              />
              <div class="flex items-center gap-2">
                <ImageUpload
                  scene="app_logo"
                  :confirm="confirmLogo"
                  @success="onLogoSuccess"
                  @error="onLogoError"
                />
                <el-button v-if="form.logoUrl" link type="danger" @click="clearLogo">
                  清除
                </el-button>
              </div>
              <el-input v-model="form.logoUrl" maxlength="512" placeholder="或填写外部 https://… 地址" />
              <p class="m-0 text-xs text-ink-500">上传后立即生效并回填地址；也可保留外部 URL</p>
            </div>
          </el-form-item>

          <el-form-item label="回调地址" prop="redirectUris">
            <div class="w-full">
              <div v-for="(_, index) in form.redirectUris" :key="index" class="mb-2 flex items-center gap-2">
                <el-input v-model="form.redirectUris[index]" placeholder="https://app.example.com/cb" />
                <el-button link type="danger" @click="removeRedirect(index)">删除</el-button>
              </div>
              <el-button
                v-if="form.redirectUris.length < 10"
                link
                type="primary"
                @click="addRedirect"
              >
                + 添加回调地址
              </el-button>
              <p class="m-0 text-xs text-ink-500">1–10 条，必须与接入方一致，不支持通配符</p>
            </div>
          </el-form-item>

          <el-form-item>
            <el-button type="primary" :loading="saving" @click="save">保存</el-button>
            <el-button @click="load">重置</el-button>
            <span class="ml-3 text-xs text-ink-500">保存时自动携带版本校验，避免覆盖他人修改</span>
          </el-form-item>
        </el-form>
      </section>

      <section class="rounded-[var(--radius-card)] border border-ink-100 bg-white p-5">
        <h2 class="mb-3 text-base font-semibold text-ink-900">只读信息</h2>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="应用标识">
            <span class="font-mono">{{ detail.clientId }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="客户端类型">
            <el-tag size="small" :type="detail.isPublic ? 'primary' : 'warning'">
              {{ detail.isPublic ? '公共客户端(PKCE)' : '机密客户端' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="首页地址">
            <a
              v-if="homeUrl"
              :href="homeUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="break-all text-brand-700"
            >
              {{ homeUrl }}
            </a>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item label="启用状态">
            <el-tag size="small" :type="detail.status === 1 ? 'success' : 'info'">
              {{ detail.status === 1 ? '启用' : '停用' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="授权方式">
            <el-tag v-for="t in detail.grantTypes" :key="t" size="small" class="mr-1.5">
              {{ t }}
            </el-tag>
            <span v-if="!detail.grantTypes.length">-</span>
          </el-descriptions-item>
          <el-descriptions-item label="可申请范围">
            <el-tag
              v-for="s in detail.scopes"
              :key="s"
              size="small"
              type="info"
              class="mr-1.5"
            >
              {{ s }}
            </el-tag>
            <span v-if="!detail.scopes.length">-</span>
          </el-descriptions-item>
          <el-descriptions-item label="访问令牌有效期">
            {{ formatTtl(detail.accessTokenTtl) }}
          </el-descriptions-item>
          <el-descriptions-item label="刷新令牌有效期">
            {{ formatTtl(detail.refreshTokenTtl) }}
          </el-descriptions-item>
          <el-descriptions-item label="登出地址" :span="2">
            <div v-if="detail.postLogoutUris.length" class="flex flex-col gap-1 break-all font-mono">
              <span v-for="u in detail.postLogoutUris" :key="u">{{ u }}</span>
            </div>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">
            {{ formatTime(detail.createdAt) }}
          </el-descriptions-item>
          <el-descriptions-item label="成员数">
            {{ detail.memberCount }} 人
          </el-descriptions-item>
          <el-descriptions-item label="客户端密钥" :span="2">
            <span class="text-xs text-ink-500">
              密钥只在创建或轮换时显示一次，门户不展示；如需更换请联系平台管理员
            </span>
          </el-descriptions-item>
        </el-descriptions>
      </section>
    </template>
  </div>
</template>
