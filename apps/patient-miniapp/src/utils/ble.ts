// BLE 配网工具 — T182 对齐技师端已真机验证路径
//
// 基准：apps/tech-miniapp/src/utils/ble.ts（技师端已验）
// 适配：患者端 ble-log.ts 接口（bleLog(level, msg, ctx)）、import 路径
//
// R1: UUID 改为 B510/B511/B512/B513/B514
// R2: connectDevice() 完整流程（close+create+discover+MTU）
// R3: ensureLocationPermission() 权限前置
// R4: writeWifiConfigV2() 180B 分片写 B511
// R5: onWifiStatus() 订阅 B512 Notify + mock 状态机

import { bleLog } from './ble-log'

function _log(level: 'info' | 'warn' | 'error', msg: string, ctx?: unknown) {
  bleLog(level, msg, ctx as Record<string, unknown>)
}

// 检查是否在 H5 环境（BLE 不可用）
function isH5(): boolean {
  // #ifdef H5
  return true
  // #endif
  // #ifndef H5
  return false
  // #endif
}

// UTF-8 手动解码（微信小程序无 TextDecoder）
function decodeUtf8(bytes: Uint8Array): string {
  let result = ''
  let i = 0
  while (i < bytes.length) {
    const byte = bytes[i]
    if (byte < 0x80) {
      result += String.fromCharCode(byte)
      i += 1
    } else if (byte < 0xc0) {
      i += 1
    } else if (byte < 0xe0) {
      result += String.fromCharCode(((byte & 0x1f) << 6) | (bytes[i + 1] & 0x3f))
      i += 2
    } else if (byte < 0xf0) {
      result += String.fromCharCode(((byte & 0x0f) << 12) | ((bytes[i + 1] & 0x3f) << 6) | (bytes[i + 2] & 0x3f))
      i += 3
    } else {
      const codepoint = ((byte & 0x07) << 18) | ((bytes[i + 1] & 0x3f) << 12) | ((bytes[i + 2] & 0x3f) << 6) | (bytes[i + 3] & 0x3f)
      const offset = codepoint - 0x10000
      result += String.fromCharCode(0xd800 + (offset >> 10), 0xdc00 + (offset & 0x3ff))
      i += 4
    }
  }
  return result
}

const H5_BLUETOOTH_ERROR = '蓝牙功能仅支持真机使用，请在手机上操作'

// BLE GATT UUID（协议定稿 §1 — 技师端已验）
const SERVICE_UUID = '0000b510-0000-1000-8000-00805f9b34fb'
const CHAR_WIFI_CONFIG = '0000b511-0000-1000-8000-00805f9b34fb'
const CHAR_WIFI_STATUS = '0000b512-0000-1000-8000-00805f9b34fb'
const CHAR_REALTIME = '0000b513-0000-1000-8000-00805f9b34fb'
const CHAR_DEVICE_INFO = '0000b514-0000-1000-8000-00805f9b34fb'

// 模块级状态
let discoveryTimer: ReturnType<typeof setTimeout> | null = null
let adapterStateRegistered = false
let bleStateRegistered = false
let bleStateCallback: ((deviceId: string, connected: boolean) => void) | null = null

function isAuthDeny(errMsg: string): boolean {
  const m = (errMsg || '').toLowerCase()
  return m.includes('auth') || m.includes('deny') || m.includes('permission')
}

// ===== 权限前置 =====

/**
 * 权限前置：扫描前检查 scope.userLocation（Android BLE 必需）
 */
