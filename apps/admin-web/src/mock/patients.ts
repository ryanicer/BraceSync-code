// 患者域 mock 数据（对齐 api-contracts.ts getPatients/getPatientDetail/getPatientRealtime）
import type { Patient, PressureRecord, SensorPoint, Alert } from '@bracesync/shared-types'
import { listDates, type DailyWearDay } from '../utils/workbenchData'

function makePoints(maxValue: number): SensorPoint[] {
  const points: SensorPoint[] = []
  for (let i = 1; i <= 20; i++) {
    const pressureValue = Math.round((10 + ((i * 7) % 40) + maxValue / 10) * 10) / 10
    points.push({
      pointId: `P${String(i).padStart(2, '0')}`,
      row: Math.ceil(i / 5),
      col: ((i - 1) % 5) + 1,
      label: `R${Math.ceil(i / 5)}C${((i - 1) % 5) + 1}`,
      pressureValue,
      status: pressureValue > 50 ? 'warning' : 'normal',
      // T643 A 路：kPa 档由后端同源派生，mock 不自己换算（禁第二套口径），故恒 null
      pressureKpa: null,
    })
  }
  return points
}

// T056：热力图 20 点专用结构（对齐后端 model.HeatmapPoint）
export interface PressureHeatmapPoint {
  pointId: string
  row: number
  col: number
  label: string
  pressureValue: number
  isMax: boolean
  /** T508/T513：同一响应内派生的 kPa 展示值；null = 面积未配置/非法，前端显示 -- */
  pressureKpa?: number | null
}

// T513：mock 在这里充当「服务端替身」，造出后端算好的派生字段。
// 对齐 services/data-service/internal/model/model.go KpaFromN（①归零 → ②N/area×10 →
// ③四舍五入，真非零最小 1，0 N 仍 0，面积非法返回 null）。
// 🔴 这段不是显示层逻辑：页面只显示本函数的返回值，前端不重复换算（派发单 §三 选型 A）。
export const MOCK_AREA_CM2 = 0.64
/** 面积未配置态的载体患者（e2e 用它验 fail-closed 的「--」与提示行） */
export const AREA_UNSET_PATIENT_ID = 'PT-003'

function mockKpaOf(n: number, areaCm2: number | null): number | null {
  if (areaCm2 === null || !(areaCm2 > 0)) return null
  const zeroed = Math.max(0, n)
  const k = (zeroed / areaCm2) * 10
  if (k === 0) return 0
  return Math.max(1, Math.round(k))
}

function makeHeatmap(points: SensorPoint[], areaCm2: number | null = MOCK_AREA_CM2): PressureHeatmapPoint[] {
  let maxIdx = 0
  let maxV = -Infinity
  points.forEach((p, i) => {
    if (p.pressureValue > maxV) {
      maxIdx = i
      maxV = p.pressureValue
    }
  })
  return points.map((p, i) => ({
    pointId: p.pointId,
    row: p.row,
    col: p.col,
    label: p.label,
    pressureValue: p.pressureValue,
    isMax: i === maxIdx && maxV > 0,
    pressureKpa: mockKpaOf(p.pressureValue, areaCm2),
  }))
}

function seedHeatmap(patientId: string): PressureHeatmapPoint[] {
  let seed = 0
  for (let i = 0; i < patientId.length; i++) seed = (seed * 31 + patientId.charCodeAt(i)) >>> 0
  if (seed === 0) seed = 0x9e3779b1
  const maxIdx = seed % 20
  const pts: SensorPoint[] = []
  for (let i = 0; i < 20; i++) {
    const r = Math.floor(i / 5)
    const c = i % 5
    const base = 12 + r * 6
    const wave = (((seed >>> ((c + 1) * 3)) & 7) / 7) * 8
    const loc = i === maxIdx ? 18 : 0
    let v = Math.round((base + wave + loc) * 10) / 10
    if (v > 60) v = 58
    pts.push({
      pointId: `P${String(i + 1).padStart(2, '0')}`,
      row: r + 1,
      col: c + 1,
      label: `R${r + 1}C${c + 1}`,
      pressureValue: v,
      status: v >= 45 ? 'critical' : v >= 33.75 ? 'warning' : 'normal',
      // T643 A 路：本数组只喂 makeHeatmap（它另按 HeatmapPoint 派生），SensorPoint 侧 mock 不换算
      pressureKpa: null,
    })
  }
  return makeHeatmap(pts)
}

