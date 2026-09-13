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

  // T182: 配网序列号（防重放，每次配网递增）
  let _wifiSeq = Math.floor(Date.now() / 1000)

  function nextWifiSeq(): number {
    _wifiSeq++
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
