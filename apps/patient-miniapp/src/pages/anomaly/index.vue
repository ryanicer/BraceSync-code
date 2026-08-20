<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">异常事件</text>
      <text class="page-subtitle">近期告警与异常记录</text>
    </view>

    <!-- 筛选 -->
    <view class="section">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': filter === 'all' }]" @click="filter = 'all'"><text>全部</text></view>
        <view :class="['seg-btn', { 'seg-active': filter === 'pressure' }]" @click="filter = 'pressure'"><text>压力异常</text></view>
        <view :class="['seg-btn', { 'seg-active': filter === 'wear' }]" @click="filter = 'wear'"><text>佩戴异常</text></view>
      </view>
    </view>

    <!-- 摘要统计 -->
    <view v-if="!loading && !error && filteredAlerts.length > 0" class="section summary-section">
      <view class="summary-row">
        <view class="summary-item">
          <text class="summary-num summary-num-error">{{ unreadCount }}</text>
          <text class="summary-label">未读</text>
        </view>
        <view class="summary-item">
          <text class="summary-num summary-num-warn">{{ activeCount }}</text>
          <text class="summary-label">进行中</text>
        </view>
        <view class="summary-item">
          <text class="summary-num summary-num-ok">{{ resolvedCount }}</text>
          <text class="summary-label">已恢复</text>
        </view>
      </view>
    </view>

    <!-- 告警列表 -->
    <view class="section">
      <view v-if="loading" class="card empty-card">
        <text class="empty-text">加载中...</text>
      </view>
      <view v-else-if="error" class="card empty-card">
        <text class="empty-icon">⚠️</text>
        <text class="empty-text">{{ error }}</text>
        <view class="retry-btn" @click="loadAlerts"><text class="retry-text">重试</text></view>
      </view>
      <view v-else-if="filteredAlerts.length > 0" class="alert-list">
        <view v-for="alert in filteredAlerts" :key="alert.alertId" :class="['alert-card', { 'alert-unread': alert.readStatus === 'unread' }]" @click="viewDetail(alert)">
          <view class="alert-header">
            <view :class="['type-badge', 'type-' + getSeverity(alert.type)]">
              <text>{{ alertTypeLabel(alert.type) }}</text>
            </view>
            <view class="alert-header-right">
              <view v-if="alert.readStatus === 'unread'" class="unread-dot"></view>
              <view :class="['resolve-dot', 'resolve-' + alert.resolvedStatus]"></view>
              <text class="alert-time">{{ formatTime(alert.timestamp) }}</text>
            </view>
          </view>
          <text class="alert-detail">{{ alert.detail }}</text>
          <view v-if="alert.sensorPoint || alert.actualValue" class="alert-meta">
            <text v-if="alert.sensorPoint" class="meta-chip">{{ alert.sensorPoint }}</text>
            <text v-if="alert.actualValue" class="meta-chip meta-chip-warn">{{ alert.actualValue }}N</text>
            <text v-if="alert.thresholdValue" class="meta-chip-threshold">阈值 {{ alert.thresholdValue }}N</text>
          </view>
          <view v-if="alert.processNote" class="alert-note">
            <text class="note-label">技师反馈</text>
            <text class="note-text">{{ alert.processNote }}</text>
          </view>
        </view>
      </view>
      <view v-else class="card empty-card">
        <text class="empty-icon">🔔</text>
        <text class="empty-text">暂无异常事件</text>
        <text class="empty-sub">一切正常，请继续坚持佩戴</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { Alert, PaginatedResponse } from '@bracesync/shared-types'
import { USE_MOCK, request } from '../../utils/request'
import { mockPatientAlerts } from '../../mock/alerts'

// 数据
const alerts = ref<Alert[]>([])
const filter = ref<'all' | 'pressure' | 'wear'>('all')
const loading = ref(false)
const error = ref('')

// 患者 ID（mock 硬编码，真实环境从登录态获取）
const PATIENT_ID = 'PT-001'

// 过滤：pressure = pressure_high + pressure_fluctuation + sensor_drift，wear = wear_interrupt
const filteredAlerts = computed(() => {
  if (filter.value === 'all') return alerts.value
  if (filter.value === 'wear') return alerts.value.filter(a => a.type === 'wear_interrupt')
  return alerts.value.filter(a => a.type !== 'wear_interrupt')
})

const unreadCount = computed(() => alerts.value.filter(a => a.readStatus === 'unread').length)
const activeCount = computed(() => alerts.value.filter(a => a.resolvedStatus === 'active').length)
const resolvedCount = computed(() => alerts.value.filter(a => a.resolvedStatus === 'resolved').length)

// 告警类型标签
function alertTypeLabel(type: string): string {
  const map: Record<string, string> = {
    pressure_high: '压力偏高',
    wear_interrupt: '佩戴中断',
    pressure_fluctuation: '压力波动',
    sensor_drift: '传感器漂移',
  }
  return map[type] || type
}

// 严重程度
function getSeverity(type: string): string {
  if (type === 'pressure_high' || type === 'wear_interrupt') return 'error'
  return 'warn'
}

