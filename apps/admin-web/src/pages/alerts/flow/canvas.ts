// T275 2.3 运行态画布：LogicFlow 装配层（注册自定义节点/边类型）。
// 状态着色的实现口径来自本卡 Spike 结论（T275 卡内评论）：
// 不用 vue-node-registry（setProperties 不驱动 Vue 子应用重渲染），
// 改为自定义 model 重写 getOuterGAttributes 输出 is-<status> 类名，颜色与脉冲写在 CSS。
// 纯映射（形状/着色）在 flowGraph.ts —— 那层不 import LogicFlow，单测才跑得起来
// （LogicFlow 的 CJS 产物 require 了 ESM 的 lodash-es，CI 的 Node 18 下加载即 ERR_REQUIRE_ESM）。
import LogicFlow, {
  CircleNode, DiamondNode, EllipseNode, PolylineEdge, RectNode,
  CircleNodeModel, DiamondNodeModel, EllipseNodeModel, PolylineEdgeModel, RectNodeModel,
} from '@logicflow/core'
import { FLOW_EDGE_TYPE, statusOf } from './flowGraph'

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

/** 注册运行态自定义节点/边类型（必须在 lf.render() 之前调用） */
export function registerFlowElements(lf: LogicFlow): void {
  lf.register({ type: 'flow-rect', view: RectNode, model: FlowRectModel })
  lf.register({ type: 'flow-circle', view: CircleNode, model: FlowCircleModel })
  lf.register({ type: 'flow-diamond', view: DiamondNode, model: FlowDiamondModel })
  lf.register({ type: 'flow-ellipse', view: EllipseNode, model: FlowEllipseModel })
  lf.register({ type: FLOW_EDGE_TYPE, view: PolylineEdge, model: FlowEdgeModel })
}
