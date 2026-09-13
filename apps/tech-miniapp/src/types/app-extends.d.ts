import type { Patient } from '@bracesync/shared-types'

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

/** 患者档案（绑定 install 时存入 store 的子集，不含 Cobb 角） */
export interface PatientProfile {
  patientId: string
  name: string
  age: number | null
  diagnosis: string | null
}
