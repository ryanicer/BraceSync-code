// T275 2.3 运行态画布的纯映射层：模板图结构 + 节点状态 → 可喂给 lf.render() 的运行态图。
// 单独成文件（不 import @logicflow/core）是必须的：LogicFlow 的 CJS 产物 require 了 lodash-es（ESM），
// CI 的 Node 18 下 vitest 一加载就 ERR_REQUIRE_ESM ⇒ 单测只吃这层，装配层见 canvas.ts。
import type { FlowGraphEdge, FlowGraphNode, FlowNodeState, FlowTemplate } from '../../../api/flow'

export type FlowStatus = 'done' | 'current' | 'todo' | 'skipped'

const STATUSES: readonly string[] = ['done', 'current', 'todo', 'skipped']

export function statusOf(props: unknown): FlowStatus {
  const s = (props as { status?: unknown } | undefined)?.status
  return STATUSES.includes(String(s)) ? (s as FlowStatus) : 'todo'
}

export const FLOW_SHAPE_TYPES = ['flow-rect', 'flow-circle', 'flow-diamond', 'flow-ellipse'] as const
export const FLOW_EDGE_TYPE = 'flow-polyline'

const KIND_TO_SHAPE: Record<string, string> = {
  trigger: 'flow-circle',
  condition: 'flow-diamond',
  join: 'flow-circle',
  parallel: 'flow-diamond',
}

const BUILTIN_TO_FLOW: Record<string, string> = {
  rect: 'flow-rect',
  circle: 'flow-circle',
  diamond: 'flow-diamond',
  ellipse: 'flow-ellipse',
}

/** 模板节点 → 运行态形状：优先按业务类别（properties.kind），其次沿用模板写的内置类型 */
function shapeType(node: FlowGraphNode): string {
  const kind = String(node.properties?.kind ?? '')
  if (KIND_TO_SHAPE[kind]) return KIND_TO_SHAPE[kind]
  if (BUILTIN_TO_FLOW[node.type]) return BUILTIN_TO_FLOW[node.type]
  if ((FLOW_SHAPE_TYPES as readonly string[]).includes(node.type)) return node.type
  return 'flow-rect'
}

export interface RuntimeNode {
  id: string
  type: string
  x: number
  y: number
  text?: { x: number; y: number; value: string }
  properties: Record<string, unknown> & { status?: FlowStatus }
}

export interface RuntimeEdgeStyle {
  stroke: string
  strokeWidth: number
  strokeDasharray?: string
}

export interface RuntimeEdge {
  id: string
  type: string
  sourceNodeId: string
  targetNodeId: string
  text?: { x: number; y: number; value: string }
  properties: Record<string, unknown> & { status?: FlowStatus; executed?: boolean; style: RuntimeEdgeStyle }
}

/**
 * 着色只认 getFlowNodeStates 的 status（契约：并行/汇聚时 current 可能多个）。
 * 连线：done→(done|current) 视为已执行走绿色粗线，两端有 skipped 走灰虚线，其余待处理灰细线。
 */
export function buildRuntimeGraph(
  template: Pick<FlowTemplate, 'nodes' | 'edges'>,
  states: FlowNodeState[],
): { nodes: RuntimeNode[]; edges: RuntimeEdge[] } {
  const statusById = new Map(states.map((s) => [s.nodeId, s.status]))

  const nodes: RuntimeNode[] = template.nodes.map((n) => ({
    id: n.id,
    type: shapeType(n),
    x: n.x,
    y: n.y,
    ...(n.text ? { text: { ...n.text } } : {}),
    properties: { ...n.properties, status: statusById.get(n.id) ?? 'todo' },
  }))

  const edges: RuntimeEdge[] = template.edges.map((e: FlowGraphEdge) => {
    const from = statusById.get(e.sourceNodeId) ?? 'todo'
    const to = statusById.get(e.targetNodeId) ?? 'todo'
    const executed = from === 'done' && (to === 'done' || to === 'current')
    const skipped = from === 'skipped' || to === 'skipped'
    return {
      id: e.id,
      type: FLOW_EDGE_TYPE,
      sourceNodeId: e.sourceNodeId,
      targetNodeId: e.targetNodeId,
      ...(e.text ? { text: { ...e.text } } : {}),
      properties: {
        ...e.properties,
        executed,
        status: executed ? 'done' : skipped ? 'skipped' : 'todo',
        style: {
          stroke: executed ? '#10B981' : skipped ? '#CBD5E1' : '#94A3B8',
          strokeWidth: executed ? 2 : 1.5,
          ...(skipped ? { strokeDasharray: '6,4' } : {}),
        },
      },
    }
  })

  return { nodes, edges }
}

/** 状态 → 中文标签（图例、操作面板共用） */
export const FLOW_STATUS_LABEL: Record<FlowStatus, string> = {
  done: '已完成',
  current: '处理中',
  todo: '待处理',
  skipped: '已跳过',
}
