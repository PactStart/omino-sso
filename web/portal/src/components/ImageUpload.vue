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
    /** 直传完成后回写业务字段，返回最终 url */
    confirm: (key: string, format: string, size: number) => Promise<{ url: string }>
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
const cropOffsetX = ref(0)
const cropOffsetY = ref(0)
const imgEl = ref<HTMLImageElement>()
let pending: File | null = null
let pendingFormat = ''

function formatOf(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  return ext === 'jpeg' ? 'jpg' : ext === 'jpg' || ext === 'png' || ext === 'webp' ? ext : ''
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
    cropOffsetX.value = 0
    cropOffsetY.value = 0
    cropping.value = true
  } else {
    void upload(file, fmt)
  }
}

async function upload(blob: Blob, format: string) {
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
    const { url } = await props.confirm(cred.key, format, blob.size)
    emit('success', url)
  } catch (err) {
    const msg = err instanceof Error ? err.message : '上传失败，请重试'
    errorText.value = msg
    emit('error', msg)
  } finally {
    uploading.value = false
    emit('uploading', false)
  }
}

/* ---------- 裁剪（Canvas） ---------- */

const boxSize = computed(() => 200) // 裁剪框展示尺寸（px）

async function confirmCrop() {
  if (!pending || !imgEl.value) return
  const img = imgEl.value
  const nw = img.naturalWidth
  const nh = img.naturalHeight
  // 展示区尺寸（保持图片比例，最大 360 宽）
  const dispW = Math.min(360, nw)
  const dispH = (nh * dispW) / nw
  const size = (boxSize.value * dispW) / dispW // 裁剪框 = 边长 200 的方形
  const ratio = nw / dispW
  const sx = (-cropOffsetX.value) * ratio
  const sy = (-cropOffsetY.value) * ratio
  const sw = size * ratio
  const sh = size * ratio

  const canvas = document.createElement('canvas')
  canvas.width = 300
  canvas.height = Math.round(300 / props.cropAspectRatio)
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.drawImage(img, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height)
  const blob: Blob = await new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('裁剪失败'))), 'image/' + pendingFormat, 0.9),
  )
  cropping.value = false
  pending = null
  await upload(blob, pendingFormat)
}

function cancelCrop() {
  cropping.value = false
  pending = null
}

onBeforeUnmount(() => {
  if (cropSrc.value) URL.revokeObjectURL(cropSrc.value)
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
          <img ref="imgEl" :src="cropSrc" class="crop-img" alt="裁剪预览" />
          <div
            class="crop-box"
            :style="{
              width: boxSize + 'px',
              height: boxSize / cropAspectRatio + 'px',
              transform: `translate(${cropOffsetX}px, ${cropOffsetY}px)`,
            }"
          ></div>
        </div>
        <div class="crop-controls">
          <label>缩放 <input v-model.number="cropScale" type="range" min="50" max="300" /></label>
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
  top: 50%;
  left: 50%;
  margin-top: -100px;
  margin-left: -100px;
  border: 2px dashed #2563eb;
  box-shadow: 0 0 0 9999px rgba(0, 0, 0, 0.35);
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
