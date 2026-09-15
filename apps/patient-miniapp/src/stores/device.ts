import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Device } from '@bracesync/shared-types'

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

  // T182: 配网序列号（防重放）。
  // 🔴 起点必须是 1：固件不持久 seq，解密时只尝试 seq=1/2/3（协议 §3 定稿）。
  // 旧实现取 Date.now()/1000（≈17.9 亿）当起点 ⇒ 固件永远解不出明文，真机表现为 -1"密码错误"。
  // T212: 但"同一会话一直 +1"同样会越窗——第 4 次「开始配网」起 seq=4，每次解不开都回 -1，
  // 且只有冷启动小程序才恢复（09-15 夜真机实证）。故 seq 作用域收紧为单次配网尝试。
  let _wifiSeq = 0

  function nextWifiSeq(): number {
    _wifiSeq += 1
    return _wifiSeq
  }

  /** T212: 每轮「开始配网」前复位，使本轮首个 seq 回到 1（留在固件候选窗 1/2/3 内） */
  function resetWifiSeq(): void {
    _wifiSeq = 0
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
    resetWifiSeq,
    setDevice, clearDevice,
    setBleConnected, setWifiStatus, updateWifiStatusCode,
  }
})
