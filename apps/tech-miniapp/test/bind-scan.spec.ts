/**
 * T362 — 技师端绑定页：患者ID 示例口径 + 扫码去 mock 硬编码
 * T380 — 同页设备ID 示例口径（假串 PRS-ML05-RC-001 现网精确命中 0）
 * T507 — 同页扫码位从「扫设备码」改「扫患者码」：回填目标改患者 ID，设备码模式留码不露入口
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
import {
  DEVICE_ID_EXAMPLE, DEVICE_ID_PLACEHOLDER, DEVICE_ID_SHAPE,
  PATIENT_ID_EXAMPLE, PATIENT_ID_PLACEHOLDER, PATIENT_ID_SHAPE, SCAN_TOAST,
} from '../src/utils/bind-copy'

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

describe('设备ID 示例口径（T380 现网实测）', () => {
  it('示例串符合现网真实形态 PRS-ML05-RC- 加 11 位数字', () => {
    expect(DEVICE_ID_EXAMPLE).toMatch(DEVICE_ID_SHAPE)
    expect(DEVICE_ID_EXAMPLE).toHaveLength(23)
  })

  it('占位文案为「例: + 示例串」，且不是旧假串那一族', () => {
    expect(DEVICE_ID_PLACEHOLDER).toBe(`例: ${DEVICE_ID_EXAMPLE}`)
    // 假串是「短序号后缀」形态：现网 5 台设备里精确命中 0，照它手输必然绑不到设备
    expect(DEVICE_ID_EXAMPLE).not.toBe('PRS-ML05-RC-001')
    expect(DEVICE_ID_PLACEHOLDER.includes('PRS-ML05-RC-001')).toBe(false)
  })
})

describe('bind 页落点接线（源码契约，防回潮）', () => {
  it('患者ID 输入框用集中文案常量做占位，页内无 pat-001 残留', () => {
    expect(pageSrc).toMatch(/:placeholder="PATIENT_ID_PLACEHOLDER"/)
    expect(pageSrc).not.toMatch(/pat-001/)
  })

  it('设备ID 输入框用集中文案常量做占位，模板里无假串字面量', () => {
    expect(pageSrc).toMatch(/:placeholder="DEVICE_ID_PLACEHOLDER"/)
    // 只查模板段：script 段那句「T362: 去掉 T089 的 mock 硬编码」注释是 T362 留的历史说明，
    // 明写被删掉的假串原值，属正当引用（该引用登记在 device-id-family.spec.ts 的 LEGITIMATE 表里）；
    // 这里按文案锁引用而不写行号——行号会随 main 漂移
    const template = pageSrc.slice(0, pageSrc.indexOf('</template>'))
    expect(template).not.toMatch(/PRS-ML05-RC-00\d/)
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

/**
 * T507（承接 T478 稿面，docs PR 814 的 01-绑定页扫患者码改造图）
 *
 * 钉三件事：文案按稿面逐字、扫到的内容落到患者 ID、设备码模式留码但不进正式 UI。
 * 页面 SFC 挂不了单测（utils/ble.ts 的条件编译链 vitest 读不动，与本文件既有约定同因），
 * 所以按源码断言；「真机扫得出」那两格不在这里，见卡内交件说明的「未验」。
 */
describe('T507 扫码位改扫患者码（源码契约）', () => {
  const template = pageSrc.slice(0, pageSrc.indexOf('</template>'))
  const patientCardStart = template.indexOf('<view class="scan-card" @click="scanPatient">')
  const deviceCardStart = template.indexOf(
    '<view v-if="DEVICE_SCAN_ENTRY_ENABLED" class="scan-card" @click="scanDeviceCode">',
  )

  it('页副标题与扫码卡三句文案按稿面逐字，旧「设备码」句从可见区绝迹', () => {
    expect(pageSrc).toContain('扫患者码带出患者 ID，设备 ID 用蓝牙列表选或手输')
    expect(pageSrc).not.toContain('扫码或手动输入设备 ID 进行绑定')
    const patientCard = template.slice(patientCardStart, deviceCardStart)
    expect(patientCard).toContain('扫患者码')
    expect(patientCard).toContain('扫后台「患者详情」里的二维码，自动填入患者 ID')
    expect(patientCard).toContain('可直扫电脑屏幕，也可从相册选图识别')
    expect(patientCard).not.toContain('扫描设备背面二维码')
  })

  it('扫到的内容写进患者 ID 输入框（改造点：原先写的是设备 ID）', () => {
    expect(pageSrc).toMatch(/if \(target === 'patient'\) patientId\.value = outcome\.value/)
  })

  it('只填充：不做前端形态校验，也不自动提交（v2 §一.4 校验全走后端）', () => {
    // 稿面草图 01 的 P3 画的就是这一格；若 Boss 改判「前端拦一层」，本条要连同实现一起翻面
    expect(pageSrc).not.toMatch(/PATIENT_ID_SHAPE/)
    const okStart = pageSrc.indexOf("if (outcome.kind === 'ok') {")
    expect(okStart).toBeGreaterThan(-1)
    const okBranch = pageSrc.slice(okStart).split('\n  }')[0]
    expect(okBranch).not.toMatch(/bind|navigate|redirectTo/i)
  })

  it('设备码模式保留在码里、入口默认关（v2 §一.3 裁定 3）', () => {
    expect(patientCardStart).toBeGreaterThan(-1)
    expect(deviceCardStart).toBeGreaterThan(patientCardStart)
    // 开关必须是常量假：置真等于把设备码入口放回正式 UI，届时稿面要重出
    expect(pageSrc).toMatch(/const DEVICE_SCAN_ENTRY_ENABLED = false/)
    const deviceCard = template.slice(deviceCardStart)
    expect(deviceCard).toContain('扫描设备背面二维码快速绑定')
  })

  it('扫码失败提示的宾语跟着改成患者 ID，无内容句不带宾语未动', () => {
    expect(SCAN_TOAST.failed).toBe('扫码失败，请手动输入患者 ID')
    expect(SCAN_TOAST.failed).not.toContain('设备 ID')
    expect(SCAN_TOAST.empty).toBe('未识别到二维码内容，请手动输入')
  })
})
