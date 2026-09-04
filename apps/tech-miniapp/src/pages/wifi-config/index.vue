<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goBack">← 返回</text>
      <text class="page-title">WiFi 配置</text>
    </view>

    <!-- 配网状态步骤条 -->
    <view class="stepper">
      <template v-for="(label, i) in wifiSteps" :key="i">
        <view :class="['step', stepStatus(i)]">
          <view class="step-circle">
            <text v-if="stepStatus(i) !== 'done'">{{ i + 1 }}</text>
            <text v-else>✓</text>
          </view>
          <text class="step-label">{{ label }}</text>
        </view>
        <view v-if="i < wifiSteps.length - 1" :class="['step-line', { 'step-line-done': isStepDone(i) }]"></view>
      </template>
    </view>

    <!-- 成功态 -->
    <view v-if="wifiStatusCode === 9" class="section">
      <view class="success-card">
        <text class="success-icon">✓</text>
        <text class="success-title">配网成功</text>
        <text class="success-sub">WiFi 已连接，数据可达性验证通过</text>
        <text class="auto-return-tip">3 秒后自动返回安装流程...</text>
      </view>
    </view>

    <!-- 错误态 -->
    <view v-else-if="errorCode !== null" class="section">
      <view class="error-card">
        <text class="error-icon">!</text>
        <text class="error-title">{{ errorMessage }}</text>
        <view class="btn-outline" @click="retryWifi"><text>重新配网</text></view>
        <view v-if="errorCode === -4" class="skip-hint">
          <text class="skip-text">WiFi 已连接，但暂时无法连接云端。</text>
          <view class="btn-outline-sm" @click="skipReachability"><text>先完成安装</text></view>
        </view>
      </view>
    </view>

    <!-- 配网前 + 配网中 -->
    <template v-else>
      <view class="section">
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

      <view class="section">
        <view class="card">
          <view class="form-group">
            <text class="form-label">WiFi 密码</text>
            <view class="password-wrap">
              <input
                class="form-input password-input"
                :password="!showPassword"
                placeholder="请输入WiFi密码"
                v-model="password"
              />
              <text class="pwd-toggle" @click="showPassword = !showPassword">
                {{ showPassword ? '🙈' : '👁' }}
              </text>
            </view>
          </view>
          <view :class="['btn-primary', { 'btn-disabled': provisioning }]" @click="startWifiConfig">
            <text>{{ provisioning ? '配置中...' : '开始配网' }}</text>
          </view>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onUnmounted } from 'vue'
import { useInstallStore } from '../../stores/install'
import { getProvisionKey } from '../../api/provision'
import { setDeviceWifi } from '../../api/device'
import { encryptWifiPayload } from '../../utils/aes-ctr'
import {
  writeWifiConfigV2,
  onWifiStatus,
  startMockWifiStatusSequence,
  stopMockWifiStatusSequence,
} from '../../utils/ble'

const installStore = useInstallStore()

const wifiSteps = ['收到', '连AP', '取IP', '探测', '成功']
const wifiList = ref([
  { ssid: 'Hospital_5G', signal: '强', secure: true },
  { ssid: 'Hospital_Guest', signal: '中', secure: true },
])
const manualSSID = ref('')
const password = ref('')
const showPassword = ref(false)
const selectedSSID = ref('')
const provisioning = ref(false)

const wifiStatusCode = ref<number | null>(null)
const errorCode = ref<number | null>(null)
const autoReturnTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const timeoutTimer = ref<ReturnType<typeof setTimeout> | null>(null)
let statusListener: ((code: number) => void) | null = null

const errorMessage = computed(() => {
  const map: Record<number, string> = {
    [-1]: '密码错误，请检查 WiFi 密码',
    [-2]: '未找到 WiFi 网络，请检查 SSID',
    [-3]: '网络连接失败（DHCP），请检查路由器',
    [-4]: 'WiFi 已连接但云端暂时不可达，设备会自动重试',
  }
  return map[errorCode.value!] || '配网失败，请重试'
})

