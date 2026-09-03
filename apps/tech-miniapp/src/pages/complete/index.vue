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
          <view class="status-badge status-ok"><text>已保存</text></view>
        </view>
        <view class="summary-row">
          <text class="summary-label">WiFi 状态</text>
          <view :class="['status-badge', summary.wifiStatus === 'connected' ? 'status-ok' : 'status-pending']">
            <text>{{ summary.wifiStatus === 'connected' ? '已联网' : '待配置' }}</text>
          </view>
        </view>
        <view class="summary-row">
          <text class="summary-label">电子签名</text>
          <view class="status-badge status-ok"><text>已签署</text></view>
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
        <view class="action-btn btn-outline-style" @click="goBind">
          <text class="action-icon">📦</text>
          <text class="action-text">继续安装下一台</text>
        </view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useInstallStore } from '../../stores/install'

const installStore = useInstallStore()

const summary = ref({
  deviceId: '--',
  patientId: '--',
  calibrateTime: '--',
  wifiStatus: 'unconfigured' as string,
  notes: '',
})

onMounted(() => {
  const inst = installStore.currentInstall
  if (inst) {
    summary.value = {
      deviceId: inst.deviceId || '--',
      patientId: inst.patientId || '--',
      calibrateTime: inst.calibrateTime ? new Date(inst.calibrateTime).toLocaleString('zh-CN') : '--',
      wifiStatus: inst.wifiStatus || 'unconfigured',
      notes: inst.notes || '',
    }
  }
})

function goRecords() {
  uni.redirectTo({ url: '/pages/records/index' })
}

function goBind() {
  installStore.resetInstall()
  uni.redirectTo({ url: '/pages/bind/index' })
}
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.success-section { text-align: center; padding: 96rpx 48rpx 48rpx; }
.success-icon { font-size: 120rpx; display: block; margin-bottom: 24rpx; }
.success-title { font-size: 44rpx; font-weight: 600; color: #1e293b; display: block; margin-bottom: 12rpx; }
.success-desc { font-size: 28rpx; color: #94a3b8; display: block; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.card-title { font-size: 32rpx; font-weight: 600; color: #1e293b; display: block; margin-bottom: 24rpx; }
.summary-row { display: flex; align-items: center; justify-content: space-between; padding: 16rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.summary-row:last-child { border-bottom: none; }
.summary-label { font-size: 28rpx; color: #64748b; }
.summary-value { font-size: 28rpx; font-weight: 500; color: #1e293b; max-width: 60%; text-align: right; }
.status-badge { font-size: 22rpx; padding: 4rpx 16rpx; border-radius: 8rpx; }
.status-badge text { font-weight: 500; }
.status-ok { background: #dbeafe; }
.status-ok text { color: #2563EB; }
.status-pending { background: #fef3c7; }
.status-pending text { color: #d97706; }
.action-list { display: flex; flex-direction: column; gap: 20rpx; }
.action-btn { width: 100%; padding: 28rpx 0; border-radius: 16rpx; display: flex; align-items: center; justify-content: center; gap: 16rpx; }
.btn-primary-style { background: #2563EB; }
.btn-outline-style { border: 2rpx solid #2563EB; background: #fff; }
.action-icon { font-size: 32rpx; }
.btn-primary-style .action-text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-outline-style .action-text { color: #2563EB; font-size: 30rpx; font-weight: 500; }
</style>
