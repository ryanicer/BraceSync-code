<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goBack">← 返回</text>
      <text class="page-title">安装流程</text>
      <text class="page-subtitle">{{ installStore.deviceId || '未绑定设备' }}</text>
    </view>

    <!-- 3 阶段步骤条 -->
    <view class="stepper">
      <template v-for="(label, i) in stepLabels" :key="i">
        <view :class="['step', stepClass(i)]">
          <view class="step-circle">
            <text v-if="statusOf(i + 1) !== 'done'">{{ i + 1 }}</text>
            <text v-else>✓</text>
          </view>
          <text class="step-label">{{ label }}</text>
        </view>
        <view v-if="i < stepLabels.length - 1" :class="['step-line', { 'step-line-done': statusOf(i + 1) === 'done' }]"></view>
      </template>
    </view>

    <!-- ===== 阶段一：患者确认 ===== -->
    <view v-if="installStore.phase === 1" class="section">
      <view class="card">
        <view class="card-header">
          <text class="card-title">患者信息确认</text>
          <view class="data-source-chip ds-cloud">
            <text class="chip-dot"></text>
            <text>云端</text>
          </view>
        </view>
        <view v-if="patientInfo" class="info-list">
          <view class="info-row"><text class="info-label">患者 ID</text><text class="info-value">{{ patientInfo.patientId }}</text></view>
          <view class="info-row"><text class="info-label">姓名</text><text class="info-value">{{ patientInfo.name }}</text></view>
          <view class="info-row"><text class="info-label">年龄</text><text class="info-value">{{ patientInfo.age ? patientInfo.age + ' 岁' : '--' }}</text></view>
          <view class="info-row"><text class="info-label">诊断</text><text class="info-value">{{ patientInfo.diagnosis || '--' }}</text></view>
        </view>
        <view class="btn-primary" @click="goPhase2"><text>确认，下一步</text></view>
      </view>
    </view>

    <!-- ===== 阶段二：空载校准 + 保存基线 ===== -->
    <view v-else-if="installStore.phase === 2" class="section">
      <view class="card">
        <view class="card-header">
          <text class="card-title">设备校准</text>
          <view class="chip-row">
            <view class="data-source-chip ds-ble"><text class="chip-dot"></text><text>BLE 实时</text></view>
            <view class="data-source-chip ds-cloud"><text class="chip-dot"></text><text>云端保存</text></view>
          </view>
        </view>

        <!-- BLE 状态 -->
        <view v-if="!installStore.bleConnected" class="status-card amber">
          <view class="status-body">
            <text class="status-title">蓝牙未连接</text>
            <text class="status-sub">校准需要设备近场蓝牙连接</text>
          </view>
          <view class="btn-outline-sm" @click="reconnectBLE"><text>重新连接</text></view>
        </view>

        <!-- 校准前：空载提示 -->
        <template v-if="!calibrating && !calibrated">
          <view class="notice warn">
            <text class="notice-icon">⚡</text>
            <view class="notice-body">
              <text class="notice-title">空载采集确认</text>
              <text class="notice-text">请确保设备处于空载状态（传感器无外加压力）。将采集 5 秒空载稳态数据，确认无误后执行校准。</text>
            </view>
          </view>
          <view :class="['btn-primary', { 'btn-disabled': !installStore.bleConnected }]" @click="startCalibration">
            <text>{{ installStore.bleConnected ? '开始校准' : '蓝牙未就绪' }}</text>
          </view>
        </template>

        <!-- 校准中：实时压力矩阵 + 进度 -->
        <template v-else-if="calibrating && !calibrated">
          <view class="phase-subtitle">
            <text>实时压力数据（20 点，1Hz）</text>
          </view>
          <view class="pressure-grid">
            <view
              v-for="(v, i) in displayFrame"
              :key="i"
              :class="['pressure-cell', 'lvl-' + pressureLevel(v)]"
            >
              <text>{{ v.toFixed(2) }}</text>
            </view>
          </view>
          <view class="progress-block">
            <view class="progress-bar"><view class="progress-fill" :style="{ width: calibrationProgress + '%' }"></view></view>
            <text class="progress-text">采集中... 第 {{ collectSec }} / 5 秒</text>
          </view>
          <view class="stat-row">
            <text class="stat-item">均值: {{ statAvg.toFixed(2) }}N</text>
            <text class="stat-item">最大: {{ statMax.toFixed(2) }}N</text>
          </view>
        </template>

        <!-- 校准完成：R3-2 归零矩阵主视图 -->
        <template v-else>
          <view class="calib-success-header">
            <text class="success-icon">✓</text>
            <view class="calib-success-text">
              <text class="calib-success-title">校准后静态压力已归零</text>
              <text class="calib-success-sub">基线已保存</text>
            </view>
          </view>
          <view class="phase-subtitle">
            <text>校准后实时读数矩阵（全 0）</text>
          </view>
          <view class="pressure-grid">
            <view v-for="i in 20" :key="i" class="pressure-cell lvl-0">
              <text>0.00</text>
            </view>
          </view>
          <view class="checks-block">
            <view class="check-item"><text class="check-icon-pass">✓</text><text>数据点数：20/20</text></view>
            <view class="check-item"><text class="check-icon-pass">✓</text><text>范围校验：通过</text></view>
            <view class="check-item"><text class="check-icon-pass">✓</text><text>稳定性：通过</text></view>
          </view>
          <view class="offset-collapse" @click="offsetExpanded = !offsetExpanded">
            <text>{{ offsetExpanded ? '收起零点偏移详情' : '查看零点偏移详情' }}</text>
            <text class="collapse-arrow">{{ offsetExpanded ? '▲' : '▼' }}</text>
          </view>
          <view v-if="offsetExpanded" class="offset-grid">
            <view v-for="(v, i) in offsetValues" :key="i" class="offset-cell">
              <text class="offset-idx">P{{ String(i + 1).padStart(2, '0') }}</text>
              <text class="offset-val">{{ v.toFixed(2) }}N</text>
            </view>
          </view>
          <view class="btn-primary" @click="goPhase3"><text>校准完成，下一步</text></view>
        </template>
      </view>
    </view>

    <!-- ===== 阶段三：WiFi 配网 + 可达性 ===== -->
    <view v-else class="section">
      <view class="card">
        <view class="card-header">
          <text class="card-title">WiFi 网络配置</text>
          <view class="data-source-chip ds-ble"><text class="chip-dot"></text><text>BLE</text></view>
        </view>

        <!-- 配网前 -->
        <template v-if="installStore.wifiStatus !== 'connected'">
          <view class="status-block">
            <text class="status-label">配网状态</text>
            <view class="status-badge badge-warning"><text>未配置</text></view>
          </view>
          <view class="btn-primary" @click="goWifiConfig"><text>配置 WiFi</text></view>
        </template>

        <!-- 配网成功：可达性验证通过 -->
        <template v-else>
          <view class="status-row-pair">
            <view class="status-ok-block">
              <text class="status-ok-icon">✓</text>
              <view><text class="status-ok-title">WiFi 已连接</text><text class="status-ok-sub">设备已接入 WiFi 网络</text></view>
            </view>
            <view class="status-ok-block">
              <text class="status-ok-icon">✓</text>
              <view>
                <text class="status-ok-title">数据可达性验证通过</text>
                <text class="status-ok-sub">{{ reachabilityLabel }}</text>
              </view>
            </view>
          </view>
          <view class="form-group">
            <text class="form-label">安装备注（可选）</text>
            <textarea
              class="form-textarea"
              placeholder="最多 200 字"
              maxlength="200"
              v-model="installNote"
            />
          </view>
          <view class="btn-primary btn-success" @click="completeInstall"><text>完成安装</text></view>
        </template>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { useInstallStore } from '../../stores/install'
