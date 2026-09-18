/**
 * T218 — 技师端 BLE 链路健壮性收口（A 节）断言
 *
 * A-1: B513 实时流一次性 latch（无复位入口 ⇒ 断一次链、校准/测压页压力数恒 0）
 *      改为 T216① 同款「deviceId + 链路代次」订阅，标记只在 success 回执授予。
 * A-2: 配网页接入连接态监听 + 掉线禁止提交（ble.ts 监听回调随之改多播）。
 * A-3: 原地重连层平移（pickReconnectTarget 纯逻辑抽 ble-link.ts 可单测）。
 * A-4: 写失败受控重试（WRITE_MAX_ATTEMPTS=2，重试必须领新 seq）。
 * A-5: 超时语义逐帧续期。
 *
 * 为什么混用源码结构断言：utils/ble.ts 在 vitest 里挂不起来（ble-log → logger 的
 * #ifdef 双声明，esbuild 不做条件编译）。ble-link.ts 零依赖，走真单测。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const FILE = fileURLToPath(new URL('../src/utils/ble.ts', import.meta.url))
const src = fs.readFileSync(FILE, 'utf8')
const PAGE = fs.readFileSync(
  fileURLToPath(new URL('../src/pages/wifi-config/index.vue', import.meta.url)),
  'utf8'
)

/** 取顶层（含 export）函数体：以下一个顶格 function 声明为界 */
function fnBody(hay: string, name: string): string {
  const start = hay.indexOf(`function ${name}(`)
  expect(start, `找不到 function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
  const rest = hay.slice(start + name.length + 1)
  const next = rest.search(/\n(?:export )?(?:async )?function /)
  return next === -1 ? rest : rest.slice(0, next)
}

describe('T218 A-1 — B513 订阅按链路代次重发（T216① 同款）', () => {
  it('一次性 latch 变量 realtimeNotifyRegistered 已删除', () => {
    expect(src).not.toContain('realtimeNotifyRegistered')
  })

  it('B513 订阅 key = deviceId#linkGeneration，判定在发起之前', () => {
    const body = fnBody(src, 'startRealtimePressure')
    expect(body).toContain('${deviceId}#${linkGeneration}')
    const atKey = body.indexOf('const key = ')
    const atGuard = body.indexOf('realtimeSubscribedKey === key')
    const atNotify = body.indexOf('uni.notifyBLECharacteristicValueChange(')
    expect(atGuard, '缺少"当前链路已订阅"判定').toBeGreaterThan(-1)
    expect(atNotify).toBeGreaterThan(-1)
    expect(atKey).toBeGreaterThan(-1)
    expect(atGuard).toBeLessThan(atNotify)
  })

  it('B513 订阅标记只在 success 回执授予；fail 只清 in-flight', () => {
    const body = fnBody(src, 'startRealtimePressure')
    const atNotify = body.indexOf('uni.notifyBLECharacteristicValueChange(')
    const atSuccess = body.indexOf('success: ()', atNotify)
    const atFail = body.indexOf('fail: (err)', atNotify)
    expect(atSuccess).toBeGreaterThan(-1)
    expect(atFail).toBeGreaterThan(atSuccess)
    const grant = 'realtimeSubscribedKey = key'
    expect(body.slice(atSuccess, atFail)).toContain(grant)
    expect(body.slice(0, atNotify), '发起前不得预占坑').not.toContain(grant)
    expect(body.slice(atFail)).not.toContain(grant)
    expect(body).toMatch(/if \(realtimeInFlightKey === key\) realtimeInFlightKey = ''/)
  })

  it('复位入口：断开回调与主动关连接都会作废 B513 标记（且 in-flight 一并作废）', () => {
    const listener = fnBody(src, 'registerBleStateListener')
    expect(listener).toContain('invalidateRealtimeSubscription(')
    const close = fnBody(src, 'closeBLEConnection')
    expect(close).toContain('invalidateRealtimeSubscription(')
    const inv = fnBody(src, 'invalidateRealtimeSubscription')
    expect(inv).toMatch(/if \(!realtimeSubscribedKey && !realtimeInFlightKey\) return/)
    expect(inv).toContain("realtimeInFlightKey = ''")
  })

  it('B513 全局帧监听仍只注册一次、不叠加', () => {
    const body = fnBody(src, 'startRealtimePressure')
    expect(body).toContain('if (!realtimeListenerRegistered)')
    expect(body).toMatch(/realtimeListenerRegistered = true/)
    // 监听注册只出现一次；订阅动作（notifyBLECharacteristicValueChange）在监听块之外
    expect(body.split('uni.onBLECharacteristicValueChange(').length - 1).toBe(1)
  })
})

describe('T218 A-2 — 连接态监听多播（页面栈多页共存）', () => {
  it('ble.ts 回调容器是多播 Set，单回调 bleStateCallback 已删除', () => {
    expect(src).not.toMatch(/let bleStateCallback\b/)
    expect(src).not.toContain('bleStateCallback?.')
    expect(src).toContain('bleStateCallbacks')
    const body = fnBody(src, 'registerBleStateListener')
    expect(body).toMatch(/for \(const \w+ of bleStateCallbacks\)/)
    expect(body).toContain('bleStateCallbacks.delete(cb)')
  })

  it('wifi-config 页注册了连接态监听，掉线提示 + 禁止提交 + 卸载解绑', () => {
    expect(PAGE).toContain('registerBleStateListener(')
    expect(PAGE).toMatch(/if \(!bleLinkUp\.value\)/)
    expect(PAGE).toContain('设备连接已断开，请靠近设备后重试')
    expect(PAGE).toContain('offBleState()')
  })
})

describe('T218 A-3 — 原地重连层', () => {
  it('ble-link.ts 存在且零 import（可真单测）', () => {
    const link = fs.readFileSync(
      fileURLToPath(new URL('../src/utils/ble-link.ts', import.meta.url)),
      'utf8'
    )
    expect(link).not.toMatch(/^import /m)
    expect(link).toContain('export function pickReconnectTarget')
  })

  it('wifi-config 下发前 ensureLinkForProvision，直连失败后一次性重扫', () => {
    const body = fnBody(PAGE, 'ensureLinkForProvision')
    expect(body).toContain('tryLink(')
    expect(body).toContain('rescanOnce(')
    expect(body).toContain('pickReconnectTarget(')
    const start = PAGE.indexOf('async function startWifiConfig(')
    const startBody = PAGE.slice(start, PAGE.indexOf('/* ===== T218 A-3', start))
    expect(startBody).toContain('await ensureLinkForProvision()')
  })
})

describe('T218 A-4 — 写失败受控重试（领新 seq）', () => {
  it('WRITE_MAX_ATTEMPTS=2，重试循环里重新领 seq、重新订阅、ensureLink', () => {
    expect(PAGE).toContain('const WRITE_MAX_ATTEMPTS = 2')
    const start = PAGE.indexOf('for (let attempt = 1; attempt <= WRITE_MAX_ATTEMPTS; attempt++)')
    expect(start).toBeGreaterThan(-1)
    const end = PAGE.indexOf('if (writeErr) throw writeErr', start)
    const loop = PAGE.slice(start, end)
    expect(loop, '重试必须领新 seq（不得重放同一 IV）').toContain('installStore.nextWifiSeq()')
    expect(loop).toContain('onWifiStatus(')
    expect(loop).toContain('ensureLinkForProvision()')
    expect(loop).toContain("installStore.setBleConnected(false)")
  })
})

describe('T218 A-5 — 超时逐帧续期', () => {
  it('armProvisionTimeout 存在，statusListener 收帧续期，startWifiConfig 不再内联一次性起表', () => {
    expect(PAGE).toContain('function armProvisionTimeout()')
    expect(PAGE).toContain('else if (!provisioningDone) armProvisionTimeout()')
    const start = PAGE.indexOf('async function startWifiConfig(')
    const end = PAGE.indexOf('\nasync function ', start + 10)
    const body = PAGE.slice(start, end)
    expect(body).toContain('armProvisionTimeout()')
    // 旧实现：startWifiConfig 里一次性 setTimeout(60s)，收到帧不续期
    expect(body).not.toContain('setTimeout(')
  })
})

describe('T218 A-3 — pickReconnectTarget 真单测（零依赖模块）', () => {
  // 动态 import 顶层失败会挂掉整个文件；这里静态 import 放底部会出现 TDZ——
  // 因此用 vitest 的动态导入 + await。
  it('精确名优先 → BSYNC 兜底 → 无候选返回 null', async () => {
    const { pickReconnectTarget, RECONNECT_SCAN_MS, isBsyncDevice } = await import(
      '../src/utils/ble-link'
    )
    const found = [
      { name: 'OTHER-1', deviceId: 'a' },
      { name: 'BSYNC-701001', deviceId: 'b' },
      { name: 'BSYNC-701002', deviceId: 'c' },
    ]
    expect(pickReconnectTarget(found, 'BSYNC-701002')?.deviceId).toBe('c')
    expect(pickReconnectTarget(found, '')?.deviceId).toBe('b')
    expect(pickReconnectTarget([{ name: 'OTHER-1', deviceId: 'a' }], '')).toBeNull()
    expect(pickReconnectTarget([], 'BSYNC-701001')).toBeNull()
    expect(pickReconnectTarget(null as unknown as never[], '')).toBeNull()
    expect(isBsyncDevice('BSYNC-701001')).toBe(true)
    expect(isBsyncDevice('')).toBe(false)
    // 与患者端 RECONNECT_SCAN_MS=15000 对齐：设备断开后 ≥6s 不广播，6s 窗口必空手
    expect(RECONNECT_SCAN_MS).toBeGreaterThanOrEqual(15000)
  })
})

describe('T218 — 断言本身有效（防假绿）', () => {
  it('确实读到了 ble.ts / wifi-config 源码', () => {
    expect(path.basename(FILE)).toBe('ble.ts')
    expect(src).toContain('CHAR_REALTIME')
    expect(src.length).toBeGreaterThan(3000)
    expect(PAGE).toContain('startWifiConfig')
    expect(PAGE.length).toBeGreaterThan(3000)
  })
})
