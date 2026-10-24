/**
 * T744 — 患者端配网频段 A+B（Boss 2026-10-24 批准）
 *
 * 覆盖三层：
 * ① A 层常驻句确实进了 CONNECT.tips（且以定稿句打头）
 * ② B 层扫描适配器在真机各种受限条件下都回 unavailable（降级 A 层），不伪造"未找到"
 * ③ 失败页接线与渲染位存在（node 环境测不到模板，按源码断言，同 provision-seq.spec.ts 写法）
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { WIFI_BAND_HINT_24G, WIFI_BAND_SCAN_5G, WIFI_BAND_SCAN_NOT_FOUND } from '@bracesync/shared-utils'
import { CONNECT, FAILURE_COMMON } from '../../src/utils/wifi-copy'
import { scanPhoneSsid } from '../../src/utils/wifi-band-scan'

const here = path.dirname(fileURLToPath(import.meta.url))
const pageSrc = fs.readFileSync(path.resolve(here, '../../src/pages/wifi-setup/index.vue'), 'utf8')
const failureSrc = fs.readFileSync(
  path.resolve(here, '../../src/components/wifi-setup/WifiFailure.vue'),
  'utf8',
)

describe('T744 A 层 — 患者端 SSID 输入处常驻 2.4G 提示', () => {
  it('CONNECT.tips 以定稿句打头，原合并路由器处置句仍在', () => {
    expect(CONNECT.tips.startsWith(WIFI_BAND_HINT_24G)).toBe(true)
    expect(CONNECT.tips).toContain('若家中路由器 2.4G 与 5G 同名')
    expect(CONNECT.tips).toContain('密码区分大小写')
  })
})

describe('T744 B 层 — 扫描适配器（平台受限一律降级）', () => {
  let listeners: Array<(res: { wifiList?: Array<{ SSID?: string }> }) => void>

  beforeEach(() => {
    listeners = []
    ;(globalThis as unknown as { uni: unknown }).uni = {
      startWifi: ({ success }: { success?: () => void }) => success?.(),
      getWifiList: ({ fail }: { fail?: () => void }) => {
        if ((globalThis as unknown as { __getListFail?: boolean }).__getListFail) fail?.()
      },
      onGetWifiList: (fn: (res: { wifiList?: Array<{ SSID?: string }> }) => void) => listeners.push(fn),
      offGetWifiList: vi.fn(),
    }
  })

  afterEach(() => {
    delete (globalThis as unknown as { uni?: unknown }).uni
    delete (globalThis as unknown as { __getListFail?: boolean }).__getListFail
    vi.useRealTimers()
  })

  it('清单含填写值 ⇒ found，并解绑监听', async () => {
    const p = scanPhoneSsid('Home-2.4G')
    listeners.forEach((fn) => fn({ wifiList: [{ SSID: 'CMCC-5G' }, { SSID: 'Home-2.4G' }] }))
    await expect(p).resolves.toBe('found')
    expect((globalThis as unknown as { uni: { offGetWifiList: ReturnType<typeof vi.fn> } }).uni.offGetWifiList)
      .toHaveBeenCalledTimes(1)
  })

  it('清单非空且不含 ⇒ notfound', async () => {
    const p = scanPhoneSsid('Ghost-Net')
    listeners.forEach((fn) => fn({ wifiList: [{ SSID: 'Home-2.4G' }] }))
    await expect(p).resolves.toBe('notfound')
  })

  it('getWifiList 直接失败（iOS 受限/接口下架）⇒ unavailable', async () => {
    ;(globalThis as unknown as { __getListFail?: boolean }).__getListFail = true
    await expect(scanPhoneSsid('Home-2.4G')).resolves.toBe('unavailable')
  })

  it('3s 内收不到清单 ⇒ unavailable，不拖住失败页', async () => {
    vi.useFakeTimers()
    const p = scanPhoneSsid('Home-2.4G', 3000)
    vi.advanceTimersByTime(3000)
    await expect(p).resolves.toBe('unavailable')
  })

  it('startWifi 失败 ⇒ unavailable', async () => {
    ;(globalThis as unknown as { uni: unknown }).uni = {
      startWifi: ({ fail }: { fail?: () => void }) => fail?.(),
      getWifiList: () => {},
      onGetWifiList: () => {},
      offGetWifiList: () => {},
    }
    await expect(scanPhoneSsid('Home-2.4G')).resolves.toBe('unavailable')
  })
})

describe('T744 — 失败页接线与渲染位', () => {
  it('三档失败（-1/-2/超时）才跑扫描，addr/srv/linklost 不做频段断言', () => {
    expect(pageSrc).toContain('async function runBandDiagnosis()')
    expect(pageSrc).toContain("if (!['keycheck', 'nonet', 'timeout'].includes(failureType.value)) return")
    expect(pageSrc).toContain('await scanPhoneSsid(enteredSsid.value)')
    expect(pageSrc).toContain('wifiBandDiagnosis(verdict)')
  })

  it('状态码失败与超时两条落点都触发诊断；离开失败页清空', () => {
    const calls = pageSrc.match(/void runBandDiagnosis\(\)/g) ?? []
    expect(calls.length).toBe(2)
    expect(pageSrc).toContain(':diagnosis="failureDiagnosis"')
    expect(pageSrc).toContain('failureDiagnosis.value = \'\'')
  })

  it('WifiFailure 新增 diagnosis 可选属性，空串不渲染', () => {
    expect(failureSrc).toContain('diagnosis?: string')
    expect(failureSrc).toContain('v-if="diagnosis"')
    expect(failureSrc).toContain('{{ FAILURE_COMMON.diagnosisLabel }}')
    expect(FAILURE_COMMON.diagnosisLabel).toBe('网络诊断')
  })

  it('三档诊断句都在 shared-utils 单源里（页面不散落字符串）', () => {
    expect(pageSrc).not.toContain(WIFI_BAND_SCAN_5G)
    expect(pageSrc).not.toContain(WIFI_BAND_SCAN_NOT_FOUND)
  })
})