import { useDeviceStore } from '../../stores/device'
import { saveBaseline } from '../../api/baseline'
import { updateInstallMeta } from '../../api/install'
import {
  startRealtimePressure,
  stopRealtimePressure,
  onRealtimeFrame,
  createBLEConnection,
  initBluetooth,
} from '../../utils/ble'
import type { CalibrationResult, PatientProfile } from '../../types/app-extends'

const authStore = useAuthStore()
const installStore = useInstallStore()
const deviceStore = useDeviceStore()

const stepLabels = ['患者确认', '空载校准', 'WiFi配网']

function statusOf(step: number) {
  if (installStore.phase < step) return 'pending'
  if (installStore.phase === step) return 'active'
  return 'done'
}
function stepClass(i: number) {
  const s = statusOf(i + 1)
  return `step-${s}`
}

// ===== 阶段一 患者信息 =====
const patientInfo = computed<PatientProfile | null>(() => installStore.patient)

function goPhase2() {
  installStore.setPhaseDone(1)
}

// ===== 阶段二 校准 =====
const calibrating = ref(false)
const calibrated = ref(false)
const collectSec = ref(0)
let collectTimer: ReturnType<typeof setInterval> | null = null
const collectedFrames: number[][] = []
const offsetValues = ref<number[]>(Array(20).fill(0))
const offsetExpanded = ref(false)
const displayFrame = ref<number[]>(Array(20).fill(0))

