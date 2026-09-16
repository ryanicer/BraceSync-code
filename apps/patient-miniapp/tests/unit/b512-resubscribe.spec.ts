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
const PAGE = fileURLToPath(new URL('../../src/pages/wifi-setup/index.vue', import.meta.url))
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

  it('③-4 订阅标记只在 success 回执里授予（发起前置真＝fail 后本会话永久不再重试）', () => {
    const body = fnBody('subscribeB512Notify')
    const atNotify = body.indexOf('uni.notifyBLECharacteristicValueChange(')
    const atFail = body.indexOf('fail: (err)')
    const atSuccess = body.indexOf('success: ()')
    expect(atNotify).toBeGreaterThan(-1)
    expect(atSuccess).toBeGreaterThan(atNotify)
    expect(atFail).toBeGreaterThan(atNotify)

    // 授予动作必须落在 success 分支里
    expect(body.slice(atSuccess, atFail)).toContain('b512SubscribedKey = key')
    // 发起之前不得预先占坑；fail 分支也不得授予
    expect(body.slice(0, atNotify), '发起前就写 b512SubscribedKey = key ⇒ 订阅 fail 后永不重试').not.toContain('b512SubscribedKey = key')
    expect(body.slice(atFail)).not.toContain('b512SubscribedKey = key')
  })

  it('回执必须比对本次 key（旧链路迟到的 success/fail 不能污染新链路标记）', () => {
    const body = fnBody('subscribeB512Notify')
    expect(body).toMatch(/if \(b512InFlightKey !== key\) return/)
    expect(body).toMatch(/if \(b512InFlightKey === key\) b512InFlightKey = ''/)
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

  it('复位函数在两个标记都空时不重复打日志（避免 connectDevice 前置 close 刷屏），且 in-flight 一并作废', () => {
    const body = fnBody('invalidateB512Subscription')
    expect(body).toMatch(/if \(!b512SubscribedKey && !b512InFlightKey\) return/)
    expect(body).toContain("b512InFlightKey = ''")
  })
})

describe('T216① — 掉线重连路径接线（卡片 ③-3：重连＝新连接、无订阅）', () => {
  /** 取页面里某个顶层 function 的函数体（按下一个顶层 function 为界） */
  function pageFnBody(name: string): string {
    const page = fs.readFileSync(PAGE, 'utf8')
    const start = page.indexOf(`async function ${name}(`)
    expect(start, `页面里找不到 async function ${name}()`).toBeGreaterThan(-1)
    const rest = page.slice(start + name.length + 1)
    const next = rest.search(/\nasync function |\nfunction /)
    return next === -1 ? rest : rest.slice(0, next)
  }

  it('runProvision 内「重连判定」早于「订阅」——原地重连出来的新链路必须被订阅到', () => {
    const body = pageFnBody('runProvision')
    const atLink = body.indexOf('ensureLinkForProvision()')
    const atSub = body.indexOf('onWifiStatus(')

    expect(atLink).toBeGreaterThan(-1)
    expect(atSub).toBeGreaterThan(-1)
    expect(atLink, '订阅排在重连判定之前 ⇒ 新链路无订阅，复刻 09-16 00:18 的零收帧').toBeLessThan(atSub)
  })

  it('断言确实读到了页面源码（防路径写错导致空内容假绿）', () => {
    const page = fs.readFileSync(PAGE, 'utf8')
    expect(path.basename(PAGE)).toBe('index.vue')
    expect(page).toContain('writeWifiConfigV2')
    expect(page.length).toBeGreaterThan(1000)
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
