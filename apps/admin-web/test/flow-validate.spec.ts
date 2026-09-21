// T285 §6.2 结构校验（V1-V13）逐码用例 —— 设计器保存前的唯一防线。
// 为什么这一层必须有单测：后端 parseFlowGraph 只挡自环/量程/悬空边，环、不可达、缺出边它原样存，
// 存下去要到 2.3 推进流程时才炸（V6 更坏：实例被误判为已完成）。
// 同 kinds.ts：不 import @logicflow/core，否则 CI 的 Node 18 会 ERR_REQUIRE_ESM 让整个 spec 零收集。
import { describe, it, expect } from 'vitest'
import {
  GRAPH_LIMITS, issueNodeIds, validateGraph, type IssueCode, type ValidateResult,
} from '../src/pages/alerts/flow/designer/validateGraph'
import type { FlowKind, RawEdge, RawGraph, RawNode } from '../src/pages/alerts/flow/designer/kinds'

function node(id: string, kind: FlowKind, extra: Record<string, unknown> = {}): RawNode {
  return { id, type: 'rect', x: 0, y: 0, properties: { kind, ...extra }, text: { x: 0, y: 0, value: id } }
}

function edge(id: string, from: string, to: string, text = ''): RawEdge {
  return {
    id, type: 'polyline', sourceNodeId: from, targetNodeId: to, properties: {},
    ...(text ? { text: { x: 0, y: 0, value: text } } : {}),
  }
}

/** 无错无警的基准图：触发 → 判断（两条具名分支）→ 处理/通知 → 归档。
 * 返回值刻意把 nodes/edges 收成必填数组 —— 用例要在基准图上做增删，RawGraph 的可选字段会让 TS 到处报错。 */
function okGraph(): { nodes: RawNode[]; edges: RawEdge[] } {
  return {
    nodes: [node('t', 'trigger'), node('c', 'condition'), node('p', 'process'), node('n', 'notice'), node('a', 'archive')],
    edges: [edge('e1', 't', 'c'), edge('e2', 'c', 'p', '压力过高'), edge('e3', 'c', 'n', '正常'), edge('e4', 'p', 'a'), edge('e5', 'n', 'a')],
  }
}

function codes(r: ValidateResult): IssueCode[] {
  return [...r.errors.map((i) => i.code), ...r.warnings.map((i) => i.code)]
}

function find(r: ValidateResult, code: IssueCode) {
  return [...r.errors, ...r.warnings].find((i) => i.code === code)
}

