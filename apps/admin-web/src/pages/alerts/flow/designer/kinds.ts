// T276 2.4 拖拽式流程设计器的纯层：8 类节点定义、形状与序列化映射、连接规则、属性表单 ⇄ properties。
// 与 flowGraph.ts 同样的理由独立成文件：@logicflow/core 的 CJS 产物 require 了 ESM 的 lodash-es，
// CI 的 Node 18 下加载即 ERR_REQUIRE_ESM ⇒ 单测只能吃这层，LogicFlow 装配见 designerCanvas.ts。
//
// 类别键以契约为准（docs/api/api-contracts.ts 的 properties.kind 注释）：
// trigger / notice / process / condition / parallel / join / delay / archive。
// 设计稿 Tab4 左栏用的是同一批东西的旧称：notify=notice、handle=process、decision=condition，
// 这里用 KIND_ALIAS 收口，落库只写契约键。

export type FlowKind =
  | 'trigger' | 'notice' | 'process' | 'condition'
  | 'parallel' | 'join' | 'delay' | 'archive'

export type FlowShape = 'circle' | 'diamond' | 'rect'

/** 右栏属性面板可按节点类型渲染的属性组（设计稿 fcUpdatePropsPanel 的分组，只影响 UI，不落库） */
export type PropGroup = 'role' | 'deadline' | 'notify' | 'escalation' | 'condition' | 'note'

export interface FlowKindDef {
  kind: FlowKind
  /** 节点面板与属性面板显示的中文名（设计稿 FC_NODE_TYPES.name） */
  name: string
  /** 描边色（设计稿 FC_NODE_TYPES.color） */
  color: string
  /** 形状（设计稿：触发=circle、判断=diamond、其余=rect，靠颜色区分） */
  shape: FlowShape
  /** 除「名称/类型」外还要配哪些属性 */
  groups: PropGroup[]
  /** 无附加属性时面板里的说明 */
  hint?: string
}

/** 属性组的适用范围照 T285 §2.3 的字段表（T276 卡片 ⑤ 的落库口径），note 全类别可用 */
export const FLOW_KIND_DEFS: FlowKindDef[] = [
  { kind: 'trigger', name: '触发节点', color: '#F59E0B', shape: 'circle', groups: ['note'], hint: '流程入口，只能连出，不需要额外属性。' },
  { kind: 'notice', name: '通知节点', color: '#F59E0B', shape: 'rect', groups: ['role', 'deadline', 'notify', 'escalation', 'note'] },
  { kind: 'process', name: '处理节点', color: '#10B981', shape: 'rect', groups: ['role', 'deadline', 'notify', 'escalation', 'note'] },
  { kind: 'condition', name: '判断节点', color: '#3B82F6', shape: 'diamond', groups: ['condition', 'role', 'deadline', 'note'] },
  { kind: 'parallel', name: '并行节点', color: '#3B82F6', shape: 'rect', groups: ['note'], hint: '并行分叉由出边决定，画几条出边就同时走几条。' },
  { kind: 'join', name: '汇聚节点', color: '#8B5CF6', shape: 'rect', groups: ['note'], hint: '汇聚等待全部入边完成，不需要额外属性。' },
  { kind: 'delay', name: '延时节点', color: '#F59E0B', shape: 'rect', groups: ['role', 'deadline', 'note'], hint: '本期没有定时器，延时时长只作为声明值存库（不自动执行）。' },
  { kind: 'archive', name: '归档节点', color: '#94A3B8', shape: 'rect', groups: ['note'], hint: '流程出口，只能连入，不需要额外属性。' },
]

const KIND_BY_DEF = new Map<string, FlowKind>(FLOW_KIND_DEFS.map((d) => [d.kind, d.kind]))

const KIND_ALIAS: Record<string, FlowKind> = {
  notify: 'notice',
  handle: 'process',
  decision: 'condition',
}

/** 未知类别一律按处理节点渲染，保证脏数据不至于画不出节点 */
export function kindOf(raw: unknown): FlowKind {
  const key = String(raw ?? '')
  if (KIND_BY_DEF.has(key)) return key as FlowKind
  const alias = KIND_ALIAS[key]
  return alias ?? 'process'
}

