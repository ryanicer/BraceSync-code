/**
 * T212 → T216 — 技师端配网 seq：每轮自增，超出固件候选窗上限（64）才回绕
 *
 * 与患者端同缺陷、同修法：T212「每轮复位为 1」解决了越窗，却因 provision_key 不轮换而重用
 * (key, IV)；T216 改「自增 + 到 64 回绕」（窗 1..64 = T213 固件 kCandidateSeqMax）。
 * 密码学侧（同 seq 同明文必同密文、不同 seq 密文不同）已由 test/aes-ctr.spec.ts 覆盖，此处不重复。
 *
 * 页面挂载不了单测（utils/ble.ts → ble-log → logger 的条件编译链 vitest 读不动），故落点接线按源码断言。
 * 患者端同款断言见 apps/patient-miniapp/tests/unit/provision-seq.spec.ts。
 */
import { describe, it, expect, vi } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../src/utils/request', () => {
  const request = vi.fn()
  return { request, USE_MOCK: false }
})

import { useInstallStore, WIFI_SEQ_CANDIDATE_MAX } from '../src/stores/install'

const PAGE = fileURLToPath(new URL('../src/pages/wifi-config/index.vue', import.meta.url))

/** 复刻一轮「开始配网」：页面每轮只领取一次 seq */
function provisionOnce(): number {
  return useInstallStore().nextWifiSeq()
}

describe('T216 — install store seq 自增语义', () => {
  it('连续 6 轮「开始配网」seq = [1,2,3,4,5,6]（不再是 T212 那版的 [1,1,1,1,1,1]）', () => {
    setActivePinia(createPinia())
    const seqs = Array.from({ length: 6 }, () => provisionOnce())

    expect(seqs).toEqual([1, 2, 3, 4, 5, 6])
  })

  it('回绕：64 之后下一次回到 1，且全程不越固件候选窗 1..64', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()

    expect(WIFI_SEQ_CANDIDATE_MAX, '窗上限必须与 T213 固件 kCandidateSeqMax 一致').toBe(64)
    for (let i = 0; i < 63; i++) store.nextWifiSeq()
    expect(store.nextWifiSeq()).toBe(64)
    expect(store.nextWifiSeq()).toBe(1)
    expect(store.nextWifiSeq()).toBe(2)
    expect(store.wifiSeq).toBe(3)
  })

  it('同一轮内重复领取仍递增（协议 §3：同轮多次写设备 seq 须逐次递增）', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()

    expect(store.nextWifiSeq()).toBe(1)
    expect(store.nextWifiSeq()).toBe(2)
  })

  it('resetInstall 不复位 seq（换装机可能还是同一把不轮换的 provision_key）', () => {
    setActivePinia(createPinia())
    const store = useInstallStore()
    store.nextWifiSeq()
    store.nextWifiSeq()

    store.resetInstall()
    expect(store.wifiSeq).toBe(3)
    expect(store.nextWifiSeq()).toBe(3)
  })
})

describe('T216 — 落点接线（技师端下发前不复位、订阅先于写入）', () => {
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

  it('每轮只领取一次 seq，且不存在复位调用（复位＝重用 IV）', () => {
    expect([...src.matchAll(/nextWifiSeq\(\)/g)]).toHaveLength(1)
    expect(body).not.toContain('resetWifiSeq')
  })

  it('B512 订阅先于 B511 下发（T216①：顺序倒了首帧 0/1 会丢）', () => {
    const atSubscribe = body.indexOf('onWifiStatus(')
    const atWrite = body.indexOf('writeWifiConfigV2(')

    expect(atSubscribe, 'startWifiConfig 里没有 onWifiStatus 订阅调用').toBeGreaterThan(-1)
    expect(atWrite).toBeGreaterThan(-1)
    expect(atSubscribe).toBeLessThan(atWrite)
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
