// T679 医护待办 mock（PRD V3.45 §7D.16；设计稿 docs/design/admin/医护待办列表.html / 医护待办详情.html，定稿 commit 5994aa53）。
// 结构与后端 model/flow_t679.go 的 DTO 逐字段对齐；演示四行/四例取设计稿 TODOS + DATA。
// 类型从 '../api' 做 type-only import（照抄 mock/flow.ts ← api/flow 的先例，消除运行时循环）。
import type { FlowTodoDetail, FlowTodoItem, FlowTodoList } from '../api'
import type { FlowNodeAction } from '../api/flow'

// 设计稿列表引擎 TODOS 四行（T1/T3/T4/T5）。node 为 null = 已办结（设计稿办结行显示「已办结」）。
const LIST_ROWS: FlowTodoItem[] = [
  {
    instanceId: 'T1',
    alertId: 'ALM-20261011031',
    patientId: 'P20260001',
    patientName: '患者小明',
    alertType: 'pressure_high',
    alertTypeName: '压力偏高',
    triggeredAt: '2026-10-11T13:10:00+08:00',
    processStatus: 'processing',
    processStatusText: '处理中',
    node: { nodeId: 'n3', name: '护士确认', limitText: '时限 2 小时', leftText: '超时 47 分', tone: 'over' },
  },
  {
    instanceId: 'T3',
    alertId: 'ALM-20261011009',
    patientId: 'P20260003',
    patientName: '患者小杰',
    alertType: 'wear_duration_short',
    alertTypeName: '佩戴时长不足',
    triggeredAt: '2026-10-11T13:58:00+08:00',
    processStatus: 'processing',
    processStatusText: '处理中',
    node: { nodeId: 'n3', name: '医生确认', limitText: '时限 2 小时', leftText: '剩 1 时 40 分', tone: 'due' },
  },
  {
    instanceId: 'T4',
    alertId: 'ALM-20261011051',
    patientId: 'P20260001',
    patientName: '患者小明',
    alertType: 'sensor_drift',
    alertTypeName: '传感器标定异常',
    triggeredAt: '2026-10-11T11:20:00+08:00',
    processStatus: 'pending',
    processStatusText: '待处理',
    node: { nodeId: 'n2', name: '护士复核', limitText: '时限 24 小时', leftText: '剩 22 时', tone: 'ok' },
  },
  {
    instanceId: 'T5',
    alertId: 'ALM-20261011003',
    patientId: 'P20260004',
    patientName: '患者小琳',
    alertType: 'wear_duration_short',
    alertTypeName: '佩戴时长不足',
    triggeredAt: '2026-10-11T13:45:00+08:00',
    processStatus: 'processed',
    processStatusText: '已处理',
    node: null,
  },
]

function act(
  actionId: string,
  nodeId: string,
  nodeName: string,
  operator: string,
  operatorName: string,
  label: string,
  createdAt: string,
): FlowNodeAction {
  return {
    actionId, nodeId, nodeName, action: 'confirm', actionLabel: label,
    operator, operatorName, remark: null, attachments: [], targetOperator: null, createdAt,
  }
}

