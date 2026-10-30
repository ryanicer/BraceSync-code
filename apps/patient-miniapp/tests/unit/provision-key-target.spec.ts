/**
 * T730 增补 2 — 配网密钥目标必须「以设备自报为准」（患者端）
 *
 * 卡面 10-20 实锤：患者端拿云端缓存的 cloudDeviceId（20260701001）去领 provision-key，
 * 而 BLE 实连的是另一台（20261020001）⇒ 领到的 key 与手里这台的 secret 不同源
 * ⇒ 设备解不出明文回 -1 ⇒ 界面显示「WiFi 密码不正确」，现场按密码排查数小时。
 *
 * 为什么又是结构断言：wifi-setup 页面在 vitest 里挂不起来（uni-app 组件 + #ifdef 双分支，
 * 同 b512-resubscribe.spec.ts 的困境），所以这里把「目标是谁、谁优先、读不到怎么办」钉成契约，
 * 改动即红。行为验证只能落真机（判据：日志出现「[T730] 领 key 目标判定」且
 * targetFromDevice=true、target≠cloudDeviceId 时页面弹出两台广播名）。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const PAGE = fileURLToPath(new URL('../../src/pages/wifi-setup/index.vue', import.meta.url))
const BLE = fileURLToPath(new URL('../../src/utils/ble.ts', import.meta.url))
const COPY = fileURLToPath(new URL('../../src/utils/wifi-copy.ts', import.meta.url))
const page = fs.readFileSync(PAGE, 'utf8')

/** 取页面/工具里某个顶层函数的函数体：以下一个顶格 function 声明为界 */
function fnBody(name: string, src: string = page): string {
  const start = src.search(new RegExp(`\\n(?:export )?(?:async )?function ${name}\\(`))
  expect(start, `找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
  const rest = src.slice(start)
  const next = rest.slice(1).search(/\n(?:export )?(?:async )?function /)
  return next === -1 ? rest : rest.slice(1, next + 1)
}

describe('T730② — 设备自报编号读得到（B514）', () => {
  const bleSrc = fs.readFileSync(BLE, 'utf8')

  it('ble.ts 导出 readDeviceInfo，读的是 B514 设备信息特征', () => {
    const body = fnBody('readDeviceInfo', bleSrc)
    expect(body).toContain('uni.readBLECharacteristicValue(')
    expect(body).toContain('characteristicId: CHAR_DEVICE_INFO')
    expect(body).toContain('serviceId: SERVICE_UUID')
  })

  it('解析的是设备自报 JSON 的 device_id（不是手机侧临时 deviceId 顶包）', () => {
    const body = fnBody('readDeviceInfo', bleSrc)
    expect(body).toContain('data.device_id')
    expect(body).toContain('decodeUtf8(new Uint8Array(res.value))')
  })

  it('读不到一律 null + 超时兜底，不在这层猜设备号', () => {
    const body = fnBody('readDeviceInfo', bleSrc)
    expect(body).toContain('finish(null)')
    expect(body).toMatch(/B514 读取超时/)
    // 一次性监听必须摘掉，否则每次读 B514 都会给全局叠一个 handler
    expect(body).toContain('uni.offBLECharacteristicValueChange(handler)')
  })
})

describe('T730② — 领 key 的目标以设备为准，云端只做一致性校验', () => {
  it('getProvisionKey 不再吃 cloudDeviceId', () => {
    expect(page).not.toContain('getProvisionKey(cloudDeviceId.value)')
    expect(page).toContain('getProvisionKey(keyTargetDeviceId)')
  })

  it('pickProvisionKeyTarget 的三条出口：一致直用 / 不一致按手里这台 / 读不到回退云端', () => {
    const body = fnBody('pickProvisionKeyTarget')
    expect(body).toContain('probeBleProtocolId()')
    // ③ 回退（B514 不实现不能把配网卡死＝验收 3）
    expect(body).toMatch(/if \(!ble\)[\s\S]*?return cloudDeviceId\.value/)
    // ① 一致（或云端为空）
    expect(body).toMatch(/if \(!cloudDeviceId\.value \|\| cloudDeviceId\.value === ble\) return ble/)
    // ② 不一致：问过用户之后仍然以设备自报值为准（云端陈旧不构成拒绝配网的理由）
    const atAsk = body.indexOf('askDeviceMismatch(')
    const lastReturn = body.lastIndexOf('return ble')
    expect(atAsk).toBeGreaterThan(-1)
    expect(lastReturn, '不一致时没有以设备自报值为出口 ⇒ 回到旧缺陷').toBeGreaterThan(atAsk)
  })

  it('判定顺序：读设备 → 领 key → 写 B511（读排在领之前才有意义）', () => {
    const body = fnBody('runProvision')
    const atPick = body.indexOf('pickProvisionKeyTarget()')
    const atKey = body.indexOf('getProvisionKey(keyTargetDeviceId)')
    const atWrite = body.indexOf('writeWifiConfigV2(bleDeviceId.value')
    expect(atPick).toBeGreaterThan(-1)
    expect(atPick, '领 key 排在设备判定之前 ⇒ 还在拿缓存设备号领密钥').toBeLessThan(atKey)
    expect(atKey).toBeLessThan(atWrite)
  })

  it('弹窗排在 showLoading 之前：遮罩上的 modal 用户点不动', () => {
    const body = fnBody('runProvision')
    expect(body.indexOf('pickProvisionKeyTarget()')).toBeLessThan(body.indexOf('uni.showLoading('))
  })

  it('每次下发都重读设备（换设备/重连后缓存比不读更危险）', () => {
    const body = fnBody('probeBleProtocolId')
    expect(body.startsWith('async function probeBleProtocolId(): Promise<string> {\n  bleProtocolId.value = \'\'')).toBe(true)
    // H5 无真实设备：拿桩返回的手机侧临时 id 去比必然"不一致"，会把 E2E 正常路径全拦停
    expect(body).toMatch(/#ifdef H5\s*\n\s*return ''/)
    expect(body.indexOf('#ifdef H5')).toBeLessThan(body.indexOf('readDeviceInfo('))
  })

  it('刷新走快照端点，绕开 store 里那份陈旧 currentDevice', () => {
    const body = fnBody('refreshCloudDeviceId')
    expect(body).toContain('/realtime')
    expect(body).not.toContain('deviceStore.currentDevice')
    expect(fnBody('ensureCloudDevice')).toContain('deviceStore.currentDevice?.deviceId')
  })
})

describe('T730② — 不一致提示把差异摆明（验收 2），且患者端零技术术语', () => {
  it('弹窗内容同时填两台（只说"不一致"不算把差异摆明）', () => {
    const body = fnBody('askDeviceMismatch')
    expect(body).toContain("replace('{ble}', broadcastNameOf(bleId))")
    expect(body).toContain("replace('{cloud}', broadcastNameOf(cloudId))")
    expect(body).toContain('CONNECT.mismatch.content')
    // 患者端只到广播名，不出现完整编号
    expect(body).not.toMatch(/replace\('\{ble\}', bleId\)/)
  })

  it('两个动作都在：刷新云端绑定（卡面"引导刷新"）+ 按手里这台继续（不卡死）', () => {
    const copySrc = fs.readFileSync(COPY, 'utf8')
    expect(copySrc).toContain('refreshLabel')
    expect(copySrc).toContain('proceedLabel')
    const body = fnBody('askDeviceMismatch')
    expect(body).toContain("res.confirm ? 'refresh' : 'proceed'")
    expect(body).toContain("fail: () => resolve('proceed')")
  })

  it('mismatch 文案块挂在 CONNECT 下（被 wifi-copy.spec 的禁词全量采集覆盖）', () => {
    const copySrc = fs.readFileSync(COPY, 'utf8')
    const connectBlock = copySrc.slice(copySrc.indexOf('export const CONNECT'), copySrc.indexOf('export const PROGRESS'))
    expect(connectBlock).toContain('mismatch:')
    expect(connectBlock).toContain('{ble}')
    expect(connectBlock).toContain('{cloud}')
  })
})

describe('T730② / T757 — 配网成功日志目标与源码守卫', () => {
  it('onProvisionSuccess 的日志目标跟着领 key 的目标走，且页面不再引用患者端设备回写模块', () => {
    const body = fnBody('onProvisionSuccess')
    expect(body).toContain("keyTargetDeviceId || cloudDeviceId.value")
    expect(page).not.toContain("api/device")
  })

  it('断言确实读到了源码（防路径写错导致空内容假绿）', () => {
    expect(path.basename(PAGE)).toBe('index.vue')
    expect(page.length).toBeGreaterThan(1000)
    expect(page).toContain('writeWifiConfigV2')
    expect(path.basename(COPY)).toBe('wifi-copy.ts')
  })
})
