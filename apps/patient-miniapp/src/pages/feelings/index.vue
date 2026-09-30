<template>
  <view class="page">
    <!-- ===== 列表态（稿面 feelings.html:54-84 logList）=====
         稿面 header 的「← 返回」与页名由系统导航栏承担（全站非 tab 页同画法，见 report/index.vue），
         页内只留标题与「+ 新增」。稿面 :57 的「即将开放」徽标本页不呈现，理由见 script 顶部注释。 -->
    <view v-if="!editing" class="page-header">
      <text class="page-title">矫形日志</text>
      <view class="add-btn" @click="openCreate">
        <text>+ 新增</text>
      </view>
    </view>

    <view v-if="!editing && loading" class="card state-card">
      <text class="state-text">加载中...</text>
    </view>

    <view v-else-if="!editing && errorMsg" class="card state-card">
      <text class="state-text">{{ errorMsg }}</text>
      <text class="state-sub" @click="loadLogs">点击重试</text>
    </view>

    <view v-else-if="!editing && logs.length === 0" class="card state-card">
      <text class="state-text">暂无矫形日志</text>
      <text class="state-sub">点击「+ 新增」记录今天的佩戴感受</text>
    </view>

    <view v-else-if="!editing" class="section">
      <view v-for="item in logs" :key="item.logId" class="card log-card" @click="openView(item)">
        <text class="log-date">{{ logDateLabel(item.logDate) }}</text>
        <text v-if="item.notes" class="log-summary">{{ item.notes }}</text>
        <view v-if="feelingLabel(item.feeling)" class="log-tags">
          <view :class="['log-tag', feelingTagClass(item.feeling)]">
            <text>{{ feelingLabel(item.feeling) }}</text>
          </view>
        </view>
        <!-- PRD §7A.7「医生回复区」：回复时间 + 回复内容，🔴 不含医生姓名（Boss D4，feeling_logs 无 replied_by 列） -->
        <view v-if="item.replyContent" class="reply-bubble">
          <text class="reply-label">医生回复</text>
          <text class="reply-content">{{ item.replyContent }}</text>
          <text v-if="item.replyTime" class="reply-time">{{ replyTimeLabel(item.replyTime) }}</text>
        </view>
      </view>
    </view>

    <!-- ===== 录入 / 编辑态（稿面 :87-123 logEditor，稿面用 display 切换，本页用 v-if）===== -->
    <view v-if="editing" class="page-header">
      <text class="page-title">{{ editingLogId ? '编辑矫形日志' : '新增矫形日志' }}</text>
    </view>

    <view v-if="editing" class="card editor-card">
      <view class="form-group">
        <text class="form-label">日期</text>
        <picker mode="date" :value="draft.logDate" @change="onDateChange">
          <view class="form-input">
            <text>{{ draft.logDate }}</text>
          </view>
        </picker>
      </view>

      <view class="form-group">
        <text class="form-label">佩戴感受</text>
        <radio-group class="feeling-options" @change="onFeelingChange">
          <label v-for="level in FEELING_LEVELS" :key="level" class="feeling-opt">
            <radio :value="level" :checked="draft.feeling === level" color="#2563EB" />
            <text>{{ feelingLabel(level) }}</text>
          </label>
        </radio-group>
      </view>

      <view class="form-group">
        <text class="form-label">不适部位 (可多选)</text>
        <view class="body-map">
          <view
            v-for="zone in FEELING_AREAS"
            :key="zone"
            :class="['body-zone', { 'body-zone-active': draft.areas.includes(zone) }]"
            @click="pickArea(zone)"
          >
            <text>{{ zone }}</text>
          </view>
        </view>
      </view>

      <view class="form-group">
        <text class="form-label">详细描述</text>
        <!-- :maxlength 与后端 feelingNotesMaxLen 同源（超长后端直接 400），默认 140 会截掉患者已写内容 -->
        <textarea
          v-model="draft.notes"
          class="form-textarea"
          :maxlength="FEELING_NOTES_MAX_LEN"
          placeholder="记录今天的佩戴感受、不适情况及调整措施..."
        />
      </view>

      <view class="form-actions">
        <button class="btn-outline" @click="hideEditor">取消</button>
        <button class="btn-primary" :disabled="saving" @click="saveLog">{{ saving ? '保存中...' : '保存' }}</button>
      </view>
    </view>

    <!-- ===== 查看弹层（稿面 :126-145 viewModal，底部弹出画法同 profile bottom-sheet）===== -->
    <view v-if="viewing" class="sheet-overlay" @click="closeView"></view>
    <view v-if="viewing" class="bottom-sheet">
      <view class="sheet-header">
        <text class="sheet-title">{{ logDateLabel(viewing.logDate) }}</text>
      </view>
      <view class="sheet-body">
        <text class="view-label">佩戴感受</text>
        <text class="view-value">{{ feelingLabel(viewing.feeling) || '未评' }}</text>
        <text class="view-label">不适部位</text>
        <text class="view-value">{{ areasText(viewing.discomfortAreas) || '无' }}</text>
        <text class="view-label">详细描述</text>
        <text class="view-desc">{{ viewing.notes || '无' }}</text>
        <view v-if="viewing.replyContent" class="reply-bubble">
          <text class="reply-label">医生回复</text>
          <text class="reply-content">{{ viewing.replyContent }}</text>
          <text v-if="viewing.replyTime" class="reply-time">{{ replyTimeLabel(viewing.replyTime) }}</text>
        </view>
      </view>
      <view class="sheet-actions">
        <button class="btn-outline" @click="closeView">关闭</button>
        <button class="btn-primary" @click="editFromView">编辑</button>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
