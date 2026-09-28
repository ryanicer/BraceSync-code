/**
 * 技师端告警读接口（T433）。
 *
 * 改前告警页直接调 utils/request，而 request 在 USE_MOCK 下抛
 * 「Mock mode: use mock data functions directly」（utils/request.ts:35-37）
 * —— 也就是说这一页在本地/mock 构建下必然落「加载失败」，
 * 该页的渲染判据（本页两格缺陷）此前没有任何可跑绿的测试。
 * 这里按 api/install.ts 同款写法补 mock 分支，把「可本地测」这一前置补上；
 * 真实模式下走的是与改前完全相同的 GET /api/v1/alerts。
 */
import type { Alert } from '@bracesync/shared-types'
import { request, USE_MOCK } from '../utils/request'

export interface ListAlertParams {
  page?: number
  pageSize?: number
}

export interface AlertListResult {
  list: Alert[]
  total: number
}

/**
 * 与后端 alertPageData 同形的 mock 集。
 * 刻意取 60 条 > 单页 50：现网 86 条告警＝2 页（Alice T428 走查实测），
 * 只有一页装得下的 mock 会让「取数只发一页」那格在本地永远测不出来。
 */
function mockAlertSeed(): Alert[] {
  const types = ['pressure_high', 'wear_interrupt', 'sensor_drift'] as const
  // 裁定⑤（T443）：患者位改读姓名，所以 mock 也要回 patientName，否则本地永远只走回落分支。
  // 与 api/install.ts 的 mockInstallSeed 同名族（合成显示夹具，不冒充现网患者）。
  // 第 6 位刻意留 null：后端 alerts 族的空值语义是空串（COALESCE(p.name,'')），
  // 留一行「无姓名」才能让回落判据在门禁里真被执行。
  const names = ['张明远', '李欣怡', '王子轩', '刘思雨', '陈俊豪', null]
  const rows = Array.from({ length: 60 }).map((_, i) => {
    const type = i === 0 ? 'wear_duration_short' : types[i % 3]
    return {
      alertId: `ALR-T433-${String(i + 1).padStart(3, '0')}`,
      patientId: `P2026000${(i % 6) + 1}`,
      patientName: names[i % 6],
      deviceId: `PRS-ML05-RC-19700101${String((i % 9) + 1).padStart(3, '0')}`,
      type,
      // wear_duration_short 两个数值位按后端口径写**分钟**（engine.go need = targetHours*60）；
      // 首条 i=0 是 T428 走查取到的那格实况：当日佩戴 0 分钟、目标 9 小时。
      detail:
        type === 'wear_duration_short'
          ? '佩戴时长不足：P20260001 于 2026-09-27 累计佩戴 0.0 小时，低于目标 9.0 小时'
          : `T433 mock 告警 ${i + 1}`,
      sensorPoint: type === 'pressure_high' || type === 'sensor_drift' ? 'P10' : '',
      thresholdValue: type === 'wear_duration_short' ? 540 : 40,
      actualValue: type === 'wear_duration_short' ? 0 : 52.5,
      timestamp: new Date(Date.parse('2026-09-27T10:00:00+08:00') - i * 3600000).toISOString(),
      readStatus: 'unread',
      processStatus: i === 59 ? 'processed' : 'pending',
      resolvedStatus: 'active',
      resolvedAt: null,
      processedBy: null,
      processedAt: null,
      processNote: null,
    } satisfies Alert
  })
  // T451：末尾追加一行「压力波动」（admin T430 已拍的隐藏类），让后端 total（61）与页面可见数
  // （60）刻意不相等 ⇒ 现存的「共 60 条告警」「.alert-card 60」「待处理 59」三条判据
  // 从「恰好等于种子数」变成「只有过滤生效才成立」，摘掉过滤即判红。
  // 追加在末尾而不是插进循环：前 60 行的下标与文案位不变，不会把第二页那条判据挪位。
  rows.push({
    ...rows[59],
    alertId: 'ALR-T451-061',
    type: 'pressure_fluctuation',
    detail: 'T451 mock 压力波动告警（界面隐藏、数据不删）',
    processStatus: 'pending',
  })
  return rows
}

export async function listAlerts(params: ListAlertParams = {}): Promise<AlertListResult> {
  // 单页大小沿用改前页面写死的 50（现网 86 条告警 = 2 页），本卡不动这个数
  const pageSize = params.pageSize ?? 50
  const page = params.page ?? 1
  if (USE_MOCK) {
    await new Promise((r) => setTimeout(r, 100))
    const seed = mockAlertSeed()
    const start = (page - 1) * pageSize
    return { list: seed.slice(start, start + pageSize), total: seed.length }
  }
  const res = await request<{ list: Alert[]; total: number }>({
    url: '/api/v1/alerts',
    method: 'GET',
    data: { page, pageSize },
  })
  return { list: res.list ?? [], total: res.total }
}
