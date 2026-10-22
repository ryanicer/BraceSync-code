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
  /** 压力波动幅度阈值 (%)；T257 2.6 引擎已停产生、T419 S-6 后台表单项下线，键与默认值保留（T430：PRD §7D.6 已拍 C＝历史行界面隐藏、数据不删，配置存储面不动） */
  PRESSURE_FLUCTUATION_PCT: 30,
  /** 设备离线判定时间 (分钟，须 ≥2×采集间隔)；键名 wear_interrupt 是码值，T419 G-6 只改显示口径 */
  WEAR_INTERRUPT_MINUTES: 60,
  /** 传感器标定异常告警阈值 (N)，T203: 2.8 → 0.3（T419 G-6 同上） */
  SENSOR_DRIFT_N: 0.3,
  // T675（Boss 2026-10-12）：CALIBRATION_OFFSET_N（0.05N 空载偏差门禁）已废除——
  // 校准目的是记录任何稳定偏置并扣除，偏移大不是失败；故障哨兵阈值（恒 0 / >10N）
  // 落在 apps/tech-miniapp/src/utils/calibration.ts，只提示不拦截。
} as const

/** Device signature time window (minutes) */
export const SIGNATURE_TIME_WINDOW_MINUTES = 5

/** Nonce dedup TTL (minutes) */
export const NONCE_DEDUP_MINUTES = 10

/** Sensor count per frame */
export const SENSOR_COUNT = 20

/**
 * T173 权威口径：设备上报单位 = mN。T733 方案A（Boss 拍板）的**全仓唯一分母**：
 *   采集层（BLE 解析）与存储层（DB / API 落库）**零换算**，raw mN 原样走；
 *   ÷ 本常数**只允许出现在展示层**（页面渲染前的一次换算），且必须走 `nFromMn`。
 * 🔴 T719 事故根因：删了后端入口的 ÷1000，却没在出口补换算 ⇒ 346 mN 被显示成「346 N」
 *   （5401 kPa 而非 ~5.4 kPa）。修单向不修双向、只删不加，都会把同一个量纲坑换个位置再犯一次。
 */
export const MN_PER_N = 1000

/**
 * T733 方案A：mN → N 的**唯一展示层换算出口**（存储/采集层禁调）。
 *
 * 入参是设备/库里存的 raw mN 值，返回给渲染用的 N 值；N → kPa 再接 shared-utils
 * `kpaFromN`（那一步是 N↔kPa 的档位换算，不是量纲换算，两者不混用）。
 * 非有限入参（NaN/Infinity）回 NaN：让调用方的数字文本出「--」，
 * 不许把脏值当 0 N 渲染出去（与 pressureUnit 的 fail-closed 同语义）。
 *
 * @example nFromMn(346) // → 0.346 N
 */
export function nFromMn(mn: number): number {
  return Number.isFinite(mn) ? mn / MN_PER_N : NaN
}

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
