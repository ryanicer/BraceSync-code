/**
 * T748 — 患者端安卓位置权限：状态显示与授权引导
 *
 * 缺陷（Boss 2026-10-26 安卓真机）：微信设置里位置信息未授权，
 * 页面却显示"已授权"；点授权只循环弹框不弹系统授权框，扫描 found:0。
 *
 * 为什么按源码结构断言：utils/ble.ts 含 #ifdef 条件编译段，页面/工具挂载成本高，
 * 先例见 t740-scan-permission.spec.ts / b512-resubscribe.spec.ts。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { classifyLocationAuth } from '../../src/utils/location-permission'

const BLE = fileURLToPath(new URL('../../src/utils/ble.ts', import.meta.url))
const MANIFEST = fileURLToPath(new URL('../../src/manifest.json', import.meta.url))
const ble = fs.readFileSync(BLE, 'utf8')
const manifest = JSON.parse(fs.readFileSync(MANIFEST, 'utf8'))

function fnBody(name: string): string {
  const re = new RegExp(`export (?:async )?function ${name}\\(`)
  const m = re.exec(ble)
  expect(m, `ble.ts 里找不到 export function ${name}()——落点被挪走了？`).not.toBeNull()
  const rest = ble.slice(m!.index)
  const next = rest.slice(1).search(/\nexport (?:async )?function |\nfunction /)
  return next === -1 ? rest : rest.slice(0, next + 1)
}

describe('T748 — 三态判定只认 authSetting 真值', () => {
  it('true / false / undefined / 缺失 / 异常值分别落到 granted / denied / unasked', () => {
    expect(classifyLocationAuth({ 'scope.userLocation': true })).toBe('granted')
    expect(classifyLocationAuth({ 'scope.userLocation': false })).toBe('denied')
    expect(classifyLocationAuth({ 'scope.userLocation': undefined })).toBe('unasked')
    expect(classifyLocationAuth({})).toBe('unasked')
    expect(classifyLocationAuth(undefined)).toBe('unasked')
    expect(classifyLocationAuth(null)).toBe('unasked')
    // 非布尔值一律不算已授权
    expect(classifyLocationAuth({ 'scope.userLocation': 'yes' } as unknown as Record<string, unknown>)).toBe('unasked')
  })
})

describe('T748 — isLocationAuthed 不再冒充已授权', () => {
  const body = fnBody('isLocationAuthed')

  it('仅在 authSetting === true 时返回 true', () => {
    expect(body).toContain("classifyLocationAuth(res.authSetting) === 'granted'")
    expect(body).not.toContain('!== false')
  })

  it('读失败与异常都按未授权返回，不再 resolve(true)', () => {
    expect(body).toMatch(/fail: \(err\) => \{[\s\S]*resolve\(false\)/)
    expect(body).toMatch(/catch \(e\) \{[\s\S]*resolve\(false\)/)
    expect(body).not.toMatch(/fail: \(\) => resolve\(true\)/)
  })
})

describe('T748 — ensureLocationPermission 三态分流', () => {
  const body = fnBody('ensureLocationPermission')

  it('已拒绝（denied）直接引导去设置，不再徒劳调 wx.authorize', () => {
    const atDenied = body.indexOf("state === 'denied'")
    expect(atDenied).toBeGreaterThan(-1)
    expect(body.slice(atDenied, atDenied + 80)).toContain('guideToSetting()')
  })

  it('未询问（unasked）先过隐私授权门再弹系统授权框', () => {
    expect(body).toContain('requestWithPrivacyGate()')
    const atPrivacy = body.indexOf('requirePrivacyAuthorize')
    const atAuthorizeCall = body.indexOf('wx.authorize({')
    expect(atPrivacy).toBeGreaterThan(-1)
    expect(atAuthorizeCall).toBeGreaterThan(-1)
    // 定义顺序即阅读顺序：隐私门在最后的 unasked 分支里包住 authorize
    expect(body.slice(atPrivacy)).toContain('success: () => requestAuthorize()')
  })

  it('去设置入口在 showModal 的用户点击回调内调 wx.openSetting，返回后按真值定论', () => {
    const atModal = body.indexOf('uni.showModal({')
    const atConfirm = body.indexOf('if (!r.confirm)', atModal)
    const atOpen = body.indexOf('wx.openSetting({', atConfirm)
    expect(atModal).toBeGreaterThan(-1)
    expect(atConfirm).toBeGreaterThan(atModal)
    expect(atOpen).toBeGreaterThan(atConfirm)
    const afterOpen = body.slice(atOpen)
    expect(afterOpen).toContain('readLocationAuthState')
    expect(afterOpen).toContain("finish(state === 'granted')")
  })

  it('读 authSetting 失败按未询问处理，不返回 true', () => {
    const helper = ble.slice(ble.indexOf('function readLocationAuthState'), ble.indexOf('export async function ensureLocationPermission'))
    expect(helper).toMatch(/fail: \(err\) => \{[\s\S]*cb\('unasked'\)/)
    expect(helper).toContain("cb('unasked')")
    expect(body).not.toMatch(/resolve\(true\);/)
  })
})

describe('T748 — manifest.json 补齐授权声明', () => {
  it('mp-weixin 声明 scope.userLocation 的 desc（否则 authorize 报 require permission desc）', () => {
    const desc = manifest?.['mp-weixin']?.permission?.['scope.userLocation']?.desc
    expect(typeof desc).toBe('string')
    expect(desc.length).toBeGreaterThan(0)
  })

  it('mp-weixin 声明 requiredPrivateInfos 含 getLocation', () => {
    expect(manifest?.['mp-weixin']?.requiredPrivateInfos).toContain('getLocation')
  })
})
