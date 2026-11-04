<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { confirmPermissionImport, validatePermissionImport } from '@/api'
import { errorCode, errorMessage } from '@/api/client'
import type {
  PermissionImportDocument,
  PermissionImportPreview,
  PermissionImportResult,
} from '@/api/types'
import { readJsonFile } from '@/utils/file'
import { downloadPermissionImportExample } from '@/utils/permission-import-example'

/**
 * 权限点批量导入（011-app-permission-import）。
 *
 * 职责边界（FR-028）：前端只做「是否为空 / 是否为合法 JSON / 根是否为对象」的基础检查，
 * 编码规则、层级、重复、超量等语义结论一律以服务端校验预览为准。
 * 失败后必须保留已输入内容（FR-030），不清空编辑器。
 */
const props = defineProps<{
  modelValue: boolean
  clientId: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'imported', result: PermissionImportResult): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const mode = ref<'paste' | 'file'>('paste')
const text = ref('')
const fileName = ref('')
const localError = ref('')
const actionError = ref('')
const needRevalidate = ref(false)
const preview = ref<PermissionImportPreview | null>(null)
const previewing = ref(false)
const confirming = ref(false)
const fileInput = ref<HTMLInputElement>()

/**
 * 防御后端数组字段返回 null（permission-import-preview-frozen）：模板直接对 null 取 `.length`
 * 会在渲染期抛 TypeError，Vue 会中止本次 patch，整个弹窗冻结（loading 不复位、按钮状态不更新）。
 * 后端已归一为 []，这里是渲染侧的兜底。
 */
const movedItems = computed(() => preview.value?.movedItems ?? [])

watch(visible, (open) => {
  if (open) reset()
  else if (previewing.value || confirming.value) {
    // 请求未返回时不允许关闭（避免半截状态）
    visible.value = true
  }
})

function reset() {
  mode.value = 'paste'
  text.value = ''
  fileName.value = ''
  localError.value = ''
  actionError.value = ''
  needRevalidate.value = false
  preview.value = null
}

/** 来源只用于审计统计；由输入方式决定，用户不可伪造。 */
function source(): 'paste' | 'file' {
  return mode.value === 'file' && fileName.value ? 'file' : 'paste'
}

function downloadExample() {
  downloadPermissionImportExample()
}

function pickFile() {
  mode.value = 'file'
  fileInput.value?.click()
}

async function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  localError.value = ''
  actionError.value = ''
  preview.value = null
  try {
    text.value = await readJsonFile(file)
    fileName.value = file.name
  } catch (e) {
    fileName.value = ''
    text.value = ''
    localError.value = e instanceof Error ? e.message : '文件读取失败'
  } finally {
    input.value = ''
  }
}

/** 仅基础检查：空 → 非法 JSON → 根不是对象。语义问题交给服务端。 */
function parseDoc(): PermissionImportDocument | null {
  const raw = text.value.trim()
  if (!raw) {
    localError.value = '内容为空，请粘贴或选择一个 JSON 文件'
    return null
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    localError.value = '内容不是合法的 JSON（请检查逗号、引号与括号）'
    return null
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    localError.value = '文档根必须是 JSON 对象（形如 {"version":1,"permissions":[...]}）'
    return null
  }
  return parsed as PermissionImportDocument
}

function handleFailure(e: unknown, autoPreview: boolean) {
  const code = errorCode(e)
  if (code === 'APP_RESOURCE_STALE') {
    preview.value = null
    if (autoPreview) {
      void runPreview(true)
      actionError.value = '该应用已被他人修改，请刷新后重试（已自动重新校验）'
      return
    }
    actionError.value = '该应用已被他人修改，请刷新后重试'
    return
  }
  if (code === 'APP_PERMISSION_IMPORT_TOO_LARGE') {
    actionError.value = '导入内容超过上限（单次最多 1000 条且不超过 512KB），请裁剪后重新校验'
    needRevalidate.value = true
    return
  }
  if (code === 'APP_PERMISSION_IMPORT_INVALID') {
    actionError.value = '导入内容不合法，请先校验预览并按提示修改'
    return
  }
  actionError.value = errorMessage(e)
}

