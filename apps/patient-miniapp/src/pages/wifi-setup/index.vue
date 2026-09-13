<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goBack">← 设备管理</text>
      <text class="header-title">WiFi 配网</text>
    </view>

    <!-- 配网状态步骤条（技师端已验：0→1→2→3→9） -->
    <view class="section">
      <view class="steps">
        <template v-for="(s, i) in wifiSteps" :key="i">
          <view :class="['step', stepClass(i)]">
            <view class="step-circle">
              <text v-if="stepClass(i) !== 'step-done'">{{ i + 1 }}</text>
              <text v-else>✓</text>
            </view>
            <text class="step-label">{{ s }}</text>
          </view>
          <view v-if="i < wifiSteps.length - 1" :class="['step-line', { 'step-line-done': isStepDone(i) }]"></view>
        </template>
      </view>
    </view>

    <!-- 成功态 -->
    <view v-if="wifiStatusCode === 9" class="section">
      <view class="card success-card">
        <text class="success-icon">✅</text>
        <text class="success-text">配网成功</text>
        <text class="success-sub">WiFi 已连接，数据可达性验证通过</text>
        <text class="auto-return-tip">3 秒后自动返回设备管理...</text>
      </view>
    </view>

    <!-- 错误态 -->
    <view v-else-if="errorCode !== null" class="section">
      <view class="card error-card">
        <text class="error-icon">!</text>
        <text class="error-title">{{ errorMessage }}</text>
        <view class="btn-outline" @click="retryWifi"><text>重新配网</text></view>
        <view v-if="errorCode === -4" class="skip-hint">
          <text class="skip-text">WiFi 已连接，但暂时无法连接云端。</text>
          <view class="btn-outline-sm" @click="skipNetworkSetup"><text>先跳过</text></view>
        </view>
      </view>
    </view>

    <!-- 配网前 + 配网中 -->
    <template v-else>
      <view class="section">
        <view class="card">
          <view class="notice">
            <text class="notice-icon">ℹ️</text>
            <view class="notice-text">
              <text>请确保设备已开机且蓝牙信号良好</text>
            </view>
          </view>
          <view class="manual-wifi">
            <text class="form-label">WiFi 名称 (SSID)</text>
            <input class="form-input" type="text" placeholder="请输入 WiFi 名称" v-model="manualSSID" />
          </view>
          <view class="wifi-hint">
            <text>仅支持 2.4GHz Wi-Fi 网络，不支持 5GHz（含 5G 频段的合一路由器请填 2.4G 那个网络名）</text>
            <text>请手动输入准确的 WiFi 名称与密码</text>
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
                placeholder="请输入 WiFi 密码"
                v-model="wifiPassword"
              />
              <text class="password-toggle" @click="showPassword = !showPassword">{{ showPassword ? '👁' : '🙈' }}</text>
            </view>
          </view>
          <view class="btn-primary" :class="{ 'btn-disabled': provisioning }" @click="startWifiConfig">
            <text>{{ provisioning ? '配置中...' : '开始配网' }}</text>
          </view>
        </view>
      </view>
    </template>

    <view style="padding-bottom: 100rpx;"></view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useDeviceStore } from '../../stores/device'
import { getProvisionKey } from '../../api/provision'
import { reportDeviceWifi } from '../../api/device'
import { encryptWifiPayload } from '../../utils/aes-ctr'
import {
  initBluetooth,
  connectDevice,
  closeBLEConnection,
  writeWifiConfigV2,
  onWifiStatus,
  registerBleStateListener,
  startMockWifiStatusSequence,
  stopMockWifiStatusSequence,
} from '../../utils/ble'

const deviceStore = useDeviceStore()

// 配网步骤（技师端已验：0收到→1连AP→2取IP→3探测→9成功）
const wifiSteps = ['收到', '连AP', '取IP', '探测', '成功']

const manualSSID = ref('')
const wifiPassword = ref('')
const showPassword = ref(false)
const provisioning = ref(false)

// T119: 3s 防重入节流
const MIN_PROVISION_INTERVAL = 3000
let lastProvisionAttempt = 0

