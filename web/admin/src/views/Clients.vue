<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { confirmUpload, createClient, listClients, rotateClientSecret } from '@/api'
import { errorMessage } from '@/api/client'
import type { ClientRow } from '@/api/types'
import ImageUpload from '@/components/ImageUpload.vue'
import { formatTime, orDash } from '@/utils/format'

const router = useRouter()

/* ---------------- 列表 ---------------- */
const clients = ref<ClientRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const keyword = ref('')
/** FR-032：仅看无成员应用，便于上线前定位待补员应用 */
const onlyNoMembers = ref(false)
const loading = ref(true)
const error = ref('')

async function loadClients() {
  loading.value = true
  error.value = ''
  try {
    const data = await listClients({
      keyword: keyword.value.trim() || undefined,
      page: page.value,
      pageSize: pageSize.value,
      hasMembers: onlyNoMembers.value ? 'false' : undefined,
    })
    clients.value = data.list
    total.value = data.total
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function onSearch() {
  page.value = 1
  void loadClients()
}

function onToggleOnlyNoMembers() {
  page.value = 1
  void loadClients()
}

function onPageChange(next: number) {
  page.value = next
  void loadClients()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void loadClients()
}

function scopeText(scopes: string[]): string {
  if (!scopes || scopes.length === 0) return '-'
  const head = scopes.slice(0, 3).join(', ')
  return scopes.length > 3 ? `${head} +${scopes.length - 3}` : head
}

async function copy(text: string, label = '已复制') {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(label)
  } catch {
    ElMessage.warning('复制失败，请手动选择后复制')
  }
}

/* ---------------- 注册应用 ---------------- */
const registerOpen = ref(false)
const submitting = ref(false)
const formError = ref('')
const formRef = ref<FormInstance>()

const form = reactive({
  clientName: '',
  description: '',
  isPublic: true,
  redirectUris: [''],
  grantTypes: ['authorization_code', 'refresh_token'] as string[],
  scopes: ['openid', 'profile', 'email'] as string[],
  logoFileId: '',
  logoPreview: '',
})

function validateRedirectUris(_rule: unknown, value: string[], callback: (e?: Error) => void) {
  const uris = (value ?? []).map((item) => item.trim()).filter(Boolean)
  if (uris.length === 0) {
    callback(new Error('至少填写一条回调地址'))
    return
  }
  const invalid = uris.find((uri) => !/^https?:\/\/[^\s]+$/i.test(uri))
  if (invalid) {
    callback(new Error(`回调地址必须是完整 URL：${invalid}`))
    return
  }
  callback()
}

const rules: FormRules = {
  clientName: [
    { required: true, message: '请输入应用名称', trigger: 'blur' },
    { max: 128, message: '应用名称最长 128 个字符', trigger: 'blur' },
  ],
  description: [{ max: 255, message: '描述最长 255 个字符', trigger: 'blur' }],
  redirectUris: [{ validator: validateRedirectUris, trigger: 'blur' }],
}

function resetForm() {
  form.clientName = ''
  form.description = ''
  form.isPublic = true
  form.redirectUris = ['']
  form.grantTypes = ['authorization_code', 'refresh_token']
  form.scopes = ['openid', 'profile', 'email']
  form.logoFileId = ''
  form.logoPreview = ''
  formError.value = ''
  formRef.value?.clearValidate()
}

function openRegister() {
  resetForm()
  registerOpen.value = true
}

function addRedirectUri() {
  form.redirectUris.push('')
}

function removeRedirectUri(index: number) {
  if (form.redirectUris.length === 1) {
    form.redirectUris[0] = ''
    return
  }
  form.redirectUris.splice(index, 1)
}

/* ---------------- Logo 上传（先存文件、创建成功后绑定） ---------------- */

async function confirmLogo(key: string, format: string, size: number) {
  const file = await confirmUpload({ scene: 'app_logo', key, format, size })
  return { url: file.url, id: file.id }
}

function onLogoFile(url: string, fileId?: string) {
  form.logoFileId = fileId ?? ''
  form.logoPreview = url
}

function onLogoError(message: string) {
  ElMessage.error(message)
}

function removeLogo() {
  form.logoFileId = ''
  form.logoPreview = ''
}

/* ---------------- 密钥一次性弹窗 ---------------- */
const secretVisible = ref(false)
const secretRevealed = ref(false)
const secret = reactive({
  clientId: '',
  clientSecret: '',
  expiresAt: null as string | null,
  source: 'create' as 'create' | 'rotate',
})

function showSecret(payload: {
  clientId: string
  clientSecret: string
  expiresAt: string | null
  source: 'create' | 'rotate'
}) {
  secret.clientId = payload.clientId
  secret.clientSecret = payload.clientSecret
  secret.expiresAt = payload.expiresAt
  secret.source = payload.source
  secretRevealed.value = false
  secretVisible.value = true
}

function clearSecret() {
  secret.clientId = ''
  secret.clientSecret = ''
  secret.expiresAt = null
  secretRevealed.value = false
}

async function submitRegister() {
  formError.value = ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  if (submitting.value) return

  submitting.value = true
  try {
    const res = await createClient({
      clientName: form.clientName.trim(),
      description: form.description.trim() || undefined,
      redirectUris: form.redirectUris.map((item) => item.trim()).filter(Boolean),
      grantTypes: form.grantTypes,
      scopes: form.scopes,
      isPublic: form.isPublic,
      fileId: form.logoFileId || undefined,
    })

    registerOpen.value = false
    await loadClients()

    if (res.clientSecret) {
      ElMessage.success('应用已创建，请立即保存密钥')
      showSecret({
        clientId: res.clientId,
        clientSecret: res.clientSecret,
        expiresAt: null,
        source: 'create',
      })
    } else {
      ElMessage.success('应用已创建，请将 clientId 配置到接入方')
    }
  } catch (e) {
    formError.value = errorMessage(e)
  } finally {
    submitting.value = false
  }
}

/* ---------------- 轮换密钥 ---------------- */
async function rotate(row: ClientRow) {
  if (row.isPublic) return
  const name = row.clientName || row.clientId
  try {
    await ElMessageBox.confirm(
      `轮换后旧密钥立即失效，接入方需尽快更新配置。确定轮换「${name}」的密钥？`,
      '轮换密钥',
      { type: 'warning', confirmButtonText: '确定轮换', cancelButtonText: '取消' },
    )
  } catch {
    return
  }

  try {
    const res = await rotateClientSecret(row.clientId)
    showSecret({
      clientId: res.clientId,
      clientSecret: res.clientSecret,
      expiresAt: res.expiresAt,
      source: 'rotate',
    })
    ElMessage.success('密钥已轮换')
    await loadClients()
  } catch (e) {
    ElMessage.error(errorMessage(e))
  }
}

/* ---------------- 详情子页面（FR-001） ---------------- */
function openDetail(row: ClientRow) {
  void router.push({ name: 'client-overview', params: { clientId: row.clientId } })
}

onMounted(loadClients)
</script>

<template>
  <AdminLayout>
    <div class="page-head">
      <h1 class="page-title">应用管理</h1>
      <p class="page-desc">应用接入使用 OIDC 授权码 + PKCE(S256)</p>
    </div>

    <section class="panel">
      <div class="toolbar">
        <el-input
          v-model="keyword"
          placeholder="搜索 clientId 或应用名称"
          clearable
          style="width: 260px"
          @keyup.enter="onSearch"
          @clear="onSearch"
        />
        <el-button @click="onSearch">查询</el-button>
        <el-switch
          v-model="onlyNoMembers"
          active-text="仅看无成员"
          @change="onToggleOnlyNoMembers"
        />
        <div class="toolbar-right">
          <el-button type="primary" @click="openRegister">注册应用</el-button>
        </div>
      </div>

      <el-alert
        v-if="error && clients.length > 0"
        class="list-alert"
        type="error"
        :title="error"
        show-icon
        :closable="false"
      >
        <template #default>
          <el-button size="small" @click="loadClients">重试</el-button>
        </template>
      </el-alert>

      <el-table v-loading="loading" :data="clients" class="client-table" stripe>
        <el-table-column label="应用名" min-width="150">
          <template #default="{ row }">{{ orDash(row.clientName) }}</template>
        </el-table-column>
        <el-table-column label="clientId" min-width="190">
          <template #default="{ row }">
            <span class="mono">{{ row.clientId }}</span>
            <el-button link type="primary" size="small" @click="copy(row.clientId, 'clientId 已复制')">
              复制
            </el-button>
          </template>
        </el-table-column>
        <el-table-column label="类型" min-width="150">
          <template #default="{ row }">
            <el-tag size="small" :type="row.isPublic ? 'primary' : 'warning'">
              {{ row.isPublic ? '公共客户端(PKCE)' : '机密客户端' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="scope" min-width="200">
          <template #default="{ row }">
            <span :title="row.scopes.join(', ')" class="mono">{{ scopeText(row.scopes) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="成员数" width="130">
          <template #default="{ row }">
            <el-tag v-if="row.memberCount > 0" size="small" type="success">
              {{ row.memberCount }} 人
            </el-tag>
            <el-tooltip v-else content="非成员员工在门户看不到该应用" placement="top">
              <el-tag size="small" type="warning">0 人，待补员</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="描述" min-width="160" show-overflow-tooltip>
          <template #default="{ row }">{{ orDash(row.description) }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">查看详情</el-button>
            <el-tooltip
              :disabled="!row.isPublic"
              content="公共客户端无密钥"
              placement="top"
            >
              <span class="rotate-wrap">
                <el-button link type="warning" :disabled="row.isPublic" @click="rotate(row)">
                  轮换密钥
                </el-button>
              </span>
            </el-tooltip>
          </template>
        </el-table-column>

        <template #empty>
          <div v-if="error" class="empty-block">
            <p class="empty-text">{{ error }}</p>
            <el-button size="small" @click="loadClients">重试</el-button>
          </div>
          <el-empty v-else description="还没有注册任何应用" />
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          background
          @current-change="onPageChange"
          @size-change="onPageSizeChange"
        />
      </div>
    </section>

    <!-- 注册应用 -->
    <el-drawer v-model="registerOpen" title="注册应用" size="520px" :close-on-click-modal="false">
      <el-alert
        v-if="formError"
        class="form-alert"
        type="error"
        :title="formError"
        show-icon
        :closable="false"
      />

      <el-form ref="formRef" :model="form" :rules="rules" label-width="96px" class="client-form">
        <el-form-item label="应用名称" prop="clientName">
          <el-input v-model="form.clientName" placeholder="如：报表系统" maxlength="128" />
        </el-form-item>

        <el-form-item label="描述" prop="description">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            placeholder="选填，简要说明应用用途"
            maxlength="255"
            show-word-limit
          />
        </el-form-item>

        <el-form-item label="Logo">
          <div class="logo-field">
            <div class="logo-upload-row">
              <img v-if="form.logoPreview" :src="form.logoPreview" class="logo-preview" alt="应用 Logo" />
              <div class="logo-upload-actions">
                <ImageUpload
                  scene="app_logo"
                  :confirm="confirmLogo"
                  @success-file="onLogoFile"
                  @error="onLogoError"
                />
                <el-button v-if="form.logoFileId" link type="danger" @click="removeLogo">移除</el-button>
              </div>
            </div>
            <p class="field-hint">上传后先暂存文件，创建应用成功后自动绑定到该应用</p>
          </div>
        </el-form-item>

        <el-form-item label="类型">
          <el-radio-group v-model="form.isPublic">
            <el-radio :value="true">公共客户端（PKCE）</el-radio>
            <el-radio :value="false">机密客户端</el-radio>
          </el-radio-group>
          <p class="field-hint">公共客户端无密钥，适用于 SPA / 移动端；机密客户端创建后返回一次性密钥</p>
        </el-form-item>

        <el-form-item label="回调地址" prop="redirectUris">
          <div class="uri-list">
            <div v-for="(_, index) in form.redirectUris" :key="index" class="uri-row">
              <el-input
                v-model="form.redirectUris[index]"
                placeholder="http://localhost:7070/callback"
              />
              <el-button link type="danger" @click="removeRedirectUri(index)">删除</el-button>
            </div>
            <el-button link type="primary" @click="addRedirectUri">+ 添加回调地址</el-button>
            <p class="field-hint">
              登录成功后 IdP 会把用户连同一次性授权码（code）重定向回此地址；必须与接入方配置完全一致，不支持通配符
            </p>
          </div>
        </el-form-item>

        <el-form-item label="授权类型">
          <el-select v-model="form.grantTypes" multiple style="width: 100%">
            <el-option label="authorization_code" value="authorization_code" />
            <el-option label="refresh_token" value="refresh_token" />
          </el-select>
          <p class="field-hint">
            authorization_code=授权码模式：登录后 IdP 把一次性 code 重定向到回调地址，再由后端用 code 换令牌；
            refresh_token=允许长期续期令牌（需同时勾选 offline_access）
          </p>
        </el-form-item>

        <el-form-item label="scope">
          <el-select v-model="form.scopes" multiple style="width: 100%">
            <el-option label="openid" value="openid" />
            <el-option label="profile" value="profile" />
            <el-option label="email" value="email" />
            <el-option label="offline_access" value="offline_access" />
          </el-select>
          <p class="field-hint">
            决定本次授权能拿到哪些信息：openid=身份标识，profile=基础资料，email=邮箱，offline_access=允许刷新令牌
            （勾选 refresh_token 时需同时勾选 offline_access）
          </p>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="registerOpen = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitRegister">创建</el-button>
      </template>
    </el-drawer>

    <!-- 密钥一次性展示 -->
    <el-dialog
      v-model="secretVisible"
      title="应用密钥（仅显示一次）"
      width="520px"
      :close-on-click-modal="false"
      @closed="clearSecret"
    >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="关闭后无法再次查看，请立即保存到接入方配置"
      />
      <p v-if="secret.source === 'rotate' && secret.expiresAt === null" class="secret-tip">
        旧密钥已立即失效，请尽快更新接入方配置。
      </p>

      <div class="secret-row">
        <span class="secret-label">clientId</span>
        <div class="secret-value">
          <span class="mono">{{ secret.clientId }}</span>
          <el-button link type="primary" size="small" @click="copy(secret.clientId, 'clientId 已复制')">
            复制
          </el-button>
        </div>
      </div>

      <div class="secret-row">
        <span class="secret-label">clientSecret</span>
        <div class="secret-value">
          <span class="mono secret-text">
            {{ secretRevealed ? secret.clientSecret : '•'.repeat(Math.min(secret.clientSecret.length, 32)) }}
          </span>
          <el-button link type="primary" size="small" @click="secretRevealed = !secretRevealed">
            {{ secretRevealed ? '隐藏' : '显示' }}
          </el-button>
          <el-button link type="primary" size="small" @click="copy(secret.clientSecret, '密钥已复制')">
            复制
          </el-button>
        </div>
      </div>

      <template #footer>
        <el-button type="primary" @click="secretVisible = false">我已保存</el-button>
      </template>
    </el-dialog>
  </AdminLayout>
</template>

<style scoped>
.page-head {
  margin-bottom: 16px;
}

.panel {
  padding: 16px;
  background: #fff;
  border: 1px solid var(--app-border);
  border-radius: 10px;
}

.toolbar-right {
  margin-left: auto;
}

.list-alert {
  margin-top: 12px;
}

.client-table {
  margin-top: 12px;
  width: 100%;
}

.empty-block {
  padding: 16px 0;
}

.empty-text {
  margin: 0 0 10px;
  font-size: 13px;
  color: #d14343;
}

.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 14px;
}

.rotate-wrap {
  display: inline-block;
  margin-left: 8px;
}

.form-alert {
  margin-bottom: 14px;
}

.client-form :deep(.el-select),
.client-form :deep(.el-input) {
  width: 100%;
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

.logo-field {
  width: 100%;
}

.logo-upload-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logo-preview {
  width: 48px;
  height: 48px;
  border-radius: 8px;
  object-fit: cover;
  border: 1px solid var(--app-border);
}

.logo-upload-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.uri-block {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.uri-line {
  margin: 0;
  word-break: break-all;
}

.tag-gap {
  margin-right: 6px;
}

.link {
  color: var(--el-color-primary);
  word-break: break-all;
}

.detail-tip {
  margin-top: 16px;
  font-size: 12px;
  color: var(--app-text-secondary);
}

.secret-tip {
  margin: 12px 0 0;
  font-size: 13px;
  color: #b26a00;
}

.secret-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-top: 16px;
}

.secret-label {
  width: 96px;
  flex-shrink: 0;
  font-size: 13px;
  color: var(--app-text-secondary);
  line-height: 28px;
}

.secret-value {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.secret-text {
  word-break: break-all;
  padding: 4px 8px;
  background: #f7f8fa;
  border: 1px solid var(--app-border);
  border-radius: 6px;
}
</style>