// 设计稿详情 DATA 四例（T1/T3/T4/T5）。阈值/实际值为数值，显示格式交前端 formatAlertValue 收口。
const DETAILS: Record<string, FlowTodoDetail> = {
  T1: {
    instanceId: 'T1', templateId: 'FLOW_T1', templateName: '默认告警流程', instanceStatus: 'running',
    alertId: 'ALM-20261011031', patientId: 'P20260001', patientName: '患者小明',
    deviceId: 'PRS-ML05-RC-20260701001', alertType: 'pressure_high', alertTypeName: '压力偏高',
    triggeredAt: '2026-10-11T13:10:00+08:00', processStatus: 'processing', processStatusText: '处理中',
    detail: '采集点 P6-右侧腰段 压力 128N，超阈值 60N', sensorPoint: 'P6-右侧腰段',
    thresholdValue: 60, actualValue: 128, resolved: false, resolvedAt: null, step: 3, total: 7,
    node: {
      nodeId: 'n3', name: '护士确认', role: '护士', limitText: '时限 2 小时', leftText: '超时 47 分', tone: 'over',
      note: '核对患者佩戴与压力读数，确认是否需医生处置；无法处理可跳过或转派。',
    },
    steps: [
      { nodeId: 'n1', name: '告警触发', role: '系统自动', status: 'done' },
      { nodeId: 'n2', name: '通知推送', role: '系统自动', status: 'done' },
      { nodeId: 'n3', name: '护士确认', role: '护士', status: 'current' },
      { nodeId: 'n4', name: '护士初筛', role: '护士', status: 'todo' },
      { nodeId: 'n5', name: '医生处置', role: '主治医生', status: 'todo' },
      { nodeId: 'n6', name: '延时（不升级）', role: '系统自动', status: 'todo' },
      { nodeId: 'n7', name: '关闭归档', role: '系统自动', status: 'todo' },
    ],
    records: [
      act('A1', 'n1', '告警触发', 'system', '系统', '告警触发并创建流程实例（pressure_high → 默认告警流程）', '2026-10-11T13:10:00+08:00'),
      act('A2', 'n2', '通知推送', 'system', '系统', '通知推送完成：系统消息 + 短信已发', '2026-10-11T13:11:00+08:00'),
      act('A3', 'n3', '护士确认', 'system', '系统', '节点「护士确认」进入处理中，待办进入医护列表', '2026-10-11T13:12:00+08:00'),
    ],
  },
  T3: {
    instanceId: 'T3', templateId: 'FLOW_T3', templateName: '随访跟进 v1', instanceStatus: 'running',
    alertId: 'ALM-20261011009', patientId: 'P20260003', patientName: '患者小杰',
    deviceId: 'PRS-ML05-RC-20260701003', alertType: 'wear_duration_short', alertTypeName: '佩戴时长不足',
    triggeredAt: '2026-10-11T13:58:00+08:00', processStatus: 'processing', processStatusText: '处理中',
    detail: '当日累计佩戴 6.5 小时，低于下限 22 小时', sensorPoint: null,
    thresholdValue: 1320, actualValue: 390, resolved: false, resolvedAt: null, step: 3, total: 7,
    node: {
      nodeId: 'n3', name: '医生确认', role: '主治医生', limitText: '时限 2 小时', leftText: '剩 1 时 40 分', tone: 'due',
      note: '联系患者核实佩戴情况，判断是否需随访或现场配合。',
    },
    steps: [
      { nodeId: 'n1', name: '告警触发', role: '系统自动', status: 'done' },
      { nodeId: 'n2', name: '通知推送', role: '系统自动', status: 'done' },
      { nodeId: 'n3', name: '医生确认', role: '主治医生', status: 'current' },
      { nodeId: 'n4', name: '判断分支', role: '主治医生', status: 'todo' },
      { nodeId: 'n5', name: '随访处置', role: '值班医生', status: 'todo' },
      { nodeId: 'n6', name: '延时（不升级）', role: '系统自动', status: 'todo' },
      { nodeId: 'n7', name: '关闭归档', role: '系统自动', status: 'todo' },
    ],
    records: [
      act('A1', 'n1', '告警触发', 'system', '系统', '告警触发：当日佩戴 6.5 小时', '2026-10-11T13:58:00+08:00'),
      act('A2', 'n2', '通知推送', 'system', '系统', '通知推送：已通知主治医生', '2026-10-11T13:58:00+08:00'),
    ],
  },
  T4: {
    instanceId: 'T4', templateId: 'FLOW_T4', templateName: '标定异常处置', instanceStatus: 'running',
    alertId: 'ALM-20261011051', patientId: 'P20260001', patientName: '患者小明',
    deviceId: 'PRS-ML05-RC-20260701001', alertType: 'sensor_drift', alertTypeName: '传感器标定异常',
    triggeredAt: '2026-10-11T11:20:00+08:00', processStatus: 'pending', processStatusText: '待处理',
    detail: '空载采集点 P3-左侧胸段 读数漂移 2.8N，通知技师与运营', sensorPoint: 'P3-左侧胸段',
    thresholdValue: 0.3, actualValue: 2.8, resolved: false, resolvedAt: null, step: 2, total: 5,
    node: {
      nodeId: 'n2', name: '护士复核', role: '护士', limitText: '时限 24 小时', leftText: '剩 22 时', tone: 'ok',
      // 设计稿此例节点无 note → 走 R8 默认兜底句（与后端 model.FlowTodoDefaultNote 同值）
      note: '请核对该告警并按流程处置',
    },
    steps: [
      { nodeId: 'n1', name: '告警触发', role: '系统自动', status: 'done' },
      { nodeId: 'n2', name: '护士复核', role: '护士', status: 'current' },
      { nodeId: 'n3', name: '现场配合', role: '医护', status: 'todo' },
      { nodeId: 'n4', name: '复测确认', role: '护士', status: 'todo' },
      { nodeId: 'n5', name: '关闭归档', role: '系统自动', status: 'todo' },
    ],
    records: [
      act('A1', 'n1', '告警触发', 'system', '系统', '告警触发：P3 空载漂移 2.8N', '2026-10-11T11:20:00+08:00'),
    ],
  },
  T5: {
    instanceId: 'T5', templateId: 'FLOW_T5', templateName: '随访跟进 v1', instanceStatus: 'completed',
    alertId: 'ALM-20261011003', patientId: 'P20260004', patientName: '患者小琳',
    deviceId: 'PRS-ML05-RC-20260701004', alertType: 'wear_duration_short', alertTypeName: '佩戴时长不足',
    triggeredAt: '2026-10-11T13:45:00+08:00', processStatus: 'processed', processStatusText: '已处理',
    detail: '当日累计佩戴 6.5 小时，低于下限；患者外出未佩戴，已记录特殊情况', sensorPoint: null,
    thresholdValue: 1320, actualValue: 390, resolved: true, resolvedAt: '2026-10-11T14:05:00+08:00', step: 7, total: 7,
    node: null,
    steps: [
      { nodeId: 'n1', name: '告警触发', role: '系统自动', status: 'done' },
      { nodeId: 'n2', name: '通知推送', role: '系统自动', status: 'done' },
      { nodeId: 'n3', name: '医生确认', role: '主治医生', status: 'done' },
      { nodeId: 'n4', name: '判断分支', role: '主治医生', status: 'done' },
      { nodeId: 'n5', name: '随访处置', role: '值班医生', status: 'done' },
      { nodeId: 'n6', name: '延时（不升级）', role: '系统自动', status: 'skipped' },
      { nodeId: 'n7', name: '关闭归档', role: '系统自动', status: 'done' },
    ],
    records: [
      act('A1', 'n1', '告警触发', 'system', '系统', '告警触发：当日佩戴 6.5 小时', '2026-10-11T13:45:00+08:00'),
      act('A2', 'n3', '医生确认', 'doctor_li', '李医师', '确认：联系患者，今日外出未佩戴', '2026-10-11T13:50:00+08:00'),
      act('A3', 'n5', '随访处置', 'doctor_li', '李医师', '随访处置：记录特殊情况，通知明日恢复佩戴', '2026-10-11T13:52:00+08:00'),
      act('A4', 'n7', '关闭归档', 'ops_admin', '管理员', '关闭归档', '2026-10-11T14:00:00+08:00'),
    ],
  },
}

