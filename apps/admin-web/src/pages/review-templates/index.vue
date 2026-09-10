<template>
  <div class="review-templates">
    <!-- 上传 / 版本替换入口 -->
    <div class="page-card">
      <div class="page-card-title">{{ replaceTarget ? `版本替换：${replaceTarget.name}` : '上传空白复查报告模板' }}</div>
      <el-form label-width="90px" class="upload-form">
        <el-form-item label="模板名称" v-if="!replaceTarget">
          <el-input v-model="name" placeholder="如：XX医院脊柱侧弯复查报告模板" maxlength="128" style="width: 360px" />
        </el-form-item>
        <el-form-item label="模板文件" required>
          <el-upload
            drag
            :auto-upload="false"
            :show-file-list="false"
            :on-change="onFileChange"
            accept=".pdf,.jpg,.jpeg,.png,.doc,.docx,.xlsx,.pptx,.zip"
            class="file-upload"
          >
            <div v-if="selectedFile">
              <div class="upload-file-name">{{ selectedFile.name }}</div>
              <div class="upload-file-size">{{ formatSize(selectedFile.size) }}</div>
            </div>
            <template v-else>
              <div class="upload-placeholder">拖拽文件到此处，或<em>选择文件</em></div>
            </template>
          </el-upload>
          <div class="file-hint">
            支持 PDF / JPG / PNG / DOC / DOCX / XLSX / PPTX / ZIP，单个文件 ≤ 20MB
            <el-tag v-if="uploadedFileId" type="success" size="small" style="margin-left: 8px">已上传</el-tag>
          </div>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="submit">
            {{ replaceTarget ? '确认替换（旧版本将标记 retired）' : '上传模板' }}
          </el-button>
          <el-button v-if="replaceTarget" @click="cancelReplace">取消替换</el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 模板列表 -->
    <div class="page-card">
      <div class="page-card-title">复查报告模板列表（{{ templates.length }}）</div>
      <el-table :data="templates" size="small" v-loading="loading">
        <el-table-column prop="name" label="名称" min-width="200" show-overflow-tooltip />
        <el-table-column label="版本" width="90">
          <template #default="{ row }">v{{ row.version }}</template>
        </el-table-column>
        <el-table-column prop="uploadedAt" label="上传时间" width="120" />
        <el-table-column prop="uploadedBy" label="上传人" width="120" />
        <el-table-column label="文件" min-width="160">
          <template #default="{ row }">{{ row.fileName || row.fileId }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button v-if="row.downloadUrl" type="primary" link size="small" @click="downloadTemplate(row)">
              下载
            </el-button>
            <el-button type="warning" link size="small" @click="startReplace(row)">版本替换</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && templates.length === 0" description="暂无模板，请先上传" :image-size="60" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '../../stores/auth'
import {
  fetchReviewTemplates,
  createReviewTemplateApi,
  replaceReviewTemplateApi,
  presignFile,
  uploadFileDirect,
  completeUpload,
} from '../../api'
import { validateReviewReportFile, checkReviewReportFileSize } from '../../utils/review-report-whitelist'
import type { ReviewTemplate } from '@bracesync/shared-types'

const auth = useAuthStore()
const templates = ref<ReviewTemplate[]>([])
const loading = ref(false)

// 上传状态
const name = ref('')
const selectedFile = ref<File | null>(null)
const uploadedFileId = ref('')
const submitting = ref(false)
const replaceTarget = ref<ReviewTemplate | null>(null)

const canSubmit = computed(() => {
  if (replaceTarget.value) return !!uploadedFileId.value
  return name.value.trim() !== '' && !!uploadedFileId.value
})

function formatSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${bytes} B`
}

function validateFile(file: File): boolean {
  // R4-a 白名单（扩展名 + MIME） + R4-b 20MB 上限；后端为权威，此处为体验层快反馈
  const extResult = validateReviewReportFile(file)
  if (!extResult.ok) {
    ElMessage.error(extResult.message || '不支持的文件类型')
    return false
  }
  const sizeResult = checkReviewReportFileSize(file)
  if (!sizeResult.ok) {
    ElMessage.error(sizeResult.message || '文件大小超过 20MB')
    return false
  }
  return true
}

async function onFileChange(file: { raw: File }) {
  const f = file.raw
  if (!validateFile(f)) {
    selectedFile.value = null
    uploadedFileId.value = ''
    return
  }
  selectedFile.value = f
  uploadedFileId.value = ''
  await uploadTemplateFile(f)
}

/** 走 file-service 预签名通道上传模板文件（owner_type=ReviewTemplate 区分，复用 review_report 白名单/大小校验） */
async function uploadTemplateFile(file: File) {
  try {
    // T130 增补单：读取文件头前 8 字节（魔数指纹），base64 编码后随 presign 请求发送
    const headerBuffer = await file.slice(0, 8).arrayBuffer()
    const fileHeader = btoa(String.fromCharCode(...new Uint8Array(headerBuffer)))
    const presign = await presignFile({
      fileName: file.name,
      contentType: file.type,
      fileType: 'review_report',
      ownerType: 'ReviewTemplate',
      ownerId: auth.user?.adminId || 'admin',
      fileHeader,
    })
    await uploadFileDirect(presign.uploadUrl, file, file.type)
    const result = await completeUpload(presign.fileId)
    uploadedFileId.value = result.fileId
    ElMessage.success('文件上传成功')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '文件上传失败')
    selectedFile.value = null
    uploadedFileId.value = ''
  }
}

async function submit() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    if (replaceTarget.value) {
      await replaceReviewTemplateApi(replaceTarget.value.groupId, uploadedFileId.value)
      ElMessage.success('模板版本替换成功，旧版本已标记 retired')
      cancelReplace()
    } else {
      const created = await createReviewTemplateApi({ name: name.value.trim(), fileId: uploadedFileId.value })
      ElMessage.success(`模板「${created.name}」上传成功`)
      name.value = ''
      selectedFile.value = null
      uploadedFileId.value = ''
    }
    await loadTemplates()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '提交失败')
  } finally {
    submitting.value = false
  }
}

function startReplace(row: ReviewTemplate) {
  replaceTarget.value = row
  selectedFile.value = null
  uploadedFileId.value = ''
}

function cancelReplace() {
  replaceTarget.value = null
  selectedFile.value = null
  uploadedFileId.value = ''
}

function downloadTemplate(row: ReviewTemplate) {
  if (row.downloadUrl) {
    window.open(row.downloadUrl, '_blank')
  }
}

async function loadTemplates() {
  loading.value = true
  try {
    templates.value = await fetchReviewTemplates()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载模板列表失败')
  } finally {
    loading.value = false
  }
}

onMounted(loadTemplates)
</script>

<style scoped>
.review-templates {
  padding: 16px;
}
.page-card {
  background: #fff;
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 16px;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.04);
}
.page-card-title {
  font-size: 15px;
  font-weight: 600;
  margin-bottom: 12px;
}
.upload-form {
  max-width: 620px;
}
.file-upload {
  width: 100%;
}
.upload-placeholder {
  color: #909399;
  font-size: 14px;
}
.upload-file-name {
  font-size: 14px;
  color: #333;
  word-break: break-all;
}
.upload-file-size {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
.file-hint {
  margin-top: 8px;
  color: #909399;
  font-size: 12px;
}
</style>