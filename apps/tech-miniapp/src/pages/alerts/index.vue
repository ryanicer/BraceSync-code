<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">告警通知</text>
      <text class="page-subtitle">共 {{ filteredAlerts.length }} 条告警</text>
    </view>

    <!-- 筛选 -->
    <view class="section">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': filter === 'all' }]" @click="filter = 'all'"><text>全部</text></view>
        <view :class="['seg-btn', { 'seg-active': filter === 'pending' }]" @click="filter = 'pending'"><text>待处理</text></view>
        <view :class="['seg-btn', { 'seg-active': filter === 'processed' }]" @click="filter = 'processed'"><text>已处理</text></view>
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
        <view v-for="alert in filteredAlerts" :key="alert.alertId" class="alert-card" @click="viewDetail(alert)">
          <view class="alert-header">
            <view :class="['type-badge', 'type-' + getSeverity(alert.type)]">
              <text>{{ alertTypeLabel(alert.type) }}</text>
            </view>
            <view class="alert-header-right">
              <view v-if="alert.readStatus === 'unread'" class="unread-dot"></view>
              <text class="alert-time">{{ formatTime(alert.timestamp) }}</text>
            </view>
          </view>
          <view class="alert-body">
            <text class="alert-detail">{{ alert.detail }}</text>
            <view class="alert-meta">
              <view class="meta-item">
                <text class="meta-label">患者</text>
                <text class="meta-value">{{ alert.patientId }}</text>
              </view>
              <view v-if="alert.sensorPoint" class="meta-item">
                <text class="meta-label">传感器</text>
                <text class="meta-value">{{ alert.sensorPoint }}</text>
              </view>
              <view v-if="alert.actualValue" class="meta-item">
                <text class="meta-label">实际值</text>
                <text class="meta-value meta-warn">{{ alert.actualValue }}N</text>
              </view>
            </view>
          </view>
          <view class="alert-footer">
            <view :class="['status-tag', 'status-' + alert.processStatus]">
              <text>{{ alert.processStatus === 'pending' ? '待处理' : '已处理' }}</text>
            </view>
            <view :class="['resolve-tag', 'resolve-' + alert.resolvedStatus]">
              <text>{{ alert.resolvedStatus === 'active' ? '进行中' : '已恢复' }}</text>
            </view>
            <view
              v-if="alert.processStatus === 'pending'"
              class="process-btn"
              @click.stop="handleProcess(alert)"
            >
              <text class="process-btn-text">处理</text>
            </view>
          </view>
        </view>
      </view>
      <view v-else class="card empty-card">
        <text class="empty-icon">🔔</text>
        <text class="empty-text">暂无告警</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { Alert, PaginatedResponse } from '@bracesync/shared-types'
import { request } from '../../utils/request'

// 数据
const alerts = ref<Alert[]>([])
const filter = ref<'all' | 'pending' | 'processed'>('all')
const loading = ref(false)
const error = ref('')

// 过滤
const filteredAlerts = computed(() => {
  if (filter.value === 'all') return alerts.value
  return alerts.value.filter(a => a.processStatus === filter.value)
})

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

// 严重程度（用于 badge 颜色）
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

// 加载告警数据
async function loadAlerts() {
  loading.value = true
  error.value = ''
  try {
    // 契约: GET /api/v1/alerts?page=1&pageSize=50
    const res = await request<PaginatedResponse<Alert>>({
      url: '/api/v1/alerts',
      method: 'GET',
      data: { page: 1, pageSize: 50 },
    })
    alerts.value = res.list || []
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
    `患者: ${alert.patientId}`,
    `设备: ${alert.deviceId}`,
    alert.sensorPoint ? `传感器: ${alert.sensorPoint}` : '',
    alert.thresholdValue ? `阈值: ${alert.thresholdValue}N` : '',
    alert.actualValue ? `实际值: ${alert.actualValue}N` : '',
    `详情: ${alert.detail}`,
    alert.processNote ? `处理备注: ${alert.processNote}` : '',
    `状态: ${alert.processStatus === 'pending' ? '待处理' : '已处理'}`,
  ].filter(Boolean)
  uni.showModal({
    title: `告警 ${alert.alertId}`,
    content: lines.join('\n'),
    showCancel: false,
  })
}

