/**
 * T216① — 技师端 B512 订阅必须「每条链路重发」的结构断言（与患者端同缺陷、同修法）
 *
 * 技师端 wifi-config 页此前还有一个次序问题：onWifiStatus 写在 writeWifiConfigV2 之后 ⇒
 * 设备回的首帧（0/1）在订阅生效前就丢了。本文件连同该次序一起钉住。
 * 为什么只能按源码结构断言：utils/ble.ts 在 vitest 里挂不起来（ble-log → logger 的 #ifdef 双声明，
 * esbuild 不做条件编译；且 isH5() 会因两支都保留而恒真）。行为验证见自报的真机判据。
 * 患者端同款断言见 apps/patient-miniapp/tests/unit/b512-resubscribe.spec.ts。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const FILE = fileURLToPath(new URL('../src/utils/ble.ts', import.meta.url))
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

  it('B512 的 uni.onBLECharacteristicValueChange 只注册一次（B513 另有自己的 latch，不在本卡范围）', () => {
    const body = fnBody('subscribeB512Notify')
    expect(count(body, 'uni.onBLECharacteristicValueChange(')).toBe(1)
    expect(body).toContain('if (!b512ListenerRegistered)')
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
    expect(count(fnBody('createBLEConnection'), 'linkGeneration += 1')).toBeGreaterThanOrEqual(2)
  })

  it('主动关连接与断开回调都会作废订阅标记', () => {
    expect(fnBody('closeBLEConnection')).toContain('invalidateB512Subscription(')
    const listener = fnBody('registerBleStateListener')
    expect(listener).toContain('invalidateB512Subscription(')
    // T218 A-2 起断开分支体改为多语句块（B513 一并复位），不再限定单行形式
    expect(listener).toMatch(/!res\.connected\)\s*\{[^}]*invalidateB512Subscription/)
  })

  it('复位函数在两个标记都空时不重复打日志（避免 connectDevice 前置 close 刷屏），且 in-flight 一并作废', () => {
    const body = fnBody('invalidateB512Subscription')
    expect(body).toMatch(/if \(!b512SubscribedKey && !b512InFlightKey\) return/)
    expect(body).toContain("b512InFlightKey = ''")
  })
})

describe('T216① — 真机可观测性 + 技师端下发次序', () => {
  it('订阅与收帧都带序号：第 N 次订阅 / 第 M 帧', () => {
    const body = fnBody('subscribeB512Notify')
    expect(body).toContain('B512 Notify 订阅成功 第${round}次')
    expect(body).toContain('B512 发起订阅 第${round}次')
    expect(body).toContain('帧 订阅轮次=')
  })

  it('startWifiConfig 先订阅 B512 再写 B511', () => {
    const page = fs.readFileSync(fileURLToPath(new URL('../src/pages/wifi-config/index.vue', import.meta.url)), 'utf8')
    const start = page.indexOf('async function startWifiConfig(')
    const end = page.indexOf('\nasync function ', start + 10)
    const body = page.slice(start, end)

    const atSubscribe = body.indexOf('onWifiStatus(')
    const atWrite = body.indexOf('writeWifiConfigV2(')
    expect(atSubscribe).toBeGreaterThan(-1)
    expect(atWrite).toBeGreaterThan(-1)
    expect(atSubscribe, '订阅晚于下发 ⇒ 首帧 0/1 必丢').toBeLessThan(atWrite)
  })

  it('断言确实读到了 ble.ts 源码（防路径写错导致空内容假绿）', () => {
    expect(path.basename(FILE)).toBe('ble.ts')
    expect(src).toContain('CHAR_WIFI_STATUS')
    expect(src.length).toBeGreaterThan(3000)
  })
})
