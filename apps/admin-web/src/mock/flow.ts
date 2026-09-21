// T275 流程画布 mock（选型 R2：后端未就绪时前端可先按契约 mock 开发）。
// 结构与 docs/api/api-contracts.ts 的 Flow* 类型逐字段对齐；状态机口径同契约：
// confirm 推进后继、reject 置 skipped 不推进、transfer 只改 assignee、urge 只留痕。
import type {
  FlowGraphEdge, FlowGraphNode, FlowInstance, FlowNodeAction, FlowNodeActionRequest, FlowNodeState, FlowTemplate,
} from '../api/flow'

const NODES: FlowGraphNode[] = [
  { id: 'r1', type: 'circle', x: 380, y: 60, properties: { kind: 'trigger', r: 29 }, text: { x: 380, y: 60, value: '告警触发' } },
  { id: 'r2', type: 'rect', x: 380, y: 160, properties: { kind: 'notice', width: 140, height: 48, assigneeRole: '系统自动', channels: ['system', 'sms'] }, text: { x: 380, y: 160, value: '通知推送' } },
  { id: 'r3', type: 'rect', x: 380, y: 250, properties: { kind: 'process', width: 140, height: 48, assigneeRole: '主治医生', timeLimit: 2, timeUnit: 'hours', channels: ['system'] }, text: { x: 380, y: 250, value: '医生确认' } },
  { id: 'r4', type: 'diamond', x: 380, y: 345, properties: { kind: 'condition', rx: 50, ry: 28, assigneeRole: '主治医生' }, text: { x: 380, y: 345, value: '判断分支' } },
  { id: 'r5', type: 'rect', x: 200, y: 450, properties: { kind: 'process', width: 140, height: 48, assigneeRole: '值班医生', timeLimit: 24, timeUnit: 'hours' }, text: { x: 200, y: 450, value: '常规处理' } },
  { id: 'r6', type: 'rect', x: 540, y: 450, properties: { kind: 'delay', width: 140, height: 48, delayMinutes: 360 }, text: { x: 540, y: 450, value: '延时升级' } },
  { id: 'r7', type: 'rect', x: 380, y: 550, properties: { kind: 'archive', width: 140, height: 48 }, text: { x: 380, y: 550, value: '关闭归档' } },
]

const EDGES: FlowGraphEdge[] = [
  { id: 'e1', type: 'polyline', sourceNodeId: 'r1', targetNodeId: 'r2', properties: {} },
  { id: 'e2', type: 'polyline', sourceNodeId: 'r2', targetNodeId: 'r3', properties: {} },
  { id: 'e3', type: 'polyline', sourceNodeId: 'r3', targetNodeId: 'r4', properties: {} },
  { id: 'e4', type: 'polyline', sourceNodeId: 'r4', targetNodeId: 'r5', properties: {}, text: { x: 290, y: 400, value: '通过' } },
  { id: 'e5', type: 'polyline', sourceNodeId: 'r4', targetNodeId: 'r6', properties: {}, text: { x: 470, y: 400, value: '驳回' } },
  { id: 'e6', type: 'polyline', sourceNodeId: 'r5', targetNodeId: 'r7', properties: {} },
  { id: 'e7', type: 'polyline', sourceNodeId: 'r6', targetNodeId: 'r3', properties: {}, text: { x: 620, y: 300, value: '重新确认' } },
]

const TEMPLATE: FlowTemplate = {
  templateId: 'FLOW_TMOCK00001',
  name: '默认告警流程',
  nodes: NODES,
  edges: EDGES,
  creator: 'ops_admin',
  creatorName: '运营管理员',
  version: 3,
  createdAt: '2026-07-01T09:00:00+08:00',
  updatedAt: '2026-07-18T11:20:00+08:00',
  instanceCount: 2,
}

interface MockInstance {
  instance: FlowInstance
  states: FlowNodeState[]
  actions: FlowNodeAction[]
}

