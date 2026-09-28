<template>
  <view class="page">
    <view class="success-section">
      <text class="success-icon">✅</text>
      <text class="success-title">安装完成</text>
      <text class="success-desc">设备已成功安装并配置完毕</text>
    </view>

    <view class="section">
      <view class="card">
        <text class="card-title">安装摘要</text>
        <view class="summary-row">
          <text class="summary-label">设备 ID</text>
          <text class="summary-value">{{ summary.deviceId }}</text>
        </view>
        <view class="summary-row">
          <text class="summary-label">患者 ID</text>
          <text class="summary-value">{{ summary.patientId }}</text>
        </view>
        <view class="summary-row">
          <text class="summary-label">安装时间</text>
          <text class="summary-value">{{ summary.calibrateTime }}</text>
        </view>
        <view class="summary-row">
          <text class="summary-label">基线状态</text>
          <view :class="['status-badge', baselineBadgeClass]"><text>{{ baselineLabel }}</text></view>
        </view>
        <view class="summary-row">
          <text class="summary-label">WiFi 状态</text>
          <view :class="['status-badge', wifiBadgeClass]"><text>{{ wifiLabel }}</text></view>
        </view>
        <view class="summary-row">
          <text class="summary-label">数据可达性</text>
          <view :class="['status-badge', networkStatusBadgeClass]">
            <text>{{ networkStatusLabel }}</text>
          </view>
        </view>
        <view v-if="summary.notes" class="summary-row">
          <text class="summary-label">备注</text>
          <text class="summary-value">{{ summary.notes }}</text>
        </view>
      </view>
    </view>

    <view class="section">
      <view class="action-list">
        <view class="action-btn btn-primary-style" @click="goRecords">
          <text class="action-icon">📋</text>
          <text class="action-text">查看安装记录</text>
        </view>
        <view class="action-btn btn-outline-style" @click="goNextInstall">
          <text class="action-icon">📦</text>
          <text class="action-text">继续安装下一台</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useInstallStore } from '../../stores/install'
import {
  baselineStatusLabel,
  baselineStatusBadgeClass,
  wifiRowLabel,
  reachabilityLabel,
  reachabilityBadgeClass,
} from '../../utils/installStatus'

const installStore = useInstallStore()

const summary = ref({
  deviceId: '--',
  patientId: '--',
  calibrateTime: '--',
  wifiStatus: 'unconfigured' as string,
  notes: '',
})

// TC-1：改前这一格硬编码「已保存」，与 install 页 :107 的 baselineSaved 同源判定冲突
// ⇒ 基线保存失败时 install 报「未保存」、complete 仍报「已保存」。现两页读同一个标志。
const baselineLabel = computed(() => baselineStatusLabel(installStore.baselineSaved))
const baselineBadgeClass = computed(() => baselineStatusBadgeClass(installStore.baselineSaved))

// TC-2：WiFi 行不再与可达性行同屏互斥（跳过态显「已跳过配网」，不冒充「已联网」）
const wifiLabel = computed(() => wifiRowLabel(summary.value.wifiStatus, installStore.networkSkipped))
const wifiBadgeClass = computed(() =>
  !installStore.networkSkipped && summary.value.wifiStatus === 'connected' ? 'status-ok' : 'status-pending')

const networkStatusLabel = computed(() =>
  reachabilityLabel(installStore.networkSkipped, summary.value.wifiStatus === 'connected'))
const networkStatusBadgeClass = computed(() =>
  reachabilityBadgeClass(installStore.networkSkipped, summary.value.wifiStatus === 'connected'))

onMounted(() => {
  const inst = installStore
  summary.value = {
    deviceId: inst.deviceId || '--',
    patientId: inst.patientId || '--',
    calibrateTime: inst.calibrateTime
      ? new Date(inst.calibrateTime).toLocaleString('zh-CN')
      : new Date().toLocaleString('zh-CN'),
    wifiStatus: inst.wifiStatus,
    notes: inst.installNote || '',
  }
})

function goRecords() {
  uni.navigateTo({ url: '/pages/records/index' })
}

function goNextInstall() {
  installStore.resetInstall()
  uni.reLaunch({ url: '/pages/home/index' })
}
</script>

<style scoped>
.page { min-height: 100vh; background: #f3f4f6; padding-bottom: 120rpx; }
.success-section { padding: 96rpx 48rpx 48rpx; text-align: center; }
.success-icon { font-size: 120rpx; display: block; }
.success-title { display: block; font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 24rpx; }
.success-desc { display: block; font-size: 26rpx; color: #64748b; margin-top: 12rpx; }
.section { padding: 0 40rpx; margin-top: 16rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0,0,0,0.04); }
.card-title { font-size: 32rpx; font-weight: 600; color: #1e293b; display: block; margin-bottom: 24rpx; }
.summary-row { display: flex; justify-content: space-between; align-items: center; padding: 20rpx 0; border-bottom: 2rpx solid #f1f5f9; }
.summary-row:last-of-type { border-bottom: none; }
.summary-label { font-size: 28rpx; color: #64748b; }
.summary-value { font-size: 28rpx; color: #1e293b; font-weight: 500; }
.status-badge { padding: 6rpx 20rpx; border-radius: 16rpx; font-size: 22rpx; }
.status-badge text { font-weight: 500; }
.status-ok { background: #dcfce7; color: #15803d; }
.status-pending { background: #f1f5f9; color: #64748b; }
.status-warn { background: #fef3c7; color: #b45309; }
.action-list { display: flex; flex-direction: column; gap: 24rpx; }
.action-btn { display: flex; align-items: center; gap: 24rpx; padding: 32rpx; border-radius: 20rpx; }
.btn-primary-style { background: #2563EB; }
.btn-primary-style .action-text { color: #fff; }
.btn-outline-style { background: #fff; border: 2rpx solid #e2e8f0; }
.btn-outline-style .action-text { color: #1e293b; }
.action-icon { font-size: 44rpx; }
.action-text { font-size: 30rpx; font-weight: 500; flex: 1; }
</style>
