/**
 * 蓝牙工具（患者端，T094b）
 *
 * BLE 配网流程：扫描 BSYNC- 设备 → 连接 → 获取 provisionKey → 加密 WiFi 凭据 → 写入设备 → 读取状态 → 完成
 *
 * 参考：apps/tech-miniapp/src/utils/ble.ts（两端暂各存一份，测试稳定后再抽公共包）
 */

import { bleLog } from './ble-log'
import { encryptWifiPayload } from './aes-ctr'
import { getProvisionKey, clearProvisionCache } from '../api/provision'
import { filterBleDevices, nextProvisionStep, resolveProvisionStatus, isProvisionTimeout } from './wifi-state'

// BLE 常量
const SERVICE_UUID = '0000ffe0-0000-1000-8000-00805f9b34fb'
const CHAR_UUID = '0000ffe1-0000-1000-8000-00805f9b34fb'
const MTU = 247
const PROVISION_TIMEOUT_MS = 15000

export interface BleDeviceInfo {
  deviceId: string
  name: string
  localName?: string
  RSSI: number
}

export interface ProvisionResult {
  success: boolean
  error_code?: string
  error_message?: string
}

let isScanning = false
let scanListener: ((devices: BleDeviceInfo[]) => void) | null = null

function normalizeName(name: string): string {
  return name.replace(/[^a-zA-Z0-9-_]/g, '')
}

/** 扫描 BLE 设备（自动过滤 BSYNC- 前缀 + 2.4G 可配设备） */
export function scanBleDevices(
  callback: (devices: BleDeviceInfo[]) => void,
  durationMs = 10000
): Promise<void> {
  return new Promise((resolve, reject) => {
    scanListener = callback

    uni.openBluetoothAdapter({
      success: () => {
        uni.startBluetoothDevicesDiscovery({
          allowDuplicatesKey: false,
          interval: 0,
          success: () => {
            isScanning = true
            bleLog('info', 'BLE scan started')

            uni.onBluetoothDeviceFound((res) => {
              const raw = res.devices as BleDeviceInfo[]
              const filtered = filterBleDevices(raw)
              bleLog('info', `BLE found ${raw.length} devices, ${filtered.length} after filter`)
              scanListener?.(filtered)
            })

            setTimeout(() => {
              stopBleScan()
              resolve()
            }, durationMs)
          },
          fail: (err) => {
            bleLog('error', 'startBluetoothDevicesDiscovery failed', { err })
            reject(new Error('蓝牙扫描启动失败，请检查蓝牙权限'))
          },
        })
      },
      fail: (err) => {
        bleLog('error', 'openBluetoothAdapter failed', { err })
        reject(new Error('无法打开蓝牙，请确认手机蓝牙已开启'))
      },
    })
  })
}

export function stopBleScan(): void {
  if (!isScanning) return
  uni.stopBluetoothDevicesDiscovery({
    complete: () => {
      isScanning = false
      bleLog('info', 'BLE scan stopped')
    },
  })
}

export function connectBleDevice(deviceId: string): Promise<void> {
  return new Promise((resolve, reject) => {
    bleLog('info', 'BLE connecting', { deviceId })
    uni.createBLEConnection({
      deviceId,
      timeout: 10000,
      success: () => {
        bleLog('info', 'BLE connected', { deviceId })
        // MTU 协商（247B，适配 40 字节数据帧）
        ;(uni as any).setBLEMTU?.({
          deviceId,
          mtu: MTU,
          complete: () => resolve(),
        })
      },
      fail: (err) => {
        bleLog('error', 'BLE connect failed', { err })
        reject(new Error('设备连接失败，请靠近设备重试'))
      },
    })
  })
}

export function disconnectBleDevice(deviceId: string): void {
  uni.closeBLEConnection({ deviceId, complete: () => bleLog('info', 'BLE disconnected', { deviceId }) })
}

function writeBleCharacteristic(deviceId: string, data: ArrayBuffer): Promise<void> {
  return new Promise((resolve, reject) => {
    uni.writeBLECharacteristicValue({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_UUID,
      value: data,
      success: () => resolve(),
      fail: (err) => {
        bleLog('error', 'writeBLECharacteristicValue failed', { err })
        reject(new Error('数据写入失败'))
      },
    })
  })
}

