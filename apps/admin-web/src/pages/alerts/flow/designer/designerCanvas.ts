// T276 2.4 设计器的 LogicFlow 装配层：design-* 节点（按业务类别着色 + 声明式连接规则）。
// 纯映射（连接规则本体、序列化）在 kinds.ts —— 那层不 import LogicFlow，单测才跑得起来
// （LogicFlow 的 CJS 产物 require 了 ESM 的 lodash-es，CI 的 Node 18 下加载即 ERR_REQUIRE_ESM）。
// 与运行态画布（canvas.ts）分开注册：运行态按节点状态着色、设计态按类别着色，两套类名互不影响。
import LogicFlow, {
  CircleNode, DiamondNode, RectNode,
  CircleNodeModel, DiamondNodeModel, RectNodeModel,
} from '@logicflow/core'
import type { BaseNodeModel, Model } from '@logicflow/core'
import { canConnect, DESIGN_SHAPE_TYPE, kindDef, kindOf } from './kinds'

/** 节点描边/底色按类别（设计稿 FC_NODE_TYPES.color） */
function designStyle(props: Record<string, unknown> | undefined): Record<string, string | number> {
  const def = kindDef(kindOf(props?.kind))
  return { stroke: def.color, strokeWidth: 2, fill: '#FFFFFF' }
}

/** 类别写进外层 g 的 class 与 data-*，供样式与 e2e 取证用（同运行态的 is-<status> 口径）。
 * invalid 是结构校验标（§6.2），只在设计器内存里，保存前会被 stripRuntimeProps 剥掉。 */
function designOuterG(props: Record<string, unknown> | undefined): LogicFlow.DomAttributes {
  const kind = kindOf(props?.kind)
  const invalid = props?.invalid === true ? ' is-invalid' : ''
  return { className: `lf-design-node is-${kind}${invalid}`, 'data-flow-kind': kind }
}

/**
 * 一条规则覆盖全部三种判定，消息由 canConnect 现算，避免规则本体在两处漂移。
 * 注意：必须把规则 push 进 super 返回的同一个数组 —— LogicFlow 用 hasSetSourceRules
 * 记一次状态，之后只读 this.sourceRules，返回新数组会让规则第二次连线起就失效。
 */
function designConnectRules(host: BaseNodeModel): Model.ConnectRule[] {
  const cached = (host as { _designRule?: Model.ConnectRule })._designRule
  if (cached) return [cached]
  const rule: Model.ConnectRule = {
    message: '不允许的连接',
    validate: (source?: BaseNodeModel, target?: BaseNodeModel) => {
      if (!source || !target) return true
      const verdict = canConnect(
        kindOf(source.properties?.kind),
        kindOf(target.properties?.kind),
        source.id === target.id,
      )
      rule.message = verdict.message || '不允许的连接'
      return verdict.allowed
    },
  }
  ;(host as { _designRule?: Model.ConnectRule })._designRule = rule
  return [rule]
}

function installDesignRules(host: BaseNodeModel, rules: Model.ConnectRule[]): Model.ConnectRule[] {
  const [rule] = designConnectRules(host)
  if (!rules.includes(rule)) rules.push(rule)
  return rules
}

class DesignRectModel extends RectNodeModel {
  getNodeStyle() {
    return designStyle(this.properties)
  }
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return designOuterG(this.properties)
  }
  getConnectedSourceRules(): Model.ConnectRule[] {
    return installDesignRules(this, super.getConnectedSourceRules())
  }
  getConnectedTargetRules(): Model.ConnectRule[] {
    return installDesignRules(this, super.getConnectedTargetRules())
  }
}

class DesignCircleModel extends CircleNodeModel {
  getNodeStyle() {
    return designStyle(this.properties)
  }
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return designOuterG(this.properties)
  }
  getConnectedSourceRules(): Model.ConnectRule[] {
    return installDesignRules(this, super.getConnectedSourceRules())
  }
  getConnectedTargetRules(): Model.ConnectRule[] {
    return installDesignRules(this, super.getConnectedTargetRules())
  }
}

class DesignDiamondModel extends DiamondNodeModel {
  getNodeStyle() {
    return designStyle(this.properties)
  }
  getOuterGAttributes(): LogicFlow.DomAttributes {
    return designOuterG(this.properties)
  }
  getConnectedSourceRules(): Model.ConnectRule[] {
    return installDesignRules(this, super.getConnectedSourceRules())
  }
  getConnectedTargetRules(): Model.ConnectRule[] {
    return installDesignRules(this, super.getConnectedTargetRules())
  }
}

/** 注册设计器自定义节点类型（必须在 lf.render() 之前调用）。
 * 连线不注册自定义类型：设计态无动画/着色需求，直接用 LogicFlow 内置 polyline，落库也更通用。 */
export function registerDesignElements(lf: LogicFlow): void {
  lf.register({ type: DESIGN_SHAPE_TYPE.rect, view: RectNode, model: DesignRectModel })
  lf.register({ type: DESIGN_SHAPE_TYPE.circle, view: CircleNode, model: DesignCircleModel })
  lf.register({ type: DESIGN_SHAPE_TYPE.diamond, view: DiamondNode, model: DesignDiamondModel })
}
