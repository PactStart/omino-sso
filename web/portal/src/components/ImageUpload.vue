<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { getUploadCredential } from '@/api'

/**
 * 单文件图片上传组件（admin 与 portal 共用，行为完全一致，FR-016）。
 * 流程：选文件 → 校验 →（可选裁剪）→ 签发直传凭证 → 直传 OSS → 调用 confirm 回写业务字段 → 回显。
 * 裁剪为非强制能力，默认关闭（FR-018），由 enableCrop 控制。
 */
const props = withDefaults(
  defineProps<{
    scene: 'app_logo' | 'user_avatar'
    /** 直传完成后回写业务字段，返回最终 url 与（可选）文件记录 id */
    confirm: (key: string, format: string, size: number) => Promise<{ url: string; id?: string }>
    enableCrop?: boolean
    cropAspectRatio?: number
    accept?: string
    maxSize?: number
    disabled?: boolean
  }>(),
  {
    enableCrop: false,
    cropAspectRatio: 1,
    accept: 'image/jpeg,image/png,image/webp',
    maxSize: 5 * 1024 * 1024,
    disabled: false,
  },
)

const emit = defineEmits<{
  (e: 'success', url: string): void
  (e: 'success-file', url: string, fileId?: string): void
  (e: 'error', message: string): void
  (e: 'uploading', value: boolean): void
}>()

const inputRef = ref<HTMLInputElement>()
const uploading = ref(false)
const errorText = ref('')

// 裁剪态
const cropping = ref(false)
const cropSrc = ref('')
const cropScale = ref(100)
const cropBoxX = ref(0)
const cropBoxY = ref(0)
const cropBoxW = ref(200)
const cropError = ref('')
const imgEl = ref<HTMLImageElement>()
let pending: File | null = null
let pendingFormat = ''

function formatOf(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  return ext === 'jpeg' ? 'jpg' : ext === 'jpg' || ext === 'png' || ext === 'webp' ? ext : ''
}

function formatMime(format: string): string {
  return format === 'jpg' ? 'image/jpeg' : 'image/' + format
}

function pick() {
  if (props.disabled || uploading.value) return
  errorText.value = ''
  inputRef.value?.click()
}

function onFileChange(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (inputRef.value) inputRef.value.value = ''
  if (!file) return
  const fmt = formatOf(file.name)
  if (!fmt) {
    errorText.value = '仅支持 JPG / PNG / WebP 图片'
    emit('error', errorText.value)
    return
  }
  if (file.size < 1 || file.size > props.maxSize) {
    errorText.value = `图片大小须在 ${Math.floor(props.maxSize / 1024 / 1024)}MB 以内`
    emit('error', errorText.value)
    return
  }
  pending = file
  pendingFormat = fmt
  if (props.enableCrop) {
    cropSrc.value = URL.createObjectURL(file)
    cropScale.value = 100
    cropBoxX.value = 0
    cropBoxY.value = 0
    cropBoxW.value = 200
    cropping.value = true
  } else {
    void upload(file, fmt)
  }
}

async function upload(blob: Blob, format: string): Promise<boolean> {
  uploading.value = true
  emit('uploading', true)
  try {
    const cred = await getUploadCredential({ scene: props.scene, format, size: blob.size })
    const form = new FormData()
    form.append('key', cred.key)
    form.append('OSSAccessKeyId', cred.accessKeyId)
    form.append('x-oss-security-token', cred.securityToken)
    form.append('policy', cred.policy)
    form.append('signature', cred.signature)
    form.append('file', blob, cred.key.split('/').pop() || 'file')
    const resp = await fetch(cred.uploadUrl, { method: 'POST', body: form })
    if (!resp.ok) throw new Error('直传对象存储失败，请重试')
    const res = await props.confirm(cred.key, format, blob.size)
    emit('success', res.url)
    emit('success-file', res.url, res.id)
    return true
  } catch (err) {
    const msg = err instanceof Error ? err.message : '上传失败，请重试'
    errorText.value = msg
    emit('error', msg)
    return false
  } finally {
    uploading.value = false
    emit('uploading', false)
  }
}

/* ---------- 裁剪（Canvas） ---------- */

const boxH = computed(() => cropBoxW.value / props.cropAspectRatio)

function clamp(v: number, min: number, max: number) {
  return Math.min(Math.max(v, min), max)
}

// 图片加载完成后，把裁剪框初始化为居中
function onImgLoad() {
  const img = imgEl.value
  if (!img) return
  const w = Math.min(200, img.clientWidth)
  const h = w / props.cropAspectRatio
  cropBoxW.value = h > img.clientHeight ? img.clientHeight * props.cropAspectRatio : w
  cropBoxX.value = (img.clientWidth - cropBoxW.value) / 2
  cropBoxY.value = (img.clientHeight - boxH.value) / 2
  cropScale.value = 100
}

// 拖动裁剪框
let dragging = false
let dragStartX = 0
let dragStartY = 0
let dragBoxX = 0
let dragBoxY = 0

