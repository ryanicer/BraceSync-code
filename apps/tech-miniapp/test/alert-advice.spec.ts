/**
 * T443 裁定①丙（Boss 2026-09-28）— 告警详情「处置建议」按前端固定模板落地。
 * 依据：docs/tasks/winner/T446-字段级对读-结论与字段表.md §五（告警域后端零建议字段）＋
 * 稿面 docs/design/tech/alerts.html:84,85,87 的 action 四步（撰写示例、不是数据源）。
 *
 * 模板表在本包可直接测；页面接线按源码断言（同 test/patient-display.spec.ts 的写法）。
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Alert } from '@bracesync/shared-types'
import { ALERT_TYPE_LABELS, HIDDEN_ALERT_TYPES } from '@bracesync/shared-utils'
import { ALERT_ADVICE, alertAdviceLines } from '../src/utils/alertAdvice'
import { buildAlertDetailLines } from '../src/utils/alertDisplay'

function utilSrc(): string {
  return fs.readFileSync(
    fileURLToPath(new URL('../src/utils/alertAdvice.ts', import.meta.url)),
    'utf8',
  )
}

function alert(over: Partial<Alert> = {}): Alert {
  return {
    alertId: 'ALR-T443-R1-001',
    patientId: 'P20260001',
    patientName: '张明远',
    deviceId: 'PRS-ML05-RC-19700101001',
    type: 'pressure_high',
    detail: '压力偏高：胸椎右侧压力 4.2 N，超过阈值 3.5 N',
    sensorPoint: 'P12',
    thresholdValue: 3.5,
    actualValue: 4.2,
    timestamp: '2026-09-28T10:00:00+08:00',
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

describe('ALERT_ADVICE 键表（跟术语真源，不另起一套码值）', () => {
  it('键 ⊆ ALERT_TYPE_LABELS 键：不引入新码值', () => {
    const known = Object.keys(ALERT_TYPE_LABELS)
    for (const key of Object.keys(ALERT_ADVICE)) {
      expect(known, `${key} 不在术语真源里`).toContain(key)
    }
  })

  it('已裁展示侧隐藏的码值刻意不配文案 —— 配了等于把砍除的类型又露回界面', () => {
    for (const hidden of HIDDEN_ALERT_TYPES) {
      expect(ALERT_ADVICE[hidden], `${hidden} 被恢复了`).toBeUndefined()
      expect(alertAdviceLines(hidden)).toEqual([])
    }
  })

  it('四类都配四步，与稿面 action 块同构（alerts.html:84,85,87 各四条）', () => {
    for (const [key, steps] of Object.entries(ALERT_ADVICE)) {
      expect(steps.length, `${key} 步数与稿面不符`).toBe(4)
      for (const s of steps) expect(s.length).toBeGreaterThan(0)
    }
  })
})

describe('alertAdviceLines（详情弹窗整块行）', () => {
  it('标签行 + 逐条编号，编号从 1 开始连续', () => {
    const lines = alertAdviceLines('pressure_high')
    expect(lines[0]).toBe('处置建议:')
    expect(lines.length).toBe(5)
    expect(lines[1].startsWith('1. ')).toBe(true)
    expect(lines[4].startsWith('4. ')).toBe(true)
  })

  it('未知码值与缺字段都返回空数组，调用方整块不渲染', () => {
    expect(alertAdviceLines('no_such_type')).toEqual([])
    expect(alertAdviceLines('')).toEqual([])
    expect(alertAdviceLines(null)).toEqual([])
    expect(alertAdviceLines(undefined)).toEqual([])
  })

  it('模板里不写死样例点位／内部编号（稿面示例的 P12、P15 是举例，不是通用文案）', () => {
    const body = utilSrc()
    const rendered = Object.values(ALERT_ADVICE)
      .flat()
      .join('\n')
    expect(rendered).not.toMatch(/P1[0-9]/)
    expect(rendered).not.toMatch(/ALR-/)
    // 反证：字面量确实只存在于注释里，说明本断言扫的是可渲染文本而非整份文件
    expect(body).toMatch(/P12|P15/)
  })
})

describe('裁定①接线：建议块并进详情弹窗正文，页面不另写一套', () => {
  it('pressure_high 详情弹窗末尾出现「处置建议:」与四条步骤', () => {
    const lines = buildAlertDetailLines(alert())
    const at = lines.indexOf('处置建议:')
    expect(at).toBeGreaterThan(-1)
    // 建议块在正文末尾，不打断改前既有的字段顺序
    expect(at).toBe(lines.length - 5)
    expect(lines.slice(at + 1, at + 5).every((l) => /^\d\. /.test(l))).toBe(true)
  })

  it('无模板的码值（如已裁隐藏的 pressure_fluctuation）整块不落，不硬凑文案', () => {
    const lines = buildAlertDetailLines(alert({ type: 'pressure_fluctuation' }))
    expect(lines.some((l) => l.startsWith('处置建议'))).toBe(false)
    expect(lines.some((l) => /^\d\. /.test(l))).toBe(false)
  })

  it('建议文本只存在于 utils/alertAdvice.ts，页面与弹窗构造处不重复字面量', () => {
    const page = fs.readFileSync(
      fileURLToPath(new URL('../src/pages/alerts/index.vue', import.meta.url)),
      'utf8',
    )
    const display = fs.readFileSync(
      fileURLToPath(new URL('../src/utils/alertDisplay.ts', import.meta.url)),
      'utf8',
    )
    expect(display).toContain('alertAdviceLines(a.type)')
    for (const src of [page, display]) {
      for (const line of src.split(/\r?\n/)) {
        if (line.trim().startsWith('//') || line.trim().startsWith('*')) continue
        expect(line, '页面／弹窗层又写了一遍建议字面量').not.toMatch(/处置建议/)
        expect(line).not.toMatch(/检查支架佩戴紧固程度/)
      }
    }
  })
})