/** keepError=true 时保留调用方刚写入的提示（409 自动重跑预览场景）。 */
async function runPreview(keepError = false) {
  if (previewing.value) return
  localError.value = ''
  if (!keepError) actionError.value = ''
  needRevalidate.value = false
  const doc = parseDoc()
  if (!doc) {
    preview.value = null
    return
  }
  previewing.value = true
  try {
    preview.value = await validatePermissionImport(props.clientId, doc, source())
  } catch (e) {
    preview.value = null
    handleFailure(e, false)
  } finally {
    previewing.value = false
  }
}

async function confirmImport() {
  if (!preview.value?.ok || confirming.value) return
  const doc = parseDoc()
  if (!doc) return
  const snapshot = preview.value
  confirming.value = true
  actionError.value = ''
  try {
    const result = await confirmPermissionImport(props.clientId, doc, source())
    if (result.added !== snapshot.added || result.updated !== snapshot.updated) {
      ElMessage.warning('数据在预览后有变化，已按最新状态写入')
    }
    ElMessage.success(
      `导入完成：新增 ${result.added} 条、更新 ${result.updated} 条、无变化 ${result.unchanged} 条`,
    )
    emit('imported', result)
    visible.value = false
  } catch (e) {
    // 409 时自动重跑一次预览（T023）；输入内容保留
    handleFailure(e, true)
  } finally {
    confirming.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    title="批量导入权限点"
    width="780px"
    :close-on-click-modal="false"
    class="permission-import"
  >
    <div class="toolbar">
      <el-button link type="primary" @click="downloadExample">下载示例文档</el-button>
      <span class="hint">示例含 3 层 5 条，可直接改内容后粘贴回来</span>
    </div>

    <el-alert
      v-if="localError"
      type="error"
      :title="localError"
      show-icon
      :closable="false"
      class="block-alert"
    />
    <el-alert
      v-if="actionError"
      type="error"
      :title="actionError"
      show-icon
      :closable="false"
      class="block-alert"
    >
      <template v-if="needRevalidate" #default>
        <el-button size="small" @click="runPreview()">重新校验</el-button>
      </template>
    </el-alert>

    <el-tabs v-model="mode">
      <el-tab-pane label="粘贴 JSON" name="paste">
        <textarea
          v-model="text"
          class="json-input"
          rows="12"
          spellcheck="false"
          placeholder='{"version":1,"mode":"merge","permissions":[{"code":"order","name":"订单","children":[]}]}'
        />
      </el-tab-pane>
      <el-tab-pane label="选择文件" name="file">
        <input
          ref="fileInput"
          type="file"
          accept="application/json,.json"
          class="hidden-input"
          @change="onFileChange"
        />
        <div class="file-row">
          <el-button @click="pickFile">选择 .json 文件</el-button>
          <span class="hint">{{ fileName || '未选择文件（上限 512KB）' }}</span>
        </div>
        <textarea
          v-model="text"
          class="json-input"
          rows="10"
          spellcheck="false"
          placeholder="选择文件后内容会显示在这里，也可以直接在这里修改"
        />
      </el-tab-pane>
    </el-tabs>

    <el-collapse class="field-guide">
      <el-collapse-item title="字段说明与「表格行 → 嵌套 JSON」对照" name="guide">
        <ul class="guide-list">
          <li><code>version</code>：固定 1；<code>mode</code>：固定 merge（暂不支持 replace）。</li>
          <li><code>code</code>：应用内唯一，只允许字母、数字与 <code>_.:-</code>，1–64 字符。</li>
          <li><code>name</code>：必填，≤64 字节；<code>description</code>：可选，≤255 字节。</li>
          <li><code>sort</code>：可选，缺省按同级书写顺序；<code>children</code>：子权限点数组。</li>
          <li>层级只用嵌套表达，不要写 <code>id</code> / <code>parentId</code>（会被判为未知字段）。</li>
        </ul>
        <pre class="guide-code">表格行                    嵌套 JSON
order              | 订单      →  {"code":"order","name":"订单","children":[...]}
order.write        | 创建订单  →  {"code":"order.write","name":"创建订单","children":[...]}   // 放进 order.children
order.write.refund | 退款      →  {"code":"order.write.refund","name":"退款"}              // 放进 order.write.children</pre>
      </el-collapse-item>
    </el-collapse>

    <div class="actions">
      <el-button type="primary" :loading="previewing" @click="runPreview()">校验预览</el-button>
      <el-button
        type="success"
        :disabled="!preview?.ok"
        :loading="confirming"
        @click="confirmImport"
      >
        确认导入
      </el-button>
      <span v-if="preview && !preview.ok" class="hint">校验未通过，确认导入不可用</span>
    </div>

    <section v-if="preview" class="preview">
      <h3 class="preview-title">
        {{ preview.ok ? '校验通过' : '校验未通过' }}
        <span class="mono">共 {{ preview.total }} 条</span>
      </h3>

      <div class="counts">
        <span class="count added">将新增 {{ preview.added }}</span>
        <span class="count updated">将更新 {{ preview.updated }}</span>
        <span class="count unchanged">无变化 {{ preview.unchanged }}</span>
        <span class="count moved">上级将变更 {{ preview.moved }}</span>
      </div>

      <div v-if="preview.issues.length > 0" class="issue-block">
        <h4>问题清单（{{ preview.issueTotal }} 条）</h4>
        <ul class="issue-list">
          <li v-for="(issue, idx) in preview.issues" :key="`${issue.path}-${idx}`">
            <span class="mono path">{{ issue.path || '（文档）' }}</span>
            <span>{{ issue.message }}</span>
          </li>
        </ul>
        <p v-if="preview.issueTotal > preview.issues.length" class="hint">
          共 {{ preview.issueTotal }} 条，仅列出前 {{ preview.issues.length }} 条
        </p>
      </div>

      <div v-if="preview.warnings.length > 0" class="warning-block">
        <h4>提示（不阻断）</h4>
        <ul class="issue-list">
          <li v-for="(warn, idx) in preview.warnings" :key="`w-${warn.path}-${idx}`">
            <span class="mono path">{{ warn.path || '（文档）' }}</span>
            <span>{{ warn.message }}</span>
          </li>
        </ul>
        <p v-if="preview.warningTotal > preview.warnings.length" class="hint">
          共 {{ preview.warningTotal }} 条，仅列出前 {{ preview.warnings.length }} 条
        </p>
      </div>

      <div v-if="movedItems.length > 0" class="moved-block">
        <h4>上级将变更的权限点</h4>
        <el-table :data="movedItems" size="small" border>
          <el-table-column label="权限编码" min-width="160">
            <template #default="{ row }">
              <span class="mono">{{ row.code }}</span>
            </template>
          </el-table-column>
          <el-table-column label="原上级" min-width="120">
            <template #default="{ row }">
              <span class="mono">{{ row.fromParent || '根级' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="新上级" min-width="120">
            <template #default="{ row }">
              <span class="mono">{{ row.toParent || '根级' }}</span>
            </template>
          </el-table-column>
        </el-table>
        <p v-if="preview.moved > movedItems.length" class="hint">
          共 {{ preview.moved }} 条，仅列出前 {{ movedItems.length }} 条
        </p>
      </div>
    </section>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.hint {
  font-size: 12px;
  color: var(--app-text-secondary);
}

.block-alert {
  margin-bottom: 10px;
}

.json-input {
  width: 100%;
  box-sizing: border-box;
  padding: 10px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.6;
  color: var(--app-text);
  background: #fafafa;
  border: 1px solid var(--app-border);
  border-radius: 6px;
  resize: vertical;
}

.hidden-input {
  display: none;
}

.file-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.field-guide {
  margin-top: 8px;
}

.guide-list {
  margin: 0;
  padding-left: 18px;
  font-size: 12px;
  line-height: 1.8;
  color: var(--app-text-secondary);
}

.guide-code {
  margin: 8px 0 0;
  padding: 10px;
  font-size: 12px;
  line-height: 1.7;
  background: #f5f7fa;
  border-radius: 6px;
  overflow-x: auto;
}

.actions {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 14px 0;
}

.preview {
  border-top: 1px solid var(--app-border);
  padding-top: 12px;
}

.preview-title {
  margin: 0 0 8px;
  font-size: 14px;
  font-weight: 600;
}

.counts {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}

.count {
  padding: 2px 8px;
  font-size: 12px;
  border-radius: 4px;
  background: #f5f7fa;
}

.count.added {
  color: #1a7f37;
}

.count.updated {
  color: #9a6700;
}

.count.unchanged {
  color: var(--app-text-secondary);
}

.count.moved {
  color: #b54708;
}

.issue-block h4,
.warning-block h4,
.moved-block h4 {
  margin: 10px 0 6px;
  font-size: 13px;
}

.issue-list {
  margin: 0;
  padding-left: 18px;
  font-size: 12px;
  line-height: 1.8;
}

.path {
  margin-right: 8px;
  color: var(--app-text-secondary);
}

.warning-block {
  margin-top: 6px;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
