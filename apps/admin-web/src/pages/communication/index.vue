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
      <el-button @click="openWechatKF">打开微信客服后台</el-button>
      <span class="kf-hint">小程序客服消息 · 需使用运营账号登录微信公众平台</span>
    </div>

    <div class="layout">
      <!-- 左：反馈列表 -->
      <div class="left-pane page-card">
        <div class="pane-title">反馈列表</div>
        <el-table
          :data="list"
          size="small"
          v-loading="loading"
          highlight-current-row
          @row-click="selectFeedback"
        >
          <el-table-column prop="feedbackId" label="ID" width="80" />
          <el-table-column label="患者" width="90">
            <template #default="{ row }">{{ patientNameOf(row.patientId) }}</template>
          </el-table-column>
          <el-table-column prop="type" label="类型" width="90" />
          <el-table-column prop="content" label="反馈内容" min-width="180" show-overflow-tooltip />
          <el-table-column label="状态" width="80">
            <template #default="{ row }">
              <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <!-- 右：反馈详情 + 回复 -->
      <div class="right-pane page-card">
        <template v-if="current">
          <div class="pane-title">反馈 {{ current.feedbackId }} 详情</div>
          <el-descriptions :column="1" border size="small" class="desc">
            <el-descriptions-item label="患者">{{ patientNameOf(current.patientId) }}</el-descriptions-item>
            <el-descriptions-item label="类型">{{ current.type }}</el-descriptions-item>
            <el-descriptions-item label="内容">{{ current.content }}</el-descriptions-item>
            <el-descriptions-item label="提交时间">{{ formatTime(current.submitTime) }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="statusTagType(current.status)" size="small">{{ statusLabel(current.status) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item v-if="current.replyContent" label="客服处理备注">{{ current.replyContent }}</el-descriptions-item>
          </el-descriptions>

          <template v-if="current.status === 'pending'">
            <div class="reply-box">
              <div class="reply-label">处理备注</div>
              <el-input
                v-model="replyText"
                type="textarea"
                :rows="4"
                placeholder="记录本次处理说明（仅内部可见，不会发送给患者）"
              />
              <div class="reply-actions">
                <el-button type="primary" :loading="replying" @click="saveNote">保存处理备注</el-button>
                <el-button @click="markResolved(current)">仅标记已处理</el-button>
              </div>
            </div>
          </template>
          <template v-else-if="current.status === 'replied'">
            <el-button type="success" @click="markResolved(current)">标记为已处理</el-button>
          </template>
          <el-empty v-else description="该反馈已解决" :image-size="60" class="empty-desc" />
        </template>
        <el-empty v-else description="选择左侧反馈查看详情" :image-size="80" class="empty-desc" />
      </div>
    </div>
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

function openWechatKF() {
  window.open('https://mpkf.weixin.qq.com/', '_blank')
}

async function loadData() {
  loading.value = true
  try {
    list.value = await fetchFeedbacks({ keyword: keyword.value || undefined })
    if (list.value.length > 0 && !current.value) {
      current.value = list.value[0]
    }
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function selectFeedback(row: Feedback) {
  current.value = row
  replyText.value = ''
}

async function saveNote() {
  if (!current.value) return
  const note = replyText.value.trim()
  if (!note) {
    ElMessage.warning('请先填写处理备注')
    return
  }
  replying.value = true
  try {
    await processFeedbackApi(current.value.feedbackId, { replyContent: note })
    current.value.replyContent = note
    current.value.replyTime = new Date().toISOString()
    if (current.value.status !== 'resolved') current.value.status = 'replied'
    current.value.handler = auth.user?.name ?? null
    ElMessage.success('处理备注已保存')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    replying.value = false
  }
}

async function markResolved(row: Feedback) {
  try {
    await processFeedbackApi(row.feedbackId, { markResolved: true })
    row.status = 'resolved'
    row.handler = row.handler || auth.user?.name || null
    ElMessage.success('已标记为已处理')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  }
}

onMounted(loadData)
</script>

<style scoped>
.role-hint { margin-bottom: 16px; }
.search-input { width: 240px; }
.kf-hint { font-size: 12px; color: var(--el-text-color-secondary); margin-left: 8px; }

.layout {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  align-items: start;
}
@media (max-width: 992px) {
  .layout { grid-template-columns: 1fr; }
}
.left-pane, .right-pane { padding: 16px; }
.pane-title {
  font-size: 15px;
  font-weight: 600;
  color: #1a6db5;
  margin-bottom: 12px;
}
.desc { margin-bottom: 16px; }
.reply-box { margin-top: 12px; }
.reply-label { font-size: 13px; color: #64748b; margin-bottom: 6px; }
.reply-actions { margin-top: 10px; display: flex; gap: 8px; }
.empty-desc { margin-top: 40px; }
</style>
