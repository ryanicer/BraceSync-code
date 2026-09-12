// T089 技师端小程序局部类型扩展
// reachabilityStatus 等字段属 T084 后端契约范围；T089 前端先行在本地扩展，等 shared-types 发布后替换。

import type { InstallRecord, Patient } from '@bracesync/shared-types'

declare module '@bracesync/shared-types' {
  interface InstallRecord {
    /** 可达性验证状态（T084 后端未实现，前端先行） */
    reachabilityStatus?: 'verified' | 'skipped' | 'pending'
  }
}

/** 20 点实时压力帧（单位 N，由 BLE Realtime Notify 解析） */
export type RealtimeFrame = number[]

/** WiFi 配网状态机（V2.1 §4.3） */
export type WifiStatusCode = 0 | 1 | 2 | 3 | 9 | -1 | -2 | -3 | -4

/** 空载校准结果 */
export interface CalibrationResult {
  /** 20 点零点偏移值（N） */
  offsetValues: number[]
  /** 数据校验：点数/范围/稳定性 */
  checks: {
    pointCount: boolean
    range: boolean
    stability: boolean
  }
}

/** 技师端安装阶段编号 */
export type InstallPhase = 1 | 2 | 3

/** 阶段状态 */
export type PhaseStatus = 'pending' | 'active' | 'done'

/** 可达性验证状态 */
export type ReachabilityStatus = 'verified' | 'skipped' | 'pending'

/** 患者档案（绑定 install 时存入 store 的子集，不含 Cobb 角） */
export interface PatientProfile {
  patientId: string
  name: string
  age: number | null
  diagnosis: string | null
}