function freshStates(
  nodes: FlowGraphNode[],
  edges: FlowGraphEdge[],
  currentIds: string[],
  doneIds: string[],
  skippedIds: string[],
): FlowNodeState[] {
  const nextMap: Record<string, string[]> = {}
  edges.forEach((e) => { (nextMap[e.sourceNodeId] ||= []).push(e.targetNodeId) })
  return nodes.map((n) => ({
    nodeId: n.id,
    status: skippedIds.includes(n.id) ? 'skipped' as const
      : doneIds.includes(n.id) ? 'done' as const
        : currentIds.includes(n.id) ? 'current' as const
          : 'todo' as const,
    operator: doneIds.includes(n.id) ? 'doctor_li' : null,
    operatorName: doneIds.includes(n.id) ? '王医生' : null,
    operatedAt: doneIds.includes(n.id) ? '2026-07-21T14:32:20+08:00' : null,
    remark: doneIds.includes(n.id) ? '已接收告警通知，进入处理流程' : null,
    attachments: [],
    assignee: null,
    assigneeName: null,
    nextNodeIds: nextMap[n.id] ?? [],
  }))
}

function seedInstance(alertId: string, instanceId: string, template: FlowTemplate): MockInstance {
  return {
    instance: {
      instanceId,
      templateId: template.templateId,
      templateName: template.name,
      alertId,
      currentNodeId: 'r3',
      status: 'running',
      startedAt: '2026-07-21T14:32:15+08:00',
      endedAt: null,
    },
    states: freshStates(template.nodes, template.edges, ['r3'], ['r1', 'r2'], []),
    actions: [
      { actionId: 'ACT_1', nodeId: 'r1', nodeName: '告警触发', action: 'confirm', actionLabel: '确认处理', operator: 'system', operatorName: '系统自动', remark: '检测到 P6-右侧腰段 压力偏高 128N（阈值 60N）', attachments: [], targetOperator: null, createdAt: '2026-07-21T14:32:15+08:00' },
      { actionId: 'ACT_2', nodeId: 'r2', nodeName: '通知推送', action: 'confirm', actionLabel: '确认处理', operator: 'system', operatorName: '系统自动', remark: '已通知主治医生 王医生', attachments: [], targetOperator: null, createdAt: '2026-07-21T14:32:18+08:00' },
      { actionId: 'ACT_3', nodeId: 'r3', nodeName: '医生确认', action: 'confirm', actionLabel: '确认处理', operator: 'doctor_li', operatorName: '王医生', remark: '已接收告警通知，进入处理流程', attachments: [], targetOperator: null, createdAt: '2026-07-21T14:32:20+08:00' },
    ],
  }
}

// ALR-001 在途（r3 current）；ALR-002 已走完且 r6 被跳过，用于验四色
const DB = new Map<string, MockInstance>()

// 模板改为可增删改的存储（T276 设计器新建/保存/切换/删除要能在 mock 下走通全流程）。
// 🔴 默认模板被两个种子实例引用，删掉会让运行态画布空掉，故它的 instanceCount 固定 >0。
const TEMPLATES = new Map<string, FlowTemplate>()
TEMPLATES.set(TEMPLATE.templateId, structuredClone(TEMPLATE))

function ensure(alertId: string): MockInstance | null {
  if (!DB.has(alertId)) {
    const base = TEMPLATES.get(TEMPLATE.templateId)
    if (!base) return null
    if (alertId === 'ALR-001') DB.set(alertId, seedInstance(alertId, 'FLOW_IMOCK00001', base))
    else if (alertId === 'ALR-002') {
      const done = ['r1', 'r2', 'r3', 'r4', 'r5', 'r7']
      const inst = seedInstance(alertId, 'FLOW_IMOCK00002', base)
      inst.states = freshStates(base.nodes, base.edges, [], done, ['r6'])
      inst.instance.currentNodeId = null
      inst.instance.status = 'completed'
      inst.instance.endedAt = '2026-07-21T16:05:00+08:00'
      DB.set(alertId, inst)
    }
  }
  return DB.get(alertId) ?? null
}

let seq = 100

/** 契约 validateFlowName：必填、去空格后 1–64 字符 */
function assertTemplateName(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) throw new Error('模板名称不能为空')
  if ([...trimmed].length > 64) throw new Error('模板名称最多 64 个字符')
  return trimmed
}

