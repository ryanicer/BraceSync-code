/**
 * T218-B(A20) — 配网途中掉线不得硬判失败
 *
 * 旧实现（2026-09-16 10:47 真机实证）：progress 期间断连回调立刻 failureType='timeout' +
 * view='failure'。而设备侧配网不依赖 BLE、可能仍在继续甚至已配好 ⇒ 用户看到"失败"，
 * 其实设备已经联网。
 * 新行为：断连 → 原地重连（与写失败重试共用同一把在途 promise，防并发 connectDevice 互踩）
 * → 续上则重订阅继续等；重连不回或续上后 60s 无推送才判失败，且落 linklost 文案
 * （"设备可能已配置成功"），与 timeout（"没等到设备响应"）区分。
 *
 * 按源码结构断言：页面挂不起来（utils/ble.ts #ifdef 链），先例见 b511-write-retry.spec.ts。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const PAGE = fileURLToPath(new URL('../../src/pages/wifi-setup/index.vue', import.meta.url))
const COPY = fileURLToPath(new URL('../../src/utils/wifi-copy.ts', import.meta.url))
const page = fs.readFileSync(PAGE, 'utf8')
const copy = fs.readFileSync(COPY, 'utf8')

function pageFnBody(name: string): string {
  // 不带 async 前缀匹配：页面里 armTimeout 等同步函数同样适用（"async function X" 也包含 "function X"）
  const start = page.indexOf(`function ${name}(`)
  expect(start, `页面里找不到 async function ${name}()——落点被挪走了？`).toBeGreaterThan(-1)
  const rest = page.slice(start + name.length + 1)
  const next = rest.search(/\nasync function |\nfunction /)
  return next === -1 ? rest : rest.slice(0, next)
}

describe('T218-B — 断连回调不再硬判失败', () => {
  it('断连分支里不再直接落失败页（view=failure / failureType=timeout 都不许出现）', () => {
    const atListener = page.indexOf('registerBleStateListener(')
    const atEnd = page.indexOf('deviceReady = await ensureCloudDevice()', atListener)
    const callback = page.slice(atListener, atEnd)
    expect(callback, '断连回调不得直接把页面打到失败页').not.toContain("view.value = 'failure'")
    expect(callback).not.toContain("failureType.value = 'timeout'")
    expect(callback).toContain('recoverAfterLinkLoss()')
  })

  it('恢复路径：重连失败才落 linklost；续上则重订阅 + 重起无推送表', () => {
    const body = pageFnBody('recoverAfterLinkLoss')
    const atOk = body.indexOf('if (!ok)')
    const atLinklost = body.indexOf("failureType.value = 'linklost'")
    const atResub = body.indexOf('onWifiStatus(')
    const atArm = body.indexOf('armTimeout()')
    expect(atOk).toBeGreaterThan(-1)
    expect(atLinklost).toBeGreaterThan(atOk)
    expect(atResub).toBeGreaterThan(-1)
    expect(atArm).toBeGreaterThan(atResub)
    // 判失败前先回到 progress 检查：用户已取消/已成功时不得改状态
    expect(body.indexOf("view.value !== 'progress' || successHandled")).toBeLessThan(atOk)
  })

  it('重连与写失败重试共用同一把在途 promise（防两个 connectDevice 打同一设备互踩）', () => {
    const body = pageFnBody('ensureLinkForProvision')
    expect(body).toContain('linkRecovery')
    expect(body).toContain('doEnsureLinkForProvision()')
    // runProvision 重试循环仍调 ensureLinkForProvision（自动吃到同一把锁）
    expect(pageFnBody('runProvision')).toContain('await ensureLinkForProvision()')
  })

  it('断链后的无推送超时落 linklost 文案；未断链的纯超时仍落 timeout', () => {
    const body = pageFnBody('armTimeout')
    expect(body).toMatch(/linkLostDuringProvision \? 'linklost' : 'timeout'/)
    expect(page).toContain('linkLostDuringProvision = false')
  })

  it('本轮配网开始时复位断链标记（上一轮的断链不得污染本轮文案）', () => {
    const body = pageFnBody('runProvision')
    const atReset = body.indexOf('linkLostDuringProvision = false')
    const atProgress = body.indexOf("view.value = 'progress'")
    expect(atReset).toBeGreaterThan(-1)
    expect(atReset).toBeGreaterThan(atProgress)
  })
})

describe('T218-B — linklost 失败态文案（区分"没等到响应"与"可能已配好"）', () => {
  it('wifi-copy 有 linklost 键，desc 表达"可能已配置成功"', () => {
    expect(copy).toContain("linklost: {")
    expect(copy).toContain('有可能已经配置成功')
    expect(copy).toMatch(/linklost: '连接中断，结果未确认'/)
  })

  it('linklost 不混入设备失败码映射（-1~-4 是设备权威答复，与断链无关）', () => {
    expect(copy).not.toMatch(/\[-1\]:\s*'linklost'/)
  })

  it('断言确实读到了源码（防路径写错导致空内容假绿）', () => {
    expect(path.basename(PAGE)).toBe('index.vue')
    expect(page).toContain('recoverAfterLinkLoss')
    expect(page.length).toBeGreaterThan(1000)
    expect(copy.length).toBeGreaterThan(1000)
  })
})
