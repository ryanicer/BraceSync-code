import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Device } from '@bracesync/shared-types'

/** 固件解密候选窗上限（T213：provision_crypto.h kCandidateSeqMax）。seq 到该值后回绕为 1。 */
export const WIFI_SEQ_CANDIDATE_MAX = 64

export const useDeviceStore = defineStore('device', () => {
  const currentDevice = ref<Device | null>(null)
  const isBound = ref(false)

  // T192: BLE 连接相关字段——唯一来源是蓝牙扫描结果（配网页 onSelectDevice 前写入），
  // 后端设备契约不提供 BLE 标识（冲突 C，已上报 PM）。
  const bleDeviceId = ref<string>('') // 扫描结果 deviceId（微信 createBLEConnection 用此值）
  const bleName = ref<string>('') // BLE 广播名（BSYNC-xxx）
  const bleConnected = ref(false)

  // T182: WiFi 配网状态
  const wifiStatus = ref<'unconfigured' | 'configuring' | 'connected' | 'failed'>('unconfigured')
  const wifiStatusCode = ref<number | null>(null)

  // T182: 配网序列号（防重放 + 防 CTR keystream 复用）。
  // 🔴 取值必须落在固件候选窗内：固件不持久 seq，解密时只在 1..kCandidateSeqMax 里试。
  //   T213 起固件窗上限 = 64（旧固件仍是 1..3，未重烧的设备升到 4 即锁死，故须与 T213 同批上线）。
  // 旧实现取 Date.now()/1000（≈17.9 亿）当起点 ⇒ 固件永远解不出明文，真机表现为 -1"密码错误"。
  // T212 曾改"每轮复位为 1"解决了越窗，但 provision_key 每次配网不轮换 ⇒ 两轮用同一 (key, IV)
  //   加密不同明文 = 经典 CTR keystream 复用（两段密文异或即得两段明文异或）。
  // T216: 每轮自增、只在将要越出窗上限时回绕 ⇒ 越窗与 keystream 复用同时避免。
  let _wifiSeq = 0

  function nextWifiSeq(): number {
    _wifiSeq = _wifiSeq >= WIFI_SEQ_CANDIDATE_MAX ? 1 : _wifiSeq + 1
    return _wifiSeq
  }

  function setDevice(device: Device) {
    currentDevice.value = device
    isBound.value = device.status !== 'unbound'
  }

  function clearDevice() {
    currentDevice.value = null
    isBound.value = false
    bleDeviceId.value = ''
    bleName.value = ''
    bleConnected.value = false
    wifiStatus.value = 'unconfigured'
    wifiStatusCode.value = null
  }

  function setBleConnected(connected: boolean) {
    bleConnected.value = connected
  }

  function setWifiStatus(status: 'unconfigured' | 'configuring' | 'connected' | 'failed') {
    wifiStatus.value = status
  }

  function updateWifiStatusCode(code: number) {
    wifiStatusCode.value = code
    if (code === 9) wifiStatus.value = 'connected'
    else if (code < 0) wifiStatus.value = 'failed'
    else if (code >= 0) wifiStatus.value = 'configuring'
  }

  return {
    currentDevice, isBound,
    bleDeviceId, bleName, bleConnected,
    wifiStatus, wifiStatusCode,
    nextWifiSeq,
    setDevice, clearDevice,
    setBleConnected, setWifiStatus, updateWifiStatusCode,
  }
})