const PATIENTS: Patient[] = [
  { patientId: 'PT-001', name: '林小雨', gender: 'female', age: 13, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 28, deviceId: 'DEV-A3F312', teamId: 'TEAM-001', doctorId: 'DOC-001', status: 'active', createdAt: '2026-03-12T09:00:00+08:00', updatedAt: '2026-08-10T18:00:00+08:00' },
  { patientId: 'PT-002', name: '陈子航', gender: 'male', age: 15, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 35, deviceId: 'DEV-B7E456', teamId: 'TEAM-001', doctorId: 'DOC-001', status: 'active', createdAt: '2026-04-02T10:30:00+08:00', updatedAt: '2026-08-11T08:00:00+08:00' },
  { patientId: 'PT-003', name: '王梓萌', gender: 'female', age: 12, diagnosis: '先天性脊柱侧弯', cobbAngle: 22, deviceId: 'DEV-C9D789', teamId: 'TEAM-002', doctorId: 'DOC-002', status: 'active', createdAt: '2026-05-18T14:00:00+08:00', updatedAt: '2026-08-09T16:20:00+08:00' },
  { patientId: 'PT-004', name: '刘俊熙', gender: 'male', age: 14, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 31, deviceId: 'DEV-D2A012', teamId: 'TEAM-002', doctorId: 'DOC-002', status: 'active', createdAt: '2026-06-01T09:30:00+08:00', updatedAt: '2026-08-11T07:40:00+08:00' },
  { patientId: 'PT-005', name: '赵欣然', gender: 'female', age: 16, diagnosis: '青少年特发性脊柱侧弯', cobbAngle: 40, deviceId: null, teamId: 'TEAM-003', doctorId: 'DOC-003', status: 'pending', createdAt: '2026-08-08T11:00:00+08:00', updatedAt: '2026-08-08T11:00:00+08:00' },
  { patientId: 'PT-006', name: '孙浩然', gender: 'male', age: 13, diagnosis: '姿势性脊柱侧弯', cobbAngle: 18, deviceId: 'DEV-E5B347', teamId: 'TEAM-003', doctorId: 'DOC-003', status: 'active', createdAt: '2026-07-15T15:30:00+08:00', updatedAt: '2026-08-10T20:10:00+08:00' },
  // T289 4.2：设计稿 患者管理.html:92/:105 的「未分配」样例（批量患者-团队绑定卡片的数据源）
  { patientId: 'PT-007', name: '王小红', gender: 'female', age: 12, diagnosis: '胸腰段双弯', cobbAngle: 35, deviceId: null, teamId: null, doctorId: null, status: 'active', createdAt: '2026-08-09T10:00:00+08:00', updatedAt: '2026-08-09T10:00:00+08:00' },
  { patientId: 'PT-008', name: '赵阳', gender: 'male', age: 14, diagnosis: '腰椎左侧弯', cobbAngle: 24, deviceId: null, teamId: null, doctorId: null, status: 'active', createdAt: '2026-08-10T16:40:00+08:00', updatedAt: '2026-08-10T16:40:00+08:00' },
]

export function mockPatients(params: { keyword?: string; teamId?: string; page?: number; pageSize?: number }): { list: Patient[]; total: number; page: number; pageSize: number } {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 10
  let list = [...PATIENTS]
  if (params.keyword) {
    const kw = params.keyword.toLowerCase()
    list = list.filter((p) => p.name.toLowerCase().includes(kw) || p.patientId.toLowerCase().includes(kw))
  }
  if (params.teamId) {
    list = list.filter((p) => p.teamId === params.teamId)
  }
  const start = (page - 1) * pageSize
  return { list: list.slice(start, start + pageSize), total: list.length, page, pageSize }
}

export function mockPatientDetail(patientId: string): Patient | null {
  return PATIENTS.find((p) => p.patientId === patientId) ?? null
}

/** FNV-1a 归一化到 0-1：同一患者同一天每次调用出同一组数，用例才钉得住 */
function dayNoise(seed: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return (h >>> 0) / 0xffffffff
}

/**
 * mock 日佩戴聚合（GET /api/v1/patients/:id/daily-wear，闭区间逐日出数）。
 * 未绑定设备的患者返回 []，对齐后端「无统计行即空数组」，也让工作台图表有空态可测。
 */
