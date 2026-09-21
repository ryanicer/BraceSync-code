// T275 2.3 运行态画布：LogicFlow 装配层。
// 状态着色的实现口径来自本卡 Spike 结论（T275 卡内评论）：
// 不用 vue-node-registry（setProperties 不驱动 Vue 子应用重渲染），
// 改为自定义 model 重写 getOuterGAttributes 输出 is-<status> 类名，颜色与脉冲写在 CSS。
import LogicFlow, {
  CircleNode, DiamondNode, EllipseNode, PolylineEdge, RectNode,
  CircleNodeModel, DiamondNodeModel, EllipseNodeModel, PolylineEdgeModel, RectNodeModel,
} from '@logicflow/core'
import type { FlowGraphEdge, FlowGraphNode, FlowNodeState, FlowTemplate } from '../../../api/flow'

export type FlowStatus = 'done' | 'current' | 'todo' | 'skipped'

const STATUSES: readonly string[] = ['done', 'current', 'todo', 'skipped']

function statusOf(props: unknown): FlowStatus {
  const s = (props as { status?: unknown } | undefined)?.status
  return STATUSES.includes(String(s)) ? (s as FlowStatus) : 'todo'
}

/** 四个形状共用：把状态写进外层 g 的 class，颜色与脉冲动画由页面 CSS 决定 */
function flowOuterG(props: Record<string, unknown>): LogicFlow.DomAttributes {
  const status = statusOf(props)
  return { className: `lf-flow-node is-${status}`, 'data-flow-status': status }
}

class FlowRectModel extends RectNodeModel {
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return flowOuterG(this.properties)
  }
}
class FlowCircleModel extends CircleNodeModel {
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return flowOuterG(this.properties)
  }
}
class FlowDiamondModel extends DiamondNodeModel {
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return flowOuterG(this.properties)
  }
}
class FlowEllipseModel extends EllipseNodeModel {
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return flowOuterG(this.properties)
  }
}

/** 连线：动画态默认取主题蓝，会盖掉我们写入的绿色，这里让动画也吃 properties.style 的色 */
class FlowEdgeModel extends PolylineEdgeModel {
  getEdgeAnimationStyle() {
    const style = super.getEdgeAnimationStyle()
    const stroke = (this.properties?.style as { stroke?: string } | undefined)?.stroke
    if (stroke) style.stroke = stroke
    return style
  }
}

export const FLOW_SHAPE_TYPES = ['flow-rect', 'flow-circle', 'flow-diamond', 'flow-ellipse'] as const
export const FLOW_EDGE_TYPE = 'flow-polyline'

/** 注册运行态自定义节点/边类型（必须在 lf.render() 之前调用） */
export function registerFlowElements(lf: LogicFlow): void {
  lf.register({ type: 'flow-rect', view: RectNode, model: FlowRectModel })
  lf.register({ type: 'flow-circle', view: CircleNode, model: FlowCircleModel })
  lf.register({ type: 'flow-diamond', view: DiamondNode, model: FlowDiamondModel })
  lf.register({ type: 'flow-ellipse', view: EllipseNode, model: FlowEllipseModel })
  lf.register({ type: FLOW_EDGE_TYPE, view: PolylineEdge, model: FlowEdgeModel })
}

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

export interface RuntimeEdge {
  id: string
  type: string
  sourceNodeId: string
  targetNodeId: string
  text?: { x: number; y: number; value: string }
  properties: Record<string, unknown> & { status?: FlowStatus; executed?: boolean; style: RuntimeEdgeStyle }
}

export interface RuntimeEdgeStyle {
  stroke: string
  strokeWidth: number
  strokeDasharray?: string
}

/**
 * 模板图结构 + 节点状态 → 可直接喂给 lf.render() 的运行态图。
 * 着色只认 getFlowNodeStates 的 status（契约：并行/汇聚时 current 可能多个）。
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