// 配网状态
const wifiStatusCode = ref<number | null>(null)
const statusHistory: number[] = []
const errorCode = ref<number | null>(null)
const autoReturnTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const timeoutTimer = ref<ReturnType<typeof setTimeout> | null>(null)
let successProcessed = false
let bleMac = ''

const errorMessage = computed(() => {
  const map: Record<number, string> = {
    [-1]: '密码错误，请检查 WiFi 密码',
    [-2]: '未找到 WiFi 网络，请检查 SSID',
    [-3]: '网络连接失败（DHCP），请检查路由器',
    [-4]: '云端暂不可达，设备将在后台持续重试（约每 5 分钟一次），请保持设备通电与 WiFi 环境',
  }
  return map[errorCode.value!] || '配网失败，请重试'
})

function stepClass(i: number): string {
  const code = wifiStatusCode.value
  if (code === null) return i === 0 ? 'step-active' : ''
  if (code === 9) return 'step-done'
  if (code < 0) return i <= 3 ? 'step-done' : 'step-error'
  // 0/1/2/3
  if (i < code) return 'step-done'
  if (i === code) return 'step-active'
  return ''
}

function isStepDone(i: number): boolean {
  const code = wifiStatusCode.value
  if (code === null) return false
  if (code === 9) return true
  if (code < 0) return i < 4
  return i < code
}

function goBack() {
  if (autoReturnTimer.value) clearTimeout(autoReturnTimer.value)
  uni.navigateBack({
    fail: () => uni.switchTab({ url: '/pages/device/index' }),
  })
}

onMounted(() => {
  // 注册 BLE 断连监听
  registerBleStateListener((deviceId, connected) => {
    if (!connected && provisioning.value) {
      provisioning.value = false
      uni.showToast({ title: '设备已断开连接', icon: 'none' })
    }
    deviceStore.setBleConnected(connected)
  })
})

async function startWifiConfig() {
  // 3s 防重入节流
  const now = Date.now()
  if (now - lastProvisionAttempt < MIN_PROVISION_INTERVAL) {
    const waitSec = Math.ceil((MIN_PROVISION_INTERVAL - (now - lastProvisionAttempt)) / 1000)
    uni.showToast({ title: `操作过于频繁，请${waitSec}秒后重试`, icon: 'none' })
    return
  }
  lastProvisionAttempt = now

  // 参数校验
  const ssid = manualSSID.value.trim()
  if (!ssid) {
    uni.showToast({ title: '请输入 WiFi 名称', icon: 'none' })
    return
  }
  if (!wifiPassword.value) {
    uni.showToast({ title: '请输入 WiFi 密码', icon: 'none' })
    return
  }

  // BLE 标识：优先 store，fallback 用设备 ID，再 fallback 硬编码 mock 值（H5/E2E）
  const DEVICE_ID_FALLBACK = 'PRS-ML05-RC-20260701001'
  const deviceId = deviceStore.currentDevice?.id || DEVICE_ID_FALLBACK
  bleMac = deviceStore.bleDeviceId || deviceStore.currentDevice?.id || DEVICE_ID_FALLBACK

  if (!ssid) { uni.showToast({ title: '请输入 WiFi 名称', icon: 'none' }); return }
  if (!wifiPassword.value) { uni.showToast({ title: '请输入 WiFi 密码', icon: 'none' }); return }

  provisioning.value = true
  wifiStatusCode.value = null
  errorCode.value = null
  successProcessed = false
  statusHistory.length = 0

  try {
    // 1. 蓝牙初始化 + BLE 连接
    //    H5 环境下跳过（BLE 不可用），直接走 mock 状态机
    const IS_H5 = false
    // #ifdef H5
    IS_H5 = true
    // #endif
    if (!IS_H5) {
      uni.showLoading({ title: '连接设备中...' })
      await initBluetooth()
      await connectDevice(bleMac)
      deviceStore.setBleConnected(true)
      uni.hideLoading()
    }

    // 2. 申领 provision-key（内存缓存 + 60s 失效）
    const { provision_key_hex } = await getProvisionKey(deviceId)

    // 3. AES-128-CTR 加密 WiFi 凭据
    const seq = deviceStore.nextWifiSeq()
    const encrypted = encryptWifiPayload(ssid, wifiPassword.value, provision_key_hex, seq)

    // 4. B511 分片写入加密配置（H5 mock 直接 return true）
    await writeWifiConfigV2(bleMac, encrypted)

    // 5. 监听 B512 配网状态（0→1→2→3→9 或 -1~-4）
    onWifiStatus((code: number) => {
      wifiStatusCode.value = code
      statusHistory.push(code)
      deviceStore.updateWifiStatusCode(code)
      if (code === 9) handleSuccess(ssid)
      else if (code < 0) handleError(code)
    }, bleMac)

    // 6. H5 mock：真机由硬件 B512 Notify 驱动
    startMockWifiStatusSequence()

    // 7. 15s 超时兜底（固件解密失败不 Notify，无响应时提示重试）
    timeoutTimer.value = setTimeout(() => {
      if (wifiStatusCode.value !== 9) {
        handleTimeout()
      }
    }, 15000)
  } catch (e) {
    uni.hideLoading()
    const msg = e instanceof Error ? e.message : '配网失败'
    uni.showToast({ title: msg, icon: 'none' })
    provisioning.value = false
    // 清理 BLE 连接
    try { await closeBLEConnection(bleMac) } catch {}
  }
}

