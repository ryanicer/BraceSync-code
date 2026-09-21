// T276 2.4 流程设计器：纯层单测（8 类节点、连接规则、属性写回、graphData ⇄ 契约图、模板 CRUD）。
// 保存前结构校验 V1-V13 的用例在 flow-validate.spec.ts。
// 🔴 只 import designer/kinds.ts 与 api 层，不 import designerCanvas.ts / FlowDesigner.vue ——
// LogicFlow 的 CJS 产物 require 了 ESM 的 lodash-es，CI 的 Node 18 下加载即 ERR_REQUIRE_ESM，
// 会把整个 spec 变成「0 收集」的假绿（T275 CI 实测，见 flowGraph.ts 头注释）。
import { describe, it, expect } from 'vitest'
import {
  DECLARED_ONLY_GROUPS, DESIGN_SHAPE_TYPE, NOTE_MAX, RUNTIME_ONLY_KEYS, canConnect, deserializeGraph,
  emptyForm, filterKinds, formFromProps, fromMinutes, graphCount, kindDef, kindOf, nodeName, nodeGeom,
  paletteIcon, paletteItems, persistType, propsFromForm, reshapeProps, serializeGraph, designerTypeOf,
  stripRuntimeProps, toMinutes, FLOW_KIND_DEFS,
  type FlowShape, type NotifyChannel,
} from '../src/pages/alerts/flow/designer/kinds'
import { buildRuntimeGraph, deadlineOf } from '../src/pages/alerts/flow/flowGraph'
import {
  createFlowTemplateApi, deleteFlowTemplateApi, fetchFlowTemplate, fetchFlowTemplates, updateFlowTemplateApi,
} from '../src/api/flow'

const NO_CONNECTION = { allowed: true, message: '' }