export function kindDef(kind: FlowKind): FlowKindDef {
  return FLOW_KIND_DEFS.find((d) => d.kind === kind) as FlowKindDef
}

/** 设计器内部注册的节点类型（着色按类别，与运行态按状态着色区分开） */
export const DESIGN_SHAPE_TYPE: Record<FlowShape, string> = {
  circle: 'design-circle',
  diamond: 'design-diamond',
  rect: 'design-rect',
}
/** 连线用 LogicFlow 内置类型，落库与加载同值 */
export const DESIGN_EDGE_TYPE = 'polyline'

export function designType(kind: FlowKind): string {
  return DESIGN_SHAPE_TYPE[kindDef(kind).shape]
}

/** 落库类型只写 LogicFlow 内置形状名，运行态与第三方消费方不依赖本卡的 design-* 注册 */
export function persistType(kind: FlowKind): FlowShape {
  return kindDef(kind).shape
}

/** 节点几何（设计稿 Tab4：触发 58 圆、判断 100×56 菱、其余 140×48 矩形）。
 * 键名按各自 Model 实际消费的属性来写，随节点落 properties：
 * RectNodeModel 读 width/height，CircleNodeModel 只读半径 r，DiamondNodeModel 只读半轴 rx/ry
 * （给圆/菱写 width/height 会被静默忽略，尺寸回落到 LF 默认的 80 圆、60×100 菱）。 */
export function nodeGeom(kind: FlowKind): Record<string, number> {
  const shape = kindDef(kind).shape
  if (shape === 'circle') return { r: 29 }
  if (shape === 'diamond') return { rx: 50, ry: 28 }
  return { width: 140, height: 48 }
}

/** 节点面板图标：形状 + 类别色，画成 data-uri SVG。
 * 只返回 URI 本身 —— DndPanel 内部是 `backgroundImage = url(${item.icon})`，自己会包一层 url()。 */
export function paletteIcon(kind: FlowKind): string {
  const { color, shape } = kindDef(kind)
  const body = shape === 'circle'
    ? '<circle cx="14" cy="14" r="10.5" fill="#fff" stroke="' + color + '" stroke-width="2.5"/><circle cx="14" cy="14" r="4" fill="' + color + '"/>'
    : shape === 'diamond'
      ? '<path d="M14 2.5 25.5 14 14 25.5 2.5 14Z" fill="#fff" stroke="' + color + '" stroke-width="2.5"/>'
      : '<rect x="2.5" y="6" width="23" height="16" rx="3" fill="#fff" stroke="' + color + '" stroke-width="2.5"/>'
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="28" height="28" viewBox="0 0 28 28">' + body + '</svg>'
  return 'data:image/svg+xml,' + encodeURIComponent(svg)
}

export interface PaletteItem {
  type: string
  label: string
  /** 拖到画布上时的默认节点名 */
  text: string
  icon: string
  properties: Record<string, unknown>
}

/** 左栏节点面板条目（LogicFlow DndPanel 的 ShapeItem 形状） */
export function paletteItems(keyword = ''): PaletteItem[] {
  return filterKinds(keyword).map((def) => ({
    type: designType(def.kind),
    label: def.name,
    text: def.name,
    icon: paletteIcon(def.kind),
    properties: { kind: def.kind, ...nodeGeom(def.kind) },
  }))
}

/** 模板节点 → 设计器节点类型：类别优先，无类别时按内置/flow-* 形状名回落 */
export function designerTypeOf(node: RawNode): string {
  if (node.type.startsWith('design-')) return node.type
  const kindKey = String(node.properties?.kind ?? '')
  if (kindKey) return designType(kindOf(kindKey))
  const shape = node.type.replace(/^flow-/, '') as FlowShape
  return DESIGN_SHAPE_TYPE[shape] ?? DESIGN_SHAPE_TYPE.rect
}