function stepStatus(i: number): string {
  // i: 0-4 对应 步骤1-5
  const code = wifiStatusCode.value
  if (code === null) return i === 0 ? 'active' : 'pending'
  if (code === 9) return 'done'
  if (code < 0) return i <= 3 ? 'done' : 'error'
  // 0/1/2/3
  if (i < code) return 'done'
  if (i === code) return 'active'
  return 'pending'
}
function isStepDone(i: number): boolean {
  const code = wifiStatusCode.value
  if (code === null) return false
  if (code === 9) return true
  if (code < 0) return i < 4
  return i < code
}

function selectWifi(ssid: string) {
  selectedSSID.value = ssid
  manualSSID.value = ''
}
function goBack() {
  uni.navigateBack()
}

async function startWifiConfig() {
  const ssid = manualSSID.value || selectedSSID.value
  if (!ssid) {
    uni.showToast({ title: '请选择或输入 WiFi 网络', icon: 'none' })
    return
  }
  if (!password.value) {
    uni.showToast({ title: '请输入 WiFi 密码', icon: 'none' })
    return
  }
  provisioning.value = true
  wifiStatusCode.value = null
  errorCode.value = null

  try {
    // 1. 申领 provision-key（真实 API）
    const { provision_key_hex } = await getProvisionKey(installStore.deviceId)

    // 2. AES-CTR 加密 WiFi 凭据
    const seq = Math.floor(Math.random() * 0xffffffff)
    const encrypted = await encryptWifiPayload(ssid, password.value, provision_key_hex, seq)

    // 3. BLE 写入加密配置
    await writeWifiConfigV2(installStore.deviceId, encrypted)

    // 4. 监听配网状态
    statusListener = (code: number) => {
      wifiStatusCode.value = code
      if (code === 9) handleSuccess(ssid)
      else if (code < 0) handleError(code)
    }
    onWifiStatus(statusListener)

    // H5 mock：启动状态机序列
    // T089-MOCK: 真机由硬件 WiFi Status Notify 驱动
    startMockWifiStatusSequence()

    // 30s 超时
    timeoutTimer.value = setTimeout(() => {
      if (wifiStatusCode.value !== 9) {
        handleTimeout()
      }
    }, 30000)
  } catch (e) {
    uni.showToast({ title: e instanceof Error ? e.message : '配网失败', icon: 'none' })
    provisioning.value = false
  }
}

async function handleSuccess(ssid: string) {
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  provisioning.value = false

  // 云端 WiFi 状态回写（mock 先行）
  try {
    await setDeviceWifi(installStore.deviceId, ssid)
  } catch (e) {
    uni.showToast({ title: 'WiFi 状态同步失败', icon: 'none' })
  }

  installStore.setWifiStatus('connected')
  installStore.updateWifiStatusCode(9)
  installStore.setReachabilityVerified('verified')

  // 3 秒后自动返回 install
  autoReturnTimer.value = setTimeout(() => {
    uni.navigateBack()
  }, 3000)
}

function handleError(code: number) {
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  provisioning.value = false
  errorCode.value = code
}

function handleTimeout() {
  // P2-5: 迟到状态 9 回转——继续监听，不立即标失败
  // 这里给提示，但保留 statusListener（未移除），迟到状态 9 仍可触发 handleSuccess
  uni.showToast({ title: '配网超时，等待设备响应中...', icon: 'none' })
}

function retryWifi() {
  wifiStatusCode.value = null
  errorCode.value = null
  selectedSSID.value = ''
  password.value = ''
}

function skipReachability() {
  // -4 状态：标记可达性 skipped，返回 install
  installStore.setWifiStatus('connected')
  installStore.setReachabilityVerified('skipped')
  uni.navigateBack()
}

