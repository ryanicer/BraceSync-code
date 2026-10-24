/**
 * T740 — 配网扫描（T192 路径）接入位置权限前置
 *
 * 缺陷（2026-10-23 16:59 患者端日志）：Android 微信在位置权限缺失时，
 * startBluetoothDevicesDiscovery 直接失败 `location permission is denied`；
 * 配网扫描路径未调用 ble.ts 既有 ensureLocationPermission，且失败后无指引。
 *
 * 为什么按源码结构断言：utils/ble.ts 含 #ifdef 条件编译段，页面/工具挂载成本高，
 * 先例见 b511-write-retry.spec.ts / b512-resubscribe.spec.ts。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const BLE = fileURLToPath(new URL('../../src/utils/ble.ts', import.meta.url))
const ble = fs.readFileSync(BLE, 'utf8')

/** 取某个顶层函数体（export function / export async function），以定义位置为起点 */
function fnBody(name: string): string {
  const re = new RegExp(`export (?:async )?function ${name}\\(`)
  const m = re.exec(ble)
  expect(m, `ble.ts 里找不到 export function ${name}()——落点被挪走了？`).not.toBeNull()
  const start = m!.index
  const rest = ble.slice(start)
  const next = rest.slice(1).search(/\nexport (?:async )?function |\nfunction /)
  return next === -1 ? rest : rest.slice(0, next + 1)
}

describe('T740 — startBleScan 权限前置接线', () => {
  const body = fnBody('startBleScan')

  it('discovery 只在 beginDiscovery 内出现一次，且前置 ensureLocationPermission', () => {
    expect(body.match(/startBluetoothDevicesDiscovery\(/g)).toHaveLength(1)
    expect(body).toMatch(/ensureLocationPermission\(\)\.then\(\(ok\)/)
    const atBegin = body.indexOf('const beginDiscovery = () =>')
    const atPerm = body.indexOf('ensureLocationPermission().then((ok)')
    expect(atBegin).toBeGreaterThan(-1)
    expect(atPerm).toBeGreaterThan(atBegin) // 先授权，成功分支才 discovery
    expect(body.slice(atPerm)).toContain('beginDiscovery()')
  })

  it('权限未授权时 onDone(0) 收口，不发起 discovery', () => {
    const atPerm = body.indexOf('ensureLocationPermission().then((ok)')
    const branch = body.slice(atPerm)
    expect(branch).toMatch(/if \(!ok\) \{[\s\S]*stopForPermission\(/)
    expect(branch).toContain('本次扫描不发起 discovery')
  })

  it('discovery 权限类失败只扫一次即停，warn 带 errMsg 原文', () => {
    expect(body).toContain('isLocationPermissionDenied(msg)')
    expect(body).toContain('_log(\'warn\', `startBluetoothDevicesDiscovery 位置权限被拒: ${msg}`)')
    expect(body).toContain('permissionRetryDone')
    expect(body).toContain('stopForPermission(`位置权限仍被拒，停止本次扫描: ${msg}`)')
    // 权限分支不得复用普通失败路径的 setTimeout(finish, 0) 静默收尾
    const failTail = body.slice(body.indexOf('fail: (err) =>'))
    const atPermWarn = failTail.indexOf("_log('warn', `startBluetoothDevicesDiscovery 位置权限被拒")
    const atRetryFlag = failTail.indexOf('permissionRetryDone = true', atPermWarn)
    expect(atPermWarn).toBeGreaterThan(-1)
    expect(atRetryFlag).toBeGreaterThan(atPermWarn)
    expect(failTail.slice(atPermWarn, atRetryFlag)).not.toContain('setTimeout(finish, 0)')
  })

  it('引导授权返回成功后自动重扫一次', () => {
    expect(body).toContain('位置权限已恢复，自动重扫一次')
    const atRetry = body.indexOf('位置权限已恢复，自动重扫一次')
    expect(body.slice(atRetry, atRetry + 80)).toContain('beginDiscovery()')
  })

  it('重连的一次性重扫同样走 startBleScan（继承前置）', () => {
    expect(fnBody('rescanOnce')).toContain('startBleScan(')
  })
})

describe('T740 — 引导文案与入口', () => {
  it('弹窗文案含 Boss 指定原句，并保留 wx.openSetting 去设置入口', () => {
    expect(ble).toContain('需要在手机设置中允许微信使用位置信息')
    expect(ble).toContain('wx.openSetting')
    expect(ble).toContain("confirmText: '去设置'")
  })
})

describe('T740 — errMsg 判定', () => {
  it('只认权限类原文，普通扫描失败不误判', async () => {
    const { isLocationPermissionDenied } = await import('../../src/utils/ble-scan-permission')
    expect(isLocationPermissionDenied('startBluetoothDevicesDiscovery:fail:location permission is denied')).toBe(true)
    expect(isLocationPermissionDenied('startBluetoothDevicesDiscovery:fail:permission is denied')).toBe(true)
    expect(isLocationPermissionDenied('startBluetoothDevicesDiscovery:fail:system error:Inner error.')).toBe(false)
    expect(isLocationPermissionDenied('')).toBe(false)
  })
})
