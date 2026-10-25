/**
 * T744 — 配网频段体验 A+B 的文案与判定（Boss 2026-10-24 批准）
 *
 * A 层句、B 层两档提示逐字钉死：这是卡面验收判据，也是设计稿/PRD 同步的对照源。
 */
import { describe, it, expect } from 'vitest'
import {
  WIFI_BAND_HINT_24G,
  WIFI_BAND_SCAN_5G,
  WIFI_BAND_SCAN_NOT_FOUND,
  classifyScannedSsid,
  wifiBandDiagnosis,
} from '../src/wifiBand'

describe('T744 — 定稿文案（卡面 A/B 逐字）', () => {
  it('A 层常驻句逐字', () => {
    expect(WIFI_BAND_HINT_24G).toBe(
      '设备仅支持 2.4G WiFi，请确认输入的是 2.4G 网络名称（5G 网络设备无法连接）',
    )
  })

  it('B 层扫描有该网络 → 5G 引导句逐字', () => {
    expect(WIFI_BAND_SCAN_5G).toBe('该网络可能为仅 5G 频段，设备无法连接，请连接 2.4G 网络')
  })

  it('B 层扫描无该网络 → 未找到句逐字', () => {
    expect(WIFI_BAND_SCAN_NOT_FOUND).toBe('未找到该网络，请检查名称拼写或走近路由器')
  })
})

describe('T744 — 扫描清单核对判据', () => {
  it('精确命中 ⇒ found', () => {
    expect(classifyScannedSsid('Home-2.4G', ['CMCC-5G', 'Home-2.4G'])).toBe('found')
  })

  it('清单非空且不含 ⇒ notfound（区分大小写，SSID 本身区分）', () => {
    expect(classifyScannedSsid('home-2.4g', ['Home-2.4G'])).toBe('notfound')
    expect(classifyScannedSsid('MyNet', ['OtherNet'])).toBe('notfound')
  })

  it('两端空格可容忍（用户误贴空格不判成"没找到"）', () => {
    expect(classifyScannedSsid('  Home-2.4G  ', [' Home-2.4G '])).toBe('found')
  })

  it('输入为空 ⇒ unavailable，不冒充未找到', () => {
    expect(classifyScannedSsid('', ['A'])).toBe('unavailable')
  })

  it('扫描清单为空或全是空串 ⇒ unavailable（没拿到数据不能断言网络不存在）', () => {
    expect(classifyScannedSsid('Home', [])).toBe('unavailable')
    expect(classifyScannedSsid('Home', ['', '   '])).toBe('unavailable')
  })

  it('判据映射：found→5G 引导、notfound→未找到、unavailable→A 层兜底', () => {
    expect(wifiBandDiagnosis('found')).toBe(WIFI_BAND_SCAN_5G)
    expect(wifiBandDiagnosis('notfound')).toBe(WIFI_BAND_SCAN_NOT_FOUND)
    expect(wifiBandDiagnosis('unavailable')).toBe(WIFI_BAND_HINT_24G)
  })
})
