<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goHome">← 返回</text>
      <text class="page-title">设备绑定</text>
      <text class="page-subtitle">扫码或手动输入设备 ID 进行绑定</text>
    </view>

    <template v-if="authStore.isLoggedIn">
      <!-- 扫码绑定 -->
      <view class="section">
        <view class="scan-card" @click="scanDevice">
          <view class="scan-icon-box"><text class="scan-icon">📷</text></view>
          <text class="scan-title">扫码绑定</text>
          <text class="scan-desc">扫描设备背面二维码快速绑定</text>
        </view>
      </view>

      <!-- 手动输入 -->
      <view class="section">
        <text class="section-title">手动输入设备 ID</text>
        <view class="card">
          <view class="form-group">
            <input class="form-input" type="text" placeholder="例: PRS-ML05-RC-001" v-model="manualDeviceId" />
          </view>
          <view class="form-group">
            <text class="form-label">患者 ID<span class="required">*</span></text>
            <input class="form-input" type="text" placeholder="例: pat-001" v-model="patientId" />
          </view>
          <view :class="['btn-primary', { 'btn-disabled': binding }]" @click="bindManual">
            <text>{{ binding ? '绑定中...' : '绑定设备' }}</text>
          </view>
        </view>
      </view>

      <!-- BLE 扫描 -->
      <view class="section">
        <view class="section-header">
          <text class="section-title">附近设备</text>
          <view class="refresh-btn" @click="scanBLE"><text>{{ scanning ? '扫描中...' : '刷新' }}</text></view>
        </view>
        <view v-if="scanResults.length > 0" class="device-list">
          <view
            v-for="item in scanResults"
            :key="item.deviceId"
            class="device-item"
            @click="selectDevice(item.deviceId)"
          >
            <view class="device-info">
              <text class="device-name">{{ item.name }}</text>
              <text class="device-rssi">信号: {{ item.RSSI }}dBm</text>
            </view>
            <view :class="['signal-dot', item.RSSI > -60 ? 'signal-strong' : item.RSSI > -75 ? 'signal-medium' : 'signal-weak']"></view>
          </view>
        </view>
        <view v-else class="card empty-card">
          <text class="empty-text">{{ scanning ? '正在扫描附近设备...' : '未发现附近设备，请确保设备已开机' }}</text>
        </view>
      </view>
    </template>

    <view v-if="toastVisible" class="toast">
      <text class="toast-icon">✓</text>
      <text class="toast-text">{{ toastText }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { useDeviceStore } from '../../stores/device'
import { useInstallStore } from '../../stores/install'
import { discoverDevices, initBluetooth, createBLEConnection } from '../../utils/ble'
import { bindDevice } from '../../api/device'
import { createInstall } from '../../api/install'
import { getPatient } from '../../api/patient'

const authStore = useAuthStore()
const deviceStore = useDeviceStore()
const installStore = useInstallStore()

const manualDeviceId = ref('')
const patientId = ref('')
const scanning = ref(false)
const binding = ref(false)
const scanResults = ref<{ deviceId: string; name: string; RSSI: number }[]>([])
const toastVisible = ref(false)
const toastText = ref('')

function showToast(text: string) {
  toastText.value = text
  toastVisible.value = true
  setTimeout(() => { toastVisible.value = false }, 1500)
}

function goHome() {
  uni.navigateBack({ fail: () => uni.reLaunch({ url: '/pages/home/index' }) })
}

function scanDevice() {
  // T089-MOCK: 真机用 uni.scanCode，mock 模式直接填入
  manualDeviceId.value = 'PRS-ML05-RC-001'
  uni.showToast({ title: '扫码成功（mock）', icon: 'none' })
}

async function bindManual() {
  const devId = manualDeviceId.value.trim()
  const patId = patientId.value.trim()
  if (!devId) {
    uni.showToast({ title: '请输入设备 ID', icon: 'none' })
    return
  }
  if (!patId) {
    uni.showToast({ title: '请输入患者 ID', icon: 'none' })
    return
  }

  binding.value = true
  try {
    // 1. 设备绑定（真实 API）
    const bindResp = await bindDevice(devId, patId)

    // 换绑提示
    if (bindResp.swapped) {
      uni.showToast({ title: '设备已从其他患者换绑至当前患者', icon: 'none' })
    }

    // 2. 创建安装记录（install 先行，拿 installId）— mock 先行
    const inst = await createInstall(devId, patId, authStore.techId || '')
    installStore.setInstallId(inst.installId)
    installStore.setDeviceInfo(devId, patId, authStore.techId || '')

    deviceStore.setDevice({
      deviceId: bindResp.deviceId || devId,
      model: 'PRS-ML05-RC',
      firmwareVersion: 'v1.2.3',
      patientId: patId,
      wifiSsid: null,
      bindTime: new Date().toISOString(),
      status: bindResp.status || 'unbound',
      lastReportAt: null,
    })

    // 3. 自动 BLE 连接（失败不阻断）
    try {
      await initBluetooth()
      const connected = await createBLEConnection(devId)
      installStore.setBleConnected(connected, devId)
      deviceStore.setBleConnected(connected)
      if (!connected) {
        uni.showToast({ title: '蓝牙连接失败，后续校准需重新连接', icon: 'none' })
      }
    } catch (e) {
      installStore.setBleConnected(false)
      uni.showToast({ title: '蓝牙连接失败，后续校准需重新连接', icon: 'none' })
    }

    // 4. 拉取患者档案
    const patient = await getPatient(patId)
    installStore.setPatient(patient)

    showToast('设备绑定成功')
    setTimeout(() => {
      uni.navigateTo({ url: '/pages/install/index' })
    }, 1200)
  } catch (e) {
    uni.showToast({ title: e instanceof Error ? e.message : '绑定失败', icon: 'none' })
  } finally {
    binding.value = false
  }
}

async function scanBLE() {
  scanning.value = true
  scanResults.value = []
  try {
    await initBluetooth()
    scanResults.value = await discoverDevices()
  } catch (e) {
    uni.showToast({ title: e instanceof Error ? e.message : '扫描失败，请开启蓝牙', icon: 'none' })
  } finally {
    scanning.value = false
  }
}

async function selectDevice(deviceId: string) {
  try {
    uni.showLoading({ title: '连接中...' })
    await initBluetooth()
    const connected = await createBLEConnection(deviceId)
    uni.hideLoading()
    installStore.setBleConnected(connected, deviceId)
    deviceStore.setBleConnected(connected)
    manualDeviceId.value = deviceId
    showToast(connected ? '设备已连接' : '连接失败，请靠近设备')
  } catch (e) {
    uni.hideLoading()
    uni.showToast({ title: '连接失败，请靠近设备', icon: 'none' })
  }
}

onMounted(() => {
  if (!authStore.isLoggedIn) {
    uni.reLaunch({ url: '/pages/login/index' })
  }
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.back-link { font-size: 28rpx; color: #2563EB; display: block; margin-bottom: 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.page-subtitle { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 32rpx; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 20rpx; display: block; letter-spacing: 0.6rpx; }
.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20rpx; }
.section-header .section-title { margin-bottom: 0; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.form-group { margin-bottom: 24rpx; }
.form-label { font-size: 26rpx; color: #64748b; display: block; margin-bottom: 12rpx; }
.required { color: #ef4444; margin-left: 4rpx; }
.form-input { width: 100%; height: 76rpx; line-height: 76rpx; padding: 0 28rpx; border: 1rpx solid #e2e8f0; border-radius: 16rpx; font-size: 30rpx; color: #1e293b; background: #f8fafc; box-sizing: border-box; }
.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; margin-top: 8rpx; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-disabled { opacity: 0.6; }
.scan-card { background: #eff6ff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 48rpx; text-align: center; }
.scan-icon-box { width: 120rpx; height: 120rpx; border-radius: 24rpx; background: #fff; border: 1rpx solid #e2e8f0; display: inline-flex; align-items: center; justify-content: center; margin-bottom: 20rpx; }
.scan-icon { font-size: 56rpx; }
.scan-title { display: block; font-size: 32rpx; font-weight: 500; color: #1e293b; margin-bottom: 8rpx; }
.scan-desc { display: block; font-size: 26rpx; color: #94a3b8; }
.refresh-btn { padding: 8rpx 20rpx; background: #f1f5f9; border-radius: 12rpx; }
.refresh-btn text { font-size: 24rpx; color: #2563EB; }
.device-list { display: flex; flex-direction: column; gap: 16rpx; }
.device-item { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 16rpx; padding: 24rpx 28rpx; display: flex; align-items: center; justify-content: space-between; }
.device-info { display: flex; flex-direction: column; gap: 6rpx; }
.device-name { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.device-rssi { font-size: 22rpx; color: #94a3b8; }
.signal-dot { width: 16rpx; height: 16rpx; border-radius: 50%; flex-shrink: 0; }
.signal-strong { background: #22c55e; }
.signal-medium { background: #f59e0b; }
.signal-weak { background: #ef4444; }
.empty-card { text-align: center; padding: 48rpx; }
.empty-text { font-size: 26rpx; color: #94a3b8; }
.toast { position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: #1e293b; padding: 32rpx 56rpx; border-radius: 24rpx; display: flex; align-items: center; gap: 16rpx; z-index: 200; box-shadow: 0 16rpx 48rpx rgba(0, 0, 0, 0.2); }
.toast-icon { color: #22c55e; font-size: 36rpx; }
.toast-text { color: #fff; font-size: 28rpx; }
</style>
