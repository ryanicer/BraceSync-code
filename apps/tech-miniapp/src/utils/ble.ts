// BLE 近场调试工具：T089 扩展
// 新增：Realtime 推送 / WiFi 加密配置写入 / WiFi Status 状态机监听 / 设备信息读取
// R5：全链路日志（ble-log.ts）、B512 Notify 订阅、权限前置、蓝牙开关监听
// H5 dev 模式下 BLE API 不可用 → 蓝牙相关方法抛错由上层处理；discoverDevices 返回空数组；
// 实时推送 & 配网状态机在 H5 下使用模拟数据（mock），真机联调以硬件为准。

import { bleLog } from './ble-log'

// 检查是否在 H5 环境（BLE 不可用）
function isH5(): boolean {
  // #ifdef H5
  return true
  // #endif
  // #ifndef H5
  return false
  // #endif
}

// T101: 微信小程序不支持 TextDecoder，手动实现 UTF-8 字节解码
function decodeUtf8(bytes: Uint8Array): string {
  let result = ''
  let i = 0
  while (i < bytes.length) {
    const byte = bytes[i]
    if (byte < 0x80) {
      result += String.fromCharCode(byte)
      i += 1
    } else if (byte < 0xc0) {
      // 非法续字节，跳过
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

// T109 修复版本标识（通过日志可确认小顾测试的是否是修复版）
const BLE_FIX_VERSION = 'T109-fix-v2-close-before-connect'

const H5_BLUETOOTH_ERROR = '蓝牙功能仅支持真机使用，请在手机上操作'

// BLE GATT UUID（协议定稿 §1）
const SERVICE_UUID = '0000b510-0000-1000-8000-00805f9b34fb'
const CHAR_WIFI_CONFIG = '0000b511-0000-1000-8000-00805f9b34fb'
const CHAR_WIFI_STATUS = '0000b512-0000-1000-8000-00805f9b34fb'
const CHAR_REALTIME = '0000b513-0000-1000-8000-00805f9b34fb'
const CHAR_DEVICE_INFO = '0000b514-0000-1000-8000-00805f9b34fb'

// 模块级扫描定时器（供蓝牙开关监听回调清理）
let discoveryTimer: ReturnType<typeof setTimeout> | null = null
let adapterStateRegistered = false

// T109: BLE 连接状态变化监听（全局只注册一次）
let bleStateRegistered = false
let bleStateCallback: ((deviceId: string, connected: boolean) => void) | null = null

function isAuthDeny(errMsg: string): boolean {
  const m = (errMsg || '').toLowerCase()
  return m.includes('auth') || m.includes('deny') || m.includes('permission')
}

/**
 * T101 权限前置：扫描前检查 scope.userLocation（Android BLE 必需）
 *
 * 修复点（相对 R5-2）：
 *  1. 前置 wx.getSystemSetting 检查系统位置服务开关，区分"权限被拒"与"位置服务关闭"，
 *     避免引导用户去 openSetting 却无法解决系统开关问题。
 *  2. openSetting 返回后不依赖其 authSetting（部分 Android 机型不即时刷新），
 *     改为 complete 回调中延迟重新 wx.getSetting 取最新状态。
 *  3. 日志区分四种状态，便于真机排查。
 *
 * iOS 无需位置权限，但仍走 getSetting 不影响。
 */
export async function ensureLocationPermission(): Promise<boolean> {
  if (isH5()) return true
  // #ifdef MP-WEIXIN
  bleLog.info('ensureLocationPermission 入口')
  return new Promise((resolve) => {
    const checkAndRequest = () => {
      try {
        wx.getSetting({
          success: (res) => {
            bleLog.info('wx.getSetting 成功，完整 authSetting', JSON.stringify(res.authSetting))
            if (res.authSetting['scope.userLocation']) {
              bleLog.info('位置权限已授权（scope.userLocation=true）')
              resolve(true)
              return
            }
            bleLog.warn('scope.userLocation 未授权，尝试 wx.authorize')
            wx.authorize({
              scope: 'scope.userLocation',
              success: () => {
                bleLog.info('wx.authorize 位置权限成功')
                resolve(true)
              },
              fail: (err) => {
                bleLog.warn(`wx.authorize 位置权限被拒，errMsg=${err?.errMsg || 'unknown'}，引导 openSetting`)
                uni.showModal({
                  title: '需要位置权限',
                  content: '扫描附近蓝牙设备需要位置权限，请在设置中开启后重试',
                  confirmText: '去设置',
                  success: (r) => {
                    bleLog.info(`用户对权限弹窗的选择: confirm=${r.confirm}`)
                    if (r.confirm) {
                      bleLog.info('调用 wx.openSetting')
                      wx.openSetting({
                        success: (s) => {
                          bleLog.info(`wx.openSetting 成功回调，authSetting=${JSON.stringify(s.authSetting)}`)
                        },
                        fail: (err) => {
                          bleLog.warn(`wx.openSetting 失败回调，errMsg=${err?.errMsg || 'unknown'}`)
                        },
                        complete: () => {
                          bleLog.info('wx.openSetting complete 回调，延迟 300ms 后重新 getSetting')
                          // T101: 不依赖 openSetting 成功回调的 authSetting（Android 可能不即时刷新），
                          // 延迟后重新 wx.getSetting 取最新授权状态
                          setTimeout(() => {
                            wx.getSetting({
                              success: (s2) => {
                                const ok = !!s2.authSetting['scope.userLocation']
                                bleLog.info(`openSetting 返回后重检 getSetting，authSetting=${JSON.stringify(s2.authSetting)}，scope.userLocation=${ok}`)
                                resolve(ok)
                              },
                              fail: (err2) => {
                                bleLog.warn(`openSetting 后重检 getSetting 失败，errMsg=${err2?.errMsg || 'unknown'}，按未授权处理`)
                                resolve(false)
                              },
                            })
                          }, 300)
                        },
                      })
                    } else {
                      bleLog.info('用户取消权限引导弹窗，resolve(false)')
                      resolve(false)
                    }
                  },
                  fail: (err) => {
                    bleLog.warn(`权限引导弹窗失败，errMsg=${err?.errMsg || 'unknown'}，resolve(false)`)
                    resolve(false)
                  },
                })
              },
            })
          },
          fail: (err) => {
            bleLog.warn(`wx.getSetting 失败，errMsg=${err?.errMsg || 'unknown'}，不阻断，resolve(true)`)
            resolve(true) // getSetting 失败不阻断
          },
        })
      } catch (e) {
        bleLog.error('ensureLocationPermission checkAndRequest 异常', e instanceof Error ? e.message : String(e))
        resolve(true)
      }
    }

    // T101: 前置系统位置服务检查（Android BLE 扫描依赖系统位置开关）
    let systemSettingResolved = false
    const finishSystemCheck = () => {
      if (systemSettingResolved) return
      systemSettingResolved = true
    }
    try {
      wx.getSystemSetting({
        success: (sys) => {
          finishSystemCheck()
          bleLog.info(`wx.getSystemSetting 成功，locationEnabled=${sys.locationEnabled}，bluetoothEnabled=${sys.bluetoothEnabled}`)
          if (sys.locationEnabled === false) {
            bleLog.warn('系统位置服务未开启（locationEnabled=false），提示用户开启')
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
          bleLog.info('系统位置服务已开启，进入 scope 授权检查流程')
          checkAndRequest()
        },
        fail: (err) => {
          finishSystemCheck()
          bleLog.warn(`wx.getSystemSetting 失败，errMsg=${err?.errMsg || 'unknown'}，回退正常授权流程`)
          // 旧基础库或不支持 getSystemSetting，回退正常授权流程
          checkAndRequest()
        },
      })
    } catch (e) {
      finishSystemCheck()
      bleLog.error('ensureLocationPermission getSystemSetting 异常，回退正常授权流程', e instanceof Error ? e.message : String(e))
      checkAndRequest()
    }
    // T101: 超时兜底——若 wx.getSystemSetting 500ms 内未回调，直接进入正常授权流程，避免卡死（部分 Android 机型此 API 不回调）
    setTimeout(() => {
      if (!systemSettingResolved) {
        bleLog.warn('wx.getSystemSetting 超时（500ms）未回调，回退正常授权流程')
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

/** R5-3 注册蓝牙适配器状态变化监听（available=false 时提示并终止扫描） */
function registerAdapterStateListener() {
  if (adapterStateRegistered || isH5()) return
  adapterStateRegistered = true
  uni.onBluetoothAdapterStateChange((res) => {
    bleLog.info(`onBluetoothAdapterStateChange: available=${res.available}, discovering=${res.discovering}`)
    if (!res.available) {
      bleLog.warn('手机蓝牙已关闭（available=false），终止扫描')
      uni.showToast({ title: '手机蓝牙已关闭', icon: 'none' })
      if (discoveryTimer) {
        clearTimeout(discoveryTimer)
        discoveryTimer = null
      }
      uni.stopBluetoothDevicesDiscovery({ fail: () => {} })
    }
  })
}

export async function initBluetooth(): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  bleLog.info(`initBluetooth 入口 version=${BLE_FIX_VERSION}，调用 openBluetoothAdapter`)
  return new Promise((resolve, reject) => {
    uni.openBluetoothAdapter({
      success: () => {
        bleLog.info('adapter 初始化成功（openBluetoothAdapter success）')
        registerAdapterStateListener()
        resolve(true)
      },
      fail: (err) => {
        const errMsg = err?.errMsg || 'unknown'
        bleLog.error(`adapter 初始化失败（openBluetoothAdapter fail），errMsg=${errMsg}，isAuthDeny=${isAuthDeny(errMsg)}`)
        if (isAuthDeny(errMsg)) {
          bleLog.info('adapter 失败原因为权限类，调用 ensureLocationPermission')
          ensureLocationPermission().then((ok) => {
            bleLog.info(`ensureLocationPermission 返回: ${ok}`)
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

export async function discoverDevices(): Promise<{ deviceId: string; name: string; RSSI: number }[]> {
  if (isH5()) {
    // H5 不造假设备，返回空数组由上层展示空态
    return []
  }
  bleLog.info('discoverDevices 入口，先检查位置权限')
  // R5-2 权限前置
  const permOk = await ensureLocationPermission()
  bleLog.info(`discoverDevices 权限检查结果: permOk=${permOk}`)
  if (!permOk) {
    throw new Error('蓝牙/位置权限未授权，请在设置中开启后重试')
  }
  return new Promise((resolve, reject) => {
    const found = new Map<string, { deviceId: string; name: string; RSSI: number }>()

    const cleanup = () => {
      if (discoveryTimer) {
        clearTimeout(discoveryTimer)
        discoveryTimer = null
      }
      uni.stopBluetoothDevicesDiscovery({ fail: (e) => { bleLog.warn(`stopBluetoothDevicesDiscovery fail: ${e?.errMsg || ''}`) } })
      uni.offBluetoothDeviceFound()
    }

    // T101: 扫描时长 3s→6s，给 scan response（含设备名）留足时间
    const SCAN_DURATION = 6000

    bleLog.info('调用 startBluetoothDevicesDiscovery')
    uni.startBluetoothDevicesDiscovery({
      allowDuplicatesKey: false,
      success: () => {
        bleLog.info('startBluetoothDevicesDiscovery 成功，注册 onBluetoothDeviceFound')
        uni.onBluetoothDeviceFound((res) => {
          const devs = res.devices as unknown as { deviceId: string; name: string; localName?: string; RSSI: number }[]
          bleLog.info(`onBluetoothDeviceFound 原始设备数=${devs.length}`, devs.slice(0, 20).map((d) => ({ name: d.name || '(空)', localName: d.localName || '(空)', deviceId: d.deviceId, RSSI: d.RSSI })))
          for (const dev of devs) {
            const d = dev
            // T101: Android 上广播名常在 scan response 的 localName 字段，name 可能为空
            const devName = d.name || d.localName || ''
            // 协议 §1：广播名 BSYNC-{device_id 后 6 位}，仅保留 BraceSync 设备
            if (devName.startsWith('BSYNC-')) {
              bleLog.info(`[匹配] BraceSync 设备: name=${devName}, deviceId=${d.deviceId}, RSSI=${d.RSSI}`)
              found.set(d.deviceId, { deviceId: d.deviceId, name: devName, RSSI: d.RSSI })
            } else {
              bleLog.info(`[过滤] 非 BSYNC- 设备: name=${devName || '(空)'}, deviceId=${d.deviceId}, RSSI=${d.RSSI}`)
            }
          }
        })
        discoveryTimer = setTimeout(() => {
          cleanup()
          // T101: 扫描结束后调 getBluetoothDevices 拿系统缓存设备（名字更全），补齐可能漏掉的 BSYNC 设备
          wx.getBluetoothDevices({
            success: (res) => {
              const cached = res.devices as unknown as { deviceId: string; name: string; localName?: string; RSSI: number }[]
              bleLog.info(`getBluetoothDevices 缓存设备数=${cached.length}`)
              for (const dev of cached) {
                const devName = dev.name || dev.localName || ''
                if (devName.startsWith('BSYNC-')) {
                  if (!found.has(dev.deviceId)) {
                    bleLog.info(`[缓存补入] BraceSync 设备: name=${devName}, deviceId=${dev.deviceId}, RSSI=${dev.RSSI}`)
                    found.set(dev.deviceId, { deviceId: dev.deviceId, name: devName, RSSI: dev.RSSI })
                  }
                }
              }
              bleLog.info(`扫描结束，最终 BSYNC 设备数=${found.size}`)
              resolve(Array.from(found.values()))
            },
            fail: (err) => {
              bleLog.warn(`getBluetoothDevices 失败，errMsg=${err?.errMsg || ''}，使用 onBluetoothDeviceFound 结果`)
              bleLog.info(`扫描结束，BSYNC 设备数=${found.size}`)
              resolve(Array.from(found.values()))
            },
          })
        }, SCAN_DURATION)
      },
      fail: async (err) => {
        const errMsg = err?.errMsg || 'unknown'
        bleLog.error(`startBluetoothDevicesDiscovery 失败，errMsg=${errMsg}，isAuthDeny=${isAuthDeny(errMsg)}`)
        cleanup()
        // T101: 若失败原因为权限类，重新触发授权流程，避免用户卡在"扫描失败"无法回到授权引导
        if (isAuthDeny(err?.errMsg || '')) {
          bleLog.warn('扫描失败原因为权限问题，重新触发 ensureLocationPermission')
          const ok = await ensureLocationPermission()
          if (ok) {
            resolve([]) // 权限已恢复，返回空结果让用户可再次点击刷新
            return
          }
        }
        reject(new Error(`设备扫描失败: ${err.errMsg}`))
      },
    })
  })
}

export async function createBLEConnection(deviceId: string): Promise<boolean> {
  if (isH5()) {
    // H5 下 BLE 不可用，返回 false 让上层走"连接失败但不阻断"分支
    return false
  }
  return new Promise((resolve, reject) => {
    let settled = false
    const timer = setTimeout(() => {
      if (settled) return
      settled = true
      bleLog.error(`createBLEConnection 超时（10s）deviceId=${deviceId}，清理连接状态`)
      // 超时必须 closeBLEConnection 释放微信内部"连接中"状态，否则后续重连必报 create BLE connect fail
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
        bleLog.info(`连接成功 deviceId=${deviceId}`)
        resolve(true)
      },
      fail: (err) => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        const errMsg = err?.errMsg || ''
        // T109: "already connect" 表示设备已处于连接状态，视为连接成功
        //       （常见于小程序未关闭干净重进、或 selectDevice 后 bindManual 重复连接）
        if (errMsg.includes('already connect')) {
          bleLog.info(`设备已连接（already connect），视为连接成功 deviceId=${deviceId}`)
          resolve(true)
          return
        }
        bleLog.error(`连接失败 deviceId=${deviceId}`, errMsg)
        // T109: 失败时主动 closeBLEConnection 释放微信内部"连接中"状态
        uni.closeBLEConnection({ deviceId, fail: () => {} })
        reject(new Error(`连接失败: ${errMsg}`))
      },
    })
  })
}

/**
 * T109: 完整 BLE 连接流程 —— createBLEConnection + getBLEDeviceServices + getBLEDeviceCharacteristics
 *
 * 微信 BLE API 要求：连接后必须发现服务与特征值，否则 read/write/notify 全部失败。
 * Android 部分机型连接后需短暂延时再发现服务，这里统一延时 300ms。
 * H5 下返回 true（mock）。
 */
export async function connectDevice(deviceId: string): Promise<boolean> {
  if (isH5()) {
    return true
  }
  bleLog.info(`connectDevice 入口 version=${BLE_FIX_VERSION} deviceId=${deviceId}`)
  // T109: 连接前先 closeBLEConnection 清理可能残留的"连接中"状态。
  //       若上次连接失败/超时未释放，微信内部会认为设备仍在连接中，
  //       再次 createBLEConnection 直接报 "create BLE connect fail"。
  try {
    await closeBLEConnection(deviceId)
  } catch (e) {
    bleLog.warn(`connectDevice 前置 closeBLEConnection 异常（忽略）`, e instanceof Error ? e.message : String(e))
  }
  try {
    await createBLEConnection(deviceId)
  } catch (e) {
    // createBLEConnection 内部已 closeBLEConnection 清理，这里直接抛出
    throw e
  }
  // Android 兼容：连接后延时再发现服务
  await new Promise((r) => setTimeout(r, 300))
  try {
    const services = await new Promise<string[]>((resolve, reject) => {
      uni.getBLEDeviceServices({
        deviceId,
        success: (res) => {
          const uuids = (res.services || []).map((s: any) => s.uuid)
          bleLog.info(`getBLEDeviceServices 成功，服务列表=${JSON.stringify(uuids)}`)
          resolve(uuids)
        },
        fail: (err) => {
          bleLog.error(`getBLEDeviceServices 失败`, err?.errMsg)
          reject(new Error(`服务发现失败: ${err.errMsg}`))
        },
      })
    })
    // 确认目标服务存在
    const targetService = services.find(
      (u) => u.toLowerCase() === SERVICE_UUID.toLowerCase()
    )
    if (!targetService) {
      bleLog.warn(`未找到目标服务 ${SERVICE_UUID}，可用服务=${JSON.stringify(services)}`)
      throw new Error('设备未暴露 BraceSync 服务，请确认设备固件版本')
    }
    await new Promise<void>((resolve, reject) => {
      uni.getBLEDeviceCharacteristics({
        deviceId,
        serviceId: SERVICE_UUID,
        success: (res) => {
          const chars = (res.characteristics || []).map((c: any) => c.uuid)
          bleLog.info(`getBLEDeviceCharacteristics 成功，特征列表=${JSON.stringify(chars)}`)
          resolve()
        },
        fail: (err) => {
          bleLog.error(`getBLEDeviceCharacteristics 失败`, err?.errMsg)
          reject(new Error(`特征发现失败: ${err.errMsg}`))
        },
      })
    })
  } catch (e) {
    // T109: 连接已建立但服务/特征发现失败 → 必须 closeBLEConnection 释放连接，
    //       否则微信内部认为设备已连接，后续重连必报 "create BLE connect fail"。
    bleLog.warn(`connectDevice 服务发现失败，关闭已建立的连接 deviceId=${deviceId}`)
    await closeBLEConnection(deviceId)
    throw e
  }
  bleLog.info(`connectDevice 完成 deviceId=${deviceId}`)
  return true
}

/**
 * T109: 注册 BLE 连接状态变化监听（全局只注册一次）
 * 回调参数：(deviceId, connected) —— connected=false 表示设备意外断连
 */
export function registerBleStateListener(
  cb: (deviceId: string, connected: boolean) => void
): void {
  bleStateCallback = cb
  if (bleStateRegistered || isH5()) return
  bleStateRegistered = true
  uni.onBLEConnectionStateChange((res) => {
    bleLog.info(
      `onBLEConnectionStateChange: deviceId=${res.deviceId}, connected=${res.connected}`
    )
    bleStateCallback?.(res.deviceId, res.connected)
  })
}

// T089-DEPRECATED: 旧明文版 writeWiFiConfig，联调稳定后清理（用 writeWifiConfigV2 替代）
export async function writeWiFiConfig(ssid: string, password: string): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  const payload = JSON.stringify({ ssid, password })
  const buffer = new TextEncoder().encode(payload)
  return new Promise((resolve) => {
    resolve(Boolean(buffer.byteLength > 0))
  })
}

export async function closeBLEConnection(deviceId: string): Promise<void> {
  if (isH5()) {
    return
  }
  return new Promise((resolve) => {
    uni.closeBLEConnection({
      deviceId,
      success: () => resolve(),
      fail: () => resolve(),
    })
  })
}

// ===== 技师端扩展：校准 & 基线 =====

// T089-DEPRECATED: 用 startRealtimePressure + onRealtimeFrame 替代
export async function readCalibrationData(deviceId: string): Promise<number[]> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return []
}

// T089-DEPRECATED: 用 startRealtimePressure/stopRealtimePressure 封装
export async function writeCalibrationCommand(deviceId: string, command: 'start' | 'stop'): Promise<boolean> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return true
}

export async function readFirmwareVersion(deviceId: string): Promise<string> {
  if (isH5()) {
    throw new Error(H5_BLUETOOTH_ERROR)
  }
  return ''
}

// ===== T089 新增：实时压力推送 =====

let realtimeTimer: ReturnType<typeof setInterval> | null = null
let realtimeCallback: ((frame: number[]) => void) | null = null
let realtimeNotifyRegistered = false
let realtimeDeviceId = ''

/**
 * 启动 BLE 实时压力推送
 * 协议：向 B513 特征 Write 0x01 启动 Notify，固件以 1Hz 推送 20×uint16 小端（值 = N×100）。
 * 解析：raw / 100 → number[20]（单位 N）。
 */
export async function startRealtimePressure(deviceId: string): Promise<void> {
  if (isH5()) {
    // H5 mock：每秒推送 20 个接近 0 的随机小数（模拟空载）
    realtimeTimer = setInterval(() => {
      const frame = Array.from({ length: 20 }, () => Math.random() * 0.3 - 0.15)
      realtimeCallback?.(frame)
    }, 1000)
    return
  }
  realtimeDeviceId = deviceId
  bleLog.info(`startRealtimePressure deviceId=${deviceId}，写入 0x01 启动 B513 Notify`)
  // 1. 订阅 B513 Notify（仅注册一次）
  if (!realtimeNotifyRegistered) {
    realtimeNotifyRegistered = true
    uni.notifyBLECharacteristicValueChange({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_REALTIME,
      success: () => bleLog.info('B513 Notify 订阅成功'),
      fail: (err) => bleLog.error('B513 Notify 订阅失败', err?.errMsg),
    })
    uni.onBLECharacteristicValueChange((res) => {
      if (res.characteristicId?.toLowerCase() !== CHAR_REALTIME.toLowerCase()) return
      try {
        const bytes = new Uint8Array(res.value)
        // 20 个 uint16 小端 → number[20]（÷100 转 N）
        const frame: number[] = []
        for (let i = 0; i < 20 && i * 2 + 1 < bytes.length; i++) {
          const raw = bytes[i * 2] | (bytes[i * 2 + 1] << 8)
          const signed = raw > 32767 ? raw - 65536 : raw
          frame.push(signed / 100)
        }
        if (frame.length === 20) {
          realtimeCallback?.(frame)
        } else {
          bleLog.warn(`B513 数据帧长度不符：期望 40 字节，实际 ${bytes.length} 字节，解析出 ${frame.length} 点`)
        }
      } catch (e) {
        bleLog.error('B513 数据解析失败', e instanceof Error ? e.message : String(e))
      }
    })
  }
  // 2. 写入 0x01 启动推送
  await new Promise<void>((resolve, reject) => {
    uni.writeBLECharacteristicValue({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_REALTIME,
      value: new Uint8Array([0x01]).buffer,
      success: () => {
        bleLog.info('B513 写入 0x01 成功，实时推送已启动')
        resolve()
      },
      fail: (err) => {
        bleLog.error('B513 写入 0x01 失败', err?.errMsg)
        reject(new Error(`启动实时推送失败: ${err.errMsg}`))
      },
    })
  })
}

/** 停止 BLE 实时压力推送：向 B513 写 0x00 停止 Notify */
export async function stopRealtimePressure(deviceId: string): Promise<void> {
  if (realtimeTimer) {
    clearInterval(realtimeTimer)
    realtimeTimer = null
  }
  if (isH5()) return
  bleLog.info(`stopRealtimePressure deviceId=${deviceId}，写入 0x00 停止 B513 Notify`)
  await new Promise<void>((resolve) => {
    uni.writeBLECharacteristicValue({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_REALTIME,
      value: new Uint8Array([0x00]).buffer,
      success: () => {
        bleLog.info('B513 写入 0x00 成功，实时推送已停止')
        resolve()
      },
      fail: (err) => {
        bleLog.warn('B513 写入 0x00 失败（不阻断流程）', err?.errMsg)
        resolve()
      },
    })
  })
}

/** 注册实时帧回调 */
export function onRealtimeFrame(cb: (frame: number[]) => void): void {
  realtimeCallback = cb
}

// ===== T089 新增：WiFi 加密配置写入 + 状态机 =====

let wifiStatusTimer: ReturnType<typeof setInterval> | null = null
let wifiStatusCallback: ((code: number) => void) | null = null
let b512NotifyRegistered = false

/**
 * 写入加密后的 WiFi 配置（AES-128-CTR，由 aes-ctr.ts 加密后传入密文 hex）
 * 协议 §4：密文按每片 ≤180 字节切片，对 B511 特征顺序 Write，无分包序号字节。
 * 写完即完成发送（固件侧 400ms 静默判包）。
 */
const WIFI_CHUNK_SIZE = 180

function hexToBytesLocal(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substr(i * 2, 2), 16)
  }
  return bytes
}

export async function writeWifiConfigV2(
  deviceId: string,
  encryptedHex: string
): Promise<boolean> {
  if (isH5()) {
    // H5 mock：直接成功，启动状态机模拟
    return true
  }
  const cipher = hexToBytesLocal(encryptedHex)
  const totalChunks = Math.ceil(cipher.length / WIFI_CHUNK_SIZE)
  bleLog.info(`B511 写入开始：密文长=${cipher.length}B，分片数=${totalChunks}`)
  for (let off = 0, idx = 0; off < cipher.length; off += WIFI_CHUNK_SIZE, idx++) {
    const chunk = cipher.slice(off, off + WIFI_CHUNK_SIZE)
    try {
      await new Promise<void>((resolve, reject) => {
        uni.writeBLECharacteristicValue({
          deviceId,
          serviceId: SERVICE_UUID,
          characteristicId: CHAR_WIFI_CONFIG,
          value: chunk.buffer,
          success: () => resolve(),
          fail: (err) => reject(new Error(`B511 写入失败: ${err.errMsg}`)),
        })
      })
      bleLog.info(`B511 分片 ${idx + 1}/${totalChunks} 写入成功 len=${chunk.length}`)
    } catch (e) {
      bleLog.error(`B511 分片 ${idx + 1}/${totalChunks} 写入失败`, e instanceof Error ? e.message : String(e))
      throw e
    }
  }
  bleLog.info('B511 全部分片写入完成')
  return true
}

/**
 * 注册配网状态机回调
 * 状态码：0收到 1连AP 2取IP 3探测 9成功 -1密码错 -2 SSID不见 -3 DHCP失败 -4云端不可达
 * R5：真机订阅 B512 Notify，记录每次状态原始 int8 值。
 */
let wifiStatusDeviceId = ''

export function onWifiStatus(cb: (code: number) => void, deviceId?: string): void {
  wifiStatusCallback = cb
  if (isH5()) return
  // T109: 调用方传入 deviceId（BLE MAC），赋值后再订阅 B512 Notify
  if (deviceId) wifiStatusDeviceId = deviceId
  if (b512NotifyRegistered) return
  if (!wifiStatusDeviceId) return
  b512NotifyRegistered = true
  // 开启 B512 通知
  uni.notifyBLECharacteristicValueChange({
    deviceId: wifiStatusDeviceId,
    serviceId: SERVICE_UUID,
    characteristicId: CHAR_WIFI_STATUS,
    success: () => bleLog.info('B512 Notify 订阅成功'),
    fail: (err) => bleLog.error('B512 Notify 订阅失败', err?.errMsg),
  })
  uni.onBLECharacteristicValueChange((res) => {
    if (res.characteristicId?.toLowerCase() !== CHAR_WIFI_STATUS.toLowerCase()) return
    try {
      const code = new Int8Array(res.value)[0]
      bleLog.info(`B512 状态原始值=${code}`)
      wifiStatusCallback?.(code)
    } catch (e) {
      bleLog.error('B512 状态解析失败', e instanceof Error ? e.message : String(e))
    }
  })
}


/** H5 mock：模拟配网状态机推进（0→1→2→3→9） */
export function startMockWifiStatusSequence(): void {
  if (!isH5()) return
  const seq = [0, 1, 2, 3, 9]
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

// ===== T089 新增：设备信息 =====

/**
 * 读取设备信息（协议 §5：B514 Read 一次返回 UTF-8 JSON，snake_case）
 * {"device_id":"...","firmware":"...","battery":100}
 * 字段容错：缺 battery 默认 100；解析失败返回 null，不阻塞流程。
 * R5：记录原始文本（截断 200 字符）与解析结果。
 */
export async function readDeviceInfo(deviceId: string): Promise<{
  deviceId: string
  firmware: string
  battery: number
} | null> {
  if (isH5()) {
    return { deviceId, firmware: 'v1.2.3', battery: 85 }
  }
  return new Promise((resolve) => {
    let settled = false
    const finish = (val: any) => {
      if (settled) return
      settled = true
      uni.offBLECharacteristicValueChange(handler)
      resolve(val)
    }
    const handler = (res: any) => {
      try {
        const text = decodeUtf8(new Uint8Array(res.value))
        const truncated = text.length > 200 ? text.slice(0, 200) + '...(truncated)' : text
        bleLog.info(`B514 原始文本=${truncated}`)
        const data = JSON.parse(text)
        const result = {
          deviceId: data.device_id || deviceId,
          firmware: data.firmware || '',
          battery: typeof data.battery === 'number' ? data.battery : 100,
        }
        bleLog.info(`B514 解析成功`, { firmware: result.firmware, battery: result.battery })
        finish(result)
      } catch (e) {
        bleLog.error('B514 解析失败', e instanceof Error ? e.message : String(e))
        finish(null)
      }
    }
    uni.onBLECharacteristicValueChange(handler)
    uni.readBLECharacteristicValue({
      deviceId,
      serviceId: SERVICE_UUID,
      characteristicId: CHAR_DEVICE_INFO,
      fail: (err) => {
        bleLog.error('B514 读取失败', err?.errMsg)
        finish(null)
      },
    })
    // 3s 超时兜底
    setTimeout(() => {
      if (!settled) {
        bleLog.warn('B514 读取超时')
        finish(null)
      }
    }, 3000)
  })
}