export async function ensureLocationPermission(): Promise<boolean> {
  if (isH5()) return true
  // #ifdef MP-WEIXIN
  _log('info', 'ensureLocationPermission 入口')
  return new Promise((resolve) => {
    const checkAndRequest = () => {
      try {
        wx.getSetting({
          success: (res) => {
            _log('info', `wx.getSetting 成功，authSetting=${JSON.stringify(res.authSetting)}`)
            if (res.authSetting['scope.userLocation']) {
              _log('info', '位置权限已授权')
              resolve(true)
              return
            }
            _log('warn', 'scope.userLocation 未授权，尝试 wx.authorize')
            wx.authorize({
              scope: 'scope.userLocation',
              success: () => {
                _log('info', 'wx.authorize 位置权限成功')
                resolve(true)
              },
              fail: (err) => {
                _log('warn', `wx.authorize 被拒，errMsg=${err?.errMsg || 'unknown'}`)
                uni.showModal({
                  title: '需要位置权限',
                  content: '扫描附近蓝牙设备需要位置权限，请在设置中开启后重试',
                  confirmText: '去设置',
                  success: (r) => {
                    if (r.confirm) {
                      wx.openSetting({
                        success: () => {
                          setTimeout(() => {
                            wx.getSetting({
                              success: (s2) => resolve(!!s2.authSetting['scope.userLocation']),
                              fail: () => resolve(false),
                            })
                          }, 300)
                        },
                        complete: () => {},
                      })
                    } else {
                      resolve(false)
                    }
                  },
                  fail: () => resolve(false),
                })
              },
            })
          },
          fail: (err) => {
            _log('warn', `wx.getSetting 失败（不阻断），errMsg=${err?.errMsg || ''}`)
            resolve(true)
          },
        })
      } catch (e) {
        _log('error', 'ensureLocationPermission 异常', e instanceof Error ? e.message : String(e))
        resolve(true)
      }
    }

    let systemSettingResolved = false
    const finishSystemCheck = () => {
      if (systemSettingResolved) return
      systemSettingResolved = true
    }
    try {
      wx.getSystemSetting({
        success: (sys) => {
          finishSystemCheck()
          if (sys.locationEnabled === false) {
            uni.showModal({
              title: '请开启手机位置服务',
              content: '扫描蓝牙设备需要开启手机位置服务，请在系统设置中开启后重试',
              confirmText: '我知道了',
              showCancel: false,
              success: () => resolve(false),
              fail: () => resolve(false),
            })
            return
          }
          checkAndRequest()
        },
        fail: () => {
          finishSystemCheck()
          checkAndRequest()
        },
      })
    } catch {
      finishSystemCheck()
      checkAndRequest()
    }
    setTimeout(() => {
      if (!systemSettingResolved) {
        finishSystemCheck()
        checkAndRequest()
      }
    }, 500)
  })
  // #endif
  // #ifndef MP-WEIXIN
  return true
  // #endif
}

function registerAdapterStateListener() {
  if (adapterStateRegistered || isH5()) return
  adapterStateRegistered = true
  uni.onBluetoothAdapterStateChange((res) => {
    _log('info', `onBluetoothAdapterStateChange: available=${res.available}`)
    if (!res.available) {
      uni.showToast({ title: '手机蓝牙已关闭', icon: 'none' })
      if (discoveryTimer) { clearTimeout(discoveryTimer); discoveryTimer = null }
      uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
    }
  })
}

// ===== BLE 基础操作 =====

export async function initBluetooth(): Promise<boolean> {
  if (isH5()) throw new Error(H5_BLUETOOTH_ERROR)
  _log('info', 'initBluetooth 入口')
  return new Promise((resolve, reject) => {
    uni.openBluetoothAdapter({
      success: () => {
        _log('info', 'adapter 初始化成功')
        registerAdapterStateListener()
        resolve(true)
      },
      fail: (err) => {
        const errMsg = err?.errMsg || 'unknown'
        _log('error', `adapter 初始化失败: ${errMsg}`)
        if (isAuthDeny(errMsg)) {
          ensureLocationPermission().then((ok) => {
            if (ok) resolve(true)
            else reject(new Error(`蓝牙初始化失败: ${errMsg}`))
          })
        } else {
          reject(new Error(`蓝牙初始化失败: ${errMsg}`))
        }
      },
    })
  })
}

export async function createBLEConnection(deviceId: string): Promise<boolean> {
  if (isH5()) return false
  return new Promise((resolve, reject) => {
    let settled = false
    const timer = setTimeout(() => {
      if (settled) return
      settled = true
      _log('error', `createBLEConnection 超时 deviceId=${deviceId}`)
      uni.closeBLEConnection({ deviceId, fail: () => {} })
      reject(new Error('连接超时，请靠近设备后重试'))
    }, 10000)
    uni.createBLEConnection({
      deviceId,
      timeout: 10000,
      success: () => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        _log('info', `连接成功 deviceId=${deviceId}`)
        resolve(true)
      },
      fail: (err) => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        const errMsg = err?.errMsg || ''
        if (errMsg.includes('already connect')) {
          _log('info', `设备已连接（already connect），视为成功 deviceId=${deviceId}`)
          resolve(true)
          return
        }
        _log('error', `连接失败 deviceId=${deviceId}: ${errMsg}`)
        uni.closeBLEConnection({ deviceId, fail: () => {} })
        reject(new Error(`连接失败: ${errMsg}`))
      },
    })
  })
}