describe('V1-V13 判定', () => {
  it('基准图：无错无警', () => {
    const r = validateGraph(okGraph(), '默认告警流程')
    expect(r).toEqual({ errors: [], warnings: [] })
  })

  it('V1 空图：只报 V1 并立即返回（后面的拓扑判定对空图没有意义）', () => {
    const r = validateGraph({ nodes: [], edges: [] })
    expect(codes(r)).toEqual(['V1_EMPTY_GRAPH'])
    expect(r.errors[0].level).toBe('error')
  })

  it('V2 没有触发节点', () => {
    const r = validateGraph({ nodes: [node('p', 'process'), node('a', 'archive')], edges: [edge('e1', 'p', 'a')] })
    expect(codes(r)).toEqual(['V2_NO_TRIGGER'])
  })

  it('V3 多触发：两个入口会把实例推成两条主流程，nodeIds 给全', () => {
    const g = okGraph()
    g.nodes.push(node('t2', 'trigger'), node('x', 'process'))
    g.edges.push(edge('e6', 't2', 'x'), edge('e7', 'x', 'a'))
    const r = validateGraph(g)
    expect(find(r, 'V3_MULTI_TRIGGER')?.nodeIds).toEqual(['t', 't2'])
  })

  it('V4 环（后端只挡自环，环必须前端拦）：nodeIds 给出成环链路', () => {
    const g = okGraph()
    // n → c 的反向边：c → n → c
    g.edges.push(edge('e6', 'n', 'c'))
    const r = validateGraph(g)
    const issue = find(r, 'V4_CYCLE')
    expect(issue?.level).toBe('error')
    expect(issue?.nodeIds).toEqual(['c', 'n'])
    expect(issue?.message).toBe('存在回路：c → n → c')
  })

  it('V4 判断节点的分支也参与环检测（demo 数据 r6→r3 就是这种回炉）', () => {
    const g: RawGraph = {
      nodes: [node('t', 'trigger'), node('c', 'condition'), node('p', 'process'), node('d', 'delay', { delayMinutes: 60 }), node('a', 'archive')],
      edges: [edge('e1', 't', 'c'), edge('e2', 'c', 'p', '是'), edge('e3', 'c', 'a', '否'), edge('e4', 'p', 'd'), edge('e5', 'd', 'c', '重新判断')],
    }
    const r = validateGraph(g)
    expect(find(r, 'V4_CYCLE')?.nodeIds).toEqual(['c', 'p', 'd'])
  })

  it('V5 不可达节点：走不到的节点状态行永远停在待处理', () => {
    const g = okGraph()
    g.nodes.push(node('orphan', 'process'), node('orphan2', 'archive'))
    g.edges.push(edge('e6', 'orphan', 'orphan2'))
    const r = validateGraph(g)
    expect(find(r, 'V5_UNREACHABLE')?.nodeIds).toEqual(['orphan', 'orphan2'])
  })

  it('V6 非归档节点没有出边：会被误判成整条流程走完', () => {
    const g = okGraph()
    g.edges = g.edges.filter((e) => e.id !== 'e4')
    const r = validateGraph(g)
    expect(find(r, 'V6_NO_OUTGOING')?.nodeIds).toEqual(['p'])
  })

  it('V7 判断节点出边不足 2 条', () => {
    const g = okGraph()
    g.edges = g.edges.filter((e) => e.id !== 'e3')
    const r = validateGraph(g)
    const issue = find(r, 'V7_CONDITION_EDGES')
    expect(issue?.nodeIds).toEqual(['c'])
    expect(issue?.message).toContain('只有 1 条出边')
  })

  it('V7 判断节点出边够但没有分支名：edgeIds 指到没填的那条', () => {
    const g = okGraph()
    g.edges[2] = edge('e3', 'c', 'n')
    const r = validateGraph(g)
    const issue = find(r, 'V7_CONDITION_EDGES')
    expect(issue?.edgeIds).toEqual(['e3'])
    expect(issue?.message).toContain('没填分支名')
  })

  it('V8 汇聚节点入边 <2 ⇒ warning（不阻断）', () => {
    const g = okGraph()
    g.nodes.push(node('j', 'join'))
    g.edges.push(edge('e6', 'p', 'j'), edge('e7', 'j', 'a'))
    const r = validateGraph(g)
    expect(codes(r)).toEqual(['V8_JOIN_ARITY'])
    expect(find(r, 'V8_JOIN_ARITY')?.level).toBe('warning')
  })

  it('V9 没有归档节点 ⇒ warning，文案按 §4.1 点明「在途实例该节点名回空」', () => {
    const g = okGraph()
    g.nodes = g.nodes.filter((n) => n.id !== 'a')
    g.edges = g.edges.filter((e) => !e.targetNodeId.includes('a'))
    const r = validateGraph(g)
    const issue = find(r, 'V9_NO_ARCHIVE')
    expect(issue?.level).toBe('warning')
    expect(issue?.message).toContain('在途实例')
  })

  it('V10 并行节点出边 <2 ⇒ warning', () => {
    const g = okGraph()
    g.nodes.push(node('pl', 'parallel'))
    g.edges.push(edge('e6', 'pl', 'a'), edge('e7', 'p', 'pl'))
    const r = validateGraph(g)
    expect(codes(r)).toEqual(['V10_PARALLEL_ARITY'])
  })

  it('V11 延时节点没填延时时长 ⇒ warning；0 与缺失同罪', () => {
    const mk = (props: Record<string, unknown>) => ({
      nodes: [node('t', 'trigger'), node('d', 'delay', props), node('a', 'archive')],
      edges: [edge('e1', 't', 'd'), edge('e2', 'd', 'a')],
    })
    expect(codes(validateGraph(mk({})))).toEqual(['V11_DELAY_UNSET'])
    expect(codes(validateGraph(mk({ delayMinutes: 0 })))).toEqual(['V11_DELAY_UNSET'])
    expect(codes(validateGraph(mk({ delayMinutes: 360 })))).toEqual([])
  })

  it('V12 量程与后端同源（节点/边/id/模板名）', () => {
    expect(GRAPH_LIMITS).toEqual({ nodes: 100, edges: 300, nodeId: 64, name: 64 })
    const many = Array.from({ length: GRAPH_LIMITS.nodes + 1 }, (_, i) => node(`n${i}`, 'process'))
    const r1 = validateGraph({ nodes: many, edges: [] })
    expect(find(r1, 'V12_OVER_LIMIT')?.message).toContain('节点 101 个，上限 100')

    const r2 = validateGraph({ nodes: [node('x'.repeat(65), 'process')], edges: [] })
    expect(find(r2, 'V12_OVER_LIMIT')?.nodeIds).toEqual(['x'.repeat(65)])

    const r3 = validateGraph(okGraph(), '名'.repeat(65))
    expect(find(r3, 'V12_OVER_LIMIT')?.message).toContain('模板名超过 64')
  })

  it('V13 悬空边：端点指向不存在的节点（后端 B4 同规则，前端早拦）', () => {
    const g = okGraph()
    g.edges.push(edge('e9', 'p', 'ghost'))
    const r = validateGraph(g)
    expect(find(r, 'V13_EDGE_DANGLING')?.edgeIds).toEqual(['e9'])
    // 悬空边不参与拓扑计算，不能因为它把可达性算错
    expect(codes(r)).not.toContain('V5_UNREACHABLE')
  })

  it('id 缺失的悬空边也有兜底标识（右栏要点得中）', () => {
    const r = validateGraph({ nodes: [node('t', 'trigger')], edges: [{ sourceNodeId: 't', targetNodeId: 'nope', properties: {} }] })
    expect(find(r, 'V13_EDGE_DANGLING')?.edgeIds).toEqual(['edge_0'])
  })
})

describe('校验器自身的边界', () => {
  it('只报不改：不复制也不修改入参', () => {
    const g = okGraph()
    const snapshot = JSON.stringify(g)
    validateGraph(g)
    expect(JSON.stringify(g)).toBe(snapshot)
  })

  it('多个问题命中同一节点时 issueNodeIds 去重（画布标红不重复刷属性）', () => {
    const g = okGraph()
    g.edges = g.edges.filter((e) => e.id !== 'e3') // 判断节点既少出边（V7）又让 n 不可达（V5）
    const r = validateGraph(g)
    const ids = issueNodeIds(r)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toContain('c')
  })

  it('错误与提示分栏：errors 阻断保存，warnings 只弹确认', () => {
    const g = okGraph()
    g.nodes.push(node('j', 'join'))
    g.edges = g.edges.filter((e) => e.id !== 'e4' && e.id !== 'e5')
    g.edges.push(edge('e6', 'n', 'j'), edge('e7', 'j', 'a'))
    const r = validateGraph(g)
    expect(r.errors.map((i) => i.code)).toEqual(['V6_NO_OUTGOING'])
    expect(r.warnings.map((i) => i.code)).toEqual(['V8_JOIN_ARITY'])
  })
})
