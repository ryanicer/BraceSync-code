/**
 * T212 — 配网 seq 每轮从 1 起（固件解密只尝试 seq=1/2/3）
 *
 * 真机（2026-09-15 夜）：同一会话内连点 4 次「开始配网」后，seq 累加到 4 ⇒ 固件三个候选值
 * 全对不上 ⇒ 每次都解成乱码回 -1，且只有冷启动小程序才恢复。
 *
 * 两条判据都要有：
 *   ① store 语义——复位后领取恒为 1，且同一轮内多次领取仍递增（没有退化成常量 1）；
 *   ② 落点接线——页面确实在 nextWifiSeq 之前调了 resetWifiSeq。
 * ② 只能按源码断言：配网页 import 链经 ble-log → logger（同名常量在 #ifdef 两支各声明一次），
 * vitest/esbuild 不处理条件编译，import 即失败 ⇒ 挂载不了页面。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'
import { useDeviceStore } from '../../src/stores/device'
import { encryptWifiPayload } from '../../src/utils/aes-ctr'

const PAGE = fileURLToPath(new URL('../../src/pages/wifi-setup/index.vue', import.meta.url))

/** 16B 测试密钥（32hex）；凭据一律假值 */
const KEY = '2b7e151628aed2a6abf7158809cf4f3c'

/** 复刻 03 页一轮「开始配网」的 seq 取用与载荷组装（顺序须与页面一致，由下面的接线断言把守） */
function provisionOnce(ssid: string, pwd: string): { seq: number; payload: string } {
  const store = useDeviceStore()
  store.resetWifiSeq()
  const seq = store.nextWifiSeq()
  return { seq, payload: encryptWifiPayload(ssid, pwd, KEY, seq) }
}

describe('T212 — seq 复位后每轮恒为 1', () => {
  it('连续两次「开始配网」，两次传给 encryptWifiPayload 的 seq 都是 1', () => {
    setActivePinia(createPinia())
    const first = provisionOnce('Clinic_24G', 'p@ssw0rd-01')
    const second = provisionOnce('Clinic_24G', 'p@ssw0rd-02')

    expect(first.seq).toBe(1)
    expect(second.seq).toBe(1)
    // seq 真进了载荷：IV 大端前 4 字节 = 00000001 ⇒ 与手工按 seq=1 加密的密文逐字节相同
    expect(first.payload).toBe(encryptWifiPayload('Clinic_24G', 'p@ssw0rd-01', KEY, 1))
    expect(second.payload).toBe(encryptWifiPayload('Clinic_24G', 'p@ssw0rd-02', KEY, 1))
  })

  it('连点 6 次仍不越固件候选窗 1/2/3（旧实现第 4 次起 seq=4 必失败）', () => {
    setActivePinia(createPinia())
    const seqs = Array.from({ length: 6 }, () => provisionOnce('Clinic_24G', 'p@ssw0rd-01').seq)

    expect(seqs).toEqual([1, 1, 1, 1, 1, 1])
  })

  it('同一轮内重复领取仍递增（防 CTR 重用的原意没被改成常量 1）', () => {
    setActivePinia(createPinia())
    const store = useDeviceStore()

    store.resetWifiSeq()
    expect(store.nextWifiSeq()).toBe(1)
    expect(store.nextWifiSeq()).toBe(2) // 同轮内第二次写设备
    store.resetWifiSeq()
    expect(store.nextWifiSeq()).toBe(1) // 下一轮重新从 1 起
  })
})

describe('T212 — 落点接线（页面必须在下发前复位）', () => {
  /** 取出某个顶层 function 的函数体（按下一个顶层 function 为界） */
  function fnBody(src: string, name: string): string {
    const start = src.indexOf(`function ${name}(`)
    expect(start, `页面里找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
    const rest = src.slice(start + name.length + 1) // 跳过定义行自身，避免函数名被当成调用点
    const next = rest.search(/\nasync function |\nfunction /)
    return next === -1 ? rest : rest.slice(0, next)
  }

  const body = fnBody(fs.readFileSync(PAGE, 'utf8'), 'runProvision')
  const atReset = body.indexOf('resetWifiSeq()')
  const atNext = body.indexOf('nextWifiSeq()')

  it('runProvision 内确有 resetWifiSeq() 调用', () => {
    expect(atReset).toBeGreaterThan(-1)
  })

  it('复位在领取 seq 之前（放到之后就等于每轮仍 +1）', () => {
    expect(atNext).toBeGreaterThan(-1)
    expect(atReset).toBeLessThan(atNext)
  })

  it('整个页面只有 runProvision 领取 seq（不存在绕过复位的其他下发口）', () => {
    const src = fs.readFileSync(PAGE, 'utf8')
    const calls = [...src.matchAll(/nextWifiSeq\(\)/g)].length
    expect(calls).toBe(1)
  })

  it('断言确实读到了页面源码（防路径写错导致空文件假绿）', () => {
    expect(path.basename(PAGE)).toBe('index.vue')
    expect(body).toContain('writeWifiConfigV2')
  })
})
