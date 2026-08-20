<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">安装流程</text>
      <text class="page-subtitle">{{ deviceInfo.deviceId || '未绑定设备' }}</text>
    </view>

    <!-- 6 阶段步骤条 -->
    <view class="section">
      <view class="stepper">
        <template v-for="(s, i) in stages" :key="i">
          <view :class="['stage', stageClass(i)]">
            <view class="stage-circle"><text>{{ i + 1 }}</text></view>
            <text class="stage-label">{{ s }}</text>
          </view>
          <view v-if="i < stages.length - 1" :class="['stage-line', { 'stage-line-done': currentStage > i + 1 }]"></view>
        </template>
      </view>
    </view>

    <!-- 阶段 1: 患者信息确认 -->
    <view v-if="currentStage === 1" class="section">
      <view class="card">
        <text class="card-title">患者信息确认</text>
        <view class="info-row"><text class="info-label">患者 ID</text><text class="info-value">{{ patientInfo.patientId }}</text></view>
        <view class="info-row"><text class="info-label">姓名</text><text class="info-value">{{ patientInfo.name }}</text></view>
        <view class="info-row"><text class="info-label">年龄</text><text class="info-value">{{ patientInfo.age }} 岁</text></view>
        <view class="info-row"><text class="info-label">诊断</text><text class="info-value">{{ patientInfo.diagnosis }}</text></view>
        <view class="info-row"><text class="info-label">Cobb 角</text><text class="info-value">{{ patientInfo.cobbAngle }}°</text></view>
        <view class="btn-primary" @click="nextStage"><text>确认，下一步</text></view>
      </view>
    </view>

    <!-- 阶段 2: 传感器安装定位 -->
    <view v-if="currentStage === 2" class="section">
      <view class="card">
        <text class="card-title">传感器安装定位</text>
        <view class="notice">
          <text class="notice-icon">ℹ️</text>
          <text class="notice-text">请将传感器矩阵按照图示位置贴合支具内侧，确保 20 个压力点均与患者背部接触。</text>
        </view>
        <view class="sensor-preview">
          <view class="sensor-grid-mini">
            <view v-for="i in 20" :key="i" class="sensor-cell-mini"><text>{{ i }}</text></view>
          </view>
        </view>
        <text class="hint-text">确认 20 个传感器全部贴合到位后继续</text>
        <view class="btn-primary" @click="nextStage"><text>确认安装，下一步</text></view>
      </view>
    </view>

    <!-- 阶段 3: 设备校准 -->
    <view v-if="currentStage === 3" class="section">
      <view class="card">
        <text class="card-title">设备校准</text>
        <view class="notice">
          <text class="notice-icon">⚡</text>
          <text class="notice-text">请确保患者当前未佩戴支具（空载状态），点击开始校准采集零点基线。</text>
        </view>
        <view v-if="!calibrating" class="btn-outline" @click="startCalibration"><text>开始校准</text></view>
        <view v-else class="calibration-progress">
          <view class="progress-bar"><view class="progress-fill" :style="{ width: calibrationProgress + '%' }"></view></view>
          <text class="progress-text">{{ calibrationProgress < 100 ? '校准中...' : '校准完成' }}</text>
        </view>
        <view v-if="calibrationDone" class="btn-primary" style="margin-top: 24rpx;" @click="nextStage"><text>校准完成，下一步</text></view>
      </view>
    </view>

    <!-- 阶段 4: 保存基线 -->
    <view v-if="currentStage === 4" class="section">
      <view class="card">
        <text class="card-title">保存基线数据</text>
        <text class="card-desc">将采集的 20 点 offset_values 保存为安装基线</text>
        <view v-if="baselineValues.length > 0" class="baseline-preview">
          <view v-for="(v, i) in baselineValues" :key="i" class="baseline-item">
            <text class="baseline-idx">P{{ String(i + 1).padStart(2, '0') }}</text>
            <text class="baseline-val">{{ v }}</text>
          </view>
        </view>
        <view class="btn-primary" style="margin-top: 24rpx;" @click="goSaveBaseline"><text>保存基线</text></view>
      </view>
    </view>

    <!-- 阶段 5: WiFi 配置 -->
    <view v-if="currentStage === 5" class="section">
      <view class="card">
        <text class="card-title">WiFi 网络配置</text>
        <text class="card-desc">配置设备 WiFi 使其可联网上报数据</text>
        <view class="btn-primary" style="margin-top: 24rpx;" @click="goWifiConfig"><text>配置 WiFi</text></view>
      </view>
    </view>

    <!-- 阶段 6: 签名确认 -->
    <view v-if="currentStage === 6" class="section">
      <view class="card">
        <text class="card-title">签名确认</text>
        <text class="card-desc">请在下方签名区域签名确认安装完成</text>
        <view class="signature-area">
          <view v-if="signatureDone" class="signature-done">
            <text class="signature-check">✓</text>
            <text class="signature-done-text">已签名</text>
          </view>
          <view v-else class="signature-placeholder" @click="openSignature">
            <text class="signature-hint">点击此处打开签名板</text>
          </view>
        </view>
        <view class="form-group" style="margin-top: 24rpx;">
          <text class="form-label">备注</text>
          <textarea class="form-textarea" placeholder="安装备注（可选）" v-model="notes" maxlength="200" />
        </view>
        <view class="btn-primary" @click="completeInstall"><text>完成安装</text></view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useDeviceStore } from '../../stores/device'