/** 节点面板搜索（设计稿 fcFilterNodes：按中文名与类别键双向匹配） */
export function filterKinds(keyword: string): FlowKindDef[] {
  const kw = keyword.trim().toLowerCase()
  if (!kw) return FLOW_KIND_DEFS
  return FLOW_KIND_DEFS.filter((d) => d.name.toLowerCase().includes(kw) || d.kind.toLowerCase().includes(kw))
}

export interface ConnectVerdict {
  allowed: boolean
  message: string
}

/**
 * 连接规则（声明式，卡片 ④）：
 * 触发节点是入口不能被连入；归档节点是出口不能连出；自环后端直接 400，这里提前拦住。
 */
export function canConnect(sourceKind: FlowKind, targetKind: FlowKind, sameNode: boolean): ConnectVerdict {
  if (sameNode) return { allowed: false, message: '不能连接节点自身' }
  if (targetKind === 'trigger') return { allowed: false, message: '触发节点只能连出，不能作为连线终点' }
  if (sourceKind === 'archive') return { allowed: false, message: '归档节点只能连入，不能作为连线起点' }
  return { allowed: true, message: '' }
}

// ─────────────── 属性表单 ⇄ node.properties ───────────────

/** D4（T285 §12）待裁定：这些取值后端不解析，只会被 2.3 当文本显示，故用管理端可读的中文名 */
export const ROLE_OPTIONS = ['主治医生', '值班医生', '护士', '系统自动', '管理员']
export const ESCALATION_TARGETS = ['主治医生', '值班医生', '科室主任']
export const TIME_UNITS: { value: TimeUnit; label: string }[] = [
  { value: 'minutes', label: '分钟' },
  { value: 'hours', label: '小时' },
  { value: 'days', label: '天' },
]
export const NOTIFY_CHANNELS: { value: NotifyChannel; label: string }[] = [
  { value: 'system', label: '系统通知' },
  { value: 'sms', label: '短信' },
  { value: 'email', label: '邮件' },
  { value: 'wechat', label: '微信' },
]

export type TimeUnit = 'minutes' | 'hours' | 'days'
export type NotifyChannel = 'system' | 'sms' | 'email' | 'wechat'

/** §9.3：这几项后端本期没有消费者，右栏必须标注，否则 admin 会以为已生效 */
export const DECLARED_ONLY_GROUPS: PropGroup[] = ['notify', 'escalation', 'condition']

export interface NodeForm {
  name: string
  /** 落库键 assigneeRole —— 契约 :1197 与 T285 §2.3 同键；2.3 的「处理人」从这里自取 */
  assigneeRole: string
  timeLimit: number
  timeUnit: TimeUnit
  channels: NotifyChannel[]
  escalationEnabled: boolean
  /** 落库键 escalation.targetRole */
  escalationTargetRole: string
  /** 落库键 expression（判断节点条件，仅提示人工判断，后端不求值） */
  expression: string
  note: string
}

/** 设计稿默认值：时限 30 分钟、通知走系统 + 短信 */
export function emptyForm(name = ''): NodeForm {
  return {
    name,
    assigneeRole: '',
    timeLimit: 30,
    timeUnit: 'minutes',
    channels: ['system', 'sms'],
    escalationEnabled: false,
    escalationTargetRole: '',
    expression: '',
    note: '',
  }
}

const MINUTES_PER_UNIT: Record<TimeUnit, number> = { minutes: 1, hours: 60, days: 1440 }

export function toMinutes(limit: number, unit: TimeUnit): number {
  return Math.max(1, Math.round(limit * MINUTES_PER_UNIT[unit]))
}

/** 分钟数拆回「数值 + 单位」：取能整除的最大单位，保证 4320 → 3 天而不是 4320 分钟 */
export function fromMinutes(total: number): { timeLimit: number; timeUnit: TimeUnit } {
  for (const unit of ['days', 'hours'] as TimeUnit[]) {
    if (total % MINUTES_PER_UNIT[unit] === 0) return { timeLimit: total / MINUTES_PER_UNIT[unit], timeUnit: unit }
  }
  return { timeLimit: total, timeUnit: 'minutes' }
}

