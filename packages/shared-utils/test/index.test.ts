import { describe, it, expect } from 'vitest'
import {
  formatPressure,
  formatWearDuration,
  pressureChangeRate,
  isPressureHigh,
  formatAlertValue,
  ALERT_TYPE_LABELS,
  HIDDEN_ALERT_TYPES,
  isHiddenAlertType,
  alertTypeLabel,
} from '../src/index'

describe('formatPressure', () => {
  it('formats pressure value with Newton unit', () => {
    expect(formatPressure(45.0)).toBe('45.0 N')
  })

  it('handles zero pressure', () => {
    expect(formatPressure(0)).toBe('0.0 N')
  })

  it('formats with custom decimal places', () => {
    expect(formatPressure(22.456, 2)).toBe('22.46 N')
  })
})

describe('formatWearDuration', () => {
  it('formats minutes only', () => {
    expect(formatWearDuration(30)).toBe('30 分钟')
  })

  it('formats hours only', () => {
    expect(formatWearDuration(120)).toBe('2 小时')
  })

  it('formats hours and minutes', () => {
    expect(formatWearDuration(90)).toBe('1 小时 30 分钟')
  })

  it('handles boundary: exactly 60 minutes', () => {
    expect(formatWearDuration(60)).toBe('1 小时')
  })

  it('handles zero', () => {
    expect(formatWearDuration(0)).toBe('0 分钟')
  })
})

describe('pressureChangeRate', () => {
  it('calculates positive change percentage', () => {
    const rate = pressureChangeRate(10, 13)
    expect(rate).toBeCloseTo(30, 1)
  })

  it('calculates negative change percentage', () => {
    const rate = pressureChangeRate(10, 7)
    expect(rate).toBeCloseTo(30, 1)
  })

  it('returns Infinity when previous is 0 and current > 0', () => {
    expect(pressureChangeRate(0, 10)).toBe(Infinity)
  })

  it('returns 0 when both are 0', () => {
    expect(pressureChangeRate(0, 0)).toBe(0)
  })

  it('returns 0 for identical values', () => {
    expect(pressureChangeRate(15, 15)).toBe(0)
  })
})

describe('isPressureHigh', () => {
  it('returns true when pressure exceeds threshold', () => {
    expect(isPressureHigh(46, 45)).toBe(true)
  })

  it('returns false when pressure equals threshold', () => {
    expect(isPressureHigh(45, 45)).toBe(false)
  })

  it('returns false when pressure below threshold', () => {
    expect(isPressureHigh(44.9, 45)).toBe(false)
  })
})

describe('formatAlertValue (T235)', () => {
  it('pressure_high → N', () => {
    expect(formatAlertValue('pressure_high', 68.5)).toBe('68.50N')
  })

  it('pressure_high threshold with prefix', () => {
    expect(formatAlertValue('pressure_high', 60, { prefix: '>' })).toBe('>60.00N')
  })

  it('pressure_fluctuation → %', () => {
    expect(formatAlertValue('pressure_fluctuation', 12.5)).toBe('12.5%')
    expect(formatAlertValue('pressure_fluctuation', 15, { prefix: '>' })).toBe('>15.0%')
  })

  it('sensor_drift negative value clamps to 0', () => {
    expect(formatAlertValue('sensor_drift', -3.2)).toBe('0.00N')
  })

  it('sensor_drift positive value shows N', () => {
    expect(formatAlertValue('sensor_drift', 10, { prefix: '>' })).toBe('>10.00N')
  })

  it('wear_interrupt → min', () => {
    expect(formatAlertValue('wear_interrupt', 45.6)).toBe('46min')
    expect(formatAlertValue('wear_interrupt', -5)).toBe('0min')
  })

  it('unknown type → bare value, no unit', () => {
    expect(formatAlertValue('foobar', 5)).toBe('5')
    expect(formatAlertValue('foobar', 5, { prefix: '>' })).toBe('>5')
  })

  it('no prefix by default', () => {
    expect(formatAlertValue('pressure_high', 40)).toBe('40.00N')
  })
})

describe('isHiddenAlertType (T430 展示侧隐藏)', () => {
  it('已裁砍除的历史类型在隐藏集合里', () => {
    expect(HIDDEN_ALERT_TYPES).toContain('pressure_fluctuation')
    expect(isHiddenAlertType('pressure_fluctuation')).toBe(true)
  })

  it('现行四类不被误伤（隐藏集合只收砍除项）', () => {
    for (const type of ['pressure_high', 'wear_interrupt', 'wear_duration_short', 'sensor_drift']) {
      expect(isHiddenAlertType(type)).toBe(false)
    }
  })

  it('空值与未知码值都不隐藏，交由展示层原样处理', () => {
    expect(isHiddenAlertType(undefined)).toBe(false)
    expect(isHiddenAlertType(null)).toBe(false)
    expect(isHiddenAlertType('')).toBe(false)
    expect(isHiddenAlertType('legacy_type')).toBe(false)
  })

  it('🔴 隐藏类型必须仍留在词表里：删键会让历史行露出裸码值', () => {
    for (const type of HIDDEN_ALERT_TYPES) {
      expect(type in ALERT_TYPE_LABELS).toBe(true)
      expect(alertTypeLabel(type)).not.toBe(type)
    }
  })
})