import { useInstallStore } from '../../stores/install'
import { useAuthStore } from '../../stores/auth'
import { writeCalibrationCommand, readCalibrationData } from '../../utils/ble'
import { mockRealtimeSensorData } from '../../mock/baseline'

const deviceStore = useDeviceStore()
const installStore = useInstallStore()
const authStore = useAuthStore()

const stages = ['患者确认', '安装定位', '设备校准', '保存基线', 'WiFi配置', '签名完成']
const currentStage = ref(1)

const deviceInfo = ref(deviceStore.currentDevice || { deviceId: 'PRS-ML05-RC-001' })

// MOCK 患者信息
// 替换计划: 接 user-service GET /api/v1/patients/{patientId}
const patientInfo = ref({
  patientId: 'pat-001',
  name: '张小明',
  age: 14,
  diagnosis: '胸腰段脊柱侧弯',
  cobbAngle: 28,
})

const calibrating = ref(false)
const calibrationProgress = ref(0)
const calibrationDone = ref(false)
const baselineValues = ref<number[]>([])
const notes = ref('')
const signatureDone = ref(false)
let calTimer: ReturnType<typeof setInterval> | null = null

function stageClass(i: number): string {
  if (i + 1 < currentStage.value) return 'stage-done'
  if (i + 1 === currentStage.value) return 'stage-active'
  return ''
}

function nextStage() {
  if (currentStage.value < 6) {
    currentStage.value++
    installStore.setMatrixStep(currentStage.value)
  }
}

function startCalibration() {
  calibrating.value = true
  calibrationProgress.value = 0
  const devId = deviceInfo.value.deviceId

  // MOCK: 模拟校准过程
  // 替换计划: 真机通过 BLE writeCalibrationCommand + readCalibrationData
  calTimer = setInterval(async () => {
    calibrationProgress.value += 20
    if (calibrationProgress.value >= 100) {
      if (calTimer) clearInterval(calTimer)
      calTimer = null
      calibrating.value = false
      calibrationDone.value = true
      try {
        await writeCalibrationCommand(devId, 'stop')
        baselineValues.value = await readCalibrationData(devId)
      } catch (e) {
        // fallback to mock sensor data
        baselineValues.value = mockRealtimeSensorData().map(p => parseFloat((p.pressureValue * 0.01).toFixed(2)))
      }
    }
  }, 800)
}

function goSaveBaseline() {
  installStore.setBaseline(baselineValues.value.length > 0 ? baselineValues.value : mockRealtimeSensorData().map(() => 0.15))
  uni.navigateTo({ url: '/pages/save-baseline/index' })
}

function goWifiConfig() {
  uni.navigateTo({ url: '/pages/wifi-config/index' })
}

function openSignature() {
  // 签名板交互 — 实际使用 SignaturePad 组件
  // MOCK: 模拟签名完成
  signatureDone.value = true
  installStore.setSignature('mock-signature-data-url')
  uni.showToast({ title: '签名完成（mock）', icon: 'none' })
}

function completeInstall() {
  if (!signatureDone.value) {
    uni.showToast({ title: '请先完成签名', icon: 'none' })
    return
  }
  installStore.setNotes(notes.value)
  installStore.completeInstall()
  uni.navigateTo({ url: '/pages/complete/index' })
}