export function mockPatientDailyWear(patientId: string, start: string, end: string): DailyWearDay[] {
  const patient = PATIENTS.find((p) => p.patientId === patientId)
  if (!patient?.deviceId) return []
  // T643：日聚合的 kPa 同属「服务端替身」派生（选型 A，前端零换算）；
  // PT-003 面积未配置 → 两枚都出 null，让矫形日志的 fail-closed 档在 mock 模式下也可测。
  const areaCm2 = patientId === AREA_UNSET_PATIENT_ID ? null : MOCK_AREA_CM2
  return listDates({ start, end }).map((date) => {
    const a = dayNoise(`${patientId}|${date}`)
    const b = dayNoise(`${date}|${patientId}`)
    const avgPressure = Number((22 + a * 18).toFixed(1))
    const maxPressure = Number((38 + b * 26).toFixed(1))
    return {
      date,
      wearMinutes: Math.round(360 + a * 840),
      avgPressure,
      maxPressure,
      avgPressureKpa: mockKpaOf(avgPressure, areaCm2),
      maxPressureKpa: mockKpaOf(maxPressure, areaCm2),
      maxPoint: `P${String(1 + Math.floor(b * 20)).padStart(2, '0')}`,
      frameCount: Math.round(600 + a * 900),
      abnormalCount: b > 0.82 ? 1 : 0,
    }
  })
}

export interface RealtimeSnapshot {
  status: 'online' | 'offline' | 'abnormal'
  todayHours: number
  maxPressure: number
  maxPoint: string
  events: number
  pressureRecords: PressureRecord[]
  alerts: Alert[]
  pressureHeatmap: PressureHeatmapPoint[]
  /** T296 展示口径：色阶上界 / 偏高分界（N），与后端 sys_configs 同源；缺字段时前端回落到 6 / 5 */
  heatmapMaxN?: number
  pressureHighN?: number
  /** T513 双单位（候选 A：随快照下发）：设备有效受压面积 + 色阶上界的 kPa 派生值。
   *  两者同为 null = 面积未配置，kPa 档显示 -- 并出页内提示（fail-closed）。kPa 不落库。 */
  contactAreaCm2?: number | null
  heatmapMaxKpa?: number | null
}

// T322：mock 帧时刻改为「每次调用取当下」，并让 PT-002 固定落后 3 小时。
// 原来写死 2026-08-11 —— 帧龄永远超过后端 2h 口径，mock 模式下的实时监控页一打开就该判过期，
// 「设备在报」这条路（含用例）结构性不可达；改实时后，过期态由 PT-002 专门承载。
const STALE_FRAME_PATIENT_ID = 'PT-002'
// 末次帧是一个**固定时刻**（设备最后一次上报后就没了），不能按「当下减 3 小时」每次重算：
// 那样每次轮询采集时刻都往前挪 2 秒，页面上「数据已过期」却带着一个在跳的采集时刻，
// 恰好是 T322 要修的「拿拉取时刻冒充数据新鲜度」的镜像版本，用例也钉不住。
const STALE_FRAME_AT_MS = Date.now() - 3 * 60 * 60 * 1000
// 帧内容与时刻一并冻结：设备停报后 20 个点位不会自己每 2 秒换一版数字。
const STALE_FRAME_POINTS = makePoints(35)

// T322 问题二：把这份冻结帧的 5 个点位改成**负读数**，照抄 staging 末次帧实测的那几个值。
// 后端按点位减校准基线后才落库（data-service calibration.Apply），基线大于读数的点就是负数，
// 不是异常数据。前端展示层必须归零；mock 若全给正数，这条判据在 mock 模式下就永远测不到。
const NEGATIVE_BASELINE_POINTS: Record<string, number> = {
  P05: -0.0426,
  P06: -0.0898,
  P07: -0.0216,
  P08: -0.1072,
  P17: -0.0266,
}
for (const p of STALE_FRAME_POINTS) {
  const neg = NEGATIVE_BASELINE_POINTS[p.pointId]
  if (neg !== undefined) p.pressureValue = neg
}

