import { describe, it, expect } from 'vitest'
import {
  ALERT_TYPES,
  DEFAULT_THRESHOLDS,
  SIGNATURE_TIME_WINDOW_MINUTES,
  NONCE_DEDUP_MINUTES,
  SENSOR_COUNT,
  PATIENT_STATUS,
  DEVICE_STATUS,
} from '../src/index'

describe('ALERT_TYPES', () => {
  it('defines four alert types', () => {
    expect(ALERT_TYPES.PRESSURE_HIGH).toBe('pressure_high')
    expect(ALERT_TYPES.PRESSURE_FLUCTUATION).toBe('pressure_fluctuation')
    expect(ALERT_TYPES.WEAR_INTERRUPT).toBe('wear_interrupt')
    expect(ALERT_TYPES.SENSOR_DRIFT).toBe('sensor_drift')
  })

  it('all alert types are unique', () => {
    const values = Object.values(ALERT_TYPES)
    expect(new Set(values).size).toBe(values.length)
  })
})

describe('DEFAULT_THRESHOLDS', () => {
  it('pressure high threshold is 45N', () => {
    expect(DEFAULT_THRESHOLDS.PRESSURE_HIGH_N).toBe(45)
  })

  it('pressure fluctuation threshold is 30%', () => {
    expect(DEFAULT_THRESHOLDS.PRESSURE_FLUCTUATION_PCT).toBe(30)
  })

  it('wear interrupt threshold is ≥ 2× collection interval', () => {
    // 采集间隔默认 30min → 中断阈值 ≥ 60min
    expect(DEFAULT_THRESHOLDS.WEAR_INTERRUPT_MINUTES).toBeGreaterThanOrEqual(60)
  })

  it('sensor drift threshold is 2.8N', () => {
    expect(DEFAULT_THRESHOLDS.SENSOR_DRIFT_N).toBe(2.8)
  })

  it('calibration offset threshold is 0.5N', () => {
    expect(DEFAULT_THRESHOLDS.CALIBRATION_OFFSET_N).toBe(0.5)
  })
})

describe('Signature and nonce constants', () => {
  it('signature time window is 5 minutes', () => {
    expect(SIGNATURE_TIME_WINDOW_MINUTES).toBe(5)
  })

  it('nonce dedup TTL is 10 minutes', () => {
    expect(NONCE_DEDUP_MINUTES).toBe(10)
  })

  it('sensor count per frame is 20', () => {
    expect(SENSOR_COUNT).toBe(20)
  })
})

describe('PATIENT_STATUS', () => {
  it('defines three statuses', () => {
    expect(PATIENT_STATUS.ACTIVE).toBe('active')
    expect(PATIENT_STATUS.PENDING).toBe('pending')
    expect(PATIENT_STATUS.INACTIVE).toBe('inactive')
  })
})

describe('DEVICE_STATUS', () => {
  it('defines three statuses', () => {
    expect(DEVICE_STATUS.ONLINE).toBe('online')
    expect(DEVICE_STATUS.OFFLINE).toBe('offline')
    expect(DEVICE_STATUS.UNBOUND).toBe('unbound')
  })
})