describe('节点面板（卡片 ①：8 类节点 + 搜索）', () => {
  it('恰好 8 类，且类别键用契约口径（notify/handle/decision 不入库）', () => {
    expect(FLOW_KIND_DEFS.map((d) => d.kind)).toEqual([
      'trigger', 'notice', 'process', 'condition', 'parallel', 'join', 'delay', 'archive',
    ])
    expect(FLOW_KIND_DEFS.every((d) => /^#[0-9A-Fa-f]{6}$/.test(d.color))).toBe(true)
    expect(FLOW_KIND_DEFS.map((d) => d.name)).toEqual([
      '触发节点', '通知节点', '处理节点', '判断节点', '并行节点', '汇聚节点', '延时节点', '归档节点',
    ])
  })

  it('类别键归一：设计稿旧称映射到契约类别，脏值回落处理节点', () => {
    expect(kindOf('notify')).toBe('notice')
    expect(kindOf('handle')).toBe('process')
    expect(kindOf('decision')).toBe('condition')
    expect(kindOf(undefined)).toBe('process')
    expect(kindOf('不存在的类别')).toBe('process')
  })

  it('搜索按中文名和类别键双向命中，空串给全量', () => {
    expect(filterKinds('判断').map((d) => d.kind)).toEqual(['condition'])
    expect(filterKinds('delay')).toEqual([expect.objectContaining({ kind: 'delay' })])
    expect(filterKinds('  ').length).toBe(8)
    expect(filterKinds('不存在')).toEqual([])
  })

  it('面板条目带拖拽落库需要的 properties（kind + 尺寸）与图标', () => {
    const items = paletteItems('')
    expect(items).toHaveLength(8)
    expect(items[0]).toMatchObject({
      type: DESIGN_SHAPE_TYPE.circle,
      label: '触发节点',
      text: '触发节点',
      properties: { kind: 'trigger', r: 29 },
    })
    // DndPanel 自己会包 url()，这里只给裸 data-uri（写成 url(...) 会变成 url(url(...)) 而不显图）
    expect(items[0].icon).toMatch(/^data:image\/svg\+xml,/)
    expect(paletteIcon('join')).not.toBe(paletteIcon('trigger'))
  })
})

describe('节点形状（卡片 ②：触发 circle、判断 diamond、其余 rect）', () => {
  it('形状只有三种，且与类别一一对应', () => {
    const byShape: Record<FlowShape, string[]> = { circle: [], diamond: [], rect: [] }
    FLOW_KIND_DEFS.forEach((d) => byShape[d.shape].push(d.kind))
    expect(byShape).toEqual({ circle: ['trigger'], diamond: ['condition'], rect: ['notice', 'process', 'parallel', 'join', 'delay', 'archive'] })
  })

  it('设计器内部类型按形状分三种，落库类型退回内置形状名', () => {
    expect(designerTypeOf({ id: 'n1', type: 'rect', x: 0, y: 0, properties: { kind: 'parallel' } }))
      .toBe(DESIGN_SHAPE_TYPE.rect)
    expect(persistType('trigger')).toBe('circle')
    expect(persistType('archive')).toBe('rect')
    expect(paletteItems('').every((i) => i.type.startsWith('design-'))).toBe(true)
  })

  it('无 kind 的历史数据按自带形状走，几何按形状给默认值', () => {
    expect(designerTypeOf({ id: 'x', type: 'diamond', x: 0, y: 0 })).toBe(DESIGN_SHAPE_TYPE.diamond)
    expect(designerTypeOf({ id: 'x', type: 'flow-circle', x: 0, y: 0 })).toBe(DESIGN_SHAPE_TYPE.circle)
    expect(designerTypeOf({ id: 'x', type: 'design-rect', x: 0, y: 0 })).toBe(DESIGN_SHAPE_TYPE.rect)
    expect(nodeGeom('trigger')).toEqual({ r: 29 })
    expect(nodeGeom('condition')).toEqual({ rx: 50, ry: 28 })
    expect(nodeGeom('delay')).toEqual({ width: 140, height: 48 })
  })
})

describe('连接规则（卡片 ④：触发只连出、归档只连入）', () => {
  it('触发节点不能当终点、归档节点不能当起点', () => {
    expect(canConnect('process', 'trigger', false)).toEqual({
      allowed: false, message: '触发节点只能连出，不能作为连线终点',
    })
    expect(canConnect('archive', 'process', false)).toEqual({
      allowed: false, message: '归档节点只能连入，不能作为连线起点',
    })
  })

  it('自环拒绝（后端 parseFlowGraph 对 self-loop 直接 400）', () => {
    expect(canConnect('process', 'process', true)).toEqual({ allowed: false, message: '不能连接节点自身' })
  })

  it('其余组合放行：判断可以多分叉、延时可以回到处理、并行汇聚自由连', () => {
    expect(canConnect('condition', 'process', false)).toEqual(NO_CONNECTION)
    expect(canConnect('condition', 'delay', false)).toEqual(NO_CONNECTION)
    expect(canConnect('delay', 'process', false)).toEqual(NO_CONNECTION)
    expect(canConnect('parallel', 'join', false)).toEqual(NO_CONNECTION)
    expect(canConnect('trigger', 'notice', false)).toEqual(NO_CONNECTION)
    // 同一节点既当起点又当终点时，同节点判定优先于类别判定
    expect(canConnect('trigger', 'trigger', true).message).toBe('不能连接节点自身')
  })
})

describe('属性面板写回（卡片 ⑤：按类别动态渲染并写 properties，键名对齐 T285 §2.3）', () => {
  it('各类别的属性组照 §2.3 字段表：note 全类别可用，触发/并行/汇聚/归档只有备注', () => {
    expect(kindDef('process').groups).toEqual(['role', 'deadline', 'notify', 'escalation', 'note'])
    expect(kindDef('notice').groups).toEqual(['role', 'deadline', 'notify', 'escalation', 'note'])
    expect(kindDef('condition').groups).toEqual(['condition', 'role', 'deadline', 'note'])
    expect(kindDef('delay').groups).toEqual(['role', 'deadline', 'note'])
    expect(['trigger', 'parallel', 'join', 'archive'].map((k) => kindDef(k as never).groups))
      .toEqual([['note'], ['note'], ['note'], ['note']])
    expect(kindDef('archive').hint).toContain('只能连入')
    // §9.3：后端本期没有消费者的组，面板要标注（右栏渲染的是这句，别改文案）
    expect(DECLARED_ONLY_GROUPS).toEqual(['notify', 'escalation', 'condition'])
  })

  it('处理节点写 assigneeRole + timeLimit/timeUnit，不写运行态的 deadline/assigneeName', () => {
    const props = propsFromForm('process', { ...emptyForm('医生确认'), assigneeRole: '主治医生', timeLimit: 2, timeUnit: 'hours' })
    expect(props).toEqual({
      kind: 'process', assigneeRole: '主治医生', timeLimit: 2, timeUnit: 'hours',
      channels: ['system', 'sms'],
    })
    expect(props.escalation).toBeUndefined()
    // 时限文本由 2.3 现算（flowGraph.deadlineOf），模板里不留冗余字段
    expect(deadlineOf(props)).toBe('2小时')
  })

  it('延时节点的时长只落 delayMinutes（后端与 2.3 都读这一个键）', () => {
    expect(propsFromForm('delay', { ...emptyForm(), timeLimit: 6, timeUnit: 'hours' }))
      .toEqual({ kind: 'delay', delayMinutes: 360 })
    expect(toMinutes(3, 'days')).toBe(4320)
    expect(fromMinutes(4320)).toEqual({ timeLimit: 3, timeUnit: 'days' })
    expect(fromMinutes(90)).toEqual({ timeLimit: 90, timeUnit: 'minutes' })
    expect(fromMinutes(90)).not.toEqual({ timeLimit: 1.5, timeUnit: 'hours' })
  })

  it('未勾选超时升级时不落 escalation 空壳；勾上才写 targetRole', () => {
    expect(propsFromForm('process', { ...emptyForm(), escalationEnabled: false, escalationTargetRole: '科室主任' }).escalation).toBeUndefined()
    expect(propsFromForm('process', { ...emptyForm(), escalationEnabled: true, escalationTargetRole: '科室主任' }).escalation)
      .toEqual({ enabled: true, targetRole: '科室主任' })
  })

  it('通知节点写 channels 数组（拷贝，改表单不回写落库对象）', () => {
    const form = { ...emptyForm(), channels: ['system', 'email'] as NotifyChannel[] }
    const props = propsFromForm('notice', form)
    expect(props.channels).toEqual(['system', 'email'])
    form.channels.push('sms')
    expect(props.channels).toEqual(['system', 'email'])
  })

  it('判断节点写 expression；触发节点除备注外不落字段', () => {
    expect(propsFromForm('condition', { ...emptyForm(), expression: '压力值 > 100' })).toMatchObject({
      kind: 'condition', expression: '压力值 > 100',
    })
    expect(propsFromForm('trigger', emptyForm())).toEqual({ kind: 'trigger' })
    expect(propsFromForm('trigger', { ...emptyForm(), assigneeRole: '主治医生', timeLimit: 5 }))
      .toEqual({ kind: 'trigger' })
  })

  it('备注去首尾空白并截到 NOTE_MAX，脏值不会把库写爆', () => {
    const props = propsFromForm('archive', { ...emptyForm(), note: '  ' + '归'.repeat(NOTE_MAX + 30) + '  ' })
    expect(props.note).toHaveLength(NOTE_MAX)
    expect(propsFromForm('archive', { ...emptyForm(), note: '   ' }).note).toBeUndefined()
  })

  it('读回表单：缺字段回落默认值（30 分钟 + 系统/短信），脏 timeUnit 不采信', () => {
    const form = formFromProps('process', { kind: 'process', timeUnit: '秒针', assigneeRole: '护士' }, '医生确认')
    expect(form).toMatchObject({ name: '医生确认', assigneeRole: '护士', timeLimit: 30, timeUnit: 'minutes' })
    expect(form.channels).toEqual(['system', 'sms'])
  })

  it('读回延时节点：delayMinutes 拆成数值 + 单位；脏 channels 项被丢掉', () => {
    const form = formFromProps('delay', { kind: 'delay', delayMinutes: 360 }, '复核等待')
    expect(form).toMatchObject({ name: '复核等待', timeLimit: 6, timeUnit: 'hours' })
    expect(formFromProps('notice', { kind: 'notice', channels: ['sms', 'carrier-pigeon'] }, '').channels).toEqual(['sms'])
  })

  it('升级开关只认 true，其他脏值（字符串/缺省）都算关', () => {
    expect(formFromProps('process', { kind: 'process', escalation: { enabled: 'yes', targetRole: '科室主任' } }, '').escalationEnabled).toBe(false)
    expect(formFromProps('process', { kind: 'process', escalation: { enabled: true, targetRole: '科室主任' } }, ''))
      .toMatchObject({ escalationEnabled: true, escalationTargetRole: '科室主任' })
  })

  it('reshapeProps 保留未变字段但剥掉运行态键', () => {
    const next = reshapeProps({ kind: 'process', status: 'done', remark: '备注' }, 'process', { ...emptyForm('改名'), assigneeRole: '护士' })
    expect(next).toMatchObject({ kind: 'process', remark: '备注', assigneeRole: '护士' })
    expect(next.status).toBeUndefined()
  })

  it('§3.3 剥离清单：状态/处理人/操作人/标红 + _runtime 前缀都不许进模板', () => {
    expect(RUNTIME_ONLY_KEYS).toEqual(['status', 'assigneeName', 'operatorName', 'invalid'])
    expect(stripRuntimeProps({
      kind: 'process', status: 'current', assigneeName: '张三', operatorName: '李四',
      invalid: true, _runtimeEdge: 1, assigneeRole: '主治医生',
    })).toEqual({ kind: 'process', assigneeRole: '主治医生' })
  })
})

describe('graphData ⇄ 契约模板图（卡片 ⑥：存 getGraphData、回显 render）', () => {
  const designerGraph = {
    nodes: [
      { id: 'a', type: DESIGN_SHAPE_TYPE.circle, x: 10, y: 20, properties: { kind: 'trigger', r: 29 }, text: { x: 10, y: 20, value: '告警触发' } },
      { id: 'b', type: DESIGN_SHAPE_TYPE.rect, x: 10, y: 120, properties: { kind: 'join', status: 'done', width: 140, height: 48 }, text: '汇聚' },
    ],
    edges: [
      { id: 'e1', type: 'polyline', sourceNodeId: 'a', targetNodeId: 'b', properties: {} },
      { id: 'e2', type: 'polyline', sourceNodeId: 'b', targetNodeId: 'a', properties: {}, text: { x: 1, y: 2, value: '重新处理' } },
    ],
  }

  it('design-* 收成内置形状名，kind 归一到契约键，status 不落库', () => {
    const out = serializeGraph(designerGraph)
    expect(out.nodes.map((n) => n.type)).toEqual(['circle', 'rect'])
    expect(out.nodes.map((n) => n.properties.kind)).toEqual(['trigger', 'join'])
    expect(out.nodes[1].properties.status).toBeUndefined()
    expect(out.nodes[1].text).toEqual({ x: 10, y: 120, value: '汇聚' })
    expect(out.edges.every((e) => e.type === 'polyline')).toBe(true)
    expect(out.edges[1].text).toEqual({ x: 1, y: 2, value: '重新处理' })
    expect(out.edges[0].text).toBeUndefined()
  })

  it('运行态键在导出这一步就被剥掉（校验标红的 invalid、2.3 的 status 都不入库）', () => {
    const out = serializeGraph({
      nodes: [{
        id: 'n9', type: DESIGN_SHAPE_TYPE.rect, x: 0, y: 0,
        properties: { kind: 'process', invalid: true, status: 'current', assigneeName: '张三', operatorName: '李四' },
      }],
    })
    expect(out.nodes[0].properties).toEqual({ kind: 'process' })
  })

  it('旧称（notify/handle/decision）入库前换成契约类别，运行态与后端都只认一套键', () => {
    const out = serializeGraph({
      nodes: [
        { id: 'n1', type: 'design-rect', x: 0, y: 0, properties: { kind: 'handle' } },
        { id: 'n2', type: 'design-diamond', x: 0, y: 0, properties: { kind: 'decision' } },
      ],
    })
    expect(out.nodes.map((n) => `${n.type}:${n.properties.kind}`)).toEqual(['rect:process', 'diamond:condition'])
  })

  it('往返稳定：契约图 → 设计器图 → 契约图，形状/坐标/属性/节点数不变', () => {
    const first = serializeGraph(designerGraph)
    const back = serializeGraph(deserializeGraph(first))
    expect(back).toEqual(first)
    expect(graphCount(first)).toEqual({ nodes: 2, edges: 2 })
  })

  it('回显时按形状补齐几何：旧模板只写了 width/height，圆和菱也能拿到正确尺寸', () => {
    const { nodes } = deserializeGraph({
      nodes: [
        { id: 'c', type: 'circle', x: 0, y: 0, properties: { kind: 'trigger', width: 60, height: 60 } },
        { id: 'd', type: 'diamond', x: 0, y: 100, properties: { kind: 'condition', width: 100, height: 60 } },
        { id: 'r', type: 'rect', x: 0, y: 200, properties: { kind: 'process', width: 120, height: 40 } },
      ],
    })
    // 圆/菱的尺寸键是 r 与 rx/ry（LogicFlow 的 Circle/DiamondNodeModel 只读这几个），
    // 模板自带的 width/height 原样保留不动，只是对这两个模型不生效
    expect(nodes[0].properties).toMatchObject({ width: 60, r: 29 })
    expect(nodes[1].properties).toMatchObject({ rx: 50, ry: 28 })
    expect(nodes[2].properties).toMatchObject({ width: 120, height: 40 })
  })

  it('设计器产物喂给运行态画布时保持设计稿形状（并行/汇聚仍是矩形、触发是圆）', () => {
    const saved = serializeGraph({
      nodes: [
        { id: 'p', type: DESIGN_SHAPE_TYPE.rect, x: 0, y: 0, properties: { kind: 'parallel', width: 140, height: 48 } },
        { id: 'j', type: DESIGN_SHAPE_TYPE.rect, x: 0, y: 100, properties: { kind: 'join', width: 140, height: 48 } },
        { id: 't', type: DESIGN_SHAPE_TYPE.circle, x: 0, y: -100, properties: { kind: 'trigger', width: 58, height: 58 } },
      ],
      edges: [{ id: 'e', type: 'polyline', sourceNodeId: 't', targetNodeId: 'p', properties: {} }],
    })
    const runtime = buildRuntimeGraph(saved, [{ nodeId: 'p', status: 'current' } as never])
    expect(runtime.nodes.map((n) => `${n.id}=${n.type}`)).toEqual(['p=flow-rect', 'j=flow-rect', 't=flow-circle'])
    expect(runtime.edges[0].type).toBe('flow-polyline')
  })
})

describe('模板 CRUD（mock 分支，字段口径对齐 T274 契约）', () => {
  const graph = {
    nodes: [{ id: 'x1', type: 'circle', x: 1, y: 2, properties: { kind: 'trigger' }, text: { x: 1, y: 2, value: '触发' } }],
    edges: [],
  }

  it('新建 → 列表可见 → 保存覆盖 → 详情回显 → 删除归零', async () => {
    const before = await fetchFlowTemplates()
    const created = await createFlowTemplateApi('T276 单测模板', graph)
    expect(created.templateId).toMatch(/^FLOW_T/)
    expect(created.version).toBe(1)
    expect(created.nodes).toHaveLength(1)
    expect((await fetchFlowTemplates()).length).toBe(before.length + 1)

    const updated = await updateFlowTemplateApi(created.templateId, {
      nodes: [...graph.nodes, { id: 'x2', type: 'rect', x: 3, y: 4, properties: { kind: 'archive' } }],
    })
    expect(updated.version).toBe(2)
    expect(updated.nodes).toHaveLength(2)
    const detail = await fetchFlowTemplate(created.templateId)
    expect(detail.nodes.map((n) => n.id)).toEqual(['x1', 'x2'])

    await deleteFlowTemplateApi(created.templateId)
    expect((await fetchFlowTemplates()).length).toBe(before.length)
    await expect(fetchFlowTemplate(created.templateId)).rejects.toThrow('模板不存在')
  })

  it('列表搜索走 keyword（模板管理下拉的搜索框）', async () => {
    const created = await createFlowTemplateApi('T276 可搜索模板', graph)
    try {
      expect((await fetchFlowTemplates('可搜索')).map((t) => t.name)).toEqual(['T276 可搜索模板'])
      expect(await fetchFlowTemplates('不存在的关键字')).toEqual([])
    } finally {
      await deleteFlowTemplateApi(created.templateId)
    }
  })

  it('列表项不带图数据（契约 :1082），只有详情给 —— 设计器下拉的节点数靠详情回填', async () => {
    const created = await createFlowTemplateApi('T276 列表瘦身模板', graph)
    try {
      const [row] = (await fetchFlowTemplates('T276 列表瘦身')).filter((t) => t.templateId === created.templateId)
      expect(row.nodes).toEqual([])
      expect(row.edges).toEqual([])
      expect(row.name).toBe('T276 列表瘦身模板')
      expect(row.version).toBe(1)
      const detail = await fetchFlowTemplate(created.templateId)
      expect(detail.nodes).toHaveLength(1)
    } finally {
      await deleteFlowTemplateApi(created.templateId)
    }
  })

  it('重名与空名按契约拒绝（后端 409 / 400 的前端等价校验）', async () => {
    await expect(createFlowTemplateApi('  ', graph)).rejects.toThrow('不能为空')
    await expect(createFlowTemplateApi('默认告警流程', graph)).rejects.toThrow('已存在')
    const created = await createFlowTemplateApi('T276 改名模板', graph)
    try {
      await expect(updateFlowTemplateApi(created.templateId, { name: '默认告警流程' })).rejects.toThrow('已存在')
    } finally {
      await deleteFlowTemplateApi(created.templateId)
    }
  })

  it('已被实例使用的模板不许删（契约 409）， mock 与后端同口径', async () => {
    const [defaultTpl] = await fetchFlowTemplates('默认')
    expect(defaultTpl.instanceCount).toBeGreaterThan(0)
    await expect(deleteFlowTemplateApi(defaultTpl.templateId)).rejects.toThrow('流程实例')
  })

  it('保存时边指向不存在的节点直接报错，不会静默写坏模板', async () => {
    const created = await createFlowTemplateApi('T276 脏边模板', graph)
    try {
      await expect(updateFlowTemplateApi(created.templateId, {
        nodes: [],
        edges: [{ id: 'bad', type: 'polyline', sourceNodeId: 'x1', targetNodeId: 'x2', properties: {} }],
      })).rejects.toThrow('不存在的节点')
    } finally {
      await deleteFlowTemplateApi(created.templateId)
    }
  })
})