export function mockPatientRealtime(patientId: string): RealtimeSnapshot {
  const patient = mockPatientDetail(patientId)
  const offline = !patient || !patient.deviceId
  const abnormal = patientId === 'PT-004'
  // 帧龄超后端 2h 口径 → status 一并给 offline（record.go:537 online 判据同为 lastseen ≤2h），
  // 否则 mock 会出现「页面判过期、状态却写佩戴中」这种真后端不可能给出的组合。
  const stale = patientId === STALE_FRAME_PATIENT_ID
  const status = offline || stale ? 'offline' : abnormal ? 'abnormal' : 'online'
  const sensorPts = stale ? STALE_FRAME_POINTS : makePoints(abnormal ? 68 : 35)
  const frameAt = stale ? STALE_FRAME_AT_MS : Date.now()
  // T513：面积按设备维度给两名患者造两种态 —— PT-003 未配置（fail-closed 载体），其余 0.64
  const areaCm2 = patientId === AREA_UNSET_PATIENT_ID ? null : MOCK_AREA_CM2
  const record: PressureRecord = {
    recordId: `REC-${patientId}-latest`,
    deviceId: patient?.deviceId ?? '',
    patientId,
    timestamp: new Date(frameAt).toISOString(),
    points: sensorPts,
    uploadTime: new Date(frameAt + 1000).toISOString(),
  }
  return {
    status,
    todayHours: offline ? 0 : 6.5 + (patientId.charCodeAt(4) % 40) / 10,
    maxPressure: abnormal ? 68.5 : 42.3,
    maxPoint: abnormal ? 'P10' : 'P05',
    events: abnormal ? 3 : patientId === 'PT-002' ? 1 : 0,
    pressureRecords: offline ? [] : [record],
    alerts: [],
    pressureHeatmap: offline ? seedHeatmap(patientId) : makeHeatmap(sensorPts, areaCm2),
    // T513：与 pressureHeatmap 同一次下发的面积与色阶上界派生值（后端 T508 同型）
    heatmapMaxN: 6,
    contactAreaCm2: areaCm2,
    heatmapMaxKpa: mockKpaOf(6, areaCm2),
  }
}

// ========== T057 写功能 mock ==========

export interface CreatePatientInput {
  name: string
  phone: string                    // 11 位手机号（mock 不做判重，后端走 phone_hash 查重）
  gender?: 'male' | 'female' | null
  age?: number | null
  diagnosis?: string | null
  cobbAngle?: number | null
  teamId?: string | null
  doctorId?: string | null
}

export interface BatchBindFailure {
  patientId: string
  reason: string
}

export interface BatchBindResult {
  successCount: number
  failedCount: number
  failures: BatchBindFailure[]
}

// mock 患者自增序号（PT-100 起，避开预置 PT-001~PT-006）
let patientSeq = 100

/** 创建患者：push 到 PATIENTS，返回新行（phone 不入 Patient 类型，mock 忽略） */
export function mockCreatePatient(input: CreatePatientInput): Patient {
  patientSeq += 1
  const now = new Date().toISOString()
  const patient: Patient = {
    patientId: `PT-${String(patientSeq).padStart(3, '0')}`,
    name: input.name,
    gender: input.gender ?? null,
    age: input.age ?? null,
    diagnosis: input.diagnosis ?? null,
    cobbAngle: input.cobbAngle ?? null,
    deviceId: null,
    teamId: input.teamId ?? null,
    doctorId: input.doctorId ?? null,
    status: 'active',
    createdAt: now,
    updatedAt: now,
  }
  PATIENTS.push(patient)
  // T432：号码入「判重台账」，否则 mock 下永远撞不出后端那个 409
  if (input.phone) PHONE_BOOK.set(patient.patientId, input.phone)
  return patient
}

// ========== T432 管理端三入口（改手机号 / 档案编辑 / 解绑微信）==========
// 语义逐条对齐 services/user-service 的三个 handler，形不似则真实模式必 400/409：
//   updatePatientPhone  admin_patient.go:61-135  → validPhone 判格式、hash 撞号（排除自身）判撞、成功只回 {patientId}
//   updatePatientAdmin  admin_patient.go:154-199 → DisallowUnknownFields 拒超集、nil=不改、空编辑 400
//   unbindWechat        admin_patient.go:24-52   → 无条件置 NULL（未绑定亦成功）、成功只回 {patientId}