/** 契约 api-contracts.ts:1082：列表项的 nodes/edges 恒为空数组（图数据只在详情返回，防列表载荷膨胀）。
 * mock 必须照抄这个行为，否则设计器「下拉里显示节点数」这类逻辑在 mock 下绿、在真实后端下永远是 0。 */
export function mockListTemplates(keyword = ''): FlowTemplate[] {
  const kw = keyword.trim().toLowerCase()
  return [...TEMPLATES.values()]
    .filter((t) => !kw || t.name.toLowerCase().includes(kw))
    .map((t) => ({ ...structuredClone(t), nodes: [], edges: [] }))
}

export function mockGetTemplate(templateId: string): FlowTemplate {
  const hit = TEMPLATES.get(templateId)
  if (!hit) throw new Error('模板不存在（mock）')
  return structuredClone(hit)
}

export function mockListInstances(alertId: string): FlowInstance[] {
  const hit = ensure(alertId)
  return hit ? [structuredClone(hit.instance)] : []
}

export function mockCreateTemplate(name: string, graph: { nodes?: unknown[]; edges?: unknown[] }): FlowTemplate {
  const finalName = assertTemplateName(name)
  if ([...TEMPLATES.values()].some((t) => t.name === finalName)) throw new Error(`模板名称已存在：${finalName}`)
  const now = new Date().toISOString()
  const created: FlowTemplate = {
    templateId: `FLOW_TMOCK${++seq}`,
    name: finalName,
    nodes: structuredClone(graph.nodes ?? []) as FlowGraphNode[],
    edges: structuredClone(graph.edges ?? []) as FlowGraphEdge[],
    creator: 'ops_admin',
    creatorName: '运营管理员',
    version: 1,
    createdAt: now,
    updatedAt: now,
    instanceCount: 0,
  }
  TEMPLATES.set(created.templateId, created)
  return structuredClone(created)
}

export function mockUpdateTemplate(
  templateId: string,
  data: { name?: string; nodes?: unknown[]; edges?: unknown[] },
): FlowTemplate {
  const hit = TEMPLATES.get(templateId)
  if (!hit) throw new Error('模板不存在（mock）')
  if (data.name !== undefined) {
    const finalName = assertTemplateName(data.name)
    if ([...TEMPLATES.values()].some((t) => t.name === finalName && t.templateId !== templateId)) {
      throw new Error(`模板名称已存在：${finalName}`)
    }
    hit.name = finalName
  }
  if (data.nodes !== undefined) hit.nodes = structuredClone(data.nodes) as FlowGraphNode[]
  if (data.edges !== undefined) hit.edges = structuredClone(data.edges) as FlowGraphEdge[]
  const known = hit.nodes.filter((n) => n.id)
  const badEdge = hit.edges.find((e) => !known.some((n) => n.id === e.sourceNodeId) || !known.some((n) => n.id === e.targetNodeId))
  if (badEdge) throw new Error(`edges 指向了不存在的节点：${badEdge.sourceNodeId}→${badEdge.targetNodeId}`)
  hit.version += 1
  hit.updatedAt = new Date().toISOString()
  return structuredClone(hit)
}

export function mockDeleteTemplate(templateId: string): void {
  const hit = TEMPLATES.get(templateId)
  if (!hit) throw new Error('模板不存在（mock）')
  if (hit.instanceCount > 0) {
    throw new Error(`该模板已有 ${hit.instanceCount} 个流程实例，需先清理实例（契约 409）`)
  }
  TEMPLATES.delete(templateId)
}

/** 无入边的第一个节点即入口（对齐后端 parseFlowGraph 的 entryNodeID 口径） */
function entryNodeId(template: FlowTemplate): string | null {
  const targeted = new Set(template.edges.map((e) => e.targetNodeId))
  return template.nodes.find((n) => !targeted.has(n.id))?.id ?? null
}