/** 「待我处理」= 实例在途（node 非空），与设计稿 tRender 的 mine 判定同源。 */
function isMine(row: FlowTodoItem): boolean {
  return row.node !== null
}

/**
 * 待办列表：先按页签筛，再按 keyword 筛（患者姓名 + 患者ID），最后分页。
 * mineCount/allCount 为角标计数，不受 keyword 影响（设计稿 tRender 先算计数再过滤展示行）。
 */
export function mockTodos(params: { pane?: 'mine' | 'all'; keyword?: string; page?: number; pageSize?: number }): FlowTodoList {
  const page = params.page ?? 1
  const pageSize = params.pageSize ?? 20
  const kw = (params.keyword ?? '').trim().toLowerCase()
  const mineCount = LIST_ROWS.filter(isMine).length
  const allCount = LIST_ROWS.length

  let rows = params.pane === 'all' ? LIST_ROWS.slice() : LIST_ROWS.filter(isMine)
  if (kw) {
    rows = rows.filter((r) => `${r.patientName}${r.patientId}`.toLowerCase().includes(kw))
  }
  const start = (page - 1) * pageSize
  return {
    list: rows.slice(start, start + pageSize),
    total: rows.length,
    page,
    pageSize,
    mineCount,
    allCount,
  }
}

/** 待办详情：命中演示四例；未命中抛「不存在或已处理」（对齐后端 10404 语义）。 */
export function mockTodoDetail(instanceId: string): FlowTodoDetail {
  const detail = DETAILS[instanceId]
  if (!detail) {
    throw new Error('待办不存在或已处理')
  }
  return detail
}

/** 侧栏角标：mineCount（设计稿 .menu-badge = 待我处理条数）。 */
export function mockTodoBadgeCount(): number {
  return LIST_ROWS.filter(isMine).length
}