onMounted(() => {
  if (!authStore.isLoggedIn) {
    // 未登录时 mock 登录
    authStore.login('mock-tech-token-001', 'tech-001')
  }
  installStore.startInstall(deviceInfo.value.deviceId, patientInfo.value.patientId, authStore.techId || 'tech-001')
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.page-subtitle { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.card-title { font-size: 32rpx; font-weight: 600; color: #1e293b; display: block; margin-bottom: 16rpx; }
.card-desc { font-size: 26rpx; color: #94a3b8; display: block; margin-bottom: 16rpx; }
/* Stepper */
.stepper { display: flex; align-items: flex-start; justify-content: center; padding: 16rpx 0; }
.stage { display: flex; flex-direction: column; align-items: center; gap: 8rpx; min-width: 80rpx; }
.stage-circle { width: 48rpx; height: 48rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; border: 3rpx solid #e2e8f0; background: #fff; }
.stage-circle text { font-size: 22rpx; font-weight: 500; color: #94a3b8; }
.stage-done .stage-circle { background: #2563EB; border-color: #2563EB; }
.stage-done .stage-circle text { color: #fff; }
.stage-active .stage-circle { border-color: #2563EB; }
.stage-active .stage-circle text { color: #2563EB; }
.stage-label { font-size: 18rpx; color: #94a3b8; white-space: nowrap; text-align: center; }
.stage-line { flex: 1; height: 4rpx; background: #e2e8f0; margin: 22rpx 4rpx 0; min-width: 16rpx; }
.stage-line-done { background: #2563EB; }
/* Info rows */
.info-row { display: flex; justify-content: space-between; padding: 16rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.info-label { font-size: 28rpx; color: #64748b; }
.info-value { font-size: 28rpx; font-weight: 500; color: #1e293b; }
/* Notice */
.notice { display: flex; gap: 16rpx; padding: 20rpx; background: #eff6ff; border-radius: 16rpx; margin-bottom: 24rpx; }
.notice-icon { font-size: 28rpx; flex-shrink: 0; }
.notice-text { font-size: 24rpx; color: #475569; line-height: 1.6; }
/* Sensor preview */
.sensor-preview { display: flex; justify-content: center; padding: 24rpx 0; }
.sensor-grid-mini { display: grid; grid-template-columns: repeat(5, 1fr); gap: 8rpx; }
.sensor-cell-mini { width: 56rpx; height: 56rpx; background: #dbeafe; border-radius: 8rpx; display: flex; align-items: center; justify-content: center; }
.sensor-cell-mini text { font-size: 20rpx; color: #2563EB; font-weight: 500; }
.hint-text { font-size: 24rpx; color: #94a3b8; display: block; text-align: center; margin: 16rpx 0 24rpx; }
/* Calibration */
.calibration-progress { margin-top: 24rpx; }
.progress-bar { width: 100%; height: 12rpx; background: #e2e8f0; border-radius: 6rpx; overflow: hidden; }
.progress-fill { height: 100%; background: #2563EB; border-radius: 6rpx; transition: width 0.4s; }
.progress-text { font-size: 24rpx; color: #94a3b8; display: block; text-align: center; margin-top: 12rpx; }
/* Baseline preview */
.baseline-preview { display: grid; grid-template-columns: repeat(5, 1fr); gap: 8rpx; margin-top: 16rpx; }
.baseline-item { background: #f8fafc; border: 1rpx solid #e2e8f0; border-radius: 8rpx; padding: 8rpx; text-align: center; }
.baseline-idx { font-size: 18rpx; color: #94a3b8; display: block; }
.baseline-val { font-size: 22rpx; color: #1e293b; font-weight: 500; display: block; }
/* Signature */
.signature-area { margin-top: 16rpx; }
.signature-placeholder { height: 240rpx; border: 2rpx dashed #cbd5e1; border-radius: 16rpx; display: flex; align-items: center; justify-content: center; background: #fafbfc; }
.signature-hint { font-size: 28rpx; color: #94a3b8; }
.signature-done { height: 240rpx; border: 2rpx solid #22c55e; border-radius: 16rpx; display: flex; flex-direction: column; align-items: center; justify-content: center; background: #f0fdf4; }
.signature-check { font-size: 56rpx; color: #22c55e; }
.signature-done-text { font-size: 26rpx; color: #22c55e; margin-top: 8rpx; }
/* Form */
.form-group { margin-bottom: 24rpx; }
.form-label { font-size: 26rpx; color: #64748b; display: block; margin-bottom: 12rpx; }
.form-textarea { width: 100%; min-height: 120rpx; padding: 20rpx 24rpx; border: 1rpx solid #e2e8f0; border-radius: 16rpx; font-size: 28rpx; color: #1e293b; background: #f8fafc; }
/* Buttons */
.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; margin-top: 24rpx; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-outline { width: 100%; padding: 24rpx 0; border: 2rpx solid #2563EB; border-radius: 16rpx; text-align: center; background: #fff; }
.btn-outline text { color: #2563EB; font-size: 30rpx; font-weight: 500; }
</style>
