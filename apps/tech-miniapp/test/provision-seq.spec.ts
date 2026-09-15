/**
 * T212 — 技师端配网 seq 每轮从 1 起（与患者端同缺陷、同修法）
 *
 * 固件解密只尝试 seq=1/2/3；旧实现整会话一直 +1 ⇒ 同一装机会话第 4 次配网起永远解不开（真回 -1）。
 * 页面挂载不了单测（utils/ble.ts → ble-log → logger 的条件编译链 vitest 读不动），
 * 故落点接线按源码断言。患者端同款断言见 apps/patient-miniapp/tests/unit/provision-seq.spec.ts。
 */
import { describe, it, expect, vi } from 'vitest'
import fs from 'node:fs'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})

import { useInstallStore } from '../src/stores/install'

const PAGE = new URL('../src/pages/wifi-config/index.vue', import.meta.url)

describe('T212 — install store seq 复位语义', () => {
  it('复位后每轮首个 seq 恒为 1，同轮内再领取仍递增', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()

    expect(store.nextWifiSeq()).toBe(1) // 首轮
    expect(store.nextWifiSeq()).toBe(2) // 同一轮内重写设备
    store.resetWifiSeq()
    expect(store.nextWifiSeq()).toBe(1) // 次轮回到 1
    store.resetWifiSeq()
    expect(store.nextWifiSeq()).toBe(1)
  })

  it('连点 6 次「开始配网」不越固件候选窗 1/2/3', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    const seqs = Array.from({ length: 6 }, () => {
      store.resetWifiSeq()
      return store.nextWifiSeq()
    })

    expect(seqs).toEqual([1, 1, 1, 1, 1, 1])
  })

  it('resetInstall 后仍为 1（换装机不误留旧 seq）', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.nextWifiSeq()
    store.nextWifiSeq()

    store.resetInstall()
    expect(store.wifiSeq).toBe(1)
  })
})

describe('T212 — 落点接线（技师端下发前必须复位）', () => {
  const src = fs.readFileSync(PAGE, 'utf8')

  /** 取出某个顶层 function 的函数体（按下一个顶层 function 为界） */
  function fnBody(name: string): string {
    const start = src.indexOf(`function ${name}(`)
    expect(start, `页面里找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
    const rest = src.slice(start + name.length + 1) // 跳过定义行自身，避免函数名被当成调用点
    const next = rest.search(/\nasync function |\nfunction /)
    return next === -1 ? rest : rest.slice(0, next)
  }

  const body = fnBody('startWifiConfig')

  it('startWifiConfig 内确有 resetWifiSeq()，且在 nextWifiSeq() 之前', () => {
    const atReset = body.indexOf('resetWifiSeq()')
    const atNext = body.indexOf('nextWifiSeq()')

    expect(atReset, '缺少复位调用 ⇒ 同会话第 4 次起必失败').toBeGreaterThan(-1)
    expect(atNext).toBeGreaterThan(-1)
    expect(atReset).toBeLessThan(atNext)
  })

  it('整个页面只有 startWifiConfig 领取 seq（不存在绕过复位的下发口）', () => {
    expect([...src.matchAll(/nextWifiSeq\(\)/g)]).toHaveLength(1)
  })

  it('T212：超时兜底用 60s 常量，且不再是裸 20000', () => {
    expect(body).toContain('}, PROVISION_TIMEOUT_MS)')
    expect(src).toMatch(/const PROVISION_TIMEOUT_MS = 60000/)
    expect(src).not.toMatch(/\}, 20000\)/)
  })

  it('断言确实读到了页面源码（防路径写错导致空内容假绿）', () => {
    expect(src).toContain('writeWifiConfigV2')
    expect(src.length).toBeGreaterThan(1000)
  })
})
