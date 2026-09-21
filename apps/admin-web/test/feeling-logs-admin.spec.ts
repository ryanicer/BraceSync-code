// T289 8.1：跨患者感受日志流的筛选/排序/分页口径（镜像契约 getFeelingLogsAdmin，api-contracts.ts:590-620）
// 走 mock 纯函数而非组件：后端语义（闭区间、feeling 直比 comfort_level、姓名 keyword）在这里锁死，
// UI 断言交给 e2e/tests/admin-orthosis-log.spec.ts。
import { describe, it, expect } from 'vitest'
import { mockFeelingLogsAdmin } from '../src/mock/orthosis'

const ALL = mockFeelingLogsAdmin({ page: 1, pageSize: 100 })

describe('mockFeelingLogsAdmin（契约镜像）', () => {
  it('默认第 1 页 20 条：total 为全量条数，按 logDate 倒序、同日按 logId 倒序', () => {
    expect(ALL.total).toBe(8)
    const first = mockFeelingLogsAdmin({})
    expect(first.list).toHaveLength(8)
    expect(first.list.map((f) => f.logId)).toEqual(['FL-004', 'FL-003', 'FL-001', 'FL-005', 'FL-002', 'FL-007', 'FL-006', 'FL-008'])
  })

  it('patientName 由患者字典 join 带出（设计稿 矫形日志.html:127 患者列两行）', () => {
    expect(ALL.list.every((f) => !!f.patientName)).toBe(true)
  })

  it('feeling 筛选直比 comfort_level 两档，不走 comfortScore 派生', () => {
    const discomfort = mockFeelingLogsAdmin({ feeling: 'discomfort' })
    expect(discomfort.total).toBe(2)
    expect(discomfort.list.map((f) => f.logId).sort()).toEqual(['FL-003', 'FL-005'])
    expect(mockFeelingLogsAdmin({ feeling: 'fitted' }).total).toBe(5)
  })

  it('keyword 只匹配患者姓名（后端现状不匹配患者编号，契约 :592 待 PM 裁定）', () => {
    expect(mockFeelingLogsAdmin({ keyword: '林小雨' }).list.map((f) => f.logId).sort()).toEqual(['FL-001', 'FL-002'])
    expect(mockFeelingLogsAdmin({ keyword: 'PT-001' }).total).toBe(0)
  })

  it('startDate/endDate 对 logDate 取闭区间', () => {
    const only11 = mockFeelingLogsAdmin({ startDate: '2026-08-11', endDate: '2026-08-11' })
    expect(only11.list.map((f) => f.logId).sort()).toEqual(['FL-001', 'FL-003', 'FL-004'])
    const tail = mockFeelingLogsAdmin({ startDate: '2026-08-09', endDate: '2026-08-10' })
    expect(tail.total).toBe(4)
  })

  it('分页切片后 total 不变（设计稿「共 N 条记录」为全量）', () => {
    const p2 = mockFeelingLogsAdmin({ page: 2, pageSize: 3 })
    expect(p2.total).toBe(8)
    expect(p2.list).toHaveLength(3)
    expect(p2.list.map((f) => f.logId)).toEqual(['FL-005', 'FL-002', 'FL-007'])
  })

  it('未评行（comfort_level 为 NULL）保留，两档筛选都不命中', () => {
    const unrated = ALL.list.find((f) => f.feeling === null)
    expect(unrated?.logId).toBe('FL-008')
    expect(mockFeelingLogsAdmin({ feeling: 'fitted' }).list.some((f) => f.feeling === null)).toBe(false)
  })
})
