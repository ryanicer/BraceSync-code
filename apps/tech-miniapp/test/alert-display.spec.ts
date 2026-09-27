// T433 缺陷二（实际值为 0 时该行消失）+ 缺陷三（阈值口径）在技师端的落点判据。
// 页面模板在本包测不到（vitest.config.ts 是 environment: 'node'，无 VTU 挂载），
// 故 v-if 用的判据与弹窗行构造都下沉到 utils/alertDisplay.ts，这里测纯层。
import { describe, it, expect } from 'vitest'
import type { Alert } from '@bracesync/shared-types'
import { hasAlertNumber, alertValueText, buildAlertDetailLines } from '../src/utils/alertDisplay'

function alert(over: Partial<Alert> = {}): Alert {
  return {
    alertId: 'ALR-T433-001',
    patientId: 'P20260001',
    deviceId: 'PRS-ML05-RC-19700101001',
    type: 'wear_duration_short',
    detail: '佩戴时长不足：累计佩戴 0.0 小时，低于目标 9.0 小时',
    sensorPoint: '',
    thresholdValue: 540,
    actualValue: 0,
    timestamp: '2026-09-27T10:00:00+08:00',
    readStatus: 'unread',
    processStatus: 'pending',
    resolvedStatus: 'active',
    resolvedAt: null,
    processedBy: null,
    processedAt: null,
    processNote: null,
    ...over,
  } as Alert
}

describe('hasAlertNumber（T433 缺陷二判据）', () => {
  it('0 是合法读数 —— 改前真值判断把这一格整行吞掉', () => {
    expect(hasAlertNumber(0)).toBe(true)
    expect(hasAlertNumber(-30)).toBe(true)
  })

  it('只有 null / undefined / NaN 才算无值', () => {
    expect(hasAlertNumber(null)).toBe(false)
    expect(hasAlertNumber(undefined)).toBe(false)
    expect(hasAlertNumber(Number.NaN)).toBe(false)
    expect(hasAlertNumber('0')).toBe(false)
  })
})

describe('alertValueText', () => {
  it('0 也出文案，且按小时口径（分钟 / 60）', () => {
    expect(alertValueText('wear_duration_short', 0)).toBe('0h')
    expect(alertValueText('wear_duration_short', 540)).toBe('9h')
    expect(alertValueText('wear_duration_short', 1080)).toBe('18h')
  })

  it('无值时返回空串，由调用方决定不渲染', () => {
    expect(alertValueText('wear_duration_short', null)).toBe('')
    expect(alertValueText('pressure_high', undefined)).toBe('')
  })
})

describe('buildAlertDetailLines（详情弹窗正文）', () => {
  it('当日佩戴 0 分钟：实际值行必须在，且写 0h', () => {
    const lines = buildAlertDetailLines(alert())
    expect(lines).toContain('实际值: 0h')
    expect(lines).toContain('阈值: 9h')
  })

  it('阈值与实际值同一小时口径 —— 改前弹窗「阈值: 540h」对「低于目标 9.0 小时」（T433 缺陷三）', () => {
    const lines = buildAlertDetailLines(alert({ thresholdValue: 1080, actualValue: 390 }))
    expect(lines).toContain('阈值: 18h')
    expect(lines).toContain('实际值: 6.5h')
    expect(lines.join('\n')).not.toMatch(/540h|1080h/)
  })

  it('数值位缺失（null）才不落该行', () => {
    const lines = buildAlertDetailLines(
      alert({ type: 'pressure_high', thresholdValue: null as unknown as number, actualValue: 3.2 }),
    )
    expect(lines.some((l) => l.startsWith('阈值'))).toBe(false)
    expect(lines).toContain('实际值: 3.20N')
  })

  it('传感器位仍按空串判（字符串语义不变）', () => {
    expect(buildAlertDetailLines(alert({ sensorPoint: '' })).some((l) => l.startsWith('传感器'))).toBe(false)
    expect(buildAlertDetailLines(alert({ sensorPoint: 'P10' }))).toContain('传感器: P10')
  })

  it('三态显示照搬改前口径：processing 仍落「已处理」（该缺口已随 T433 登记，本卡不改行为）', () => {
    expect(buildAlertDetailLines(alert({ processStatus: 'processing' }))).toContain('状态: 已处理')
    expect(buildAlertDetailLines(alert({ processStatus: 'pending' }))).toContain('状态: 待处理')
  })
})
