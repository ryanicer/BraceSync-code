/**
 * T192 — 患者端配网文案契约（PRD §7A.9 患技差异表 / 设计稿 11 页）
 *
 * 目的：把"患者端不得出现技术术语""五类失败态必须各自独立"这两条
 * 从人工走查变成可执行断言，防止后续改动重新引入 SSID/DHCP/B511 等口径。
 */
import { describe, it, expect } from 'vitest'
import {
  CONNECT,
  CONTACT,
  ENTRY,
  FAILURES,
  FAILURE_KEY_BY_CODE,
  PROGRESS,
  SCAN,
  SUCCESS,
  type FailureKey,
} from '../../src/utils/wifi-copy'

const ALL_COPY: string[] = []

function collect(value: unknown): void {
  if (typeof value === 'string') ALL_COPY.push(value)
  else if (Array.isArray(value)) value.forEach(collect)
  else if (value && typeof value === 'object') Object.values(value).forEach(collect)
}
;[ENTRY, SCAN, CONNECT, PROGRESS, SUCCESS, FAILURES, CONTACT].forEach(collect)

describe('T192 — 患者端零技术术语（PRD §7A.9 患技差异表）', () => {
  const JARGON = ['SSID', 'DHCP', 'B511', 'B512', 'RSSI', 'device_id', '固件', 'int8', 'AES']

  it('全部患者可见文案不含技术术语', () => {
    const hits = ALL_COPY.flatMap((text) => JARGON.filter((word) => text.includes(word)).map((w) => `${w} ← ${text}`))
    expect(hits).toEqual([])
  })

  it('文案集合非空（防止采集逻辑失效造成假绿）', () => {
    expect(ALL_COPY.length).toBeGreaterThan(40)
  })
})

describe('T192/T218 — 失败态各自独立（设计稿 06a–06e + T218-B linklost）', () => {
  const KEYS: FailureKey[] = ['pwd', 'nonet', 'addr', 'srv', 'timeout', 'linklost']

  it('失败键齐全且各自有标题/说明/三条建议', () => {
    expect(Object.keys(FAILURES)).toHaveLength(6)
    for (const k of KEYS) {
      expect(FAILURES[k].title).not.toBe('')
      expect(FAILURES[k].desc).not.toBe('')
      expect(FAILURES[k].actions).toHaveLength(3)
      expect(FAILURES[k].primaryLabel).not.toBe('')
      expect(['connect', 'retry', 'entry']).toContain(FAILURES[k].primaryTo)
    }
  })

  it('各失败页标题互不重复', () => {
    expect(new Set(KEYS.map((k) => FAILURES[k].title)).size).toBe(KEYS.length)
  })

  it('06a/06b 有次按钮，06c/06d/06e 只有主按钮 + 联系技师（设计稿按钮组）；linklost 有次按钮（T218-B）', () => {
    expect(FAILURES.pwd.secondaryLabel).toBeTruthy()
    expect(FAILURES.nonet.secondaryLabel).toBeTruthy()
    expect(FAILURES.addr.secondaryLabel).toBeFalsy()
    expect(FAILURES.srv.secondaryLabel).toBeFalsy()
    expect(FAILURES.timeout.secondaryLabel).toBeFalsy()
    expect(FAILURES.linklost.secondaryLabel).toBeTruthy()
  })

  it('T218-B(A20)：linklost 文案必须表达"设备可能已配置成功"，不得暗示设备一定没配好', () => {
    expect(FAILURES.linklost.desc).toContain('有可能已经配置成功')
    expect(FAILURES.linklost.title).not.toBe(FAILURES.timeout.title)
  })

  it('状态码 -1~-4 映射到四个不同失败页', () => {
    expect(Object.values(FAILURE_KEY_BY_CODE)).toEqual(['pwd', 'nonet', 'addr', 'srv'])
  })
})

describe('T192 — 状态推进口径（PRD §7A.9 状态 0/1/2/3/9）', () => {
  it('进度页四态各有标题与递增百分比', () => {
    expect(PROGRESS.states).toHaveLength(4)
    const pcts = PROGRESS.states.map((s) => s.pct)
    expect([...pcts].sort((a, b) => a - b)).toEqual(pcts)
  })

  it('联系技师页覆盖六类失败 + 扫描无设备', () => {
    expect(Object.keys(CONTACT.typeMap)).toEqual(['pwd', 'nonet', 'addr', 'srv', 'timeout', 'linklost', 'nodevice'])
  })

  it('前置检查三项：蓝牙 / 位置 / 设备上电', () => {
    expect(ENTRY.prepItems.map((p) => p.key)).toEqual(['bluetooth', 'location', 'power'])
  })
})
