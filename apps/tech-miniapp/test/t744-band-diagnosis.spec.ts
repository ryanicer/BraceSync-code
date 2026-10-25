/**
 * T744 — 技师端配网频段 A+B（Boss 2026-10-24 批准）
 *
 * ① A 层常驻句：wifi-config 的 SSID 提示首行改为 shared-utils 单源插值
 * ② B 层：-1（固件兜底码）/ -2（找不到网络）失败后扫手机清单给诊断；
 *        -3/-4 是已连上 WiFi 后的地址/云端问题，不得给频段断言
 * ③ 扫描适配器在平台受限时统一回 unavailable（降级 A 层）
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { WIFI_BAND_HINT_24G, WIFI_BAND_SCAN_5G, WIFI_BAND_SCAN_NOT_FOUND } from '@bracesync/shared-utils'
import { scanPhoneSsid } from '../src/utils/wifi-band-scan'

const here = path.dirname(fileURLToPath(import.meta.url))
const pageSrc = fs.readFileSync(path.resolve(here, '../src/pages/wifi-config/index.vue'), 'utf8')

describe('T744 A 层 — 技师端常驻提示', () => {
  it('SSID 提示首行取单源常量，页面不重复字面量', () => {
    expect(pageSrc).toContain('{{ WIFI_BAND_HINT_24G }}')
    expect(pageSrc).not.toContain(WIFI_BAND_HINT_24G)
  })

  it('原合并路由器处置句与手动输入提醒仍保留', () => {
    expect(pageSrc).toContain('若路由器 2.4G 与 5G 同名，请先在路由器设置中改成不同名称后重试')
    expect(pageSrc).toContain('请手动输入准确的 WiFi 名称与密码')
  })
})

describe('T744 B 层 — handleError 接线', () => {
  it('只有 -1/-2 触发扫描，-3/-4 不触发', () => {
    expect(pageSrc).toContain('if (code === -1 || code === -2) {')
    expect(pageSrc).toContain('void scanPhoneSsid(ssid)')
    expect(pageSrc).toContain('wifiDiagnosis.value = wifiBandDiagnosis(verdict)')
  })

  it('重试时清空诊断，失败卡内渲染诊断句', () => {
    expect(pageSrc).toContain('v-if="wifiDiagnosis"')
    expect(pageSrc).toContain("errorCode.value = null\n  wifiDiagnosis.value = ''")
  })

  it('诊断句只从 shared-utils 来，页面不散落 B 层字面量', () => {
    expect(pageSrc).not.toContain(WIFI_BAND_SCAN_5G)
    expect(pageSrc).not.toContain(WIFI_BAND_SCAN_NOT_FOUND)
  })
})

describe('T744 — 扫描适配器降级', () => {
  beforeEach(() => {
    ;(globalThis as unknown as { uni: unknown }).uni = {
      startWifi: ({ success }: { success?: () => void }) => success?.(),
      getWifiList: () => {},
      onGetWifiList: () => {},
      offGetWifiList: vi.fn(),
    }
  })

  afterEach(() => {
    delete (globalThis as unknown as { uni?: unknown }).uni
    vi.useRealTimers()
  })

  it('3s 无清单回调 ⇒ unavailable（A 层兜底，不冒充未找到）', async () => {
    vi.useFakeTimers()
    const p = scanPhoneSsid('Home-2.4G', 3000)
    vi.advanceTimersByTime(3000)
    await expect(p).resolves.toBe('unavailable')
  })

  it('平台抛异常 ⇒ unavailable', async () => {
    ;(globalThis as unknown as { uni: unknown }).uni = {
      startWifi: () => {
        throw new Error('not supported')
      },
      getWifiList: () => {},
      onGetWifiList: () => {},
      offGetWifiList: () => {},
    }
    await expect(scanPhoneSsid('Home-2.4G')).resolves.toBe('unavailable')
  })
})
