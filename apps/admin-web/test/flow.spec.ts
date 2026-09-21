// T275 运行态画布：契约形状 + 着色映射 + 状态机（mock 分支，口径同 docs/api/api-contracts.ts Flow 段）
import { describe, it, expect } from 'vitest'
import {
  fetchFlowTemplates, fetchFlowTemplate, fetchFlowInstancesByAlert, fetchFlowNodeStates,
  fetchFlowInstanceActions, startFlowInstanceApi, submitFlowNodeActionApi,
  type FlowNodeState,
} from '../src/api/flow'
import { buildRuntimeGraph } from '../src/pages/alerts/flow/canvas'

function statusMap(states: FlowNodeState[]): Record<string, string> {
  return Object.fromEntries(states.map((s) => [s.nodeId, s.status]))
}

/** 只给 nodeId/status 的极简状态行，其余字段画布不读 */
function partial(rows: [string, FlowNodeState['status']][]): FlowNodeState[] {
  return rows.map(([nodeId, status]) => ({
    nodeId, status, operator: null, operatorName: null, operatedAt: null,
    remark: null, attachments: [], assignee: null, assigneeName: null, nextNodeIds: [],
  }))
}

describe('流程 API 层（USE_MOCK 契约形状）', () => {
  it('模板列表与详情：节点字段齐备，边用 2.x 的 sourceNodeId/targetNodeId', async () => {
    const templates = await fetchFlowTemplates()
    expect(templates.length).toBeGreaterThan(0)
    const detail = await fetchFlowTemplate(templates[0].templateId)
    expect(detail.nodes.every((n) => n.id && n.type && typeof n.x === 'number')).toBe(true)
    expect(detail.edges.every((e) => e.sourceNodeId && e.targetNodeId)).toBe(true)
  })

  it('按告警查实例：无流程时返回空数组而不是 null', async () => {
    expect(await fetchFlowInstancesByAlert('ALR-NOT-EXIST')).toEqual([])
  })

  it('ALR-001 在途实例：r1/r2 已完成、r3 处理中，时间线按序返回', async () => {
    const list = await fetchFlowInstancesByAlert('ALR-001')
    expect(list).toHaveLength(1)
    expect(list[0].status).toBe('running')
    expect(statusMap(await fetchFlowNodeStates(list[0].instanceId))).toMatchObject({
      r1: 'done', r2: 'done', r3: 'current', r5: 'todo',
    })
    const actions = await fetchFlowInstanceActions(list[0].instanceId)
    expect(actions.map((a) => a.nodeId)).toEqual(['r1', 'r2', 'r3'])
    expect(actions[0].actionLabel).toBe('确认处理')
  })

  it('ALR-002 走完的实例：四色齐备（含 skipped），供画布验图例', async () => {
    const list = await fetchFlowInstancesByAlert('ALR-002')
    expect(list[0].status).toBe('completed')
    expect(statusMap(await fetchFlowNodeStates(list[0].instanceId))).toMatchObject({
      r6: 'skipped', r7: 'done',
    })
  })
})

describe('buildRuntimeGraph 着色与连线高亮', () => {
  it('状态写进节点 properties.status；无状态行回落 todo', async () => {
    const template = await fetchFlowTemplate('FLOW_TMOCK00001')
    const graph = buildRuntimeGraph(template, partial([['r1', 'done']]))
    expect(graph.nodes.find((n) => n.id === 'r1')?.properties.status).toBe('done')
    expect(graph.nodes.find((n) => n.id === 'r5')?.properties.status).toBe('todo')
  })

  it('done→done/current 判为已执行（绿色加粗实线），未到达保持灰，跳过分支灰虚线', async () => {
    const template = await fetchFlowTemplate('FLOW_TMOCK00001')
    const graph = buildRuntimeGraph(template, partial([
      ['r1', 'done'], ['r2', 'done'], ['r3', 'current'], ['r4', 'todo'],
    ]))
    const edge = (id: string) => graph.edges.find((e) => e.id === id)!
    expect(edge('e1').properties.executed).toBe(true)
    expect(edge('e1').properties.style.stroke).toBe('#10B981')
    expect(edge('e1').properties.style.strokeWidth).toBe(2)
    expect(edge('e2').properties.executed).toBe(true)
    expect(edge('e3').properties.executed).toBe(false)
    expect(edge('e3').properties.status).toBe('todo')
  })

  it('端点被跳过时连线走灰色虚线', async () => {
    const template = await fetchFlowTemplate('FLOW_TMOCK00001')
    const graph = buildRuntimeGraph(template, partial([['r4', 'skipped']]))
    const e4 = graph.edges.find((e) => e.id === 'e4')!
    expect(e4.properties.executed).toBe(false)
    expect(e4.properties.style.stroke).toBe('#CBD5E1')
    expect(e4.properties.style.strokeDasharray).toBe('6,4')
  })

  it('触发/判断类节点按 kind 换形状，其余按模板内置类型', async () => {
    const template = await fetchFlowTemplate('FLOW_TMOCK00001')
    const graph = buildRuntimeGraph(template, [])
    expect(graph.nodes.find((n) => n.id === 'r1')?.type).toBe('flow-circle')
    expect(graph.nodes.find((n) => n.id === 'r4')?.type).toBe('flow-diamond')
    expect(graph.nodes.find((n) => n.id === 'r3')?.type).toBe('flow-rect')
  })
})