// 格式化时间
function formatTime(iso: string): string {
  const d = new Date(iso)
  const month = d.getMonth() + 1
  const day = d.getDate()
  const h = String(d.getHours()).padStart(2, '0')
  const m = String(d.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${h}:${m}`
}

// 加载数据
async function loadAlerts() {
  loading.value = true
  error.value = ''
  try {
    if (USE_MOCK) {
      alerts.value = mockPatientAlerts()
    } else {
      // 契约方案 A: GET /api/v1/alerts?patientId=xxx（alert-service 公开查询）
      // 契约方案 B: GET /api/v1/patients/{patientId}/realtime（data-service 快照中 alerts 数组，当前恒空）
      // 优先走方案 A，alert-service 提供完整分页
      const res = await request<PaginatedResponse<Alert>>({
        url: '/api/v1/alerts',
        method: 'GET',
        data: { patientId: PATIENT_ID, page: 1, pageSize: 50 },
      })
      alerts.value = res.list
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

// 查看详情
function viewDetail(alert: Alert) {
  const lines = [
    `类型: ${alertTypeLabel(alert.type)}`,
    alert.sensorPoint ? `传感器: ${alert.sensorPoint}` : '',
    alert.thresholdValue ? `阈值: ${alert.thresholdValue}N` : '',
    alert.actualValue ? `实际值: ${alert.actualValue}N` : '',
    `详情: ${alert.detail}`,
    alert.processNote ? `技师反馈: ${alert.processNote}` : '',
    `状态: ${alert.resolvedStatus === 'active' ? '进行中' : '已恢复'}`,
  ].filter(Boolean)
  uni.showModal({
    title: alertTypeLabel(alert.type),
    content: lines.join('\n'),
    showCancel: false,
  })
}

onMounted(() => {
  loadAlerts()
})
</script>

<style scoped>
.page { padding-bottom: 180rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; display: block; }
.page-subtitle { font-size: 24rpx; color: #cbd5e1; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; font-size: 26rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.seg-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.summary-section { margin-top: 16rpx; }
.summary-row { display: flex; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx 20rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.summary-item { flex: 1; display: flex; flex-direction: column; align-items: center; gap: 8rpx; }
.summary-num { font-size: 44rpx; font-weight: 600; line-height: 1; }
.summary-num-error { color: #ef4444; }
.summary-num-warn { color: #d97706; }
.summary-num-ok { color: #16a34a; }
.summary-label { font-size: 22rpx; color: #94a3b8; }
.alert-list { display: flex; flex-direction: column; gap: 16rpx; }
.alert-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 24rpx 28rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.alert-unread { border-left: 6rpx solid #ef4444; }
.alert-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12rpx; }
.alert-header-right { display: flex; align-items: center; gap: 10rpx; }
.unread-dot { width: 12rpx; height: 12rpx; border-radius: 50%; background: #ef4444; flex-shrink: 0; }
.resolve-dot { width: 12rpx; height: 12rpx; border-radius: 50%; flex-shrink: 0; }
.resolve-active { background: #f59e0b; }
.resolve-resolved { background: #22c55e; }
.type-badge { font-size: 20rpx; padding: 4rpx 14rpx; border-radius: 8rpx; }
.type-badge text { font-weight: 600; }
.type-error { background: #fee2e2; }
.type-error text { color: #dc2626; }
.type-warn { background: #fef3c7; }
.type-warn text { color: #d97706; }
.alert-time { font-size: 22rpx; color: #94a3b8; }
.alert-detail { font-size: 26rpx; color: #334155; line-height: 1.5; display: block; margin-bottom: 12rpx; }
.alert-meta { display: flex; gap: 12rpx; flex-wrap: wrap; margin-bottom: 12rpx; }
.meta-chip { font-size: 22rpx; padding: 4rpx 14rpx; background: #f1f5f9; border-radius: 8rpx; color: #475569; font-weight: 500; }
.meta-chip-warn { background: #fef3c7; color: #d97706; }
.meta-chip-threshold { font-size: 22rpx; color: #94a3b8; padding: 4rpx 0; }
.alert-note { padding: 16rpx 20rpx; background: #f0f9ff; border-radius: 12rpx; display: flex; flex-direction: column; gap: 6rpx; }
.note-label { font-size: 20rpx; color: #2563EB; font-weight: 500; }
.note-text { font-size: 24rpx; color: #475569; line-height: 1.4; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.empty-card { text-align: center; padding: 64rpx 32rpx; }
.empty-icon { font-size: 64rpx; display: block; margin-bottom: 16rpx; }
.empty-text { font-size: 28rpx; color: #94a3b8; display: block; margin-bottom: 8rpx; }
.empty-sub { font-size: 24rpx; color: #cbd5e1; display: block; }
.retry-btn { margin-top: 24rpx; padding: 12rpx 32rpx; background: #2563EB; border-radius: 12rpx; display: inline-block; }
.retry-text { font-size: 26rpx; color: #fff; font-weight: 500; }
</style>
