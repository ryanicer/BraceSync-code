// T275 流程画布 API 层（2.3 运行态）。
// 契约权威：docs/api/api-contracts.ts「Flow Canvas」段（T274 / code #139 已落端点）。
// 类型在本模块内声明而非 packages/shared-types —— 本卡边界只允许改 apps/admin-web/**。
import { USE_MOCK, request } from '../utils/request'
import * as flowMock from '../mock/flow'

export type FlowNodeStatus = 'done' | 'current' | 'todo' | 'skipped'
export type FlowActionType = 'confirm' | 'reject' | 'transfer' | 'urge'
export type FlowInstanceStatus = 'running' | 'completed' | 'terminated'

export interface FlowGraphNode {
  id: string
  type: string
  x: number
  y: number
  properties: Record<string, unknown> & { kind?: string; width?: number; height?: number }
  text?: { x: number; y: number; value: string }
}

export interface FlowGraphEdge {
  id: string
  type: string
  sourceNodeId: string
  targetNodeId: string
  properties: Record<string, unknown>
  text?: { x: number; y: number; value: string }
}

export interface FlowTemplate {
  templateId: string
  name: string
  nodes: FlowGraphNode[]
  edges: FlowGraphEdge[]
  creator: string
  creatorName: string | null
  version: number
  createdAt: string
  updatedAt: string
  instanceCount: number
}

export interface FlowInstance {
  instanceId: string
  templateId: string
  templateName: string
  alertId: string
  currentNodeId: string | null
  status: FlowInstanceStatus
  startedAt: string
  endedAt: string | null
}

export interface FlowNodeState {
  nodeId: string
  status: FlowNodeStatus
  operator: string | null
  operatorName: string | null
  operatedAt: string | null
  remark: string | null
  attachments: string[]
  assignee: string | null
  assigneeName: string | null
  nextNodeIds: string[]
}

export interface FlowNodeAction {
  actionId: string
  nodeId: string
  nodeName: string | null
  action: FlowActionType
  actionLabel: string
  operator: string
  operatorName: string | null
  remark: string | null
  attachments: string[]
  targetOperator: string | null
  createdAt: string
}

export interface FlowNodeActionRequest {
  action: FlowActionType
  remark?: string
  attachments?: string[]
  targetOperator?: string
  nextNodeIds?: string[]
}

async function delay(ms = 150): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms))
}

/** 可用模板（0 实例时供操作人选择启动） */
export async function fetchFlowTemplates(): Promise<FlowTemplate[]> {
  if (USE_MOCK) { await delay(); return flowMock.mockListTemplates() }
  const res = await request<{ list: FlowTemplate[]; total: number }>({ url: '/api/v1/admin/flow/templates', data: { pageSize: 100 } })
  return res.list
}

export async function fetchFlowTemplate(templateId: string): Promise<FlowTemplate> {
  if (USE_MOCK) { await delay(); return flowMock.mockGetTemplate(templateId) }
  return request<FlowTemplate>({ url: `/api/v1/admin/flow/templates/${encodeURIComponent(templateId)}` })
}

/** 按告警查实例（正常 0 或 1 条） */
export async function fetchFlowInstancesByAlert(alertId: string): Promise<FlowInstance[]> {
  if (USE_MOCK) { await delay(); return flowMock.mockListInstances(alertId) }
  const res = await request<{ list: FlowInstance[] }>({ url: '/api/v1/admin/flow/instances', data: { alertId } })
  return res.list
}

export async function startFlowInstanceApi(templateId: string, alertId: string): Promise<FlowInstance> {
  if (USE_MOCK) { await delay(); return flowMock.mockStartInstance(templateId, alertId) }
  return request<FlowInstance>({ url: '/api/v1/admin/flow/instances', method: 'POST', data: { templateId, alertId } })
}

export async function fetchFlowNodeStates(instanceId: string): Promise<FlowNodeState[]> {
  if (USE_MOCK) { await delay(); return flowMock.mockNodeStates(instanceId) }
  const res = await request<{ list: FlowNodeState[] }>({ url: `/api/v1/admin/flow/instances/${encodeURIComponent(instanceId)}/nodes` })
  return res.list
}

export async function fetchFlowInstanceActions(instanceId: string): Promise<FlowNodeAction[]> {
  if (USE_MOCK) { await delay(); return flowMock.mockInstanceActions(instanceId) }
  const res = await request<{ list: FlowNodeAction[] }>({ url: `/api/v1/admin/flow/instances/${encodeURIComponent(instanceId)}/actions` })
  return res.list
}

export async function submitFlowNodeActionApi(instanceId: string, nodeId: string, data: FlowNodeActionRequest): Promise<FlowNodeAction> {
  if (USE_MOCK) { await delay(); return flowMock.mockSubmitAction(instanceId, nodeId, data) }
  return request<FlowNodeAction>({
    url: `/api/v1/admin/flow/instances/${encodeURIComponent(instanceId)}/nodes/${encodeURIComponent(nodeId)}/actions`,
    method: 'POST',
    data: data as unknown as Record<string, unknown>,
  })
}