onUnmounted(() => {
  if (autoReturnTimer.value) clearTimeout(autoReturnTimer.value)
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.back-link { font-size: 28rpx; color: #2563EB; display: block; margin-bottom: 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }

/* 步骤条 */
.stepper { display: flex; align-items: center; justify-content: center; padding: 24rpx 48rpx; }
.step { display: flex; flex-direction: column; align-items: center; position: relative; z-index: 1; }
.step-circle { width: 52rpx; height: 52rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 24rpx; font-weight: 600; color: #fff; transition: all 0.3s; }
.step-done .step-circle { background: #10B981; }
.step-active .step-circle { background: #3B82F6; }
.step-pending .step-circle { background: #e5e7eb; color: #9ca3af; }
.step-error .step-circle { background: #ef4444; }
.step-label { font-size: 20rpx; margin-top: 6rpx; font-weight: 500; }
.step-done .step-label { color: #10B981; }
.step-active .step-label { color: #3B82F6; }
.step-pending .step-label { color: #9ca3af; }
.step-error .step-label { color: #ef4444; }
.step-line { flex: 1; height: 4rpx; margin: 0 6rpx 34rpx; background: #e5e7eb; }
.step-line-done { background: #10B981; }

.notice { display: flex; align-items: flex-start; gap: 12rpx; padding: 20rpx; background: #f0f9ff; border-radius: 12rpx; margin-bottom: 20rpx; }
.notice-icon { font-size: 28rpx; }
.notice-text { font-size: 24rpx; color: #0369a1; line-height: 1.5; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 16rpx; display: block; }
.wifi-list { display: flex; flex-direction: column; gap: 12rpx; margin-bottom: 20rpx; }
.wifi-item { background: #f8fafc; border: 1rpx solid #e2e8f0; border-radius: 12rpx; padding: 20rpx 24rpx; display: flex; align-items: center; justify-content: space-between; }
.wifi-selected { border-color: #3B82F6; background: #eff6ff; }
.wifi-info { display: flex; flex-direction: column; gap: 4rpx; }
.wifi-ssid { font-size: 28rpx; color: #1e293b; }
.wifi-signal { font-size: 20rpx; color: #94a3b8; }
.wifi-secure { font-size: 24rpx; }
.manual-wifi { margin-top: 16rpx; }
.form-group { margin-bottom: 20rpx; }
.form-label { font-size: 26rpx; color: #64748b; display: block; margin-bottom: 12rpx; }
.form-input { width: 100%; height: 76rpx; line-height: 76rpx; padding: 0 24rpx; border: 1rpx solid #e2e8f0; border-radius: 12rpx; font-size: 28rpx; color: #1e293b; background: #f8fafc; box-sizing: border-box; }
.password-wrap { display: flex; align-items: center; border: 1rpx solid #e2e8f0; border-radius: 12rpx; background: #f8fafc; padding-right: 20rpx; }
.password-input { border: none; }
.pwd-toggle { font-size: 28rpx; padding: 0 8rpx; }
.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-disabled { opacity: 0.5; }
.btn-outline { width: 100%; padding: 22rpx 0; background: #fff; border: 2rpx solid #3B82F6; border-radius: 16rpx; text-align: center; margin-top: 20rpx; }
.btn-outline text { color: #3B82F6; font-size: 28rpx; font-weight: 500; }
.btn-outline-sm { padding: 16rpx 0; background: #fff; border: 2rpx solid #f59e0b; border-radius: 12rpx; text-align: center; margin-top: 16rpx; }
.btn-outline-sm text { color: #b45309; font-size: 26rpx; }

.success-card { background: #f0fdf4; border: 1rpx solid #bbf7d0; border-radius: 24rpx; padding: 64rpx 32rpx; text-align: center; }
.success-icon { display: inline-flex; width: 120rpx; height: 120rpx; border-radius: 50%; background: #10B981; color: #fff; font-size: 64rpx; align-items: center; justify-content: center; }
.success-title { display: block; font-size: 36rpx; font-weight: 600; color: #166534; margin-top: 24rpx; }
.success-sub { display: block; font-size: 26rpx; color: #047857; margin-top: 12rpx; }
.auto-return-tip { display: block; font-size: 24rpx; color: #6b7280; margin-top: 24rpx; }

.error-card { background: #fef2f2; border: 1rpx solid #fecaca; border-radius: 24rpx; padding: 48rpx 32rpx; text-align: center; }
.error-icon { display: inline-flex; width: 80rpx; height: 80rpx; border-radius: 50%; background: #ef4444; color: #fff; font-size: 48rpx; align-items: center; justify-content: center; }
.error-title { display: block; font-size: 30rpx; color: #991b1b; margin: 24rpx 0; line-height: 1.5; }
.skip-hint { margin-top: 32rpx; }
.skip-text { display: block; font-size: 24rpx; color: #b45309; line-height: 1.5; }
</style>

