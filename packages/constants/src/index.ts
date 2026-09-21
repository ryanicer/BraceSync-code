/**
 * BraceSync application constants.
 * 对齐：docs/ · PRD §7D.12
 */

/** Alert types */
export const ALERT_TYPES = {
  PRESSURE_HIGH: 'pressure_high',
  PRESSURE_FLUCTUATION: 'pressure_fluctuation',
  WEAR_INTERRUPT: 'wear_interrupt',
  SENSOR_DRIFT: 'sensor_drift',
} as const

export type AlertType = (typeof ALERT_TYPES)[keyof typeof ALERT_TYPES]

/** System configuration threshold defaults (PRD §7D.12 + T203 ÷10) */
export const DEFAULT_THRESHOLDS = {
  /** 压力偏高阈值 (N)，T203: 45 → 5 */
  PRESSURE_HIGH_N: 5,
  /** 压力波动幅度阈值 (%) */
  PRESSURE_FLUCTUATION_PCT: 30,
  /** 佩戴中断判定时间 (分钟，须 ≥2×采集间隔) */
  WEAR_INTERRUPT_MINUTES: 60,
  /** 传感器漂移告警阈值 (N)，T203: 2.8 → 0.3 */
  SENSOR_DRIFT_N: 0.3,
  /** 空载校准偏差上限 (N)，T203: 0.5 → 0.05 */
  CALIBRATION_OFFSET_N: 0.05,
} as const

/** Device signature time window (minutes) */
export const SIGNATURE_TIME_WINDOW_MINUTES = 5

/** Nonce dedup TTL (minutes) */
export const NONCE_DEDUP_MINUTES = 10

/** Sensor count per frame */
export const SENSOR_COUNT = 20

/** T173：设备上报单位 mN → N 换算因子（权威口径 mN，÷1000 归一） */
export const MN_PER_N = 1000

/**
 * 患者端热力图色阶（N）——T203 ÷10 后量纲。
 * 与 sys_configs heatmap/threshold 键同步演进；此常量为端上兜底默认。
 */
export const HEATMAP_TIERS = {
  LOW_MAX: 2,
  NORMAL_MAX: 4,
  ELEVATED_MAX: 6,
} as const

/** 患者端趋势曲线 Y 轴上界 (N)，T203: 75 → 8 */
export const TREND_CURVE_MAX_N = 8

/** Patient statuses */
export const PATIENT_STATUS = {
  ACTIVE: 'active',
  PENDING: 'pending',
  INACTIVE: 'inactive',
} as const

export type PatientStatus = (typeof PATIENT_STATUS)[keyof typeof PATIENT_STATUS]

/** Device statuses */
export const DEVICE_STATUS = {
  ONLINE: 'online',
  OFFLINE: 'offline',
  UNBOUND: 'unbound',
} as const

export type DeviceStatus = (typeof DEVICE_STATUS)[keyof typeof DEVICE_STATUS]
