<script setup lang="ts">
import { computed, inject, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { getClient, updateClient } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type { ClientDetail } from '@/api/types'
import { formatTime } from '@/utils/format'
import { reloadClientSummaryKey } from './detail-context'

const reloadSummary = inject(reloadClientSummaryKey, async () => {})

const route = useRoute()
const clientId = computed(() => String(route.params.clientId ?? ''))

const detail = ref<ClientDetail | null>(null)
const loading = ref(true)
const loadError = ref('')

const form = reactive({
  clientName: '',
  description: '',
  logoUrl: '',
  redirectUris: [''],
  postLogoutUris: [] as string[],
  accessTokenTtl: 900,
  refreshTokenTtl: 2592000,
  status: 1,
})

const formRef = ref<FormInstance>()
const saving = ref(false)
const formError = ref('')

/** 首页地址：由首个回调地址的 origin 推导（门户"我的应用"入口使用） */
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

const ttlChanged = computed(() => {
  const d = detail.value
  if (!d) return false
  return d.accessTokenTtl !== form.accessTokenTtl || d.refreshTokenTtl !== form.refreshTokenTtl
})

function fill(d: ClientDetail) {
  form.clientName = d.clientName
  form.description = d.description ?? ''
  form.logoUrl = d.logoUrl ?? ''
  form.redirectUris = d.redirectUris.length ? [...d.redirectUris] : ['']
  form.postLogoutUris = [...d.postLogoutUris]
  form.accessTokenTtl = d.accessTokenTtl
  form.refreshTokenTtl = d.refreshTokenTtl
  form.status = d.status
}

async function load() {
  loading.value = true
  loadError.value = ''
  formError.value = ''
  try {
    const d = await getClient(clientId.value)
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

function validateUriList(min: number, max: number) {
  return (_rule: unknown, value: string[], callback: (e?: Error) => void) => {
    const uris = (value ?? []).map((u) => u.trim()).filter(Boolean)
    if (uris.length < min) {
      callback(new Error(min === 0 ? '地址格式不正确' : `至少填写 ${min} 条地址`))
      return
    }
    if (uris.length > max) {
      callback(new Error(`最多 ${max} 条地址`))
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
  redirectUris: [{ validator: validateUriList(1, 10), trigger: 'blur' }],
  postLogoutUris: [{ validator: validateUriList(0, 10), trigger: 'blur' }],
  accessTokenTtl: [
    { required: true, message: '请输入访问令牌有效期', trigger: 'blur' },
    { type: 'number', min: 60, max: 86400, message: '取值范围 60–86400 秒', trigger: 'blur' },
  ],
  refreshTokenTtl: [
    { required: true, message: '请输入刷新令牌有效期', trigger: 'blur' },
    { type: 'number', min: 300, max: 31536000, message: '取值范围 300–31536000 秒', trigger: 'blur' },
  ],
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

/* ---------------- 停用二次确认（FR-004） ---------------- */

async function beforeStatusChange(): Promise<boolean> {
  if (form.status === 1) {
    try {
      await ElMessageBox.confirm(
        '停用后全员在门户不可见，成员与角色配置保留。确定停用？',
        '停用应用',
        { type: 'warning', confirmButtonText: '确定停用', cancelButtonText: '取消' },
      )
      return true
    } catch {
      return false
    }
  }
  return true
}

/* ---------------- 保存 ---------------- */

async function save() {
  formError.value = ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  if (saving.value) return

  saving.value = true
  try {
    const updated = await updateClient(clientId.value, {
      clientName: form.clientName.trim(),
      description: form.description.trim(),
      logoUrl: form.logoUrl.trim(),
      redirectUris: form.redirectUris.map((u) => u.trim()).filter(Boolean),
      accessTokenTtl: form.accessTokenTtl,
      refreshTokenTtl: form.refreshTokenTtl,
      status: form.status,
      updatedAt: detail.value?.updatedAt,
    })
    detail.value = updated
    fill(updated)
    ElMessage.success('已保存')
    await reloadSummary()
  } catch (e) {
    if (errorCode(e) === 'APP_RESOURCE_STALE') {
      formError.value = '已被他人修改，请刷新后重试'
      ElMessage.warning(formError.value)
      await load()
    } else {
      formError.value = errorMessage(e)
    }
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="overview">
    <el-alert
      v-if="loadError"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
      class="block-alert"
    >
      <template #default>
        <el-button size="small" @click="load">重试</el-button>
      </template>
    </el-alert>

    <template v-else-if="detail">
      <section class="panel">
        <h2 class="panel-title">基本信息</h2>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="应用标识">
            <span class="mono">{{ detail.clientId }}</span>
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
              class="link mono"
            >
              {{ homeUrl }}
            </a>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item label="创建时间">
            {{ formatTime(detail.createdAt) }}
          </el-descriptions-item>
          <el-descriptions-item label="授权方式">
            <el-tag v-for="t in detail.grantTypes" :key="t" size="small" class="tag-gap">
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
              class="tag-gap"
            >
              {{ s }}
            </el-tag>
            <span v-if="!detail.scopes.length">-</span>
          </el-descriptions-item>
          <el-descriptions-item label="客户端密钥">
            <span class="readonly-note">
              密钥只在创建或轮换时显示一次，后台不再展示；如需更换请在应用列表「轮换密钥」
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="成员数">
            {{ detail.memberCount }} 人
          </el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="panel">
        <h2 class="panel-title">可编辑信息</h2>
        <el-alert
          v-if="formError"
          type="error"
          :title="formError"
          show-icon
          :closable="false"
          class="block-alert"
        />

        <el-form ref="formRef" :model="form" :rules="rules" label-width="140px" class="detail-form">
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

          <el-form-item label="Logo 地址" prop="logoUrl">
            <el-input v-model="form.logoUrl" maxlength="512" placeholder="选填，https://…" />
          </el-form-item>

          <el-form-item label="回调地址" prop="redirectUris">
            <div class="uri-list">
              <div v-for="(_, index) in form.redirectUris" :key="index" class="uri-row">
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
              <p class="field-hint">1–10 条，必须与接入方一致，不支持通配符</p>
            </div>
          </el-form-item>

          <el-form-item label="登出地址">
            <div class="uri-list">
              <div v-for="(u, index) in form.postLogoutUris" :key="index" class="uri-row">{{ u }}</div>
              <span v-if="form.postLogoutUris.length === 0">-</span>
              <p class="field-hint">仅展示，不可编辑</p>
            </div>
          </el-form-item>

          <el-form-item label="访问令牌有效期" prop="accessTokenTtl">
            <el-input-number v-model="form.accessTokenTtl" :min="60" :max="86400" :step="60" />
            <span class="unit">秒（60–86400）</span>
          </el-form-item>

          <el-form-item label="刷新令牌有效期" prop="refreshTokenTtl">
            <el-input-number v-model="form.refreshTokenTtl" :min="300" :max="31536000" :step="3600" />
            <span class="unit">秒（300–31536000）</span>
          </el-form-item>

          <el-form-item v-if="ttlChanged" label=" ">
            <el-alert
              type="info"
              :closable="false"
              show-icon
              title="仅对此后签发的令牌生效，已签发令牌不受影响"
            />
          </el-form-item>

          <el-form-item label="启用状态">
            <el-switch
              v-model="form.status"
              :active-value="1"
              :inactive-value="0"
              active-text="启用"
              inactive-text="停用"
              :before-change="beforeStatusChange"
            />
            <span class="field-hint inline">
              停用后全员在门户不可见，成员与角色配置保留
            </span>
          </el-form-item>

          <el-form-item>
            <el-button type="primary" :loading="saving" @click="save">保存</el-button>
            <el-button @click="load">重置</el-button>
            <span class="field-hint inline">保存时自动携带版本校验，避免覆盖他人修改</span>
          </el-form-item>
        </el-form>
      </section>
    </template>
  </div>
</template>

<style scoped>
.panel {
  padding: 16px;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
}

.panel + .panel {
  margin-top: 16px;
}

.panel-title {
  margin: 0 0 14px;
  font-size: 15px;
  font-weight: 600;
  color: var(--app-text);
}

.block-alert {
  margin-bottom: 12px;
}

.detail-form {
  max-width: 760px;
}

.uri-list {
  width: 100%;
}

.uri-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.field-hint {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--app-text-secondary);
  line-height: 1.5;
}

.field-hint.inline {
  margin: 0 0 0 12px;
}

.unit {
  margin-left: 10px;
  font-size: 13px;
  color: var(--app-text-secondary);
}

.readonly-note {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.tag-gap {
  margin-right: 6px;
}

.link {
  color: var(--el-color-primary);
  word-break: break-all;
}

.mono {
  font-family: var(--el-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}
</style>