// 处理告警
async function handleProcess(alert: Alert) {
  uni.showModal({
    title: '处理确认',
    content: `确认处理告警 ${alert.alertId}？`,
    success: async (res) => {
      if (!res.confirm) return
      try {
        // 契约: POST /api/v1/alerts/:alertId/process
        await request({
          url: `/api/v1/alerts/${alert.alertId}/process`,
          method: 'POST',
        })
        alert.processStatus = 'processed'
        alert.processedAt = new Date().toISOString()
        uni.showToast({ title: '处理成功', icon: 'success' })
      } catch (e: unknown) {
        uni.showToast({ title: e instanceof Error ? e.message : '处理失败', icon: 'none' })
      }
    },
  })
}

onMounted(() => {
  loadAlerts()
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.page-subtitle { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; font-size: 26rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.seg-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.alert-list { display: flex; flex-direction: column; gap: 20rpx; }
.alert-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.alert-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16rpx; }
.alert-header-right { display: flex; align-items: center; gap: 12rpx; }
.unread-dot { width: 14rpx; height: 14rpx; border-radius: 50%; background: #ef4444; flex-shrink: 0; }
.type-badge { font-size: 20rpx; padding: 4rpx 16rpx; border-radius: 8rpx; }
.type-badge text { font-weight: 600; }
.type-error { background: #fee2e2; }
.type-error text { color: #dc2626; }
.type-warn { background: #fef3c7; }
.type-warn text { color: #d97706; }
.alert-time { font-size: 24rpx; color: #94a3b8; }
.alert-body { margin-bottom: 16rpx; }
.alert-detail { font-size: 28rpx; color: #1e293b; line-height: 1.5; display: block; margin-bottom: 12rpx; }
.alert-meta { display: flex; gap: 24rpx; flex-wrap: wrap; }
.meta-item { display: flex; align-items: center; gap: 8rpx; }
.meta-label { font-size: 22rpx; color: #94a3b8; }
.meta-value { font-size: 24rpx; color: #475569; font-weight: 500; }
.meta-warn { color: #d97706; }
.alert-footer { display: flex; align-items: center; gap: 12rpx; padding-top: 16rpx; border-top: 1rpx solid #f1f5f9; }
.status-tag { font-size: 20rpx; padding: 4rpx 14rpx; border-radius: 8rpx; }
.status-tag text { font-weight: 500; }
.status-pending { background: #fef3c7; }
.status-pending text { color: #d97706; }
.status-processed { background: #dbeafe; }
.status-processed text { color: #2563EB; }
.resolve-tag { font-size: 20rpx; padding: 4rpx 14rpx; border-radius: 8rpx; }
.resolve-tag text { font-weight: 500; }
.resolve-active { background: #fee2e2; }
.resolve-active text { color: #dc2626; }
.resolve-resolved { background: #dcfce7; }
.resolve-resolved text { color: #16a34a; }
.process-btn { margin-left: auto; padding: 8rpx 28rpx; background: #2563EB; border-radius: 12rpx; }
.process-btn-text { font-size: 24rpx; color: #fff; font-weight: 500; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.empty-card { text-align: center; padding: 64rpx 32rpx; }
.empty-icon { font-size: 64rpx; display: block; margin-bottom: 16rpx; }
.empty-text { font-size: 28rpx; color: #94a3b8; display: block; }
.retry-btn { margin-top: 24rpx; padding: 12rpx 32rpx; background: #2563EB; border-radius: 12rpx; display: inline-block; }
.retry-text { font-size: 26rpx; color: #fff; font-weight: 500; }
</style>