function onBoxMouseDown(e: MouseEvent) {
  dragging = true
  dragStartX = e.clientX
  dragStartY = e.clientY
  dragBoxX = cropBoxX.value
  dragBoxY = cropBoxY.value
  window.addEventListener('mousemove', onBoxMouseMove)
  window.addEventListener('mouseup', onBoxMouseUp)
}

function onBoxMouseMove(e: MouseEvent) {
  if (!dragging || !imgEl.value) return
  const img = imgEl.value
  cropBoxX.value = clamp(dragBoxX + (e.clientX - dragStartX), 0, img.clientWidth - cropBoxW.value)
  cropBoxY.value = clamp(dragBoxY + (e.clientY - dragStartY), 0, img.clientHeight - boxH.value)
}

function onBoxMouseUp() {
  dragging = false
  window.removeEventListener('mousemove', onBoxMouseMove)
  window.removeEventListener('mouseup', onBoxMouseUp)
}

// 缩放裁剪框（保持中心）
function onScaleChange() {
  if (!imgEl.value) return
  const img = imgEl.value
  const w = clamp((200 * cropScale.value) / 100, 40, img.clientWidth)
  const realW = w / props.cropAspectRatio > img.clientHeight ? img.clientHeight * props.cropAspectRatio : w
  const cx = cropBoxX.value + cropBoxW.value / 2
  const cy = cropBoxY.value + boxH.value / 2
  cropBoxW.value = realW
  cropBoxX.value = clamp(cx - realW / 2, 0, img.clientWidth - realW)
  cropBoxY.value = clamp(cy - boxH.value / 2, 0, img.clientHeight - boxH.value)
}

async function confirmCrop() {
  if (!pending || !imgEl.value) return
  cropError.value = ''
  const img = imgEl.value
  const ratio = img.naturalWidth / img.clientWidth
  const sx = cropBoxX.value * ratio
  const sy = cropBoxY.value * ratio
  const sw = cropBoxW.value * ratio
  const sh = boxH.value * ratio

  const canvas = document.createElement('canvas')
  canvas.width = 300
  canvas.height = Math.round(300 / props.cropAspectRatio)
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.drawImage(img, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height)
  const fmt = pendingFormat
  const blob: Blob = await new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('裁剪失败'))), formatMime(fmt), 0.9),
  )
  const ok = await upload(blob, fmt)
  if (ok) {
    cropping.value = false
    pending = null
  } else {
    cropError.value = errorText.value
  }
}

function cancelCrop() {
  cropping.value = false
  pending = null
}

onBeforeUnmount(() => {
  if (cropSrc.value) URL.revokeObjectURL(cropSrc.value)
  window.removeEventListener('mousemove', onBoxMouseMove)
  window.removeEventListener('mouseup', onBoxMouseUp)
})
</script>

<template>
  <div class="image-upload">
    <input ref="inputRef" type="file" :accept="accept" class="hidden-input" @change="onFileChange" />
    <button type="button" class="upload-btn" :disabled="disabled || uploading" @click="pick">
      {{ uploading ? '上传中…' : '选择图片' }}
    </button>
    <p v-if="errorText" class="error-text">{{ errorText }}</p>

    <div v-if="cropping" class="crop-mask" @click.self="cancelCrop">
      <div class="crop-panel">
        <div class="crop-stage">
          <img ref="imgEl" :src="cropSrc" class="crop-img" alt="裁剪预览" @load="onImgLoad" />
          <div
            class="crop-box"
            :style="{
              left: cropBoxX + 'px',
              top: cropBoxY + 'px',
              width: cropBoxW + 'px',
              height: boxH + 'px',
            }"
            @mousedown.prevent="onBoxMouseDown"
          ></div>
        </div>
        <p v-if="cropError" class="crop-error">{{ cropError }}</p>
        <div class="crop-controls">
          <label>缩放 <input v-model.number="cropScale" type="range" min="50" max="300" @input="onScaleChange" /></label>
          <button type="button" @click="confirmCrop">裁剪并上传</button>
          <button type="button" @click="cancelCrop">取消</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.image-upload {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.hidden-input {
  display: none;
}
.upload-btn {
  padding: 6px 14px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  background: #fff;
  cursor: pointer;
  font-size: 14px;
}
.upload-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.error-text {
  color: #dc2626;
  font-size: 12px;
  margin: 0;
}
.crop-error {
  color: #dc2626;
  font-size: 13px;
  margin: 8px 0 0;
}
.crop-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.crop-panel {
  background: #fff;
  border-radius: 8px;
  padding: 16px;
}
.crop-stage {
  position: relative;
  overflow: hidden;
  max-width: 360px;
}
.crop-img {
  display: block;
  max-width: 360px;
}
.crop-box {
  position: absolute;
  border: 2px dashed #2563eb;
  box-shadow: 0 0 0 9999px rgba(0, 0, 0, 0.35);
  cursor: move;
}
.crop-controls {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
}
.crop-controls button {
  padding: 6px 14px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  background: #fff;
  cursor: pointer;
}
</style>
