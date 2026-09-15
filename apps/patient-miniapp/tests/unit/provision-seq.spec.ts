/**
 * T212 → T216 — 配网 seq 语义：每轮自增，超出固件候选窗上限（64）才回绕
 *
 * 演进：
 *   旧实现「整会话一直 +1」⇒ 第 4 次起 seq 越出旧固件窗 1/2/3，设备永远解不开（09-15 夜真机实证）；
 *   T212 改「每轮复位为 1」解决了越窗，但 provision_key 每次配网不轮换 ⇒ 两轮用同一 (key, IV)
 *   加密不同明文 = CTR keystream 复用（两段密文异或即得两段明文异或）；
 *   T216 定稿「每轮自增、将超 64 才回绕」（窗 1..64 = T213 固件 kCandidateSeqMax，须与已烧固件同批上线）。
 *
 * 三条判据都要有：
 *   ① store 语义——6 轮为 [1..6]、到 64 回绕、同轮内仍递增；
 *   ② 密码学后果——同凭据连点两轮密文必须不同（恒 1 的旧语义在此必红）；
 *   ③ 落点接线——页面不再复位、每轮只领取一次，且 B512 订阅先于 B511 下发（T216①）。
 * ③ 只能按源码断言：配网页 import 链经 ble-log → logger（同名常量在 #ifdef 两支各声明一次），
 * vitest/esbuild 不处理条件编译，import 即失败 ⇒ 挂载不了页面。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'
import { useDeviceStore, WIFI_SEQ_CANDIDATE_MAX } from '../../src/stores/device'
import { encryptWifiPayload } from '../../src/utils/aes-ctr'

const PAGE = fileURLToPath(new URL('../../src/pages/wifi-setup/index.vue', import.meta.url))

/** 16B 测试密钥（32hex）；凭据一律假值 */
const KEY = '2b7e151628aed2a6abf7158809cf4f3c'

/** 复刻 03 页一轮「开始配网」的 seq 取用与载荷组装（顺序须与页面一致，由下面的接线断言把守） */
function provisionOnce(ssid: string, pwd: string): { seq: number; payload: string } {
  const store = useDeviceStore()
  const seq = store.nextWifiSeq()
  return { seq, payload: encryptWifiPayload(ssid, pwd, KEY, seq) }
}

describe('T216 — device store seq 自增语义', () => {
  it('连续 6 轮「开始配网」seq = [1,2,3,4,5,6]（不再是 T212 那版的 [1,1,1,1,1,1]）', () => {
    setActivePinia(createPinia())
    const seqs = Array.from({ length: 6 }, () => provisionOnce('Clinic_24G', 'p@ssw0rd-01').seq)

    expect(seqs).toEqual([1, 2, 3, 4, 5, 6])
  })

  it('首轮恒为 1，且全程不越固件候选窗 1..64', () => {
    setActivePinia(createPinia())
    const store = useDeviceStore()

    expect(WIFI_SEQ_CANDIDATE_MAX, '窗上限必须与 T213 固件 kCandidateSeqMax 一致').toBe(64)
    const seqs = Array.from({ length: 200 }, () => store.nextWifiSeq())
    expect(seqs[0]).toBe(1)
    expect(Math.min(...seqs)).toBe(1)
    expect(Math.max(...seqs)).toBe(64)
  })

  it('回绕：走到 64 之后下一次回到 1，再往后继续 2、3', () => {
    setActivePinia(createPinia())
    const store = useDeviceStore()

    for (let i = 0; i < 63; i++) store.nextWifiSeq()
    expect(store.nextWifiSeq()).toBe(64) // 窗内最后一个
    expect(store.nextWifiSeq()).toBe(1) // 回绕
    expect(store.nextWifiSeq()).toBe(2)
  })

  it('同一轮内重复领取仍递增（协议 §3：同轮多次写设备 seq 须逐次递增）', () => {
    setActivePinia(createPinia())
    const store = useDeviceStore()

    expect(store.nextWifiSeq()).toBe(1)
    expect(store.nextWifiSeq()).toBe(2)
    expect(store.nextWifiSeq()).toBe(3)
  })
})

describe('T216 — keystream 复用防线（复位语义回归即红）', () => {
  it('同凭据连点两轮 ⇒ 密文不同（IV 随 seq 变，(key, IV) 不复用）', () => {
    setActivePinia(createPinia())
    const first = provisionOnce('Clinic_24G', 'same-p@ss')
    const second = provisionOnce('Clinic_24G', 'same-p@ss')

    expect(first.seq).not.toBe(second.seq)
    expect(first.payload).not.toBe(second.payload)
    // 反向对照：同一 seq 下同明文必同密文 ⇒ 上面不同确实来自 seq/IV，而不是随机性
    expect(encryptWifiPayload('Clinic_24G', 'same-p@ss', KEY, 1)).toBe(first.payload)
  })

  it('回绕后（64→1）与首轮同 seq：这是设计上的窗口边界，须与固件窗 1..64 同批上线', () => {
    setActivePinia(createPinia())
    const store = useDeviceStore()
    const seqs = Array.from({ length: 65 }, () => store.nextWifiSeq())

    expect(seqs[64]).toBe(1)
    expect(seqs[64]).toBe(seqs[0])
  })
})

describe('T216 — 落点接线（患者端 03 页）', () => {
  /** 取出某个顶层 function 的函数体（按下一个顶层 function 为界） */
  function fnBody(src: string, name: string): string {
    const start = src.indexOf(`function ${name}(`)
    expect(start, `页面里找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
    const rest = src.slice(start + name.length + 1) // 跳过定义行自身，避免函数名被当成调用点
    const next = rest.search(/\nasync function |\nfunction /)
    return next === -1 ? rest : rest.slice(0, next)
  }

  const src = fs.readFileSync(PAGE, 'utf8')
  const body = fnBody(src, 'runProvision')

  it('每轮只领取一次 seq，且不存在复位调用（复位＝重用 IV）', () => {
    expect([...src.matchAll(/nextWifiSeq\(\)/g)]).toHaveLength(1)
    expect(body).not.toContain('resetWifiSeq')
    expect(src).not.toMatch(/\}, 20000\)/) // T212 超时 60s 不回退
  })

  it('B512 订阅先于 B511 下发（顺序倒了首帧 0/1 会丢）', () => {
    const atSubscribe = body.indexOf('onWifiStatus(')
    const atWrite = body.indexOf('writeWifiConfigV2(')

    expect(atSubscribe, 'runProvision 里没有 onWifiStatus 订阅调用').toBeGreaterThan(-1)
    expect(atWrite).toBeGreaterThan(-1)
    expect(atSubscribe).toBeLessThan(atWrite)
  })

  it('断言确实读到了页面源码（防路径写错导致空文件假绿）', () => {
    expect(path.basename(PAGE)).toBe('index.vue')
    expect(body).toContain('writeWifiConfigV2')
    expect(src.length).toBeGreaterThan(1000)
  })
})
