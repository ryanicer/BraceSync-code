import type { Patient } from '@bracesync/shared-types'

/** 20 点实时压力帧（T733 方案A：单位 **mN**＝B513 raw int16 原值，采集层零换算；渲染时才 ÷1000） */
export type RealtimeFrame = number[]

/** WiFi 配网状态机（V2.1 §4.3） */
export type WifiStatusCode = 0 | 1 | 2 | 3 | 9 | -1 | -2 | -3 | -4

/**
 * 空载校准结果（T675 语义重构：范围门禁废除，只留点数完整性 + 故障哨兵提示）。
 * 哨兵类型定义见 utils/calibration.ts。
 */
export interface CalibrationResult {
  /** 20 点零点偏移值（**mN 原值**，与设备帧同空间；任何稳定偏置都如实记录，POST /baselines 原样上报不换算） */
  offsetValues: number[]
  /** 数据校验：只剩点数完整性（5 帧 × 20 点收齐） */
  checks: {
    pointCount: boolean
  }
  /** 故障哨兵（通道整轮恒 0 / 饱和超量程），只提示不拦截安装流程 */
  sentinels: Array<{
    /** 通道号（1-based，P01..P20） */
    channel: number
    /** dead=整轮恒 0；saturated=饱和/超量程 */
    reason: 'dead' | 'saturated'
  }>
}

/** 技师端安装阶段编号 */
export type InstallPhase = 1 | 2 | 3

/** 阶段状态 */
export type PhaseStatus = 'pending' | 'active' | 'done'

/** 患者档案（绑定 install 时存入 store 的子集，不含 Cobb 角） */
export interface PatientProfile {
  patientId: string
  name: string
  age: number | null
  diagnosis: string | null
}
