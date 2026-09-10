<template>
  <div class="review-records">
    <div class="page-toolbar">
      <span class="toolbar-label">选择患者：</span>
      <el-select v-model="patientId" placeholder="选择患者" class="patient-select" @change="loadRecords">
        <el-option v-for="p in patients" :key="p.patientId" :label="`${p.name}（${p.patientId}）`" :value="p.patientId" />
      </el-select>
      <el-tag v-if="auth.role === 'doctor'" type="info" effect="plain">医生工作台：仅本团队患者</el-tag>
    </div>

    <template v-if="patientId">
      <!-- 上传复查报告 -->
      <div class="page-card">
        <div class="page-card-title">上传复查报告</div>
        <el-form :model="form" label-width="100px" class="review-form">
          <el-form-item label="复查日期" required>
            <el-date-picker v-model="form.reviewDate" type="date" value-format="YYYY-MM-DD" placeholder="选择复查日期" style="width: 200px" />
          </el-form-item>
          <el-form-item label="复查类型" required>
            <el-radio-group v-model="form.reviewType">
              <el-radio value="initial">初诊</el-radio>
              <el-radio value="follow-up">复诊</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="检查所见">
            <el-input v-model="form.findings" type="textarea" :rows="2" placeholder="检查所见/结论（可选）" />
          </el-form-item>
          <el-form-item label="下次复查">
            <el-date-picker v-model="form.nextReviewDate" type="date" value-format="YYYY-MM-DD" placeholder="选择下次复查日期（可选）" style="width: 200px" />
          </el-form-item>
          <el-form-item label="报告文件">
            <el-upload
              :auto-upload="false"
              :show-file-list="false"
              :on-change="onFileChange"
              accept=".pdf,.jpg,.jpeg,.png,.doc,.docx,.xlsx,.pptx,.zip"
            >
              <el-button :loading="uploading">
                {{ selectedFile ? selectedFile.name : '选择文件' }}
              </el-button>
            </el-upload>
            <span class="file-hint">仅支持 PDF / JPG / PNG</span>
            <el-tag v-if="uploadedFileId" type="success" size="small" style="margin-left: 8px">已上传</el-tag>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="submitReview">
              提交复查记录
            </el-button>
          </el-form-item>
        </el-form>
      </div>

      <!-- 历史复查记录 -->
      <div class="page-card">
        <div class="page-card-title">历史复查记录（{{ records.length }}）</div>
        <el-table :data="records" size="small" v-loading="loading">
          <el-table-column prop="reviewDate" label="复查日期" width="120" />
          <el-table-column label="类型" width="100">
            <template #default="{ row }">{{ row.reviewType === 'initial' ? '初诊' : '复诊' }}</template>
          </el-table-column>
          <el-table-column prop="findings" label="检查所见" min-width="180" show-overflow-tooltip />
          <el-table-column prop="nextReviewDate" label="下次复查" width="120" />
          <el-table-column label="报告文件" min-width="160">
            <template #default="{ row }">
              <span v-if="row.reportFileName">{{ row.reportFileName }}</span>
              <span v-else-if="row.reportFileId">无文件名</span>
              <span v-else>无</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100">
            <template #default="{ row }">
              <el-button v-if="row.reportDownloadUrl" type="primary" link size="small" @click="downloadReport(row)">
                下载
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!loading && records.length === 0" description="暂无复查记录" :image-size="60" />
      </div>
    </template>
    <el-empty v-else description="请选择患者开始上传复查报告" class="empty-placeholder" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../../stores/auth'
import {
  fetchPatients,
  fetchReviewRecords,
  createReviewRecordApi,
  presignFile,
  uploadFileDirect,
  completeUpload,
} from '../../api'
import type { Patient, ReviewRecord, CreateReviewRecordRequest } from '@bracesync/shared-types'