/**
 * T505 患者端矫形日志页（稿面 docs/design/patient/feelings.html 唯一基准 + PRD §7A.7）。
 *
 * 口径：佩戴感受两档 贴合 / 不适（`fitted` / `discomfort`，Boss 2026-09-23 方案 A，
 * PRD V3.22 收口）；不适部位 8 区中文原词（PM T188 裁定 Q4 不做码值转换）；
 * 详细描述 200 字上限。三档（good/mild/pain）与「支具贴合度」下拉均已在 PRD/稿面作废，
 * 本页不采集、不呈现（comfort_score 列只对历史数据可见）。
 *
 * 写通道：POST /api/v1/patients/:patientId/feeling-logs（handler.go:294，T188）。
 * 后端按 (patient_id, log_date) upsert ⇒ 同日重复提交覆盖当日行且不清医生回复，
 * 所以「新增」与「编辑」是同一条通道，无需 PUT/PATCH（wx.request 也不支持 PATCH）。
 * 医生回复写端点是 doctorAdminOnly ⇒ 本页只读回复。
 *
 * 与稿面的一处刻意偏离：稿面 :57 的「即将开放」徽标是 F-1 注记里「实现侧无此路由」的留痕，
 * 本卡建的正是这个路由 ⇒ 呈现它等于向患者宣称功能未开放。徽标删除属稿面回写（归设计侧），
 * 已在交件单登记，代码侧不擅自实现一个自相矛盾的标签。
 */
import { onMounted, reactive, ref } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import { FEELING_AREAS, FEELING_LEVELS, FEELING_NOTES_MAX_LEN } from '@bracesync/shared-utils'
import type { FeelingLog } from '@bracesync/shared-types'
import { request } from '../../utils/request'
import { useAuthStore } from '../../stores/auth'
import {
  areasText, buildFeelingPayload, feelingLabel, feelingTagClass,
  logDateLabel, replyTimeLabel, todayLocalDate, toggleArea,
} from '../../utils/feelings'

const auth = useAuthStore()
const logs = ref<FeelingLog[]>([])
const loading = ref(false)
const errorMsg = ref('')

const editing = ref(false)
const saving = ref(false)
/** 编辑态对应的原日志 id：仅用于弹层标题（新增/编辑）与回填，写通道不区分 */
const editingLogId = ref('')
const viewing = ref<FeelingLog | null>(null)