/** patientId → 明文号码。真实库是 phone_enc + phone_hash，读侧不投影（T361），mock 只留判重所需 */
const PHONE_BOOK = new Map<string, string>()

/** 后端把 reason 只写进服务日志（admin_patient.go:125-132），mock 留一份流水供用例断言「reason 真随请求送出」 */
export const MOCK_PHONE_AUDIT: { patientId: string; phone: string; reason: string; at: string }[] = []

/** 与后端 validPhone（handler.go:1021-1028：长度 11、首位 1、全数字）同判据 */
const PHONE_RE_MOCK = /^1[0-9]{10}$/

/** 后端 editableSet 之外的键一律拒收（admin_patient.go:164-175 DisallowUnknownFields） */
const PATIENT_EDIT_UNKNOWN_KEYS = ['phone', 'teamId', 'primaryDoctorId', 'doctorId', 'status']

/**
 * T450-②b 乙案：可显式置 NULL 的请求体字段名（= 后端 repo.PatientProfileClearColumns 的键集合）。
 * patients 里只有 name 是 NOT NULL，所以「恢复为空」天然只覆盖 gender / age / diagnosis / cobbAngle 四列；
 * 不给值（键缺席）与「置空」是两件事，指针表达不出第三态，才要这一个显式键。
 */
export const PATIENT_CLEARABLE_FIELDS = ['gender', 'age', 'diagnosis', 'cobbAngle'] as const
export type PatientClearableField = (typeof PATIENT_CLEARABLE_FIELDS)[number]

export interface PatientProfilePatch {
  name?: string
  gender?: 'male' | 'female'
  age?: number
  diagnosis?: string
  cobbAngle?: number
  /** 显式声明「这些列改回 NULL」。同名字段既给值又被列进来，后端判 400（admin_patient.go resolveClearFields）。 */
  clearFields?: PatientClearableField[]
}

/** 改手机号：格式 → 撞号（排除自身）→ 落号 + 刷 updated_at。reason 后端只进审计日志、不校验。 */
export function mockUpdatePatientPhone(patientId: string, phone: string, reason: string): { patientId: string } {
  const p = PATIENTS.find((x) => x.patientId === patientId)
  if (!p) throw new Error('患者不存在')
  if (!PHONE_RE_MOCK.test(phone)) throw new Error('手机号格式不正确：需 11 位且以 1 开头')
  for (const [id, taken] of PHONE_BOOK) {
    if (id !== patientId && taken === phone) throw new Error('该手机号已被其他患者使用')
  }
  PHONE_BOOK.set(patientId, phone)
  MOCK_PHONE_AUDIT.push({ patientId, phone, reason, at: new Date().toISOString() })
  p.updatedAt = new Date().toISOString()
  return { patientId }
}

