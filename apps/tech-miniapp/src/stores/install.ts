import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Baseline } from '@bracesync/shared-types'
import type {
  InstallPhase,
  PhaseStatus,
  ReachabilityStatus,
  PatientProfile,
  RealtimeFrame,
  CalibrationResult,
} from '../types/app-extends'

/**
 * install store — V2.1 §4.2
 * 管理安装流程 3 阶段状态、安装记录 ID、BLE 连接、实时数据、校准结果、配网状态、可达性等。
 * P0-1：install 先行（绑定后即创建 installId，基线提交必须带 installId）。
 */
export const useInstallStore = defineStore('install', () => {
  // ===== 安装记录基础 =====
  const installId = ref<string | null>(null)
  const deviceId = ref<string>('')
  const patientId = ref<string>('')
  const techId = ref<string>('')
  const calibrateTime = ref<string>('')
  const installNote = ref<string>('')

  // ===== 患者档案（绑定后拉取） =====
  const patient = ref<PatientProfile | null>(null)

  // ===== 阶段状态 =====
  const phase = ref<InstallPhase>(1)
  const phaseStatus = ref<Record<InstallPhase, PhaseStatus>>({
    1: 'active',
    2: 'pending',
    3: 'pending',
  })

  // ===== BLE 连接 =====
  const bleConnected = ref(false)
  const bleDeviceId = ref<string | null>(null)

  // ===== 实时压力数据（阶段二校准） =====
  const realtimeData = ref<RealtimeFrame | null>(null)
  const isStreaming = ref(false)

  // ===== 校准结果 =====
  const calibrationData = ref<CalibrationResult | null>(null)
  const baselineId = ref<string | null>(null)
  const baselineSaved = ref(false)

  // ===== 配网 & 可达性（阶段三） =====
  const wifiStatus = ref<'unconfigured' | 'connected'>('unconfigured')
  const wifiStatusCode = ref<number | null>(null)
  const reachabilityStatus = ref<ReachabilityStatus>('pending')

  // ===== 配网 seq 计数器（协议 §3：会话内递增，防 CTR 重用；跨会话/断电重置为 1） =====
  const wifiSeq = ref(1)

  // ===== 计算属性 =====
  const phaseDone = computed(() => phaseStatus.value[phase.value] === 'done')

  // ===== 方法 =====
  function setInstallId(id: string) {
    installId.value = id
  }

  function setDeviceInfo(dId: string, pId: string, tId: string) {
    deviceId.value = dId
    patientId.value = pId
    techId.value = tId
  }

  function setPatient(p: PatientProfile | null) {
    patient.value = p
  }

  function setPhase(p: InstallPhase) {
    phase.value = p
  }

  function setPhaseDone(p: InstallPhase) {
    phaseStatus.value = { ...phaseStatus.value, [p]: 'done' }
    // 自动激活下一阶段
    if (p < 3) {
      phaseStatus.value = { ...phaseStatus.value, [p + 1]: 'active' }
      phase.value = (p + 1) as InstallPhase
    }
  }

  function setBleConnected(connected: boolean, deviceId?: string) {
    bleConnected.value = connected
    if (deviceId !== undefined) bleDeviceId.value = deviceId
  }

  function startRealtimeStream() {
    isStreaming.value = true
  }

  function stopRealtimeStream() {
    isStreaming.value = false
  }

  function updateRealtimeData(frame: RealtimeFrame) {
    realtimeData.value = frame
  }

  function setCalibrationData(data: CalibrationResult) {
    calibrationData.value = data
  }

  function setBaselineSaved(id: string) {
    baselineId.value = id
    baselineSaved.value = true
  }

  function setWifiStatus(status: 'unconfigured' | 'connected') {
    wifiStatus.value = status
  }

  function updateWifiStatusCode(code: number) {
    wifiStatusCode.value = code
  }

  function setReachabilityVerified(status: ReachabilityStatus) {
    reachabilityStatus.value = status
  }

  function setInstallNote(note: string) {
    installNote.value = note
  }

  /** 领取本次配网 seq（返回当前值并自增；首次配网 = 1，同一会话重写凭据必须 +1） */
  function nextWifiSeq(): number {
    const cur = wifiSeq.value
    wifiSeq.value += 1
    return cur
  }

  function resetInstall() {
    installId.value = null
    deviceId.value = ''
    patientId.value = ''
    techId.value = ''
    calibrateTime.value = ''
    installNote.value = ''
    patient.value = null
    phase.value = 1
    phaseStatus.value = { 1: 'active', 2: 'pending', 3: 'pending' }
    bleConnected.value = false
    bleDeviceId.value = null
    realtimeData.value = null
    isStreaming.value = false
    calibrationData.value = null
    baselineId.value = null
    baselineSaved.value = false
    wifiStatus.value = 'unconfigured'
    wifiStatusCode.value = null
    reachabilityStatus.value = 'pending'
    wifiSeq.value = 1
  }

  return {
    // state
    installId,
    deviceId,
    patientId,
    techId,
    calibrateTime,
    installNote,
    patient,
    phase,
    phaseStatus,
    bleConnected,
    bleDeviceId,
    realtimeData,
    isStreaming,
    calibrationData,
    baselineId,
    baselineSaved,
    wifiStatus,
    wifiStatusCode,
    reachabilityStatus,
    wifiSeq,
    // computed
    phaseDone,
    // methods
    setInstallId,
    setDeviceInfo,
    setPatient,
    setPhase,
    setPhaseDone,
    setBleConnected,
    startRealtimeStream,
    stopRealtimeStream,
    updateRealtimeData,
    setCalibrationData,
    setBaselineSaved,
    setWifiStatus,
    updateWifiStatusCode,
    setReachabilityVerified,
    setInstallNote,
    nextWifiSeq,
    resetInstall,
  }
})