/** 运行态专有键：模板里出现即污染（2.3 会读到过期状态），导出前一律剥掉。T285 §3.3 */
export const RUNTIME_ONLY_KEYS = ['status', 'assigneeName', 'operatorName', 'invalid'] as const

export function stripRuntimeProps(props: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(props)) {
    if ((RUNTIME_ONLY_KEYS as readonly string[]).includes(key) || key.startsWith('_runtime')) continue
    out[key] = value
  }
  return out
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

function num(v: unknown, fallback: number): number {
  const n = Number(v)
  return Number.isFinite(n) && n > 0 ? n : fallback
}

/** 从节点 properties（+ text.value）读回表单；缺字段回落默认值而不是报错。
 * 延时节点的时长只存 delayMinutes（§2.3），读回时拆成数值 + 单位。 */
export function formFromProps(kind: FlowKind, props: Record<string, unknown> | undefined, name: string): NodeForm {
  const base = emptyForm(name)
  const p = (props ?? {}) as Record<string, unknown>
  const escalation = p.escalation as { enabled?: boolean; targetRole?: unknown } | undefined
  const unit = str(p.timeUnit)
  let { timeLimit, timeUnit } = base
  if (kind === 'delay') {
    const minutes = num(p.delayMinutes, 0)
    if (minutes > 0) ({ timeLimit, timeUnit } = fromMinutes(minutes))
  } else {
    timeLimit = num(p.timeLimit, base.timeLimit)
    if (TIME_UNITS.some((u) => u.value === unit)) timeUnit = unit as TimeUnit
  }
  return {
    name,
    assigneeRole: str(p.assigneeRole),
    timeLimit,
    timeUnit,
    channels: Array.isArray(p.channels)
      ? p.channels.filter((c): c is NotifyChannel => NOTIFY_CHANNELS.some((n) => n.value === c))
      : [...base.channels],
    escalationEnabled: escalation?.enabled === true,
    escalationTargetRole: str(escalation?.targetRole),
    expression: str(p.expression),
    note: str(p.note),
  }
}

/** 备注落库上限（与契约 note 字段同口径，超出直接截断而不是报错） */
export const NOTE_MAX = 200

/** 表单 → 落库 properties：键名严格按 T285 §2.3 的字段表，不写运行态字段 */
export function propsFromForm(kind: FlowKind, form: NodeForm): Record<string, unknown> {
  const props: Record<string, unknown> = { kind }
  const groups = kindDef(kind).groups
  if (groups.includes('role') && form.assigneeRole) props.assigneeRole = form.assigneeRole
  if (groups.includes('deadline')) {
    if (kind === 'delay') props.delayMinutes = toMinutes(form.timeLimit, form.timeUnit)
    else {
      props.timeLimit = form.timeLimit
      props.timeUnit = form.timeUnit
    }
  }
  if (groups.includes('notify')) props.channels = [...form.channels]
  if (groups.includes('escalation') && form.escalationEnabled) {
    props.escalation = { enabled: true, targetRole: form.escalationTargetRole }
  }
  if (groups.includes('condition') && form.expression) props.expression = form.expression
  if (groups.includes('note') && form.note.trim()) props.note = form.note.trim().slice(0, NOTE_MAX)
  return props
}

/** 改类别时保留已填字段（几何、id 等），但按新类别重写业务属性并剥掉运行态键 */
export function reshapeProps(prev: Record<string, unknown>, nextKind: FlowKind, form: NodeForm): Record<string, unknown> {
  return { ...stripRuntimeProps(prev), ...propsFromForm(nextKind, form) }
}

// ─────────────── graphData ⇄ 契约模板图 ───────────────

export interface RawText { x?: number; y?: number; value?: string }

export interface RawNode {
  id: string
  type: string
  x: number
  y: number
  properties?: Record<string, unknown>
  text?: RawText | string
}

export interface RawEdge {
  id?: string
  type?: string
  sourceNodeId: string
  targetNodeId: string
  properties?: Record<string, unknown>
  text?: RawText | string
}

