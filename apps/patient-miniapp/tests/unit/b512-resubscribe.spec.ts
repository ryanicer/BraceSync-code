/**
 * T216① — B512 订阅必须「每条链路重发」的结构断言
 *
 * 真机（2026-09-15 夜）：一次冷启动会话 14 次连接只有 1 次订阅成功 ⇒ 之后设备回了状态帧、
 * App 一帧收不到，界面落「设备响应超时」。根因是把「全局监听只注册一次」和「订阅动作」
 * 用同一个一次性布尔一起短路了。
 *
 * 为什么只能按源码结构断言：src/utils/ble.ts 在 vitest 里挂不起来——
 *   ① import 链 ble-log → logger 的 #ifdef 两支各声明一次同名常量，esbuild 不做条件编译 ⇒ 直接 transform 失败；
 *   ② 即便挂起来，isH5() 的 #ifdef 两支会保留 `return true` 在前 ⇒ 真机分支永远走不到。
 * 所以这里把"标记复位/按链路重发"钉成结构契约（改名即红，逼改的人回来对账），
 * 行为验证只能落真机：判据见自报（同一会话连配 3 次，每次都要出现「B512 Notify 订阅成功 第N次」）。
 * 技师端同款断言见 apps/tech-miniapp/test/b512-resubscribe.spec.ts。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const FILE = fileURLToPath(new URL('../../src/utils/ble.ts', import.meta.url))
const src = fs.readFileSync(FILE, 'utf8')

/** 取顶层（含 export）函数体：以下一个顶格 function 声明为界 */
function fnBody(name: string): string {
  const start = src.indexOf(`function ${name}(`)
  expect(start, `ble.ts 里找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
  const rest = src.slice(start + name.length + 1)
  const next = rest.search(/\n(?:export )?(?:async )?function /)
  return next === -1 ? rest : rest.slice(0, next)
}

function count(hay: string, needle: string): number {
  return hay.split(needle).length - 1
}

describe('T216① — 订阅动作与全局监听解耦', () => {
  it('一次性永久短路标记 b512NotifyRegistered 已删除', () => {
    expect(src).not.toContain('b512NotifyRegistered')
  })

  it('onWifiStatus 每轮都走订阅判定（自身不再持有任何"已注册"短路）', () => {
    const body = fnBody('onWifiStatus')
    expect(body).toContain('subscribeB512Notify(')
    expect(body).not.toMatch(/Registered\s*\|\||\|\|\s*\w*Registered/)
  })

  it('uni.onBLECharacteristicValueChange 全文件只注册一次（真机 listener 会叠加，重复注册＝一帧回调多遍）', () => {
    expect(count(src, 'uni.onBLECharacteristicValueChange(')).toBe(1)
    expect(fnBody('subscribeB512Notify')).toContain('if (!b512ListenerRegistered)')
  })

  it('notify 订阅按「deviceId + 链路代次」判定，且判定在发起之前', () => {
    const body = fnBody('subscribeB512Notify')
    const atKey = body.indexOf('const key = ')
    const atGuard = body.indexOf('b512SubscribedKey === key')
    const atNotify = body.indexOf('uni.notifyBLECharacteristicValueChange(')

    expect(atKey).toBeGreaterThan(-1)
    expect(body.slice(atKey, body.indexOf('\n', atKey))).toMatch(/deviceId[\s\S]*linkGeneration/)
    expect(atGuard, '缺少"当前链路已订阅"判定 ⇒ 会退化成每帧重复 notify').toBeGreaterThan(-1)
    expect(atNotify).toBeGreaterThan(-1)
    expect(atGuard).toBeLessThan(atNotify)
    // 订阅请求必须带上当前 deviceId，而不是首次调用时缓存的旧值
    expect(body.slice(atNotify, atNotify + 200)).toContain('deviceId,')
  })

  it('订阅失败要把标记放回空值，否则下一轮仍被短路', () => {
    const body = fnBody('subscribeB512Notify')
    const atFail = body.indexOf('fail: (err)')
    expect(atFail).toBeGreaterThan(-1)
    expect(body.slice(atFail)).toContain("b512SubscribedKey = ''")
  })
})

describe('T216① — 标记复位入口（链路生命周期）', () => {
  it('每次连接建立都推进链路代次（success 与 already connect 两条都要）', () => {
    const body = fnBody('createBLEConnection')
    expect(count(body, 'linkGeneration += 1')).toBeGreaterThanOrEqual(2)
  })

  it('主动关连接与断开回调都会作废订阅标记', () => {
    expect(fnBody('closeBLEConnection')).toContain('invalidateB512Subscription(')
    const listener = fnBody('registerBleStateListener')
    expect(listener).toContain('invalidateB512Subscription(')
    expect(listener).toMatch(/!res\.connected\)\s*invalidateB512Subscription/)
  })

  it('复位函数在标记已空时不重复打日志（避免 connectDevice 前置 close 刷屏）', () => {
    const body = fnBody('invalidateB512Subscription')
    expect(body).toMatch(/if \(!b512SubscribedKey\) return/)
  })
})

describe('T216① — 真机可观测性（判据落到日志）', () => {
  it('订阅与收帧都带序号：第 N 次订阅 / 第 M 帧', () => {
    const body = fnBody('subscribeB512Notify')
    expect(body).toContain('B512 Notify 订阅成功 第${round}次')
    expect(body).toContain('B512 发起订阅 第${round}次')
    expect(body).toContain('帧 订阅轮次=')
  })

  it('断言确实读到了 ble.ts 源码（防路径写错导致空内容假绿）', () => {
    expect(path.basename(FILE)).toBe('ble.ts')
    expect(src).toContain('CHAR_WIFI_STATUS')
    expect(src.length).toBeGreaterThan(3000)
  })
})
