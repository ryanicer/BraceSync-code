/**
 * T362 — 技师端绑定页：患者ID 示例口径 + 扫码去 mock 硬编码
 *
 * 覆盖两件事：
 *  1. `readQrCode` 四条链路（成功 / 取消 / 无内容 / 失败）的判定，含"失败绝不产出可写入值"；
 *  2. 落点接线按源码断言（页面 SFC 挂不了单测：utils/ble.ts 的条件编译链 vitest 读不动，
 *     与 test/provision-seq.spec.ts 同一约定）——防的是"纯层写对了、页面又改回硬编码"。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { readQrCode, type ScanImpl } from '../src/utils/scan'
import { PATIENT_ID_EXAMPLE, PATIENT_ID_PLACEHOLDER, PATIENT_ID_SHAPE, SCAN_TOAST } from '../src/utils/bind-copy'

const PAGE = fileURLToPath(new URL('../src/pages/bind/index.vue', import.meta.url))
const pageSrc = fs.readFileSync(PAGE, 'utf8')

/** 记录调用参数的假 scanCode，用于断言「页面确实按二维码扫」 */
function spyScan(impl: ScanImpl) {
  const calls: Array<{ scanType: string[] }> = []
  const scan: ScanImpl = async (opts) => {
    calls.push(opts)
    return impl(opts)
  }
  return { scan, calls }
}

describe('readQrCode — 扫码结果归一', () => {
  it('成功：按 qrCode 扫，返回原值（仅首尾空白被剥掉）', async () => {
    const { scan, calls } = spyScan(async () => ({ result: '  PRS-ML05-RC-20260701001\n' }))
    const outcome = await readQrCode(scan)
    expect(calls).toEqual([{ scanType: ['qrCode'] }])
    expect(outcome).toEqual({ kind: 'ok', value: 'PRS-ML05-RC-20260701001' })
  })

  it('取消：errMsg 含 cancel 判为 cancelled，不判为失败也不产出值', async () => {
    const { scan } = spyScan(async () => {
      throw { errMsg: 'scanCode:fail cancel' }
    })
    const outcome = await readQrCode(scan)
    expect(outcome).toEqual({ kind: 'cancelled' })
    expect('value' in outcome).toBe(false)
  })

  it('无内容：result 为空/全空白都判 empty，不产出可写入值', async () => {
    for (const raw of ['', '   ', undefined]) {
      const { scan } = spyScan(async () => ({ result: raw }))
      expect(await readQrCode(scan)).toEqual({ kind: 'empty' })
    }
  })

  it('失败：H5 的不支持（reject 不带 errMsg 文本含 cancel）判 failed 并保留 errMsg', async () => {
    const { scan } = spyScan(async () => {
      throw { errMsg: 'scanCode:fail 暂不支持' }
    })
    expect(await readQrCode(scan)).toEqual({ kind: 'failed', message: 'scanCode:fail 暂不支持' })
  })

  it('失败：抛 Error 也能取到 message，不抛穿调用方', async () => {
    const { scan } = spyScan(async () => {
      throw new Error('camera denied')
    })
    expect(await readQrCode(scan)).toEqual({ kind: 'failed', message: 'camera denied' })
  })
})

describe('患者ID 示例口径（T362 现网实测）', () => {
  it('示例串符合后台真实生成形态 P + 年份 + 12 位 hex', () => {
    expect(PATIENT_ID_EXAMPLE).toMatch(PATIENT_ID_SHAPE)
    expect(PATIENT_ID_EXAMPLE).toHaveLength(17)
  })

  it('占位文案为「例: + 示例串」，且不再是旧口径 pat-', () => {
    expect(PATIENT_ID_PLACEHOLDER).toBe(`例: ${PATIENT_ID_EXAMPLE}`)
    expect(PATIENT_ID_EXAMPLE.startsWith('pat-')).toBe(false)
    expect(PATIENT_ID_PLACEHOLDER.includes('pat-001')).toBe(false)
  })
})

describe('bind 页落点接线（源码契约，防回潮）', () => {
  it('患者ID 输入框用集中文案常量做占位，页内无 pat-001 残留', () => {
    expect(pageSrc).toMatch(/:placeholder="PATIENT_ID_PLACEHOLDER"/)
    expect(pageSrc).not.toMatch(/pat-001/)
  })

  it('扫码走 readQrCode + uni.scanCode，不再直接给输入框赋字面量', () => {
    expect(pageSrc).toMatch(/readQrCode\(\(opts\) => uni\.scanCode\(opts\)\)/)
    expect(pageSrc).not.toMatch(/manualDeviceId\.value\s*=\s*['"`]/)
  })

  it('四条链路各有对应提示，失败与取消不复用「扫码成功」', () => {
    expect(pageSrc).toMatch(/SCAN_TOAST\.success/)
    expect(pageSrc).toMatch(/SCAN_TOAST\[outcome\.kind\]/)
    // 「扫码成功（mock）」这类自陈假数据的文案必须绝迹
    expect(pageSrc).not.toMatch(/扫码成功（mock）/)
    expect(SCAN_TOAST.success).not.toBe(SCAN_TOAST.failed)
    expect(SCAN_TOAST.success).not.toBe(SCAN_TOAST.cancelled)
  })
})