function readBleCharacteristic(deviceId: string): Promise<number> {
  return new Promise((resolve, reject) => {
    uni.readBLECharacteristicValue({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_UUID,
      success: (res) => {
        const dv = new DataView(res.characteristic.value as ArrayBuffer)
        resolve(dv.getUint8(0))
      },
      fail: (err) => {
        bleLog('error', 'readBLECharacteristicValue failed', { err })
        reject(new Error('状态读取失败'))
      },
    })
  })
}

function writeString(deviceId: string, str: string): Promise<void> {
  const buf = new ArrayBuffer(str.length)
  const view = new Uint8Array(buf)
  for (let i = 0; i < str.length; i++) view[i] = str.charCodeAt(i)
  return writeBleCharacteristic(deviceId, buf)
}

/**
 * 执行 BLE 配网
 * 1. 连接设备
 * 2. 获取 provisionKey（60s 缓存）
 * 3. 加密 WiFi 凭据（纯 JS AES-128-CTR）
 * 4. 写入密文
 * 5. 轮询读取配网状态（15s 超时）
 */
export async function provisionBleDevice(
  deviceId: string,
  ssid: string,
  password: string
): Promise<ProvisionResult> {
  const startTime = Date.now()

  try {
    await connectBleDevice(deviceId)

    // 获取 provisionKey
    const { key } = await getProvisionKey(deviceId)
    const seq = Math.floor(Date.now() / 1000)
    const cipherHex = encryptWifiPayload(ssid, password, key, seq)

    // 写入密文
    await writeString(deviceId, cipherHex)

    // 轮询配网状态
    let statusCode = 0
    let currentStep = 0
    while (!isProvisionTimeout(startTime)) {
      await new Promise((r) => setTimeout(r, 500))
      statusCode = await readBleCharacteristic(deviceId)
      const status = resolveProvisionStatus(statusCode)
      if (status.state === 'success') {
        return { success: true }
      }
      if (status.state === 'failed') {
        return { success: false, error_code: String(statusCode), error_message: status.message }
      }
      // 推进步骤（progress 状态）
      currentStep = nextProvisionStep(currentStep)
    }

    // 超时
    return { success: false, error_code: 'TIMEOUT', error_message: '配网超时，请重试' }
  } catch (e) {
    const msg = e instanceof Error ? e.message : '配网失败'
    bleLog('error', 'provision failed', { error: msg })
    clearProvisionCache(deviceId)
    return { success: false, error_code: 'ERROR', error_message: msg }
  } finally {
    disconnectBleDevice(deviceId)
  }
}

// =====================================================================
// H5 Mock BLE 函数（4 步配网页使用，H5/E2E 环境 BLE 不可用）
// 真机环境由上方 provisionBleDevice 等真实 BLE 函数处理
// =====================================================================

function isH5(): boolean {
  // #ifdef H5
  return true
  // #endif
  // #ifndef H5
  return false
  // #endif
}

/** H5 mock：蓝牙初始化（真机走 uni.openBluetoothAdapter） */
export async function initBluetooth(): Promise<boolean> {
  if (isH5()) {
    console.log('[BLE Mock] initBluetooth - H5 mode, skipping')
    return true
  }
  return new Promise((resolve, reject) => {
    uni.openBluetoothAdapter({
      success: () => resolve(true),
      fail: (err) => reject(new Error(`蓝牙初始化失败: ${err.errMsg}`)),
    })
  })
}

/** H5 mock：BLE 连接（1s 延时模拟，真机走 uni.createBLEConnection） */
export async function createBLEConnection(deviceId: string): Promise<boolean> {
  if (isH5()) {
    console.log(`[BLE Mock] createBLEConnection - ${deviceId}`)
    await new Promise((r) => setTimeout(r, 1000))
    return true
  }
  return new Promise((resolve, reject) => {
    uni.createBLEConnection({
      deviceId,
      success: () => resolve(true),
      fail: (err) => reject(new Error(`连接失败: ${err.errMsg}`)),
    })
  })
}

/** H5 mock：写入 WiFi 配置（300ms 延时，真机走 AES-128-CTR 加密写入） */
export async function writeWiFiConfig(ssid: string, password: string): Promise<boolean> {
  if (isH5()) {
    console.log(`[BLE Mock] writeWiFiConfig - SSID: ${ssid}`)
    await new Promise((r) => setTimeout(r, 300))
    return true
  }
  const payload = JSON.stringify({ ssid, password })
  const buf = new ArrayBuffer(payload.length)
  const view = new Uint8Array(buf)
  for (let i = 0; i < payload.length; i++) view[i] = payload.charCodeAt(i)
  return Boolean(buf.byteLength > 0)
}
