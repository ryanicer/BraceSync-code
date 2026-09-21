// T276 2.4 设计器保存前的全图结构校验（依据 T285 §6.2 的 V1-V13）。
// 🔴 这一层是「结构正确性的唯一防线」：后端 parseFlowGraph 只挡自环、量程、悬空边（§6.1 B1-B6），
// 环、不可达、无出口、判断节点没有分支名它全部原样存 —— 存下去的后果要到 2.3 推进流程时才炸，
// 而且不报错（V6 会让实例被误判为已完成）。所以 error 级必须在前端拦住。
//
// 与 kinds.ts 同理由独立成纯文件：不 import @logicflow/core（CI 的 Node 18 下会 ERR_REQUIRE_ESM）。
import { kindOf, nodeName, type FlowKind, type RawEdge, type RawGraph, type RawNode } from './kinds'

export type IssueLevel = 'error' | 'warning'

export type IssueCode =
  | 'V1_EMPTY_GRAPH' | 'V2_NO_TRIGGER' | 'V3_MULTI_TRIGGER' | 'V4_CYCLE' | 'V5_UNREACHABLE'
  | 'V6_NO_OUTGOING' | 'V7_CONDITION_EDGES' | 'V8_JOIN_ARITY' | 'V9_NO_ARCHIVE'
  | 'V10_PARALLEL_ARITY' | 'V11_DELAY_UNSET' | 'V12_OVER_LIMIT' | 'V13_EDGE_DANGLING'

export interface Issue {
  code: IssueCode
  level: IssueLevel
  message: string
  nodeIds?: string[]
  edgeIds?: string[]
}

/** 量程与后端同源（flow_t274.go:41-49），前端早拦，别等画完 100 个节点才吃 400 */
export const GRAPH_LIMITS = { nodes: 100, edges: 300, nodeId: 64, name: 64 }

export interface ValidateResult {
  errors: Issue[]
  warnings: Issue[]
}

function edgeText(e: RawEdge): string {
  return (typeof e.text === 'string' ? e.text : (e.text?.value ?? '')).trim()
}

function kindOfNode(n: RawNode): FlowKind {
  return kindOf(n.properties?.kind)
}

/** DFS 三色标记找长度 >1 的环；自环单独归到 V4（后端也拒） */
function findCycle(nodeIds: string[], out: Map<string, string[]>): string[] | null {
  const WHITE = 0; const GRAY = 1; const BLACK = 2
  const color = new Map<string, number>(nodeIds.map((id) => [id, WHITE]))
  const stack: string[] = []

  const visit = (id: string): string[] | null => {
    color.set(id, GRAY)
    stack.push(id)
    for (const next of out.get(id) ?? []) {
      const c = color.get(next)
      if (c === GRAY) return stack.slice(stack.indexOf(next))
      if (c === WHITE) {
        const found = visit(next)
        if (found) return found
      }
    }
    stack.pop()
    color.set(id, BLACK)
    return null
  }

  for (const id of nodeIds) {
    if (color.get(id) === WHITE) {
      const found = visit(id)
      if (found) return found
    }
  }
  return null
}

/**
 * 保存前全图校验。errors 阻止保存，warnings 只提示（由调用方弹「仍要保存？」）。
 * 只做判定不改图：T285 §6.2 明确「不做自动修复」，改动权在操作人。
 */
