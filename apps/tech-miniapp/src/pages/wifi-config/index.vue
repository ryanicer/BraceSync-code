<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goBack">← 返回</text>
      <text class="page-title">WiFi 配置</text>
    </view>

    <!-- 步骤条 -->
    <view class="section">
      <view class="steps">
        <template v-for="(s, i) in stepList" :key="i">
          <view :class="['step', stepClass(i)]">
            <view class="step-circle"><text>{{ i + 1 }}</text></view>
            <text class="step-label">{{ s }}</text>
          </view>
          <view v-if="i < stepList.length - 1" :class="['step-line', { 'step-line-done': currentStep > i + 1 }]"></view>
        </template>
      </view>
    </view>

    <!-- WiFi 选择与输入 -->
    <view v-if="currentStep <= 2" class="section">
      <view class="card">
        <view class="notice">
          <text class="notice-icon">ℹ️</text>
          <text class="notice-text">请确保手机已连接到目标 WiFi 网络，或通过 BLE 将 WiFi 信息写入设备。</text>
        </view>

        <text class="section-title">选择 WiFi 网络</text>
        <view class="wifi-list">
          <view
            v-for="wifi in wifiList"
            :key="wifi.ssid"
            :class="['wifi-item', { 'wifi-selected': selectedSSID === wifi.ssid }]"
            @click="selectWifi(wifi.ssid)"
          >
            <view class="wifi-info">
              <text class="wifi-ssid">{{ wifi.ssid }}</text>
              <text class="wifi-signal">{{ wifi.signal }}</text>
            </view>
            <text class="wifi-secure">{{ wifi.secure ? '🔒' : '' }}</text>
          </view>
        </view>

        <view class="manual-wifi">
          <text class="form-label">或手动输入 SSID</text>
          <input class="form-input" type="text" placeholder="WiFi名称" v-model="manualSSID" />
        </view>
      </view>
    </view>

    <view v-if="currentStep <= 2" class="section">
      <view class="card">
        <view class="form-group">
          <text class="form-label">WiFi 密码</text>
          <view class="password-wrap">
            <input
              class="form-input password-input"
              :password="!showPassword"
              placeholder="请输入WiFi密码"
              v-model="wifiPassword"
            />
            <text class="password-toggle" @click="showPassword = !showPassword">{{ showPassword ? '👁' : '🙈' }}</text>
          </view>
        </view>
        <view class="btn-primary" @click="startConfig"><text>开始配网</text></view>
      </view>
    </view>

    <!-- 配网进度 -->
    <view v-if="currentStep === 3" class="section">
      <view class="card progress-card">
        <text class="progress-icon">{{ configDone ? '✅' : '⏳' }}</text>
        <text class="progress-text">{{ configDone ? '配置完成' : '正在配置 WiFi...' }}</text>
        <view class="progress-bar">
          <view class="progress-fill" :style="{ width: progressPercent + '%' }"></view>
        </view>
        <text class="progress-step">{{ progressStepText }}</text>
      </view>
    </view>

    <!-- 配网成功 -->
    <view v-if="currentStep === 4" class="section">
      <view class="card progress-card">
        <text class="success-icon">✅</text>
        <text class="success-text">配网成功!</text>
        <text class="success-sub">设备已成功连接到 WiFi: {{ selectedSSID || manualSSID }}</text>
        <view class="btn-primary" style="margin-top: 24rpx;" @click="goBack"><text>返回安装流程</text></view>
      </view>
    </view>

    <view style="padding-bottom: 100rpx;"></view>
  </view>
</template>

<script setup lang="ts">
import { ref, onUnmounted } from 'vue'
import { initBluetooth, createBLEConnection, writeWiFiConfig } from '../../utils/ble'

const stepList = ['连接设备', '选择WiFi', '写入配置', '完成']
const currentStep = ref(1)
const selectedSSID = ref('Home_WiFi_5G')
const manualSSID = ref('')
const wifiPassword = ref('')
const showPassword = ref(false)
const configDone = ref(false)
const progressPercent = ref(0)
const progressStepText = ref('')

let configTimer: ReturnType<typeof setInterval> | null = null

// Mock WiFi 列表 - 替换计划: 真机环境可用 uni.getWifiList 扫描
const wifiList = ref([
  { ssid: 'Home_WiFi_5G', signal: '●●●● 信号强', secure: true },
  { ssid: 'Office_Net', signal: '●●● 信号中等', secure: true },
  { ssid: 'Guest_WiFi', signal: '●● 信号较弱', secure: false },
])

function stepClass(i: number): string {
  if (i + 1 < currentStep.value) return 'step-done'
  if (i + 1 === currentStep.value) return 'step-active'
  return ''
}

function goBack() {
  uni.navigateBack()
}

function selectWifi(ssid: string) {
  selectedSSID.value = ssid
  manualSSID.value = ''
}

