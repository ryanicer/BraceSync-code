<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">安装记录</text>
      <text class="page-subtitle">共 {{ records.length }} 条记录</text>
    </view>

    <!-- 筛选 -->
    <view class="section">
      <view class="segmented">
        <view :class="['seg-btn', { 'seg-active': filter === 'all' }]" @click="filter = 'all'"><text>全部</text></view>
        <view :class="['seg-btn', { 'seg-active': filter === 'connected' }]" @click="filter = 'connected'"><text>已联网</text></view>
        <view :class="['seg-btn', { 'seg-active': filter === 'unconfigured' }]" @click="filter = 'unconfigured'"><text>待配置</text></view>
      </view>
    </view>

    <!-- 记录列表 -->
    <view class="section">
      <view v-if="loading" class="card empty-card">
        <text class="empty-text">加载中...</text>
      </view>
      <view v-else-if="error" class="card empty-card">
        <text class="empty-icon">⚠️</text>
        <text class="empty-text">{{ error }}</text>
        <view class="retry-btn" @click="loadRecords"><text class="retry-text">重试</text></view>
      </view>
      <view v-else-if="filteredRecords.length > 0" class="record-list">
        <view v-for="rec in filteredRecords" :key="rec.installId" class="record-card" @click="viewDetail(rec)">
          <view class="record-header">
            <text class="record-device">{{ rec.deviceId }}</text>
            <view :class="['wifi-badge', rec.wifiStatus === 'connected' ? 'wifi-ok' : 'wifi-pending']">
              <text>{{ rec.wifiStatus === 'connected' ? '已联网' : '待配置' }}</text>
            </view>
          </view>
          <view class="record-info">
            <view class="info-item">
              <text class="info-label">患者</text>
              <text class="info-value">{{ rec.patientId }}</text>
            </view>
            <view class="info-item">
              <text class="info-label">安装时间</text>
              <text class="info-value">{{ formatDate(rec.calibrateTime) }}</text>
            </view>
            <view class="info-item">
              <text class="info-label">基线</text>
              <text class="info-value">{{ rec.baselineId || '未保存' }}</text>
            </view>
          </view>
          <view v-if="rec.notes" class="record-notes">
            <text class="notes-text">{{ rec.notes }}</text>
          </view>
        </view>
      </view>
      <view v-else class="card empty-card">
        <text class="empty-icon">📋</text>
        <text class="empty-text">暂无安装记录</text>
      </view>
    </view>

    <!-- 新建安装入口 -->
    <view class="section fab-section">
      <view class="fab-btn" @click="goBind">
        <text class="fab-icon">＋</text>
        <text class="fab-text">新建安装</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { InstallRecord, PaginatedResponse } from '@bracesync/shared-types'
import { request } from '../../utils/request'

const records = ref<InstallRecord[]>([])
const filter = ref<'all' | 'connected' | 'unconfigured'>('all')
const loading = ref(false)
const error = ref('')

async function loadRecords() {
  loading.value = true
  error.value = ''
  try {
    // 契约: GET /api/v1/install-records?page=1&pageSize=50
    const res = await request<PaginatedResponse<InstallRecord>>({
      url: '/api/v1/install-records',
      method: 'GET',
      data: { page: 1, pageSize: 50 },
    })
    records.value = res.list || []
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

const filteredRecords = computed(() => {
  if (filter.value === 'all') return records.value
  return records.value.filter(r => r.wifiStatus === filter.value)
})

function formatDate(iso: string): string {
  const d = new Date(iso)
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function viewDetail(rec: InstallRecord) {
  uni.showModal({
    title: `安装详情 ${rec.installId}`,
    content: `设备: ${rec.deviceId}\n患者: ${rec.patientId}\n时间: ${formatDate(rec.calibrateTime)}\n基线: ${rec.baselineId || '无'}\n备注: ${rec.notes || '无'}`,
    showCancel: false,
  })
}

function goBind() {
  uni.navigateTo({ url: '/pages/bind/index' })
}

onMounted(() => {
  loadRecords()
})
</script>

<style scoped>
.page { padding-bottom: 180rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.page-subtitle { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; font-size: 26rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.seg-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.record-list { display: flex; flex-direction: column; gap: 20rpx; }
.record-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.record-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16rpx; }
.record-device { font-size: 30rpx; font-weight: 600; color: #1e293b; }
.wifi-badge { font-size: 20rpx; padding: 4rpx 16rpx; border-radius: 8rpx; }
.wifi-badge text { font-weight: 500; }
.wifi-ok { background: #dbeafe; }
.wifi-ok text { color: #2563EB; }
.wifi-pending { background: #fef3c7; }
.wifi-pending text { color: #d97706; }
.record-info { display: flex; flex-direction: column; gap: 8rpx; }
.info-item { display: flex; align-items: center; gap: 16rpx; }
.info-label { font-size: 24rpx; color: #94a3b8; min-width: 100rpx; }
.info-value { font-size: 26rpx; color: #475569; }
.record-notes { margin-top: 12rpx; padding-top: 12rpx; border-top: 1rpx solid #f1f5f9; }
.notes-text { font-size: 24rpx; color: #64748b; line-height: 1.5; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; }
.empty-card { text-align: center; padding: 64rpx 32rpx; }
.empty-icon { font-size: 64rpx; display: block; margin-bottom: 16rpx; }
.empty-text { font-size: 28rpx; color: #94a3b8; display: block; }
.fab-section { padding-bottom: 40rpx; }
.fab-btn { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; display: flex; align-items: center; justify-content: center; gap: 12rpx; }
.fab-icon { font-size: 36rpx; color: #fff; line-height: 1; }
.fab-text { font-size: 30rpx; color: #fff; font-weight: 500; }
</style>