export async function closeBLEConnection(deviceId: string): Promise<void> {
  if (isH5()) return
  return new Promise((resolve) => {
    uni.closeBLEConnection({ deviceId, success: () => resolve(), fail: () => resolve() })
  })
}

/**
 * 完整 BLE 连接流程 — close + create + discover services + setBLEMTU
 * （技师端已验，患者端复用）
 */
export async function connectDevice(deviceId: string): Promise<boolean> {
  if (isH5()) {
    // H5 无蓝牙栈：模拟真机建连耗时（close + create + 服务发现），让 03 的「连接中」态可观测
    await new Promise((r) => setTimeout(r, 800))
    return true
  }
  _log('info', `connectDevice 入口 deviceId=${deviceId}`)

  // 前置 close 清残留状态
  try { await closeBLEConnection(deviceId) } catch {}

  try {
    await createBLEConnection(deviceId)
  } catch (e) {
    throw e
  }

  // Android 兼容：连接后延时再发现服务
  await new Promise((r) => setTimeout(r, 300))

  try {
    // 发现服务
    const services = await new Promise<string[]>((resolve, reject) => {
      uni.getBLEDeviceServices({
        deviceId,
        success: (res) => resolve((res.services || []).map((s: any) => s.uuid)),
        fail: (err) => reject(new Error(`服务发现失败: ${err?.errMsg}`)),
      })
    })
    _log('info', `服务列表=${JSON.stringify(services)}`)

    const targetService = services.find((u) => u.toLowerCase() === SERVICE_UUID.toLowerCase())
    if (!targetService) {
      throw new Error('设备未暴露 BraceSync 服务，请确认设备固件版本')
    }

    // 发现特征
    await new Promise<void>((resolve, reject) => {
      uni.getBLEDeviceCharacteristics({
        deviceId,
        serviceId: SERVICE_UUID,
        success: () => resolve(),
        fail: (err) => reject(new Error(`特征发现失败: ${err?.errMsg}`)),
      })
    })

    // 协商 MTU=247（适配 40 字节数据帧单包传输）
    try {
      await new Promise<void>((resolve) => {
        uni.setBLEMTU({
          deviceId,
          mtu: 247,
          success: (res: any) => resolve(),
          fail: () => resolve(), // 失败不阻断
        })
      })
    } catch { /* 忽略 */ }
  } catch (e) {
    _log('warn', `connectDevice 服务发现失败，关闭连接 deviceId=${deviceId}`)
    await closeBLEConnection(deviceId)
    throw e
  }

  _log('info', `connectDevice 完成 deviceId=${deviceId}`)
  return true
}

// ===== BLE 扫描（设备绑定阶段可能用到） =====

export async function discoverDevices(): Promise<{ deviceId: string; name: string; RSSI: number }[]> {
  if (isH5()) return []
  _log('info', 'discoverDevices 入口')
  const permOk = await ensureLocationPermission()
  if (!permOk) throw new Error('蓝牙/位置权限未授权')

  return new Promise((resolve, reject) => {
    const found = new Map<string, { deviceId: string; name: string; RSSI: number }>()
    const cleanup = () => {
      if (discoveryTimer) { clearTimeout(discoveryTimer); discoveryTimer = null }
      uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
      uni.offBluetoothDeviceFound()
    }
    const SCAN_DURATION = 6000

    uni.startBluetoothDevicesDiscovery({
      allowDuplicatesKey: false,
      success: () => {
        uni.onBluetoothDeviceFound((res) => {
          const devs = res.devices as unknown as { deviceId: string; name: string; localName?: string; RSSI: number }[]
          for (const dev of devs) {
            const devName = dev.name || dev.localName || ''
            if (devName.startsWith('BSYNC-')) {
              found.set(dev.deviceId, { deviceId: dev.deviceId, name: devName, RSSI: dev.RSSI })
            }
          }
        })
        discoveryTimer = setTimeout(() => {
          cleanup()
          resolve(Array.from(found.values()))
        }, SCAN_DURATION)
      },
      fail: (err) => {
        cleanup()
        reject(new Error(`设备扫描失败: ${err?.errMsg}`))
      },
    })
  })
}