async function startConfig() {
  const ssid = manualSSID.value || selectedSSID.value
  if (!ssid) {
    uni.showToast({ title: '请选择或输入WiFi名称', icon: 'none' })
    return
  }

  // BLE 配网流程（H5 dev 模式走 mock，真机走 uni.createBLEConnection）
  try {
    uni.showLoading({ title: '连接设备中...' })
    await initBluetooth()
    await createBLEConnection('PRS-ML05-RC-001')
    uni.hideLoading()
  } catch (e) {
    uni.hideLoading()
    uni.showToast({ title: '设备连接已断开，请靠近设备后重试', icon: 'none' })
    return
  }

  currentStep.value = 3
  configDone.value = false
  progressPercent.value = 0
  progressStepText.value = '连接设备热点'

  const stepsText = ['连接设备热点', '验证WiFi密码', '发送配置到设备', '设备连接中', '完成绑定']
  let step = 0

  configTimer = setInterval(async () => {
    step++
    if (step >= stepsText.length) {
      if (configTimer) clearInterval(configTimer)
      configTimer = null
      configDone.value = true
      progressPercent.value = 100
      progressStepText.value = '绑定成功'
      setTimeout(() => { currentStep.value = 4 }, 800)
      return
    }
    if (step === 2) {
      try {
        await writeWiFiConfig(ssid, wifiPassword.value)
      } catch (e) {
        if (configTimer) clearInterval(configTimer)
        configTimer = null
        currentStep.value = 1
        uni.showToast({ title: '配置写入失败，请重试', icon: 'none' })
        return
      }
    }
    progressPercent.value = (step / stepsText.length) * 100
    progressStepText.value = stepsText[step]
  }, 1200)
}

onUnmounted(() => {
  if (configTimer) clearInterval(configTimer)
})
</script>

<style scoped>
.page { padding-bottom: 100rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; display: flex; align-items: baseline; gap: 24rpx; }
.back-link { font-size: 28rpx; color: #94a3b8; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 20rpx; display: block; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
/* Steps */
.steps { display: flex; align-items: flex-start; justify-content: center; padding: 16rpx 0; }
.step { display: flex; flex-direction: column; align-items: center; gap: 8rpx; }
.step-circle { width: 48rpx; height: 48rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; border: 3rpx solid #e2e8f0; background: #fff; }
.step-circle text { font-size: 22rpx; font-weight: 500; color: #94a3b8; }
.step-done .step-circle { background: #2563EB; border-color: #2563EB; }
.step-done .step-circle text { color: #fff; }
.step-active .step-circle { border-color: #2563EB; }
.step-active .step-circle text { color: #2563EB; }
.step-label { font-size: 18rpx; color: #94a3b8; white-space: nowrap; }
.step-line { flex: 1; height: 4rpx; background: #e2e8f0; margin: 22rpx 8rpx 0; min-width: 24rpx; }
.step-line-done { background: #2563EB; }
/* Notice */
.notice { display: flex; gap: 16rpx; padding: 20rpx; background: #eff6ff; border-radius: 16rpx; margin-bottom: 24rpx; }
.notice-icon { font-size: 28rpx; flex-shrink: 0; }
.notice-text { font-size: 24rpx; color: #475569; line-height: 1.6; }
/* WiFi */
.wifi-list { margin-bottom: 16rpx; }
.wifi-item { display: flex; align-items: center; justify-content: space-between; padding: 20rpx 24rpx; border: 1rpx solid #e2e8f0; border-radius: 16rpx; margin-bottom: 12rpx; }
.wifi-selected { border-color: #2563EB; background: #eff6ff; }
.wifi-info { display: flex; flex-direction: column; gap: 4rpx; }
.wifi-ssid { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.wifi-signal { font-size: 22rpx; color: #94a3b8; }
.wifi-secure { font-size: 28rpx; }
.manual-wifi { margin-top: 16rpx; }
.form-label { font-size: 26rpx; font-weight: 500; color: #1e293b; margin-bottom: 12rpx; display: block; }
.form-input { width: 100%; padding: 20rpx 24rpx; border: 1rpx solid #e2e8f0; border-radius: 16rpx; font-size: 28rpx; color: #1e293b; background: #f8fafc; }
.form-group { margin-bottom: 24rpx; }
.password-wrap { position: relative; }
.password-input { padding-right: 72rpx; }
.password-toggle { position: absolute; right: 16rpx; top: 50%; transform: translateY(-50%); font-size: 32rpx; }
.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
/* Progress */
.progress-card { text-align: center; }
.progress-icon { font-size: 80rpx; display: block; margin-bottom: 20rpx; }
.progress-text { font-size: 30rpx; color: #1e293b; font-weight: 500; display: block; margin-bottom: 8rpx; }
.progress-bar { width: 100%; height: 12rpx; background: #e2e8f0; border-radius: 6rpx; margin: 24rpx 0; overflow: hidden; }
.progress-fill { height: 100%; background: #2563EB; border-radius: 6rpx; transition: width 0.4s; }
.progress-step { font-size: 24rpx; color: #94a3b8; display: block; }
.success-icon { font-size: 96rpx; display: block; margin-bottom: 16rpx; }
.success-text { font-size: 36rpx; font-weight: 500; color: #2563EB; display: block; margin-bottom: 8rpx; }
.success-sub { font-size: 26rpx; color: #94a3b8; display: block; }
</style>
