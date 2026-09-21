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

/**
 * T297 — 2.4GHz 提示只给能力口径，不教用户凭网络名判断频段
 * （PRD §7A.9.1 ④「只认信道不认名字」红线 + Boss 09-21 23:02 裁定 D5 选①）
 */
describe('T297 — 患者端不得出现「凭网络名称判断频段」的引导', () => {
  const NAME_BASED = ['名称带', '字样', '看名字', '名字带']

  it('三处 2.4G 引导（入口提示 / 扫描前置 / 名称怎么找第 3 步）已去掉认名字句式', () => {
    const targets: Array<[string, string]> = [
      ['ENTRY.hint', ENTRY.hint],
      ['SCAN.notice24G', SCAN.notice24G],
      ['CONNECT.findModal.steps[2]', CONNECT.findModal.steps[2]],
    ]
    for (const [name, text] of targets) {
      for (const word of NAME_BASED) expect(text, `${name} 仍留认名字句式`).not.toContain(word)
    }
  })

  it('全量患者文案零命中认名字句式（防止换个键位回潮）', () => {
    const hits = ALL_COPY.flatMap((t) => NAME_BASED.filter((w) => t.includes(w)).map((w) => `${w} ← ${t}`))
    expect(hits).toEqual([])
  })

  it('能力提示不丢：三处仍写明仅支持 2.4GHz、不支持 5G', () => {
    for (const text of [ENTRY.hint, SCAN.notice24G, CONNECT.findModal.steps[2]]) {
      expect(text).toContain('2.4GHz')
      expect(text).toContain('5G')
    }
    expect(ENTRY.hint).toContain('本设备仅支持 2.4GHz 家庭 WiFi')
    expect(SCAN.notice24G).toContain('本设备仅支持 2.4GHz 家庭 WiFi')
  })
})