export function mockStartInstance(templateId: string, alertId: string): FlowInstance {
  const template = TEMPLATES.get(templateId)
  if (!template) throw new Error('模板不存在（mock）')
  const existed = DB.get(alertId)
  if (existed) return structuredClone(existed.instance)
  const entry = entryNodeId(template)
  const created: MockInstance = {
    instance: {
      instanceId: `FLOW_IMOCK${++seq}`,
      templateId,
      templateName: template.name,
      alertId,
      currentNodeId: entry,
      status: 'running',
      startedAt: new Date().toISOString(),
      endedAt: null,
    },
    states: freshStates(template.nodes, template.edges, entry ? [entry] : [], [], []),
    actions: [],
  }
  DB.set(alertId, created)
  return structuredClone(created.instance)
}

export function mockNodeStates(instanceId: string): FlowNodeState[] {
  const hit = findInstance(instanceId)
  return structuredClone(hit.states)
}

export function mockInstanceActions(instanceId: string): FlowNodeAction[] {
  const hit = findInstance(instanceId)
  return structuredClone(hit.actions)
}

function findInstance(instanceId: string): MockInstance {
  for (const alertId of [...DB.keys()]) {
    const hit = DB.get(alertId)!
    if (hit.instance.instanceId === instanceId) return hit
  }
  throw new Error('流程实例不存在（mock，请先在列表点「流程」启动）')
}

const ACTION_LABEL: Record<FlowNodeActionRequest['action'], string> = {
  confirm: '确认处理', reject: '驳回', transfer: '转派', urge: '加急',
}

/** 节点名取模板 text.value（后端同名口径：查不到给 null，前端不自行编名字） */
function nodeNameOf(hit: MockInstance, nodeId: string): string | null {
  const template = TEMPLATES.get(hit.instance.templateId)
  const nodes = template?.nodes ?? NODES
  return nodes.find((n) => n.id === nodeId)?.text?.value ?? null
}

export function mockSubmitAction(instanceId: string, nodeId: string, data: FlowNodeActionRequest): FlowNodeAction {
  const hit = findInstance(instanceId)
  if (hit.instance.status === 'completed') throw new Error('流程已结束，不可再操作')
  const state = hit.states.find((s) => s.nodeId === nodeId)
  if (!state) throw new Error('节点不属于该实例')
  if ((data.action === 'confirm' || data.action === 'reject') && state.status !== 'current') {
    throw new Error('该节点当前不可处理')
  }
  const now = new Date().toISOString()
  const action: FlowNodeAction = {
    actionId: `ACT_${++seq}`,
    nodeId,
    nodeName: nodeNameOf(hit, nodeId),
    action: data.action,
    actionLabel: ACTION_LABEL[data.action],
    operator: 'doctor_li',
    operatorName: '王医生',
    remark: data.remark ?? null,
    attachments: data.attachments ?? [],
    targetOperator: data.targetOperator ?? null,
    createdAt: now,
  }
  hit.actions.push(structuredClone(action))

  if (data.action === 'confirm') {
    state.status = 'done'
    state.operator = 'doctor_li'
    state.operatorName = '王医生'
    state.operatedAt = now
    state.remark = data.remark ?? null
    const successors = (data.nextNodeIds?.length ? data.nextNodeIds : state.nextNodeIds)
      .map((id) => hit.states.find((s) => s.nodeId === id))
      .filter((s): s is FlowNodeState => !!s)
    successors.forEach((s) => { if (s.status === 'todo') s.status = 'current' })
    hit.instance.currentNodeId = successors[0]?.nodeId ?? null
    if (!successors.length) {
      hit.instance.status = 'completed'
      hit.instance.endedAt = now
      hit.instance.currentNodeId = null
    }
  } else if (data.action === 'reject') {
    state.status = 'skipped'
    state.operator = 'doctor_li'
    state.operatorName = '王医生'
    state.operatedAt = now
    state.remark = data.remark ?? null
  } else if (data.action === 'transfer') {
    if (!data.targetOperator) throw new Error('转派必须指定目标处理人')
    state.assignee = data.targetOperator
    state.assigneeName = data.targetOperator
  }
  return action
}
