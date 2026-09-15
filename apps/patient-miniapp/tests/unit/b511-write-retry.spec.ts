/**
 * T216③ — 重连后首写 B511 失败要「受控重试」，不能靠用户再点一次兜
 *
 * 真机（09-16 00:18:22 → 00:18:32）：设备在建连后 24s 自断，正在进行的写落在 API 层失败
 * `B511 写入失败: writeBLECharacteristicValue:fail:system error:Inner error.`（不是设备回的 -1..-4 失败码），
 * 旧实现直接弹回 03 让用户再点。现在：判死链路 → 原地重连 → 重订阅 → 换新 seq 重写一次；
 * 仍失败（或重连不回）才走既有失败路径。
 *
 * 为什么按源码结构断言：页面挂不起来（utils/ble.ts 的 #ifdef 条件编译链 esbuild 不处理），
 * 先例见 b512-resubscribe.spec.ts / provision-seq.spec.ts。真机判据见自报 §5。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const PAGE = fileURLToPath(new URL('../../src/pages/wifi-setup/index.vue', import.meta.url))
const BLE = fileURLToPath(new URL('../../src/utils/ble.ts', import.meta.url))
const page = fs.readFileSync(PAGE, 'utf8')
const ble = fs.readFileSync(BLE, 'utf8')

/** 取页面里某个顶层 function 的函数体（按下一个顶层 function 为界） */
function pageFnBody(name: string): string {
  const start = page.indexOf(`async function ${name}(`)
  expect(start, `页面里找不到 async function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
  const rest = page.slice(start + name.length + 1)
  const next = rest.search(/\nasync function |\nfunction /)
  return next === -1 ? rest : rest.slice(0, next)
}

const body = pageFnBody('runProvision')

describe('T216③ — 受控重试的接线', () => {
  it('重试次数由具名常量兜住，且就是"一次额外尝试"（=2）', () => {
    expect(body).toMatch(/attempt <= WRITE_MAX_ATTEMPTS/)
    expect(page).toMatch(/const WRITE_MAX_ATTEMPTS = 2\b/)
  })

  it('订阅、领 seq、写 B511 全在重试循环内（重连出来的新链路必须被重新订阅）', () => {
    const atLoop = body.indexOf('for (let attempt')
    const atSub = body.indexOf('onWifiStatus(', atLoop)
    const atSeq = body.indexOf('nextWifiSeq()', atLoop)
    const atWrite = body.indexOf('writeWifiConfigV2(', atLoop)

    expect(atLoop).toBeGreaterThan(-1)
    expect(atSub).toBeGreaterThan(atLoop)
    expect(atSeq).toBeGreaterThan(atLoop)
    expect(atWrite).toBeGreaterThan(atLoop)
    expect(atSub).toBeLessThan(atWrite)
  })

  it('写失败先记一条与"设备失败码"可区分的日志，再判死链路、再原地重连', () => {
    const atCatch = body.indexOf('catch (e)')
    const tail = body.slice(atCatch)
    const atLog = tail.indexOf('B511 下发失败（API 层，非设备失败码）')
    const atDead = tail.indexOf('setBleConnected(false)')
    const atLink = tail.indexOf('ensureLinkForProvision()')

    expect(atLog).toBeGreaterThan(-1)
    expect(atDead).toBeGreaterThan(-1)
    expect(atLink).toBeGreaterThan(-1)
    expect(atLog).toBeLessThan(atDead) // 先留证据再改状态
    expect(atDead).toBeLessThan(atLink) // 不判死 ⇒ ensureLink 会直接 return true，等于原地再失败一次
  })

  it('重连不回 或 次数用尽 ⇒ 一定把原始错误抛出，交既有失败路径（回 03）', () => {
    expect(body).toMatch(/if \(attempt === WRITE_MAX_ATTEMPTS \|\| !\(await ensureLinkForProvision\(\)\)\) break/)
    expect(body).toContain('if (writeErr) throw writeErr')
  })

  it('超时兜底仍只在写成功后挂上（失败路径不该再等 60s）', () => {
    const atWrite = body.indexOf('writeWifiConfigV2(')
    const atArm = body.indexOf('armTimeout()')
    expect(atArm).toBeGreaterThan(atWrite)
    expect(page).toContain('}, PROVISION_TIMEOUT_MS)')
  })
})

describe('T216③ — 红线自查（写入语义没被顺手改掉）', () => {
  it('B511 分片大小仍是 180B，写失败仍带完整 errMsg + errCode/errno', () => {
    expect(ble).toMatch(/const WIFI_CHUNK_SIZE = 180\b/)
    expect(ble).toContain('B511 写入失败: ${err?.errMsg}')
    expect(ble).toMatch(/errCode, err\?\.errno/)
  })

  it('页面仍只有一个 seq 领取口（重试也走它，不存在绕过窗口的常量 seq）', () => {
    expect([...page.matchAll(/nextWifiSeq\(\)/g)]).toHaveLength(1)
  })

  it('断言确实读到了源码（防路径写错导致空内容假绿）', () => {
    expect(path.basename(PAGE)).toBe('index.vue')
    expect(path.basename(BLE)).toBe('ble.ts')
    expect(page.length).toBeGreaterThan(1000)
    expect(ble.length).toBeGreaterThan(3000)
  })
})