export function validateGraph(graph: RawGraph, templateName = ''): ValidateResult {
  const nodes = graph.nodes ?? []
  const edges = graph.edges ?? []
  const errors: Issue[] = []
  const warnings: Issue[] = []
  const err = (code: IssueCode, message: string, extra: Partial<Issue> = {}) =>
    errors.push({ code, level: 'error', message, ...extra })
  const warn = (code: IssueCode, message: string, extra: Partial<Issue> = {}) =>
    warnings.push({ code, level: 'warning', message, ...extra })

  // V12 量程（超限后其余判定意义不大，但仍然跑完，一次把问题给全）
  const limitHits: string[] = []
  if (nodes.length > GRAPH_LIMITS.nodes) limitHits.push(`节点 ${nodes.length} 个，上限 ${GRAPH_LIMITS.nodes}`)
  if (edges.length > GRAPH_LIMITS.edges) limitHits.push(`连线 ${edges.length} 条，上限 ${GRAPH_LIMITS.edges}`)
  const longIds = nodes.filter((n) => n.id.length > GRAPH_LIMITS.nodeId).map((n) => n.id)
  if (longIds.length) limitHits.push(`节点 id 超过 ${GRAPH_LIMITS.nodeId} 字符`)
  if (templateName.length > GRAPH_LIMITS.name) limitHits.push(`模板名超过 ${GRAPH_LIMITS.name} 字符`)
  if (limitHits.length) err('V12_OVER_LIMIT', limitHits.join('；'), { nodeIds: longIds })

  // V13 悬空边（后端 B4 同规则，前端早拦）
  const ids = new Set(nodes.map((n) => n.id))
  const dangling = edges.filter((e) => !ids.has(e.sourceNodeId) || !ids.has(e.targetNodeId))
  if (dangling.length) {
    err('V13_EDGE_DANGLING', `${dangling.length} 条连线指向了不存在的节点`, {
      edgeIds: dangling.map((e, i) => e.id ?? `edge_${i}`),
    })
  }

  if (!nodes.length) {
    err('V1_EMPTY_GRAPH', '画布为空，至少需要一个触发节点')
    return { errors, warnings }
  }

  const out = new Map<string, string[]>()
  const indeg = new Map<string, number>()
  const outEdges = new Map<string, RawEdge[]>()
  for (const n of nodes) { outEdges.set(n.id, []) }
  for (const e of edges) {
    if (!ids.has(e.sourceNodeId) || !ids.has(e.targetNodeId)) continue
    out.set(e.sourceNodeId, [...(out.get(e.sourceNodeId) ?? []), e.targetNodeId])
    outEdges.get(e.sourceNodeId)?.push(e)
    indeg.set(e.targetNodeId, (indeg.get(e.targetNodeId) ?? 0) + 1)
  }
  const outgoing = (id: string) => out.get(id) ?? []
  const label = (id: string) => {
    const name = nodeName(nodes.find((n) => n.id === id))
    return name || id
  }

  const byKind = (kind: FlowKind) => nodes.filter((n) => kindOfNode(n) === kind)
  const triggers = byKind('trigger')
  const archives = byKind('archive')

  // V2 / V3 入口
  if (!triggers.length) err('V2_NO_TRIGGER', '没有触发节点，流程没有入口')
  if (triggers.length > 1) {
    err('V3_MULTI_TRIGGER', `触发节点有 ${triggers.length} 个，实例启动会同时跑多条主流程`, {
      nodeIds: triggers.map((t) => t.id),
    })
  }

  // V4 环（后端只挡自环；有环则 confirm 能把流程推进到已完成的节点上，之后再也推不动）
  const cycle = findCycle(nodes.map((n) => n.id), out)
  if (cycle) err('V4_CYCLE', `存在回路：${cycle.map(label).join(' → ')} → ${label(cycle[0])}`, { nodeIds: cycle })

  // V5 不可达（从全部触发节点出发 BFS）
  if (triggers.length) {
    const seen = new Set<string>(triggers.map((t) => t.id))
    const queue = [...seen]
    while (queue.length) {
      for (const next of outgoing(queue.shift() as string)) {
        if (!seen.has(next)) { seen.add(next); queue.push(next) }
      }
    }
    const orphans = nodes.filter((n) => !seen.has(n.id))
    if (orphans.length) {
      err('V5_UNREACHABLE', `${orphans.length} 个节点从触发节点走不到，状态会永远停在待处理`, {
        nodeIds: orphans.map((n) => n.id),
      })
    }
  }

  // V6 非归档且无出边 = 流程在这里被误判为「走完」
  const sinks = nodes.filter((n) => kindOfNode(n) !== 'archive' && !outgoing(n.id).length)
  if (sinks.length) {
    err('V6_NO_OUTGOING', `${sinks.length} 个节点没有连出归档节点，流程会在中途判为已完成`, {
      nodeIds: sinks.map((n) => n.id),
    })
  }

  // V7 判断节点：出边 ≥2 且每条出边要有分支名（2.3 的分支弹窗就显示这个文本）
  for (const c of byKind('condition')) {
    const outs = outEdges.get(c.id) ?? []
    if (outs.length < 2) {
      err('V7_CONDITION_EDGES', `判断节点「${label(c.id)}」只有 ${outs.length} 条出边，没有分支可选`, { nodeIds: [c.id] })
      continue
    }
    const unnamed = outs.filter((e) => !edgeText(e))
    if (unnamed.length) {
      err('V7_CONDITION_EDGES', `判断节点「${label(c.id)}」有 ${unnamed.length} 条出边没填分支名`, {
        nodeIds: [c.id],
        edgeIds: unnamed.map((e) => e.id ?? ''),
      })
    }
  }

  // V8-V11 编排完整性提示（不阻断：编排中途很常见）
  for (const j of byKind('join')) {
    const deg = indeg.get(j.id) ?? 0
    if (deg < 2) warn('V8_JOIN_ARITY', `汇聚节点「${label(j.id)}」入边只有 ${deg} 条，等同于普通节点`, { nodeIds: [j.id] })
  }
  // V9 的附带文案是 §4.1 要求的：模板改了不回溯在途实例，但操作人得知道节点名会变 null
  if (!archives.length) warn('V9_NO_ARCHIVE', '没有归档节点，流程只能停在最后一个处理节点；'
    + '改或删除已被流程实例引用的节点，在途实例上该节点的名字会回空（模板不回溯在途实例）')
  for (const p of byKind('parallel')) {
    const deg = outgoing(p.id).length
    if (deg < 2) warn('V10_PARALLEL_ARITY', `并行节点「${label(p.id)}」出边只有 ${deg} 条，没有真正分叉`, { nodeIds: [p.id] })
  }
  for (const d of byKind('delay')) {
    const minutes = Number(d.properties?.delayMinutes)
    if (!Number.isFinite(minutes) || minutes <= 0) {
      warn('V11_DELAY_UNSET', `延时节点「${label(d.id)}」没设延时时长`, { nodeIds: [d.id] })
    }
  }

  return { errors, warnings }
}

/** 问题命中的节点 id 全集（画布上标 is-invalid 用） */
export function issueNodeIds(result: ValidateResult): string[] {
  const ids = new Set<string>()
  for (const issue of [...result.errors, ...result.warnings]) {
    for (const id of issue.nodeIds ?? []) ids.add(id)
  }
  return [...ids]
}