/** 档案编辑：只发改过的键；返回更新后的整行（后端 updatePatientAdmin 成功回 PatientDTO） */
export function mockUpdatePatientProfile(patientId: string, patch: PatientProfilePatch): Patient {
  const p = PATIENTS.find((x) => x.patientId === patientId)
  if (!p) throw new Error('患者不存在')
  const keys = Object.keys(patch)
  for (const bad of PATIENT_EDIT_UNKNOWN_KEYS) {
    if (keys.includes(bad)) throw new Error(`请求含档案编辑白名单之外的字段：${bad}`)
  }
  // 「一个字段都没给」的判据按值键 + clearFields 一起算（后端 buildAdminPatientEdit 的 anyField 同形）
  const valueKeys = (['name', 'gender', 'age', 'diagnosis', 'cobbAngle'] as const).filter(
    (k) => patch[k] !== undefined,
  )
  const clears = (patch.clearFields ?? []) as string[]
  if (valueKeys.length === 0 && clears.length === 0) throw new Error('没有需要保存的修改')
  // T450-②b 乙案（后端 resolveClearFields 同形）：表外字段 / 重复 / 与值键撞同一列一律拒。
  // 先整体校验再落值（后端也是「校验通过才交给 repo 写」）：反过来写会漏出
  // 「撞列被拒了，但 age 已经被改过」这种库里半写状态，mock 就测不出这条判据。
  const seen = new Set<string>()
  for (const f of clears) {
    if (!PATIENT_CLEARABLE_FIELDS.includes(f as PatientClearableField)) {
      throw new Error(`clearFields 只接受 ${PATIENT_CLEARABLE_FIELDS.join('、')}`)
    }
    if (seen.has(f)) throw new Error(`clearFields 含重复字段：${f}`)
    seen.add(f)
    if (valueKeys.includes(f as (typeof valueKeys)[number])) {
      throw new Error(`${f} 既给了值又被列为清空，请二选一`)
    }
  }
  if (patch.name !== undefined) {
    const name = patch.name.trim()
    if (!name || name.length > 64) throw new Error('姓名需为 1-64 个字符')
    p.name = name
  }
  if (patch.gender !== undefined) {
    if (patch.gender !== 'male' && patch.gender !== 'female') throw new Error('性别只能是 male 或 female')
    p.gender = patch.gender
  }
  if (patch.age !== undefined) {
    if (patch.age < 0 || patch.age > 150) throw new Error('年龄需在 0-150 之间')
    p.age = patch.age
  }
  if (patch.diagnosis !== undefined) {
    if (patch.diagnosis.length > 255) throw new Error('诊断超过 255 个字符')
    p.diagnosis = patch.diagnosis
  }
  if (patch.cobbAngle !== undefined) {
    if (patch.cobbAngle < 0 || patch.cobbAngle > 180) throw new Error('Cobb 角需在 0-180 之间')
    p.cobbAngle = patch.cobbAngle
  }
  // 过关的才落 NULL。注意 age=0、cobbAngle=0 是合法值，不是一路的「清空」。
  for (const f of clears) {
    if (f === 'gender') p.gender = null
    else if (f === 'age') p.age = null
    else if (f === 'diagnosis') p.diagnosis = null
    else p.cobbAngle = null
  }
  p.updatedAt = new Date().toISOString()
  return p
}

/**
 * 解绑微信。后端是无条件 `SET wx_openid = NULL`（pg.go:168-171），对未绑定患者亦回 200，
 * 而患者域读侧没有 openid 字段可镜像 ⇒ mock 刻意不模拟「已绑 / 未绑」两态，只镜像存在性。
 */
export function mockUnbindPatientWechat(patientId: string): { patientId: string } {
  const p = PATIENTS.find((x) => x.patientId === patientId)
  if (!p) throw new Error('患者不存在')
  p.updatedAt = new Date().toISOString()
  return { patientId }
}

/** 与医护/技师 mock 同形：前缀 + 8 位随机 + 后缀，镜像 handler.go genDoctorPassword 的发号器 */
function genPatientPassword(): string {
  return `Br${Math.random().toString(36).slice(2, 10)}#7`
}

/**
 * T500 设登录口令（POST /admin/patients/:id/password，T477）。
 * 后端只回 {patientId, password}、明文不落库不进审计；未知患者 404 不写（patient_password_t477.go 判定序）。
 * mock 侧没有 password_hash 列可落，口令态在两个读接口都不投影 ⇒ 只镜像存在性与一次性返回。
 */
export function mockSetPatientPassword(patientId: string): { patientId: string; password: string } {
  if (!PATIENTS.some((x) => x.patientId === patientId)) throw new Error('患者不存在')
  return { patientId, password: genPatientPassword() }
}

/** 分配/更改患者团队（幂等：同 teamId no-op，不变更 updatedAt） */
export function mockAssignPatientTeam(patientId: string, teamId: string): Patient {
  const p = PATIENTS.find((x) => x.patientId === patientId)
  if (!p) {
    throw new Error('患者不存在')
  }
  if (p.teamId !== teamId) {
    p.teamId = teamId
    p.updatedAt = new Date().toISOString()
  }
  return p
}

/**
 * 批量绑定患者到团队（部分失败不回滚，HTTP 仍 200）
 * mock 策略：不存在的患者计入 failures；存在的更新 teamId
 */
export function mockBatchBindPatients(patientIds: string[], teamId: string): BatchBindResult {
  const failures: BatchBindFailure[] = []
  let successCount = 0
  for (const pid of patientIds) {
    const p = PATIENTS.find((x) => x.patientId === pid)
    if (!p) {
      failures.push({ patientId: pid, reason: '患者不存在' })
      continue
    }
    if (p.teamId !== teamId) {
      p.teamId = teamId
      p.updatedAt = new Date().toISOString()
    }
    successCount += 1
  }
  return {
    successCount,
    failedCount: failures.length,
    failures,
  }
}