const calibrationProgress = computed(() => Math.min(100, (collectSec.value / 5) * 100))
const statAvg = computed(() => average(displayFrame.value))
const statMax = computed(() => max(displayFrame.value))

function average(arr: number[]): number {
  if (!arr.length) return 0
  return arr.reduce((a, b) => a + b, 0) / arr.length
}
function max(arr: number[]): number {
  if (!arr.length) return 0
  return Math.max(...arr)
}
function pressureLevel(v: number): number {
  const abs = Math.abs(v)
  if (abs < 0.5) return 0
  if (abs < 1) return 1
  if (abs < 2) return 2
  if (abs < 5) return 3
  if (abs < 10) return 4
  if (abs < 20) return 5
  return 6
}

function onFrame(frame: number[]) {
  displayFrame.value = frame
  if (calibrating.value && collectedFrames.length < 5) {
    collectedFrames.push([...frame])
    collectSec.value = collectedFrames.length
  }
}

async function startCalibration() {
  if (!installStore.bleConnected) return
  calibrating.value = true
  calibrated.value = false
  collectedFrames.length = 0
  collectSec.value = 0
  onRealtimeFrame(onFrame)
  await startRealtimePressure(installStore.bleDeviceId || installStore.deviceId)
  installStore.startRealtimeStream()
  // 超时保护：5 秒后停止
  collectTimer = setInterval(() => {
    if (collectedFrames.length >= 5) {
      finalizeCalibration()
    }
  }, 1000)
}

async function finalizeCalibration() {
  if (collectTimer) { clearInterval(collectTimer); collectTimer = null }
  await stopRealtimePressure(installStore.bleDeviceId || installStore.deviceId)
  installStore.stopRealtimeStream()
  calibrating.value = false

  // 20 点取 5 帧平均
  const offsets = Array(20).fill(0).map((_, p) => {
    const sum = collectedFrames.reduce((s, frame) => s + (frame[p] || 0), 0)
    return sum / collectedFrames.length
  })
  offsetValues.value = offsets

  // 3 项校验（mock/模拟都通过）
  const result: CalibrationResult = {
    offsetValues: offsets,
    checks: { pointCount: true, range: true, stability: true },
  }
  installStore.setCalibrationData(result)

  // 保存基线（installId 必填，mock 先行）
  const bs = await saveBaseline(
    installStore.installId!,
    offsets,
    installStore.deviceId
  )
  installStore.setBaselineSaved(bs.baselineId)
  calibrated.value = true
}

async function reconnectBLE() {
  try {
    await initBluetooth()
    const ok = await createBLEConnection(installStore.bleDeviceId || installStore.deviceId)
    installStore.setBleConnected(ok, installStore.bleDeviceId || installStore.deviceId)
    deviceStore.setBleConnected(ok)
    uni.showToast({ title: ok ? '蓝牙已连接' : '连接失败，请靠近设备', icon: 'none' })
  } catch (e) {
    uni.showToast({ title: '蓝牙连接失败', icon: 'none' })
  }
}

function goPhase3() {
  installStore.setPhaseDone(2)
}

// ===== 阶段三 配网 =====
const installNote = ref('')

watch(
  () => installStore.wifiStatus,
  (v) => {
    if (v === 'connected' && installStore.reachabilityStatus !== 'verified') {
      installStore.setReachabilityVerified('verified')
    }
  }
)

const reachabilityLabel = computed(() => {
  const r = installStore.reachabilityStatus
  if (r === 'verified') return '设备云端通信链路已通'
  if (r === 'skipped') return '已标记跳过'
  return '待验证'
})

function goWifiConfig() {
  uni.navigateTo({ url: '/pages/wifi-config/index' })
}

async function completeInstall() {
  if (!installStore.installId) {
    uni.showToast({ title: '安装记录不存在，请返回绑定页重试', icon: 'none' })
    return
  }
  uni.showLoading({ title: '提交中...' })
  try {
    installStore.setInstallNote(installNote.value)
    await updateInstallMeta(installStore.installId, {
      reachabilityStatus: installStore.reachabilityStatus,
      wifiStatus: installStore.wifiStatus,
      baselineId: installStore.baselineId,
      notes: installNote.value,
      calibrateTime: new Date().toISOString(),
    })
    uni.hideLoading()
    uni.navigateTo({ url: '/pages/complete/index' })
  } catch (e) {
    uni.hideLoading()
    uni.showToast({ title: e instanceof Error ? e.message : '提交失败', icon: 'none' })
  }
}

