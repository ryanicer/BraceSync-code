<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">设备管理</text>
    </view>

    <view class="section">
      <view class="add-card" @click="goWifiSetup">
        <view class="add-icon-box"><text class="add-icon">＋</text></view>
        <text class="add-title">添加新设备</text>
        <text class="add-desc">将您的脊柱压力传感器与手机配对，开始监测</text>
      </view>
    </view>

    <view class="section">
      <text class="section-title">添加流程</text>
      <view class="card steps-card">
        <template v-for="(step, i) in steps" :key="i">
          <view class="step-row">
            <view class="step-num"><text>{{ i + 1 }}</text></view>
            <text class="step-text">{{ step }}</text>
          </view>
          <view v-if="i < steps.length - 1" class="step-divider"></view>
        </template>
      </view>
    </view>

    <view class="section">
      <text class="section-title">网络配置</text>
      <view class="card">
        <view class="input-group">
          <text class="input-label">SSID (网络名称)</text>
          <input class="app-input" type="text" placeholder="请输入WiFi名称" v-model="wifiSSID" />
        </view>
        <view class="input-group">
          <text class="input-label">密码</text>
          <input class="app-input" password placeholder="请输入WiFi密码" v-model="wifiPassword" />
        </view>
      </view>
    </view>

    <view class="section"><view class="action-btn" @click="goWifiSetup"><text>开始添加设备</text></view></view>

    <view class="section device-section">
      <text class="section-title">已添加设备</text>
      <view v-if="device" class="device-card">
        <view class="device-icon-box"><text class="device-icon">📦</text></view>
        <view class="device-info">
          <text class="device-name">{{ device.deviceId }}</text>
          <view class="device-status">
            <view :class="['dot', device.status === 'online' ? 'dot-blue' : 'dot-gray']"></view>
            <text class="device-status-text">已添加 · {{ device.status === 'online' ? '在线' : '离线' }}</text>
          </view>
        </view>
        <view class="delete-btn" @click="confirmUnbind"><text class="delete-icon">🗑</text></view>
      </view>
      <view v-else class="card empty-card">
        <text class="empty-text">暂无绑定设备，请点击上方「添加新设备」开始配对</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useDeviceStore } from '../../stores/device'
import { mockDevice } from '../../mock/device'

// MOCK 数据加载
// 替换计划: 接 device-service getDevices，解绑接 device-service 解绑接口
const deviceStore = useDeviceStore()
const device = ref(deviceStore.currentDevice)
const wifiSSID = ref('')
const wifiPassword = ref('')
const steps = ['打开手机蓝牙', '选择目标设备', '输入WiFi网络信息', '等待配对完成']

onMounted(() => {
  if (!device.value) {
    const mockDev = mockDevice()
    deviceStore.setDevice(mockDev)
    device.value = mockDev
  }
})

function goWifiSetup() {
  uni.navigateTo({ url: '/pages/wifi-setup/index' })
}

function confirmUnbind() {
  uni.showModal({
    title: '确认解绑',
    content: '确定要解除当前设备绑定吗？解绑后需重新配对。',
    confirmColor: '#ef4444',
    success: (res) => {
      if (res.confirm) {
        deviceStore.clearDevice()
        device.value = null
        uni.showToast({ title: '设备已解绑', icon: 'success' })
      }
    },
  })
}
</script>

<style scoped>
.page { padding-bottom: 180rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 0 40rpx; margin-top: 32rpx; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 20rpx; display: block; letter-spacing: 0.6rpx; }
.add-card { background: #eff6ff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 56rpx 48rpx; text-align: center; }
.add-icon-box { width: 112rpx; height: 112rpx; border-radius: 24rpx; background: #fff; border: 1rpx solid #e2e8f0; display: inline-flex; align-items: center; justify-content: center; margin-bottom: 28rpx; }
.add-icon { font-size: 56rpx; color: #2563EB; line-height: 1; }
.add-title { display: block; font-size: 32rpx; font-weight: 500; color: #1e293b; margin-bottom: 12rpx; }
.add-desc { display: block; font-size: 26rpx; color: #94a3b8; line-height: 1.5; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; margin-bottom: 16rpx; }
.steps-card { padding: 8rpx 32rpx; }
.step-row { display: flex; align-items: center; gap: 28rpx; padding: 24rpx 0; }
.step-divider { height: 1rpx; background: #e2e8f0; margin: 0 0 0 84rpx; }
.step-num { width: 56rpx; height: 56rpx; border-radius: 50%; background: #dbeafe; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.step-num text { font-size: 26rpx; font-weight: 500; color: #2563EB; }
.step-text { font-size: 28rpx; color: #1e293b; }
.input-group { margin-bottom: 24rpx; }
.input-group:last-child { margin-bottom: 0; }
.input-label { font-size: 26rpx; color: #94a3b8; display: block; margin-bottom: 12rpx; }
.app-input { width: 100%; box-sizing: border-box; height: 88rpx; line-height: 86rpx; padding: 0 28rpx; border: 1rpx solid #e2e8f0; border-radius: 24rpx; font-size: 30rpx; color: #1e293b; background: #faf7f4; }
.action-btn { width: 100%; height: 88rpx; background: #2563EB; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; }
.action-btn text { color: #fff; font-size: 30rpx; }
.device-section { padding-bottom: 40rpx; }
.device-card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx 32rpx; display: flex; align-items: center; gap: 28rpx; }
.device-icon-box { width: 88rpx; height: 88rpx; border-radius: 24rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.device-icon { font-size: 44rpx; }
.device-info { flex: 1; }
.device-name { display: block; font-size: 30rpx; font-weight: 500; color: #1e293b; margin-bottom: 6rpx; }
.device-status { display: flex; align-items: center; gap: 12rpx; }
.dot { width: 12rpx; height: 12rpx; border-radius: 50%; flex-shrink: 0; }
.dot-blue { background: #2563EB; }
.dot-gray { background: #9ca3af; }
.device-status-text { font-size: 26rpx; color: #2563EB; }
.delete-btn { width: 72rpx; height: 72rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.delete-icon { font-size: 36rpx; }
.empty-card { text-align: center; }
.empty-text { font-size: 26rpx; color: #94a3b8; }
</style>