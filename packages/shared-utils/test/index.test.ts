import { describe, it, expect } from 'vitest'
import {
  formatPressure,
  formatWearDuration,
  pressureChangeRate,
  isPressureHigh,
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
