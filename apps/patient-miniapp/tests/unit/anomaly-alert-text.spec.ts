/**
 * T298 — 患者端「异常监测」告警列表文本化（Boss 2026-09-21 23:22 裁定 ②：实现采纳设计稿的文本化展示）
 *
 * 钉死三件事：
 * 1. 4 类（+ 历史压力波动）一律显示中文类型名，码值不得出现在患者可见文本里；
 * 2. 详情行用后端 Alert.detail 原文，前端不再自己拼数；
 * 3. 数值单位口径未定的类型（佩戴时长不足，分钟 vs 小时）不得显示裸数字 —— 禁硬编假数。
 */
import { describe, it, expect } from 'vitest'
import type { Alert, AlertType } from '@bracesync/shared-types'
import { toPressureAnomalyItem, alertsToPressureMap } from '../../src/utils/anomaly'

const mk = (patch: Partial<Alert>): Alert => ({
  alertId: 'ALR-U1',
  patientId: 'PT-001',
  deviceId: 'DEV-A3F312',
  type: 'pressure_high',
  detail: '',
  sensorPoint: 'P10',
  thresholdValue: 60,
  actualValue: 63.8,
  timestamp: '2026-07-12T22:10:00+08:00',
  readStatus: 'unread',
  processStatus: 'pending',
  resolvedStatus: 'active',
  resolvedAt: null,
  processedBy: null,
  processedAt: null,
  processNote: null,
  ...patch,
})

// 患者端本页可渲染的类型（设备离线不在本页，见最后一条用例）
const RENDERABLE: Array<[AlertType, string]> = [
  ['pressure_high', '压力偏高'],
  ['wear_duration_short', '佩戴时长不足'],
  ['sensor_drift', '传感器标定异常'],
  ['pressure_fluctuation', '压力波动'],
]

describe('T298 — 4 类文本化展示', () => {
  it.each(RENDERABLE)('%s → 中文类型名「%s」', (type, label) => {
    const item = toPressureAnomalyItem(mk({ type, detail: '后端自述句原文' }))
    expect(item.type).toBe(label)
  })

  it('任何类型的可见文本都不含码值字面量', () => {
    for (const [type] of RENDERABLE) {
      const item = toPressureAnomalyItem(mk({ type, detail: '采集点 P10 压力超阈值' }))
      const visible = JSON.stringify(item)
      for (const [code] of RENDERABLE) expect(visible).not.toContain(code)
      expect(visible).not.toContain('wear_interrupt')
    }
  })

  it('详情用 Alert.detail 原文，不改写不裁剪', () => {
    const detail = '压力偏高：采集点 P10 压力 63.8N 超阈值 60.0N'
    expect(toPressureAnomalyItem(mk({ detail })).detail).toBe(detail)
  })

  it('detail 为空时兜底为类型名，不前端拼裸值', () => {
    const item = toPressureAnomalyItem(mk({ type: 'sensor_drift', detail: '   ' }))
    expect(item.detail).toBe('传感器标定异常')
  })

  it('压力偏高：阈值带上限符号、实际值进 meta（均由字段推出）', () => {
    const item = toPressureAnomalyItem(mk({ type: 'pressure_high', thresholdValue: 60, actualValue: 63.8 }))
    expect(item.threshold).toBe('>60.00N')
    expect(item.meta).toBe('实际值 63.80N · 关注中')
  })

  it('已恢复/有处理备注时 meta 状态部分随字段变化', () => {
    expect(
      toPressureAnomalyItem(mk({ actualValue: 45, resolvedStatus: 'resolved' })).meta,
    ).toBe('实际值 45.00N · 已恢复')
    expect(
      toPressureAnomalyItem(mk({ actualValue: 45, processNote: '已联系技师复查' })).meta,
    ).toBe('实际值 45.00N · 已联系技师复查')
  })

  it('佩戴时长不足：分钟/小时口径未定 ⇒ 阈值与实际值数字一律不显示', () => {
    // 真实写入：thresholdValue = 目标 18h × 60 = 1080（分钟）、actualValue = 186（分钟）
    const item = toPressureAnomalyItem(
      mk({ type: 'wear_duration_short', sensorPoint: '', thresholdValue: 1080, actualValue: 186 }),
    )
    expect(item.threshold).toBe('')
    expect(item.meta).toBe('关注中')
    expect(JSON.stringify(item)).not.toMatch(/1080|186h/)
    expect(item.detail).toBeTruthy() // 小时数由后端 detail 自述句承载
  })

  it('按日类告警无采集点 ⇒ point 空串，不再出现 P?? 占位', () => {
    const item = toPressureAnomalyItem(mk({ type: 'wear_duration_short', sensorPoint: '' }))
    expect(item.point).toBe('')
    expect(JSON.stringify(item)).not.toContain('P??')
  })

  it('设备离线不进本页列表（PRD §7D.6：归技师端现场知晓）', () => {
    expect(alertsToPressureMap([mk({ type: 'wear_interrupt', sensorPoint: '' })]).size).toBe(0)
  })

  it('按 timestamp 日期分组，同一天多类型并存', () => {
    const map = alertsToPressureMap([
      mk({ type: 'pressure_high', timestamp: '2026-07-05T10:00:00+08:00' }),
      mk({ type: 'sensor_drift', timestamp: '2026-07-05T20:00:00+08:00' }),
      mk({ type: 'wear_duration_short', timestamp: '2026-07-06T23:59:59+08:00' }),
    ])
    expect([...map.keys()]).toEqual(['2026-07-05', '2026-07-06'])
    expect(map.get('2026-07-05')!.map((it) => it.type)).toEqual(['压力偏高', '传感器标定异常'])
  })
})
