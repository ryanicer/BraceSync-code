// T433 缺陷二（实际值为 0 时该行消失）+ 缺陷三（阈值口径）在技师端的落点判据。
// 页面模板在本包测不到（vitest.config.ts 是 environment: 'node'，无 VTU 挂载），
// 故 v-if 用的判据与弹窗行构造都下沉到 utils/alertDisplay.ts，这里测纯层。
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import type { Alert } from '@bracesync/shared-types'
import {
  hasAlertNumber,
  alertValueText,
  buildAlertDetailLines,
  filterVisibleAlertRows,
} from '../src/utils/alertDisplay'

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

// T451：与 admin T430 同源的「压力波动」界面隐藏（PRD §7D.6 历史数据处置拍 C）。
describe('filterVisibleAlertRows（T451 隐藏过滤，判据真源在 shared-utils）', () => {
  it('压力波动行被滤掉，其余四类原样保留', () => {
    const rows = [
      alert({ alertId: 'A1', type: 'pressure_high' }),
      alert({ alertId: 'A2', type: 'pressure_fluctuation' }),
      alert({ alertId: 'A3', type: 'wear_interrupt' }),
      alert({ alertId: 'A4', type: 'wear_duration_short' }),
      alert({ alertId: 'A5', type: 'sensor_drift' }),
    ]
    expect(filterVisibleAlertRows(rows).map((r) => r.alertId)).toEqual(['A1', 'A3', 'A4', 'A5'])
  })

  it('入参数组不被改写（返回新数组，页面 alerts.value 仍是全量）', () => {
    const rows = [alert({ alertId: 'A1', type: 'pressure_fluctuation' })]
    expect(filterVisibleAlertRows(rows)).not.toBe(rows)
    expect(rows).toHaveLength(1)
  })

  it('数据不删：隐藏行只是不呈现，请求与后端返回值不受本函数影响（全隐藏时得空数组）', () => {
    const all = [alert({ alertId: 'A1', type: 'pressure_fluctuation' })]
    expect(filterVisibleAlertRows(all)).toEqual([])
    expect(filterVisibleAlertRows([])).toEqual([])
  })

  it('未知码值不隐藏（裸码值仍由 alertTypeLabel 兜底显示，不得被本过滤器吞掉）', () => {
    const rows = [alert({ alertId: 'A1', type: 'some_future_type' as Alert['type'] })]
    expect(filterVisibleAlertRows(rows)).toHaveLength(1)
  })
})

// 本包无 VTU 挂载 ⇒ 页面的接线只能在源码层钉；反证 M4/M5 的落点。
describe('T451 页面接线：告警页三处派生量都改读过滤后的行', () => {
  const pageSrc = () =>
    fs.readFileSync(fileURLToPath(new URL('../src/pages/alerts/index.vue', import.meta.url)), 'utf8')

  it('可见列表、筛选分档与页头「已加载」都取自 visibleAlerts', () => {
    const src = pageSrc()
    expect(src).toContain('const visibleAlerts = computed(() => filterVisibleAlertRows(alerts.value))')
    expect(src).toContain('if (filter.value === \'all\') return visibleAlerts.value')
    expect(src).toContain('return visibleAlerts.value.filter(a => a.processStatus === filter.value)')
    expect(src).toContain('已加载 ${visibleAlerts.value.length} 条')
  })

  it('页头总数按已加载的隐藏行数扣减（后端 total 含隐藏行，本卡不改契约）', () => {
    const src = pageSrc()
    expect(src).toContain('const visibleTotal = total.value - hiddenLoadedCount.value')
  })

  it('页面不写隐藏类型字面量（派单范围三：判据集中在 utility 层）', () => {
    const src = pageSrc()
    expect(src).not.toContain('pressure_fluctuation')
    expect(src).not.toContain('HIDDEN_ALERT_TYPES')
  })
})