async function handleSuccess(ssid: string) {
  // 固件可能 Notify 两次 9，第二次忽略
  if (successProcessed) return
  successProcessed = true
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  provisioning.value = false

  // 云端 WiFi 状态回写（对齐技师端 POST /devices/:id/wifi { ssid }）
  try {
    const deviceId = deviceStore.currentDevice?.id || ''
    if (deviceId) {
      await reportDeviceWifi(deviceId, ssid)
    }
  } catch (e) {
    // 回写失败不阻断配网成功状态，仅提示
    uni.showToast({ title: 'WiFi 状态同步失败', icon: 'none' })
  }

  deviceStore.setWifiStatus('connected')

  // 关闭 BLE 连接（配网完成，设备已脱离手机连 WiFi）
  try { await closeBLEConnection(bleMac) } catch {}
  deviceStore.setBleConnected(false)

  // 3 秒后自动返回
  autoReturnTimer.value = setTimeout(() => {
    uni.navigateBack({ fail: () => uni.switchTab({ url: '/pages/device/index' }) })
  }, 3000)
}

function handleError(code: number) {
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  provisioning.value = false
  errorCode.value = code
  deviceStore.setWifiStatus('failed')
}

function handleTimeout() {
  // 15s 超时 — 给提示但保留状态监听，迟到状态 9 仍可触发 handleSuccess
  uni.showToast({ title: '设备无响应，请靠近设备后重试', icon: 'none' })
  provisioning.value = false
}

function retryWifi() {
  wifiStatusCode.value = null
  errorCode.value = null
  successProcessed = false
  statusHistory.length = 0
  // 清理可能残留的 BLE 状态
  stopMockWifiStatusSequence()
  try { closeBLEConnection(bleMac) } catch {}
}

function skipNetworkSetup() {
  // -4 状态：云端不可达，本地标记 connected 但不落库
  deviceStore.setWifiStatus('connected')
  uni.navigateBack({ fail: () => uni.switchTab({ url: '/pages/device/index' }) })
}

onUnmounted(() => {
  if (autoReturnTimer.value) clearTimeout(autoReturnTimer.value)
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  // 页面离开时关闭 BLE 连接
  if (bleMac) {
    try { closeBLEConnection(bleMac) } catch {}
  }
})
</script>

<style scoped>
.page { padding-bottom: 100rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; display: flex; align-items: baseline; gap: 24rpx; }
.back-link { font-size: 28rpx; color: #94a3b8; }
.header-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); padding: 32rpx; }