// ===== 蓝牙/位置前置态查询（01-entry 用） =====

/**
 * 蓝牙是否可用（开关已开 + adapter 可初始化）。
 * 与 initBluetooth 的区别：这里不把失败当异常抛出，只回布尔，供清单展示。
 */
export async function isBluetoothReady(): Promise<boolean> {
  if (isH5()) return true
  return new Promise((resolve) => {
    uni.openBluetoothAdapter({
      success: () => {
        registerAdapterStateListener()
        uni.getBluetoothAdapterState({
          success: (res) => resolve(!!res.available),
          fail: () => resolve(true), // 能 open 即视为可用（部分机型无 state 接口）
        })
      },
      fail: (err) => {
        _log('warn', `蓝牙不可用: ${err?.errMsg || 'unknown'}`)
        resolve(false)
      },
    })
  })
}

/**
 * 位置权限是否已授权（只读查询，不触发授权弹窗；授权动作由页面按设计稿弹窗引导）。
 */
export async function isLocationAuthed(): Promise<boolean> {
  if (isH5()) return true
  // #ifdef MP-WEIXIN
  return new Promise((resolve) => {
    try {
      wx.getSetting({
        success: (res) => resolve(res.authSetting['scope.userLocation'] !== false),
        fail: () => resolve(true),
      })
    } catch {
      resolve(true)
    }
  })
  // #endif
  // #ifndef MP-WEIXIN
  return true
  // #endif
}

// ===== 渐进式扫描（02-scan：扫描期间逐个出列表） =====

export interface ScannedDevice {
  deviceId: string
  name: string
  RSSI: number
}

let scanActive = false

/**
 * 开始扫描并过滤 BSYNC- 前缀设备。
 * @param onDevice 每发现一台新设备回调一次（列表边扫边出）
 * @param onDone   扫描窗口结束回调；found=0 时页面展示空态引导
 * @param duration 扫描窗口，默认 6s
 */
export function startBleScan(
  onDevice: (dev: ScannedDevice) => void,
  onDone: (found: number) => void,
  duration = 6000
): void {
  // H5（dev / E2E）：无真实蓝牙，返回设计稿示例设备，让全流程可走通；真机不受影响
  if (isH5()) {
    scanActive = true
    const mock: ScannedDevice = { deviceId: 'h5-mock-ble-701001', name: 'BSYNC-701001', RSSI: -50 }
    setTimeout(() => {
      if (!scanActive) return
      onDevice(mock)
    }, 800)
    setTimeout(() => {
      if (!scanActive) return
      scanActive = false
      onDone(1)
    }, duration)
    return
  }

  _log('info', 'startBleScan 入口')
  scanActive = true
  const seen = new Set<string>()

  const finish = () => {
    if (discoveryTimer) { clearTimeout(discoveryTimer); discoveryTimer = null }
    uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
    uni.offBluetoothDeviceFound()
    if (scanActive) {
      scanActive = false
      onDone(seen.size)
    }
  }

  uni.startBluetoothDevicesDiscovery({
    allowDuplicatesKey: false,
    success: () => {
      uni.onBluetoothDeviceFound((res) => {
        const devs = res.devices as unknown as {
          deviceId: string; name: string; localName?: string; RSSI: number
        }[]
        for (const dev of devs) {
          const devName = dev.name || dev.localName || ''
          if (!devName.startsWith('BSYNC-') || seen.has(dev.deviceId)) continue
          seen.add(dev.deviceId)
          _log('info', `发现设备 ${devName} RSSI=${dev.RSSI}`)
          onDevice({ deviceId: dev.deviceId, name: devName, RSSI: dev.RSSI })
        }
      })
      discoveryTimer = setTimeout(finish, duration)
    },
    fail: (err) => {
      _log('error', `startBluetoothDevicesDiscovery 失败: ${err?.errMsg}`)
      discoveryTimer = setTimeout(finish, 0)
    },
  })
}

/** 中止扫描（页面卸载 / 用户离开扫描页时调用） */
export function stopBleScan(): void {
  if (!scanActive) return
  scanActive = false
  if (discoveryTimer) { clearTimeout(discoveryTimer); discoveryTimer = null }
  if (isH5()) return
  uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
  uni.offBluetoothDeviceFound()
}

// ===== BLE 连接状态监听 =====

