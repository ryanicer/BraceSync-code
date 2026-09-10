<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goHome">← 返回</text>
      <text class="page-title">安装记录</text>
      <text class="page-subtitle">共 {{ filteredRecords.length }} 条记录</text>
    </view>

    <!-- 筛选：WiFi 状态 + 可达性状态 -->
    <view class="section">
      <view class="filter-row">
        <view class="segmented">
          <view :class="['seg-btn', { 'seg-active': wifiFilter === 'all' }]" @click="wifiFilter = 'all'"><text>全部 WiFi</text></view>
          <view :class="['seg-btn', { 'seg-active': wifiFilter === 'connected' }]" @click="wifiFilter = 'connected'"><text>已联网</text></view>
          <view :class="['seg-btn', { 'seg-active': wifiFilter === 'unconfigured' }]" @click="wifiFilter = 'unconfigured'"><text>待配置</text></view>
        </view>
      </view>
      <view class="filter-row">
        <view class="segmented">
          <view :class="['seg-btn', { 'seg-active': reachabilityFilter === 'all' }]" @click="reachabilityFilter = 'all'"><text>全部可达性</text></view>
          <view :class="['seg-btn', { 'seg-active': reachabilityFilter === 'verified' }]" @click="reachabilityFilter = 'verified'"><text>已验证</text></view>
          <view :class="['seg-btn', { 'seg-active': reachabilityFilter === 'pending' }]" @click="reachabilityFilter = 'pending'"><text>待验证</text></view>
          <view :class="['seg-btn', { 'seg-active': reachabilityFilter === 'skipped' }]" @click="reachabilityFilter = 'skipped'"><text>已跳过</text></view>
        </view>
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
        <view v-for="rec in filteredRecords" :key="rec.installId" class="record-card">
          <view class="record-header">
            <text class="record-device">{{ rec.deviceId }}</text>
            <view :class="['wifi-badge', rec.wifiStatus === 'connected' ? 'wifi-ok' : 'wifi-pending']">
              <text>{{ rec.wifiStatus === 'connected' ? '已联网' : '待配置' }}</text>
            </view>
          </view>
          <view class="record-info">
            <view class="info-item">
              <text class="info-label">患者</text>
              <text class="info-value">{{ rec.patientName || rec.patientId }}</text>
            </view>
            <view class="info-item">
              <text class="info-label">安装时间</text>
              <text class="info-value">{{ formatDate(rec.calibrateTime) }}</text>
            </view>
            <view class="info-item">
              <text class="info-label">基线</text>
              <text class="info-value">{{ rec.baselineId || '未保存' }}</text>
            </view>
            <view class="info-item">
              <text class="info-label">可达性</text>
              <view :class="['reachability-badge', reachabilityClass(rec)]">
                <text>{{ reachabilityText(rec) }}</text>
              </view>
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
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { InstallRecord } from '@bracesync/shared-types'
import { listInstallRecords } from '../../api/install'
import type { ReachabilityStatus } from '../../types/app-extends'

const records = ref<InstallRecord[]>([])
const wifiFilter = ref<'all' | 'connected' | 'unconfigured'>('all')
const reachabilityFilter = ref<'all' | ReachabilityStatus>('all')
const loading = ref(false)
const error = ref('')

async function loadRecords() {
  loading.value = true
  error.value = ''
  try {
    const res = await listInstallRecords({})
    records.value = res.records
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

const filteredRecords = computed(() => {
  return records.value.filter((r) => {
    const wifiOk = wifiFilter.value === 'all' || r.wifiStatus === wifiFilter.value
    const rStat = (r as any).reachabilityStatus as ReachabilityStatus | undefined
    const reachOk = reachabilityFilter.value === 'all' || rStat === reachabilityFilter.value
    return wifiOk && reachOk
  })
})

function reachabilityClass(rec: InstallRecord): string {
  const s = (rec as any).reachabilityStatus as ReachabilityStatus | undefined
  if (s === 'verified') return 'reach-ok'
  if (s === 'skipped') return 'reach-skip'
  return 'reach-pending'
}
function reachabilityText(rec: InstallRecord): string {
  const s = (rec as any).reachabilityStatus as ReachabilityStatus | undefined
  if (s === 'verified') return '已验证'
  if (s === 'skipped') return '已跳过'
  return '待验证'
}

function formatDate(iso: string): string {
  if (!iso) return '--'
  try {
    return new Date(iso).toLocaleDateString('zh-CN')
  } catch {
    return '--'
  }
}

function goHome() {
  uni.navigateBack({ fail: () => uni.reLaunch({ url: '/pages/home/index' }) })
}

onMounted(loadRecords)
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.back-link { font-size: 28rpx; color: #2563EB; display: block; margin-bottom: 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.page-subtitle { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.filter-row { margin-bottom: 16rpx; }
.segmented { display: flex; background: #f1f5f9; border-radius: 12rpx; padding: 6rpx; }
.seg-btn { flex: 1; text-align: center; padding: 14rpx 0; border-radius: 8rpx; transition: all 0.2s; }
.seg-btn text { font-size: 24rpx; color: #64748b; }
.seg-active { background: #fff; box-shadow: 0 1rpx 3rpx rgba(0,0,0,0.06); }
.seg-active text { color: #2563EB; font-weight: 500; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0,0,0,0.04); }
.record-list { display: flex; flex-direction: column; gap: 20rpx; }
.record-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 20rpx; padding: 28rpx; box-shadow: 0 2rpx 6rpx rgba(0,0,0,0.03); }
.record-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20rpx; }
.record-device { font-size: 30rpx; font-weight: 600; color: #1e293b; }
.wifi-badge { padding: 6rpx 18rpx; border-radius: 16rpx; font-size: 22rpx; }
.wifi-ok { background: #dcfce7; color: #15803d; }
.wifi-pending { background: #f1f5f9; color: #64748b; }
.wifi-badge text { font-weight: 500; }
.record-info { display: grid; grid-template-columns: 1fr 1fr; gap: 16rpx; }
.info-item { display: flex; flex-direction: column; gap: 4rpx; }
.info-label { font-size: 22rpx; color: #94a3b8; }
.info-value { font-size: 26rpx; color: #334155; }
.reachability-badge { display: inline-flex; padding: 4rpx 16rpx; border-radius: 12rpx; font-size: 22rpx; align-self: flex-start; }
.reach-ok { background: #dcfce7; color: #15803d; }
.reach-pending { background: #fef3c7; color: #b45309; }
.reach-skip { background: #f1f5f9; color: #64748b; }
.record-notes { margin-top: 16rpx; padding-top: 16rpx; border-top: 2rpx solid #f1f5f9; }
.notes-text { font-size: 24rpx; color: #64748b; }
.empty-card { text-align: center; padding: 64rpx 32rpx; }
.empty-icon { font-size: 64rpx; display: block; margin-bottom: 16rpx; }
.empty-text { font-size: 28rpx; color: #94a3b8; display: block; }
.retry-btn { display: inline-block; margin-top: 20rpx; padding: 12rpx 32rpx; background: #2563EB; border-radius: 12rpx; }
.retry-text { color: #fff; font-size: 26rpx; }
</style>