export interface RawGraph {
  nodes?: RawNode[]
  edges?: RawEdge[]
}

export interface PersistedNode {
  id: string
  type: FlowShape
  x: number
  y: number
  properties: Record<string, unknown> & { kind: string }
  text: { x: number; y: number; value: string }
}

export interface PersistedEdge {
  id: string
  type: string
  sourceNodeId: string
  targetNodeId: string
  properties: Record<string, unknown>
  text?: { x: number; y: number; value: string }
}

function textObj(t: RawText | string | undefined, x: number, y: number): { x: number; y: number; value: string } | undefined {
  const value = typeof t === 'string' ? t : (t?.value ?? '')
  if (!value.trim()) return undefined
  const tx = typeof t === 'object' && typeof t.x === 'number' ? t.x : x
  const ty = typeof t === 'object' && typeof t.y === 'number' ? t.y : y
  return { x: tx, y: ty, value }
}

export function nodeName(node: RawNode | undefined): string {
  if (!node) return ''
  const t = node.text
  return (typeof t === 'string' ? t : (t?.value ?? '')).trim()
}

/**
 * lf.getGraphData() → 契约请求体。
 * design-* 收成内置形状名，properties.kind 归一到契约键，text 统一成 {x,y,value}
 * （后端按对象解析 text，裸字符串会 400）。
 * 这里是设计器唯一的「导出前剥离点」（T285 §3.3）：后端原样存取、不解析 properties，
 * 运行态键混进去不会报错，只会在 2.3 留下过期状态色，所以必须在这里拦掉。
 */
export function serializeGraph(graph: RawGraph): { nodes: PersistedNode[]; edges: PersistedEdge[] } {
  const nodes = (graph.nodes ?? []).map((n) => {
    const kind = kindOf(n.properties?.kind)
    const out: PersistedNode = {
      id: n.id,
      type: persistType(kind),
      x: n.x,
      y: n.y,
      properties: { ...stripRuntimeProps(n.properties ?? {}), kind },
      text: { x: n.x, y: n.y, value: nodeName(n) },
    }
    return out
  })
  const edges = (graph.edges ?? []).map((e, i) => {
    const out: PersistedEdge = {
      id: e.id ?? `edge_${i + 1}`,
      type: 'polyline',
      sourceNodeId: e.sourceNodeId,
      targetNodeId: e.targetNodeId,
      properties: { ...(e.properties ?? {}) },
    }
    const text = textObj(e.text, 0, 0)
    if (text) out.text = text
    return out
  })
  return { nodes, edges }
}

/** 契约模板图 → 喂给 lf.render() 的设计器图（design-* 类型 + 保留坐标与属性） */
export function deserializeGraph(graph: RawGraph): { nodes: RawNode[]; edges: RawEdge[] } {
  const nodes: RawNode[] = (graph.nodes ?? []).map((n) => {
    const kind = kindOf(n.properties?.kind)
    const props: Record<string, unknown> = { ...(n.properties ?? {}), kind }
    // 模板自带几何就照用（运行态读同一批键），缺的按类别默认补齐
    for (const [key, value] of Object.entries(nodeGeom(kind))) {
      if (typeof props[key] !== 'number') props[key] = value
    }
    return {
      id: n.id,
      type: designerTypeOf(n),
      x: n.x,
      y: n.y,
      properties: props,
      text: textObj(n.text, n.x, n.y),
    }
  })
  const edges: RawEdge[] = (graph.edges ?? []).map((e, i) => ({
    id: e.id ?? `edge_${i + 1}`,
    type: DESIGN_EDGE_TYPE,
    sourceNodeId: e.sourceNodeId,
    targetNodeId: e.targetNodeId,
    properties: { ...(e.properties ?? {}) },
    text: textObj(e.text, 0, 0),
  }))
  return { nodes, edges }
}

/** 统计（工具栏「节点数」与保存成功提示用） */
export function graphCount(graph: RawGraph): { nodes: number; edges: number } {
  return { nodes: (graph.nodes ?? []).length, edges: (graph.edges ?? []).length }
}