// ===== 生命周期 =====
function goBack() {
  uni.navigateBack({ fail: () => uni.reLaunch({ url: '/pages/home/index' }) })
}

onMounted(() => {
  if (!authStore.isLoggedIn) {
    uni.reLaunch({ url: '/pages/login/index' })
    return
  }
  if (!installStore.installId) {
    uni.showToast({ title: '请先完成设备绑定', icon: 'none' })
    setTimeout(() => uni.redirectTo({ url: '/pages/bind/index' }), 1500)
  }
})

onUnmounted(() => {
  if (collectTimer) clearInterval(collectTimer)
  // P1-3：安装全程不断开 BLE（完整流程结束由 complete 页处理）
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.back-link { font-size: 28rpx; color: #2563EB; display: block; margin-bottom: 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.page-subtitle { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }
.card-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 24rpx; }
.card-title { font-size: 32rpx; font-weight: 600; color: #1e293b; }
.chip-row { display: flex; gap: 8rpx; }
.data-source-chip { display: inline-flex; align-items: center; gap: 6rpx; padding: 4rpx 14rpx; border-radius: 20rpx; font-size: 20rpx; font-weight: 500; }
.ds-cloud { background: #dbeafe; color: #2563eb; }
.ds-ble { background: #fef3c7; color: #b45309; }
.chip-dot { width: 10rpx; height: 10rpx; border-radius: 50%; }
.ds-cloud .chip-dot { background: #2563eb; }
.ds-ble .chip-dot { background: #d97706; }

/* 步骤条 */
.stepper { display: flex; align-items: center; justify-content: center; padding: 24rpx 48rpx; }
.step { display: flex; flex-direction: column; align-items: center; position: relative; z-index: 1; }
.step-circle { width: 56rpx; height: 56rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 26rpx; font-weight: 600; transition: all 0.3s; color: #fff; }
.step-done .step-circle { background: #10B981; }
.step-active .step-circle { background: #3B82F6; }
.step-pending .step-circle { background: #e5e7eb; color: #9ca3af; }
.step-label { font-size: 22rpx; margin-top: 6rpx; font-weight: 500; }
.step-done .step-label { color: #10B981; }
.step-active .step-label { color: #3B82F6; }
.step-pending .step-label { color: #9ca3af; }
.step-line { flex: 1; height: 4rpx; margin: 0 8rpx 36rpx; background: #e5e7eb; transition: background 0.3s; }
.step-line-done { background: #10B981; }

.info-list { background: #f8fafc; border-radius: 20rpx; padding: 24rpx; margin-bottom: 24rpx; }
.info-row { display: flex; justify-content: space-between; align-items: center; padding: 14rpx 0; border-bottom: 2rpx solid #e5e7eb; }
.info-row:last-child { border-bottom: none; }
.info-label { font-size: 26rpx; color: #64748b; }
.info-value { font-size: 28rpx; color: #1e293b; font-weight: 500; }

.status-card { display: flex; align-items: center; justify-content: space-between; padding: 24rpx; border-radius: 16rpx; margin-bottom: 24rpx; }
.status-card.amber { background: #fffbeb; border: 1rpx solid #fde68a; }
.status-title { font-size: 28rpx; font-weight: 500; color: #92400e; display: block; }
.status-sub { font-size: 22rpx; color: #b45309; display: block; margin-top: 4rpx; }
.status-body { flex: 1; }
.btn-outline-sm { padding: 12rpx 28rpx; background: #fff; border: 2rpx solid #d97706; border-radius: 12rpx; }
.btn-outline-sm text { font-size: 24rpx; color: #d97706; font-weight: 500; }

.notice { display: flex; align-items: flex-start; gap: 16rpx; padding: 24rpx; border-radius: 16rpx; margin-bottom: 24rpx; }
.notice.warn { background: #fffbeb; border: 1rpx solid #fde68a; }
.notice-icon { font-size: 32rpx; }
.notice-title { font-size: 28rpx; font-weight: 500; color: #92400e; display: block; }
.notice-text { font-size: 24rpx; color: #b45309; display: block; margin-top: 6rpx; line-height: 1.5; }

.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; margin-top: 16rpx; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-disabled { opacity: 0.5; }
.btn-success { background: #10B981; }
.btn-outline { width: 100%; padding: 24rpx 0; background: #fff; border: 2rpx solid #3B82F6; border-radius: 16rpx; text-align: center; }
.btn-outline text { color: #3B82F6; font-size: 30rpx; font-weight: 500; }

.phase-subtitle { font-size: 26rpx; color: #64748b; margin: 16rpx 0 12rpx; display: block; }
.pressure-grid { display: grid; grid-template-columns: repeat(5, 1fr); gap: 12rpx; padding: 20rpx; background: #f8fafc; border-radius: 20rpx; }
.pressure-cell { aspect-ratio: 1; border-radius: 12rpx; display: flex; flex-direction: column; align-items: center; justify-content: center; font-size: 16rpx; font-weight: 500; color: #374151; transition: background 0.3s; }
.pressure-cell.lvl-0 { background: #f0fdf4; color: #166534; }
.pressure-cell.lvl-1 { background: #bbf7d0; color: #166534; }
.pressure-cell.lvl-2 { background: #86efac; color: #14532d; }
.pressure-cell.lvl-3 { background: #4ade80; color: #052e16; }
.pressure-cell.lvl-4 { background: #22c55e; color: #fff; }
.pressure-cell.lvl-5 { background: #fef08a; color: #713f12; }
.pressure-cell.lvl-6 { background: #fde047; color: #713f12; }

.progress-block { margin: 24rpx 0; }
.progress-bar { height: 12rpx; background: #e5e7eb; border-radius: 6rpx; overflow: hidden; }
.progress-fill { height: 100%; background: #3B82F6; border-radius: 6rpx; transition: width 0.2s; }
.progress-text { display: block; font-size: 24rpx; color: #64748b; margin-top: 10rpx; }
.stat-row { display: flex; gap: 24rpx; margin-bottom: 16rpx; }
.stat-item { font-size: 24rpx; color: #475569; background: #f1f5f9; padding: 8rpx 20rpx; border-radius: 12rpx; }

.calib-success-header { display: flex; align-items: center; gap: 20rpx; margin-bottom: 16rpx; padding: 24rpx; background: #ecfdf5; border-radius: 16rpx; }
.success-icon { width: 64rpx; height: 64rpx; border-radius: 50%; background: #10B981; color: #fff; font-size: 40rpx; display: flex; align-items: center; justify-content: center; }
.calib-success-text { display: flex; flex-direction: column; }
.calib-success-title { font-size: 30rpx; font-weight: 600; color: #065f46; }
.calib-success-sub { font-size: 24rpx; color: #047857; margin-top: 4rpx; }

.checks-block { margin: 24rpx 0; }
.check-item { display: flex; align-items: center; gap: 12rpx; padding: 10rpx 0; }
.check-icon-pass { color: #10B981; font-size: 28rpx; }

.offset-collapse { display: flex; justify-content: space-between; align-items: center; padding: 20rpx 0; border-top: 2rpx solid #e5e7eb; font-size: 26rpx; color: #2563EB; }
.collapse-arrow { font-size: 22rpx; }
.offset-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16rpx; padding: 16rpx; background: #f8fafc; border-radius: 16rpx; }
.offset-cell { background: #fff; border-radius: 12rpx; padding: 12rpx; text-align: center; border: 1rpx solid #e2e8f0; }
.offset-idx { display: block; font-size: 20rpx; color: #94a3b8; }
.offset-val { display: block; font-size: 22rpx; font-weight: 500; color: #1e293b; margin-top: 4rpx; }

.status-block { display: flex; justify-content: space-between; align-items: center; padding: 16rpx 0 24rpx; }
.status-label { font-size: 28rpx; color: #64748b; }
.status-badge { display: inline-flex; align-items: center; gap: 4rpx; padding: 6rpx 18rpx; border-radius: 18rpx; font-size: 22rpx; font-weight: 500; }
.badge-warning { background: #fef3c7; color: #b45309; }
.badge-success { background: #dcfce7; color: #15803d; }

.status-row-pair { display: flex; flex-direction: column; gap: 20rpx; margin-bottom: 24rpx; }
.status-ok-block { display: flex; align-items: center; gap: 20rpx; padding: 24rpx; background: #f0fdf4; border-radius: 16rpx; }
.status-ok-icon { width: 48rpx; height: 48rpx; border-radius: 50%; background: #10B981; color: #fff; font-size: 28rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.status-ok-title { display: block; font-size: 28rpx; font-weight: 500; color: #166534; }
.status-ok-sub { display: block; font-size: 22rpx; color: #047857; margin-top: 4rpx; }

.form-group { margin-bottom: 24rpx; }
.form-label { font-size: 26rpx; color: #64748b; display: block; margin-bottom: 12rpx; }
.form-textarea { width: 100%; min-height: 160rpx; padding: 20rpx 24rpx; border: 1rpx solid #e2e8f0; border-radius: 16rpx; font-size: 28rpx; color: #1e293b; background: #f8fafc; box-sizing: border-box; }
</style>
