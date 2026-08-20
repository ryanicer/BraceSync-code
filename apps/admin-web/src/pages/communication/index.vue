<template>
  <div class="communication">
    <el-alert
      v-if="auth.role === 'cs'"
      title="客服角色：仅可查看反馈与标记处理状态（PRD §7D.11）"
      type="info"
      :closable="false"
      class="role-hint"
    />

    <div class="page-toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索反馈内容 / 患者ID"
        clearable
        class="search-input"
        @keyup.enter="loadData"
        @clear="loadData"
      />
      <el-button type="primary" @click="loadData">查询</el-button>
    </div>

    <div class="page-card">
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column prop="feedbackId" label="反馈ID" width="100" />
        <el-table-column label="患者" width="110">
          <template #default="{ row }">{{ patientNameOf(row.patientId) }}</template>
        </el-table-column>
        <el-table-column prop="type" label="类型" width="100" />
        <el-table-column prop="content" label="反馈内容" min-width="280" show-overflow-tooltip />
        <el-table-column label="提交时间" width="140">
          <template #default="{ row }">{{ formatTime(row.submitTime) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="处理人" width="100">
          <template #default="{ row }">{{ row.handler || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="viewDetail(row)">详情</el-button>
            <el-button
              v-if="row.status !== 'resolved'"
              size="small"
              link
              type="success"
              @click="markResolved(row)"
            >标记已处理</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 反馈详情/回复对话框 -->
    <el-dialog v-model="detailVisible" :title="current ? `反馈 ${current.feedbackId}` : ''" width="480px">
      <template v-if="current">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="患者">{{ patientNameOf(current.patientId) }}</el-descriptions-item>
          <el-descriptions-item label="类型">{{ current.type }}</el-descriptions-item>
          <el-descriptions-item label="内容">{{ current.content }}</el-descriptions-item>
          <el-descriptions-item label="提交时间">{{ current.submitTime }}</el-descriptions-item>
          <el-descriptions-item v-if="current.replyContent" label="回复">{{ current.replyContent }}</el-descriptions-item>
        </el-descriptions>
        <template v-if="current.status === 'pending'">
          <el-input v-model="replyText" type="textarea" :rows="3" placeholder="回复内容（协调微信客服后填写）" class="reply-input" />
          <el-button type="primary" :loading="replying" @click="submitReply">回复并标记</el-button>
        </template>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Feedback } from '@bracesync/shared-types'
import { fetchFeedbacks, processFeedbackApi, patientNameOf } from '../../api'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const list = ref<Feedback[]>([])
const keyword = ref('')
const loading = ref(false)
const detailVisible = ref(false)
const current = ref<Feedback | null>(null)
const replyText = ref('')
const replying = ref(false)

function statusLabel(status: Feedback['status']): string {
  const map: Record<Feedback['status'], string> = { pending: '待处理', replied: '已回复', resolved: '已解决' }
  return map[status] ?? status
}

function statusTagType(status: Feedback['status']): 'warning' | 'primary' | 'success' {
  if (status === 'pending') return 'warning'
  if (status === 'replied') return 'primary'
  return 'success'
}

function formatTime(iso: string): string {
  return `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
}

async function loadData() {
  loading.value = true
  try {
    list.value = await fetchFeedbacks({ keyword: keyword.value || undefined })
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function viewDetail(row: Feedback) {
  current.value = row
  replyText.value = ''
  detailVisible.value = true
}

async function submitReply() {
  if (!current.value) return
  replying.value = true
  try {
    await processFeedbackApi(current.value.feedbackId, replyText.value)
    current.value.replyContent = replyText.value || null
    current.value.replyTime = new Date().toISOString()
    current.value.status = 'replied'
    current.value.handler = auth.user?.name ?? null
    ElMessage.success('回复成功')
    detailVisible.value = false
    loadData()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '回复失败')
  } finally {
    replying.value = false
  }
}

async function markResolved(row: Feedback) {
  try {
    await processFeedbackApi(row.feedbackId)
    row.status = 'resolved'
    row.handler = row.handler || auth.user?.name || null
    ElMessage.success('已标记处理')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  }
}

onMounted(loadData)
</script>

<style scoped>
.role-hint {
  margin-bottom: 16px;
}
.search-input {
  width: 240px;
}
.reply-input {
  margin-top: 16px;
  margin-bottom: 12px;
}
</style>