const draft = reactive({
  logDate: todayLocalDate(),
  feeling: 'fitted' as string, // 稿面 :97 默认选中第一项（fitted）
  areas: [] as string[],
  notes: '',
})

function endpoint() {
  return `/api/v1/patients/${auth.patientId}/feeling-logs`
}

async function loadLogs() {
  if (!auth.patientId) {
    errorMsg.value = '请先登录'
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    // 倒序由后端 pg.go:1060 的 ORDER BY log_date DESC, log_id DESC 保证，前端不再重排
    const list = await request<FeelingLog[]>({ url: endpoint() })
    logs.value = Array.isArray(list) ? list : []
  } catch (e: unknown) {
    errorMsg.value = userErrorCopy(e, { scope: 'patient', fallback: '加载矫形日志失败' })
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingLogId.value = ''
  draft.logDate = todayLocalDate()
  draft.feeling = 'fitted'
  draft.areas = []
  draft.notes = ''
  viewing.value = null
  editing.value = true
}

function openView(item: FeelingLog) {
  viewing.value = item
}

function closeView() {
  viewing.value = null
}

/** 稿面 :170 showEditorFromView：从查看弹层带着这条日志进编辑态 */
function editFromView() {
  const item = viewing.value
  if (!item) return
  editingLogId.value = item.logId
  draft.logDate = item.logDate
  draft.feeling = item.feeling ?? 'fitted'
  draft.areas = [...(item.discomfortAreas ?? [])]
  draft.notes = item.notes ?? ''
  viewing.value = null
  editing.value = true
}

function hideEditor() {
  editing.value = false
}

function pickArea(zone: string) {
  draft.areas = toggleArea(draft.areas, zone)
}

function onDateChange(e: { detail: { value: string } }) {
  draft.logDate = e.detail.value
}

function onFeelingChange(e: { detail: { value: string } }) {
  draft.feeling = e.detail.value
}

async function saveLog() {
  if (!auth.patientId) {
    uni.showToast({ title: '请先登录', icon: 'none' })
    return
  }
  saving.value = true
  try {
    await request<FeelingLog>({
      url: endpoint(),
      method: 'POST',
      data: buildFeelingPayload({ ...draft }),
    })
    editing.value = false
    // 稿面 saveLog() 的 alert('日志已保存') 在小程序侧的可实现画法
    uni.showToast({ title: '日志已保存', icon: 'none' })
    await loadLogs()
  } catch (e: unknown) {
    uni.showToast({
      title: userErrorCopy(e, { scope: 'patient', fallback: '保存失败' }),
      icon: 'none',
    })
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadLogs()
})
</script>

<style scoped lang="scss">
.page {
  padding: 24rpx;
  min-height: 100vh;
  background: #f8fafc;
}
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24rpx;
}
.page-title {
  font-size: 34rpx;
  font-weight: 500;
  color: #1e293b;
}
.add-btn {
  padding: 12rpx 28rpx;
  background: #2563EB;
  border-radius: 16rpx;
  text {
    font-size: 26rpx;
    color: #fff;
  }
}
.section {
  display: flex;
  flex-direction: column;
  gap: 16rpx;
}
.card {
  background: #fff;
  border: 1rpx solid #e2e8f0;
  border-radius: 24rpx;
  padding: 28rpx;
}
.state-card {
  text-align: center;
  padding: 64rpx 24rpx;
}
.state-text {
  font-size: 28rpx;
  color: #6b7280;
}
.state-sub {
  display: block;
  margin-top: 12rpx;
  font-size: 24rpx;
  color: #9ca3af;
}
.log-card {
  display: flex;
  flex-direction: column;
  gap: 12rpx;
}
.log-date {
  font-size: 26rpx;
  font-weight: 500;
  color: #1e293b;
}
.log-summary {
  font-size: 26rpx;
  color: #475569;
  line-height: 1.6;
}
.log-tags {
  display: flex;
  gap: 12rpx;
}
.log-tag {
  padding: 4rpx 16rpx;
  border-radius: 24rpx;
  text {
    font-size: 20rpx;
  }
}
/* 稿面 :192-193 两档配色（贴合=蓝底蓝字 / 不适=橙底橙字），与后台 矫形日志.html 同画法 */
.log-tag-ok {
  background: #dbeafe;
  text { color: #2563EB; }
}
.log-tag-warn {
  background: #fef3c7;
  text { color: #f59e0b; }
}
.reply-bubble {
  margin-top: 8rpx;
  padding: 16rpx 20rpx;
  background: #f1f5f9;
  border-radius: 16rpx;
  display: flex;
  flex-direction: column;
  gap: 6rpx;
}
.reply-label {
  font-size: 22rpx;
  color: #64748b;
}
.reply-content {
  font-size: 26rpx;
  color: #1e293b;
  line-height: 1.6;
}
.reply-time {
  font-size: 22rpx;
  color: #94a3b8;
}
.editor-card {
  display: flex;
  flex-direction: column;
  gap: 24rpx;
}
.form-group {
  display: flex;
  flex-direction: column;
  gap: 12rpx;
}
.form-label {
  font-size: 26rpx;
  font-weight: 500;
  color: #1e293b;
}
.form-input {
  padding: 16rpx 24rpx;
  border: 1rpx solid #e2e8f0;
  border-radius: 20rpx;
  background: #f1f5f9;
  text {
    font-size: 26rpx;
    color: #1e293b;
  }
}
.feeling-options {
  display: flex;
  gap: 32rpx;
}
.feeling-opt {
  display: flex;
  align-items: center;
  gap: 8rpx;
  font-size: 26rpx;
  color: #1e293b;
}
.body-map {
  display: flex;
  flex-wrap: wrap;
  gap: 12rpx;
}
.body-zone {
  padding: 10rpx 24rpx;
  border: 1rpx solid #e2e8f0;
  border-radius: 28rpx;
  background: #fff;
  text {
    font-size: 24rpx;
    color: #94a3b8;
  }
}
.body-zone-active {
  background: #dbeafe;
  border-color: #2563EB;
  text { color: #2563EB; }
}
.form-textarea {
  width: 100%;
  min-height: 160rpx;
  padding: 16rpx 24rpx;
  border: 1rpx solid #e2e8f0;
  border-radius: 20rpx;
  background: #f1f5f9;
  font-size: 26rpx;
  color: #1e293b;
  box-sizing: border-box;
}
.form-actions {
  display: flex;
  gap: 16rpx;
}
.btn-primary,
.btn-outline {
  flex: 1;
  font-size: 28rpx;
  border-radius: 20rpx;
  line-height: 2.6;
  margin: 0;
}
.btn-primary {
  background: #2563EB;
  color: #fff;
  &[disabled] {
    background: #94a3b8;
  }
}
.btn-outline {
  background: #fff;
  border: 1rpx solid #e2e8f0;
  color: #94a3b8;
}
.btn-primary::after,
.btn-outline::after {
  border: none;
}
.sheet-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.3);
  z-index: 200;
}
.bottom-sheet {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  background: #fff;
  border-radius: 32rpx 32rpx 0 0;
  padding: 32rpx 40rpx calc(32rpx + env(safe-area-inset-bottom, 20rpx));
  z-index: 201;
  max-height: 70vh;
  overflow-y: auto;
}
.sheet-header {
  margin-bottom: 24rpx;
}
.sheet-title {
  font-size: 32rpx;
  font-weight: 500;
  color: #1e293b;
}
.sheet-body {
  display: flex;
  flex-direction: column;
  gap: 8rpx;
}
.view-label {
  font-size: 22rpx;
  color: #94a3b8;
}
.view-value {
  font-size: 28rpx;
  color: #1e293b;
  margin-bottom: 12rpx;
}
.view-desc {
  font-size: 26rpx;
  color: #475569;
  line-height: 1.7;
}
.sheet-actions {
  display: flex;
  gap: 16rpx;
  margin-top: 32rpx;
}
</style>