const auth = useAuthStore()
const patients = ref<Patient[]>([])
const patientId = ref('')
const records = ref<ReviewRecord[]>([])
const loading = ref(false)

// 表单
const form = ref<CreateReviewRecordRequest>({
  patientId: '',
  reviewDate: '',
  reviewType: 'follow-up',
})
const selectedFile = ref<File | null>(null)
const uploadedFileId = ref('')
const uploading = ref(false)
const submitting = ref(false)

const canSubmit = computed(() => form.value.reviewDate && form.value.reviewType && patientId.value)

// 白名单校验（R4-a 硬约束）
const ALLOWED_EXT = ['.pdf', '.jpg', '.jpeg', '.png']
const ALLOWED_MIME = ['application/pdf', 'image/jpeg', 'image/png']

function validateFile(file: File): boolean {
  const ext = file.name.substring(file.name.lastIndexOf('.')).toLowerCase()
  if (!ALLOWED_EXT.includes(ext)) {
    ElMessage.error(`不支持的文件类型：${ext}，仅支持 ${ALLOWED_EXT.join(' / ')}`)
    return false
  }
  if (!ALLOWED_MIME.includes(file.type)) {
    ElMessage.error(`不支持的 MIME 类型：${file.type}`)
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
  // 自动触发上传
  await uploadReport(f)
}

async function uploadReport(file: File) {
  uploading.value = true
  try {
    // T130 增补单：读取文件头前 8 字节（魔数指纹），base64 编码后随 presign 请求发送
    const headerBuffer = await file.slice(0, 8).arrayBuffer()
    const fileHeader = btoa(String.fromCharCode(...new Uint8Array(headerBuffer)))
    // 1. 申请预签名上传 URL
    const presign = await presignFile({
      fileName: file.name,
      contentType: file.type,
      fileType: 'review_report',
      ownerType: 'patient',
      ownerId: patientId.value,
      fileHeader,
    })
    // 2. 直传 COS
    await uploadFileDirect(presign.uploadUrl, file, file.type)
    // 3. 确认上传
    const result = await completeUpload(presign.fileId)
    uploadedFileId.value = result.fileId
    ElMessage.success('文件上传成功')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '文件上传失败')
    selectedFile.value = null
    uploadedFileId.value = ''
  } finally {
    uploading.value = false
  }
}

async function submitReview() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    const input: CreateReviewRecordRequest = {
      patientId: patientId.value,
      reviewDate: form.value.reviewDate,
      reviewType: form.value.reviewType,
      findings: form.value.findings,
      nextReviewDate: form.value.nextReviewDate,
      reportFileId: uploadedFileId.value || undefined,
    }
    await createReviewRecordApi(input)
    ElMessage.success('复查记录已提交')
    // 重置表单
    form.value = { patientId: patientId.value, reviewDate: '', reviewType: 'follow-up' }
    selectedFile.value = null
    uploadedFileId.value = ''
    await loadRecords()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '提交失败')
  } finally {
    submitting.value = false
  }
}

function downloadReport(row: ReviewRecord) {
  if (row.reportDownloadUrl) {
    window.open(row.reportDownloadUrl, '_blank')
  }
}

async function loadRecords() {
  if (!patientId.value) {
    records.value = []
    return
  }
  loading.value = true
  try {
    records.value = await fetchReviewRecords(patientId.value)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载复查记录失败')
  } finally {
    loading.value = false
  }
}

async function loadPatients() {
  try {
    const res = await fetchPatients({ page: 1, pageSize: 200 })
    patients.value = res.list
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载患者列表失败')
  }
}

onMounted(() => {
  loadPatients()
})
</script>

<style scoped>
.review-records {
  padding: 16px;
}
.page-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}
.patient-select {
  width: 280px;
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
.review-form {
  max-width: 640px;
}
.file-hint {
  margin-left: 8px;
  color: #909399;
  font-size: 12px;
}
.empty-placeholder {
  padding: 48px 0;
}
</style>