/* 步骤条 */
.steps { display: flex; align-items: flex-start; justify-content: center; padding: 16rpx 0; }
.step { display: flex; flex-direction: column; align-items: center; gap: 12rpx; }
.step-circle { width: 56rpx; height: 56rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; border: 4rpx solid #e2e8f0; background: #fff; }
.step-circle text { font-size: 26rpx; font-weight: 500; color: #94a3b8; }
.step-done .step-circle { background: #2563EB; border-color: #2563EB; }
.step-done .step-circle text { color: #fff; }
.step-active .step-circle { border-color: #2563EB; }
.step-active .step-circle text { color: #2563EB; }
.step-error .step-circle { background: #ef4444; border-color: #ef4444; }
.step-label { font-size: 20rpx; color: #94a3b8; white-space: nowrap; }
.step-done .step-label { color: #2563EB; }
.step-active .step-label { color: #2563EB; font-weight: 500; }
.step-error .step-label { color: #ef4444; }
.step-line { flex: 1; height: 4rpx; background: #e2e8f0; margin: 26rpx 8rpx 0; min-width: 24rpx; }
.step-line-done { background: #2563EB; }

.notice { display: flex; gap: 20rpx; padding: 24rpx; background: #eff6ff; border-radius: 16rpx; margin-bottom: 24rpx; }
.notice-icon { font-size: 32rpx; flex-shrink: 0; }
.notice-text { font-size: 24rpx; color: #475569; line-height: 1.6; flex: 1; }
.manual-wifi { margin-top: 16rpx; }
.wifi-hint { display: flex; flex-direction: column; gap: 8rpx; margin-top: 20rpx; padding: 20rpx 24rpx; background: #fff7ed; border-radius: 16rpx; }
.wifi-hint text { font-size: 22rpx; color: #b45309; line-height: 1.5; }
.form-label { font-size: 26rpx; font-weight: 500; color: #1e293b; margin-bottom: 12rpx; display: block; }
.form-input { width: 100%; padding: 16rpx 24rpx; border: 1rpx solid #e2e8f0; border-radius: 24rpx; font-size: 26rpx; color: #1e293b; background: #f1f5f9; box-sizing: border-box; }
.form-group { margin-bottom: 24rpx; }
.password-wrap { position: relative; }
.password-input { padding-left: 24rpx; padding-right: 72rpx; }
.password-toggle { position: absolute; right: 16rpx; top: 50%; transform: translateY(-50%); font-size: 32rpx; }
.btn-primary { padding: 20rpx 40rpx; background: #2563EB; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; }
.btn-primary text { color: #fff; font-size: 28rpx; }
.btn-disabled { opacity: 0.5; }

.success-card { text-align: center; padding: 64rpx 32rpx; }
.success-icon { font-size: 96rpx; display: block; margin-bottom: 16rpx; }
.success-text { font-size: 36rpx; font-weight: 500; color: #2563EB; display: block; margin-bottom: 8rpx; }
.success-sub { font-size: 26rpx; color: #64748b; display: block; margin-top: 8rpx; }
.auto-return-tip { font-size: 24rpx; color: #94a3b8; margin-top: 24rpx; display: block; }

.error-card { padding: 48rpx 32rpx; text-align: center; }
.error-icon { display: inline-flex; width: 80rpx; height: 80rpx; border-radius: 50%; background: #ef4444; color: #fff; font-size: 48rpx; align-items: center; justify-content: center; margin-bottom: 24rpx; }
.error-title { font-size: 28rpx; color: #991b1b; line-height: 1.5; }
.btn-outline { width: 100%; padding: 22rpx 0; background: #fff; border: 2rpx solid #3B82F6; border-radius: 16rpx; text-align: center; margin-top: 20rpx; }
.btn-outline text { color: #3B82F6; font-size: 28rpx; font-weight: 500; }
.btn-outline-sm { padding: 16rpx 0; background: #fff; border: 2rpx solid #f59e0b; border-radius: 12rpx; text-align: center; margin-top: 16rpx; }
.btn-outline-sm text { color: #b45309; font-size: 26rpx; }
.skip-hint { margin-top: 24rpx; }
.skip-text { display: block; font-size: 24rpx; color: #b45309; line-height: 1.5; }
</style>