describe('节点操作状态机（前端不自行推算，一律重拉服务端状态）', () => {
  it('confirm 推进全部后继；判断节点按 nextNodeIds 只走选中的分支', async () => {
    const inst = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-A')
    expect(statusMap(await fetchFlowNodeStates(inst.instanceId))).toMatchObject({ r1: 'current' })

    await submitFlowNodeActionApi(inst.instanceId, 'r1', { action: 'confirm' })
    expect(statusMap(await fetchFlowNodeStates(inst.instanceId))).toMatchObject({ r1: 'done', r2: 'current' })

    await submitFlowNodeActionApi(inst.instanceId, 'r2', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r3', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r4', { action: 'confirm', nextNodeIds: ['r5'] })
    expect(statusMap(await fetchFlowNodeStates(inst.instanceId))).toMatchObject({
      r4: 'done', r5: 'current', r6: 'todo',
    })

    const actions = await fetchFlowInstanceActions(inst.instanceId)
    expect(actions[actions.length - 1]?.nodeName).toBe('判断分支')
  })

  it('归档节点无后继：confirm 后实例置 completed 并写 endedAt', async () => {
    const inst = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-B')
    await submitFlowNodeActionApi(inst.instanceId, 'r1', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r2', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r3', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r4', { action: 'confirm', nextNodeIds: ['r5'] })
    await submitFlowNodeActionApi(inst.instanceId, 'r5', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r7', { action: 'confirm', remark: '已归档' })
    const [after] = await fetchFlowInstancesByAlert('ALR-FLOW-B')
    expect(after.status).toBe('completed')
    expect(after.endedAt).toBeTruthy()
    expect(after.currentNodeId).toBeNull()
  })

  it('reject 只置 skipped 且不推进', async () => {
    const inst = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-C')
    await submitFlowNodeActionApi(inst.instanceId, 'r1', { action: 'reject', remark: '误报' })
    expect(statusMap(await fetchFlowNodeStates(inst.instanceId))).toMatchObject({
      r1: 'skipped', r2: 'todo',
    })
    const actions = await fetchFlowInstanceActions(inst.instanceId)
    expect(actions[actions.length - 1]?.actionLabel).toBe('驳回')
  })

  it('transfer 只改指派不动状态；urge 只留痕', async () => {
    const inst = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-D')
    await submitFlowNodeActionApi(inst.instanceId, 'r1', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r2', { action: 'transfer', targetOperator: 'doctor_zhang' })
    const r2 = (await fetchFlowNodeStates(inst.instanceId)).find((s) => s.nodeId === 'r2')!
    expect(r2.status).toBe('current')
    expect(r2.assignee).toBe('doctor_zhang')

    const before = (await fetchFlowInstanceActions(inst.instanceId)).length
    await submitFlowNodeActionApi(inst.instanceId, 'r3', { action: 'urge', remark: '患者已联系' })
    const actions = await fetchFlowInstanceActions(inst.instanceId)
    expect(actions).toHaveLength(before + 1)
    expect(actions[actions.length - 1]?.actionLabel).toBe('加急')
    expect((await fetchFlowNodeStates(inst.instanceId)).find((s) => s.nodeId === 'r3')?.status).toBe('todo')
  })

  it('非 current 节点不许 confirm；实例结束后不许再操作', async () => {
    const inst = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-E')
    await expect(submitFlowNodeActionApi(inst.instanceId, 'r5', { action: 'confirm' })).rejects.toThrow('该节点当前不可处理')

    await submitFlowNodeActionApi(inst.instanceId, 'r1', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r2', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r3', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r4', { action: 'confirm', nextNodeIds: ['r5'] })
    await submitFlowNodeActionApi(inst.instanceId, 'r5', { action: 'confirm' })
    await submitFlowNodeActionApi(inst.instanceId, 'r7', { action: 'confirm' })
    await expect(submitFlowNodeActionApi(inst.instanceId, 'r1', { action: 'urge' })).rejects.toThrow('流程已结束')
  })

  it('一条告警只有一个实例（重复启动幂等回原实例）', async () => {
    const first = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-F')
    const again = await startFlowInstanceApi('FLOW_TMOCK00001', 'ALR-FLOW-F')
    expect(again.instanceId).toBe(first.instanceId)
    expect(await fetchFlowInstancesByAlert('ALR-FLOW-F')).toHaveLength(1)
  })
})
