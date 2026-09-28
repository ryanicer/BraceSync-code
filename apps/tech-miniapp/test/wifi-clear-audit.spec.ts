/**
 * T448 — 技师端「清除设备 WiFi」成功后向云端报一次留痕
 *
 * 卡面三条硬约束（Boss 2026-09-28 11:0x 拍甲：只加审计，不扩枚举）：
 *   不新增写端点 ⇒ 复用配网回写的 POST /devices/:deviceId/wifi，body 只带 cleared=true；
 *   不改 BLE 链路 ⇒ 发 0x02 / 等 B512 notify 的调用点与次数一处不动，上报挂在 clearOk 之后；
 *   不动既有枚举 ⇒ 前端不写 wifiStatus 的任何值，状态列由后端保持不动（见后端用例）。
 *
 * 页面挂载不了单测（utils/ble.ts → ble-log → logger 的 #ifdef 双声明，esbuild 不做条件编译），
 * 所以 api 层走真单测、页面接线按源码结构断言 —— 与 test/t218-ble-robustness.spec.ts、
 * test/provision-seq.spec.ts 同一取向。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'

vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})

import { request } from '../src/utils/request'
import { reportWifiCleared, setDeviceWifi } from '../src/api/device'

const PAGE = fs.readFileSync(
  fileURLToPath(new URL('../src/pages/wifi-config/index.vue', import.meta.url)),
  'utf8'
)
const API = fs.readFileSync(
  fileURLToPath(new URL('../src/api/device.ts', import.meta.url)),
  'utf8'
)

/** 取一次请求调用的入参（vi.mock 的 request 是全局唯一替身） */
function lastRequest(): { url: string; method: string; data: Record<string, unknown> } {
  const calls = (request as ReturnType<typeof vi.fn>).mock.calls
  expect(calls).toHaveLength(1)
  return calls[0][0] as never
}

describe('T448 — api 层：清除留痕上报的请求形状', () => {
  beforeEach(() => {
    (request as ReturnType<typeof vi.fn>).mockReset()
    ;(request as ReturnType<typeof vi.fn>).mockResolvedValue(null)
  })

  it('reportWifiCleared 打既有 /wifi 路由且 body 只带 cleared', async () => {
    await reportWifiCleared('DEV-T448-001')
    const req = lastRequest()
    expect(req.url).toBe('/api/v1/devices/DEV-T448-001/wifi')
    expect(req.method).toBe('POST')
    expect(req.data).toEqual({ cleared: true })
  })

  it('setDeviceWifi 仍只带 ssid：两支互斥，不能顺手合并', async () => {
    await setDeviceWifi('DEV-T448-001', 'BraceHome-5G')
    const req = lastRequest()
    expect(req.url).toBe('/api/v1/devices/DEV-T448-001/wifi')
    expect(req.data).toEqual({ ssid: 'BraceHome-5G' })
    // 反证：合并成 {ssid, cleared} 会被后端 400 拒（互斥校验），这里从前端侧先钉住
    expect(req.data).not.toHaveProperty('cleared')
  })

  it('api 层不发 wifiStatus：状态列不在本卡范围（卡面「不动既有枚举」）', async () => {
    await reportWifiCleared('DEV-T448-002')
    expect(lastRequest().data).not.toHaveProperty('wifiStatus')
  })

  it('api 源码里没有 ssid 与 cleared 同现的请求体', () => {
    // 逐函数切片：reportWifiCleared 的函数体内不许出现 ssid
    const body = API.slice(API.indexOf('export async function reportWifiCleared'))
    expect(body).not.toContain('ssid')
    // 正对照：切片确实读到了内容（防空串恒真）
    expect(body).toContain('cleared: true')
  })
})

describe('T448 — 页面接线：只在清除成功的那两支上报，且不阻塞', () => {
  // 两处 clearOk：handleSuccess（自动流程）与 retryClear（技师手动重试）
  const successBranch = /if \(clearOk\) \{\s*\n\s*clearState\.value = 'success'\s*\n\s*reportWifiClearAudit\(\)/g
  const failedBranch = /clearState\.value = 'failed'/g

  it('两支清除成功路径各接一次上报', () => {
    const hits = PAGE.match(successBranch)
    expect(hits, '清除成功的两个分支都要接上报，少一个就有一类清除永远没留痕').toHaveLength(2)
  })

  it('清除失败路径不报（设备侧凭据还在，报了就是假留痕）', () => {
    expect(PAGE.match(failedBranch)).toHaveLength(2)
    // 全页只有「定义 + 两支成功调用」三处出现；失败分支若被接上，这个数就会变
    expect(PAGE.match(/reportWifiClearAudit/g)).toHaveLength(3)
  })

  it('上报是 fire-and-forget：不 await、失败只记日志', () => {
    const helper = PAGE.slice(
      PAGE.indexOf('function reportWifiClearAudit'),
      PAGE.indexOf('function reportWifiClearAudit') + 600
    )
    expect(helper).toContain('reportWifiCleared(deviceId).catch(')
    expect(helper).not.toContain('await reportWifiCleared')
    expect(helper).toContain('bleLog.warn')
    // 正对照：helper 读到了内容
    expect(helper).toContain('installStore.deviceId')
  })

  it('上报排在 clearState 之后、自动返回计时之前：不改既有 3s 返回时序', () => {
    // 逐支扫描：每个「clearOk 为真」的分支体里，上报都排在成功态之后、3000ms 定时器之前
    const branches = PAGE.split('if (clearOk) {').slice(1)
    expect(branches.length).toBe(2)
    for (const raw of branches) {
      const body = raw.slice(0, raw.indexOf('} else {'))
      const atReport = body.indexOf('reportWifiClearAudit()')
      const atState = body.indexOf("clearState.value = 'success'")
      const atTimer = body.indexOf('autoReturnTimer.value = setTimeout')
      expect(atState, '分支体里应仍有成功态赋值').toBeGreaterThanOrEqual(0)
      expect(atReport, '分支体里应接上报').toBeGreaterThanOrEqual(0)
      expect(atTimer, '分支体里应仍有自动返回定时器').toBeGreaterThanOrEqual(0)
      expect(atReport).toBeGreaterThan(atState)
      expect(atReport).toBeLessThan(atTimer)
      expect(body).toContain('}, 3000)')
    }
  })

  it('BLE 链路一处没动：发 0x02 与等回执的调用点仍各一次', () => {
    expect(PAGE).toContain('await sendWifiClear(bleMac)')
    expect(PAGE).toContain('await waitForWifiClear(10000)')
    expect(PAGE.match(/sendWifiClear\(bleMac\)/g)).toHaveLength(1)
    expect(PAGE.match(/waitForWifiClear\(10000\)/g)).toHaveLength(1)
    // 清除动作的入口函数没被改写成语义化 HTTP（本卡不动 BLE 形态）
    expect(PAGE).toContain('async function doWifiClear(): Promise<boolean>')
  })

  it('页面向云端回写 SSID 的那一次仍在（配网成功 ≠ 清除，两件事各自成立）', () => {
    expect(PAGE).toContain('await setDeviceWifi(installStore.deviceId, ssid)')
  })
})