export function registerBleStateListener(
  cb: (deviceId: string, connected: boolean) => void
): void {
  bleStateCallback = cb
  if (bleStateRegistered || isH5()) return
  bleStateRegistered = true
  uni.onBLEConnectionStateChange((res) => {
    _log('info', `onBLEConnectionStateChange: deviceId=${res.deviceId}, connected=${res.connected}`)
    bleStateCallback?.(res.deviceId, res.connected)
  })
}

// ===== WiFi 加密配置写入 + 状态机（配网核心） =====

let wifiStatusTimer: ReturnType<typeof setInterval> | null = null
let wifiStatusCallback: ((code: number) => void) | null = null
let b512NotifyRegistered = false
let wifiStatusDeviceId = ''

const WIFI_CHUNK_SIZE = 180

function hexToBytesLocal(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16)
  }
  return bytes
}

/**
 * 写入加密后的 WiFi 配置（AES-128-CTR 密文 hex）
 * 协议 §4：密文按每片 ≤180 字节切片，对 B511 特征顺序 Write
 */
export async function writeWifiConfigV2(
  deviceId: string,
  encryptedHex: string
): Promise<boolean> {
  if (isH5()) return true
  const cipher = hexToBytesLocal(encryptedHex)
  const totalChunks = Math.ceil(cipher.length / WIFI_CHUNK_SIZE)
  _log('info', `B511 写入开始：密文长=${cipher.length}B，分片数=${totalChunks}`)

  for (let off = 0, idx = 0; off < cipher.length; off += WIFI_CHUNK_SIZE, idx++) {
    const chunk = cipher.slice(off, off + WIFI_CHUNK_SIZE)
    await new Promise<void>((resolve, reject) => {
      uni.writeBLECharacteristicValue({
        deviceId,
        serviceId: SERVICE_UUID,
        characteristicId: CHAR_WIFI_CONFIG,
        value: chunk.buffer,
        success: () => resolve(),
        fail: (err) => reject(new Error(`B511 写入失败: ${err?.errMsg}`)),
      })
    })
    _log('info', `B511 分片 ${idx + 1}/${totalChunks} 写入成功 len=${chunk.length}`)
  }
  _log('info', 'B511 全部分片写入完成')
  return true
}

/**
 * 注册配网状态机回调
 * 状态码：0收到 1连AP 2取IP 3探测 9成功 -1密码错 -2 SSID不见 -3 DHCP失败 -4云端不可达
 */
export function onWifiStatus(cb: (code: number) => void, deviceId?: string): void {
  wifiStatusCallback = cb
  if (isH5()) return
  if (deviceId) wifiStatusDeviceId = deviceId
  if (b512NotifyRegistered || !wifiStatusDeviceId) return
  b512NotifyRegistered = true

  uni.notifyBLECharacteristicValueChange({
    deviceId: wifiStatusDeviceId,
    serviceId: SERVICE_UUID,
    characteristicId: CHAR_WIFI_STATUS,
    state: true,
    success: () => _log('info', 'B512 Notify 订阅成功'),
    fail: (err) => _log('error', `B512 Notify 订阅失败: ${err?.errMsg}`),
  })
  uni.onBLECharacteristicValueChange((res) => {
    if (res.characteristicId?.toLowerCase() !== CHAR_WIFI_STATUS.toLowerCase()) return
    try {
      const code = new Int8Array(res.value)[0]
      _log('info', `B512 状态原始值=${code}`)
      wifiStatusCallback?.(code)
    } catch (e) {
      _log('error', 'B512 状态解析失败', e instanceof Error ? e.message : String(e))
    }
  })
}

/** H5 mock：模拟配网状态机推进（默认 0→1→2→3→9；可传入失败序列） */
export function startMockWifiStatusSequence(seq: number[] = [0, 1, 2, 3, 9]): void {
  if (!isH5()) return
  let idx = 0
  if (wifiStatusTimer) clearInterval(wifiStatusTimer)
  wifiStatusTimer = setInterval(() => {
    if (idx < seq.length) {
      wifiStatusCallback?.(seq[idx])
      idx++
    } else {
      if (wifiStatusTimer) clearInterval(wifiStatusTimer)
    }
  }, 500)
}

/** 停止 mock 状态机 */
export function stopMockWifiStatusSequence(): void {
  if (wifiStatusTimer) {
    clearInterval(wifiStatusTimer)
    wifiStatusTimer = null
  }
}
