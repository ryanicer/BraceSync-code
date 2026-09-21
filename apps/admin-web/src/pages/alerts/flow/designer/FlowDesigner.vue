<template>
  <div class="flow-designer">
    <!-- 顶部工具栏（设计稿 Tab4 :408 起） -->
    <div class="fd-topbar">
      <div class="fd-topbar-left">
        <el-button type="primary" @click="newTemplate">新建流程模板</el-button>
        <el-button :disabled="!currentId" :loading="saving" @click="saveTemplate">保存</el-button>
        <el-dropdown trigger="click" popper-class="fd-tpl-popper">
          <el-button>
            模板管理<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <div class="fd-tpl-search">
              <el-input v-model="tplKeyword" size="small" placeholder="搜索模板..." clearable @input="reloadTemplates" />
            </div>
            <div class="fd-tpl-list">
              <div
                v-for="t in templates"
                :key="t.templateId"
                class="fd-tpl-item"
                :class="{ 'is-active': t.templateId === currentId }"
                @click="switchTemplate(t.templateId)"
              >
                <b>{{ t.name }}</b><span v-if="t.nodes.length">{{ t.nodes.length }} 节点</span>
              </div>
              <div v-if="!templates.length" class="fd-tpl-empty">无匹配模板</div>
            </div>
          </template>
        </el-dropdown>
        <el-button :disabled="!currentId" @click="removeTemplate">删除模板</el-button>
      </div>
      <div class="fd-topbar-meta">
        <span>模板数：<b>{{ templates.length }}</b></span>
        <span>当前模板：<b>{{ currentName || '未选择' }}</b></span>
        <span>版本：<b>{{ currentVersion ? `v${currentVersion}` : '-' }}</b></span>
        <span>最后保存：<b>{{ lastSavedAt || '-' }}</b></span>
      </div>
    </div>

    <!-- 三栏主体（设计稿 Tab4 :453 起） -->
    <div class="fd-body">
      <div class="fd-palette">
        <div class="fd-palette-head">
          <div class="fd-palette-title">节点类型</div>
          <el-input v-model="nodeKeyword" size="small" placeholder="搜索节点..." clearable @input="applyNodeFilter" />
        </div>
        <!-- LogicFlow DndPanel 挂载点 -->
        <div ref="paletteRef" class="fd-palette-panel"></div>
      </div>

      <div v-loading="loading" class="fd-canvas">
        <div ref="canvasRef" class="fd-canvas-inner"></div>
        <!-- 画布悬浮工具栏（设计稿 :527 起）；撤销/重做的可用态来自 history:change（S3：lf.isEnableUndo 不存在，用 history.undoAble()） -->
        <div class="fd-float-bar">
          <el-button text size="small" title="缩小" @click="zoomOut"><el-icon><Minus /></el-icon></el-button>
          <span class="fd-zoom">{{ zoomPct }}%</span>
          <el-button text size="small" title="放大" @click="zoomIn"><el-icon><Plus /></el-icon></el-button>
          <i class="fd-sep" />
          <el-button text size="small" title="适应画布" @click="fitCanvas"><el-icon><FullScreen /></el-icon></el-button>
          <i class="fd-sep" />
          <el-button text size="small" title="上一步" :disabled="!canUndo" @click="undo"><el-icon><RefreshLeft /></el-icon></el-button>
          <el-button text size="small" title="下一步" :disabled="!canRedo" @click="redo"><el-icon><RefreshRight /></el-icon></el-button>
        </div>
        <div class="fd-count">节点 {{ count.nodes }} · 连线 {{ count.edges }}<em v-if="dirty" class="fd-dirty">未保存</em></div>
      </div>

      <NodePropsPanel
        :kind="selectedKind"
        :form="draft"
        :issues="issues"
        @change="applyDraft"
        @delete="deleteSelected"
        @locate="focusNode"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, FullScreen, Minus, Plus, RefreshLeft, RefreshRight } from '@element-plus/icons-vue'
import LogicFlow from '@logicflow/core'
import '@logicflow/core/dist/index.css'
import { DndPanel, MiniMap } from '@logicflow/extension'
import '@logicflow/extension/dist/index.css'
import {
  createFlowTemplateApi, deleteFlowTemplateApi, fetchFlowTemplate, fetchFlowTemplates, updateFlowTemplateApi,
  type FlowTemplate,
} from '../../../../api/flow'
import { registerDesignElements } from './designerCanvas'
import {
  emptyForm, formFromProps, graphCount, kindOf, nodeName, paletteItems, reshapeProps,
  deserializeGraph, serializeGraph, type FlowKind, type NodeForm, type RawGraph, type RawNode,
} from './kinds'
import { issueNodeIds, validateGraph, type Issue } from './validateGraph'
import NodePropsPanel from './NodePropsPanel.vue'

const canvasRef = ref<HTMLDivElement | null>(null)
const paletteRef = ref<HTMLDivElement | null>(null)

const loading = ref(false)
const saving = ref(false)
const templates = ref<FlowTemplate[]>([])
const tplKeyword = ref('')
const nodeKeyword = ref('')
const currentId = ref('')
const currentName = ref('')
const currentVersion = ref(0)
const lastSavedAt = ref('')
const zoomPct = ref(100)
const count = reactive({ nodes: 0, edges: 0 })

const selectedId = ref('')
const selectedKind = ref<FlowKind | ''>('')
const draft = reactive<NodeForm>(emptyForm())

const issues = ref<Issue[]>([])
const canUndo = ref(false)
const canRedo = ref(false)
/** §6.4 dirty 基线：与 serializeGraph 同形状比对，所以标红用的 invalid 等运行态键不会把画布弄「脏」 */
const baseSnapshot = ref('')
const dirty = ref(false)

let lf: LogicFlow | null = null
let dnd: DndPanel | null = null
let miniMap: MiniMap | null = null

function fmtTime(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function graphData(): RawGraph {
  return (lf?.getGraphData() ?? { nodes: [], edges: [] }) as RawGraph
}

/** 画布当前内容指纹（已剥运行态键，与落库形状一致）—— dirty 判定与保存基线共用 */
function payloadJson(): string {
  return JSON.stringify(serializeGraph(graphData()))
}

function refreshCount() {
  Object.assign(count, graphCount(graphData()))
  dirty.value = Boolean(currentId.value) && payloadJson() !== baseSnapshot.value
}

/** 载入 / 保存成功后重设基线（§6.4：只有这两个时机改基线） */
function resetBaseline() {
  baseSnapshot.value = payloadJson()
  dirty.value = false
}

function clearIssues() {
  issues.value = []
  markInvalid([])
}

/** 校验命中的节点标红（T285 §3.4 的 is-invalid；invalid 属运行态键，保存时会被剥掉） */
function markInvalid(ids: string[]) {
  if (!lf) return
  const hit = new Set(ids)
  for (const model of lf.graphModel.nodes) {
    if (model.properties?.invalid === true) lf.deleteProperty(model.id, 'invalid')
    else if (hit.has(model.id)) lf.setProperties(model.id, { invalid: true })
  }
}

/** 切换 / 新建模板前确认丢弃未保存改动（§6.3 a：render 会清 history，回不去，只能事前问） */
async function confirmDiscard(): Promise<boolean> {
  if (!dirty.value) return true
  try {
    await ElMessageBox.confirm(
      `模板「${currentName.value}」有未保存的改动，切换或新建会丢弃这些改动。`,
      '丢弃未保存的改动？',
      { type: 'warning', confirmButtonText: '丢弃并继续', cancelButtonText: '留在当前模板' },
    )
    return true
  } catch {
    return false
  }
}

function focusNode(id: string) {
  if (!lf || !id) return
  lf.selectElementById(id)
  selectNode(id)
}

function clearSelection() {
  selectedId.value = ''
  selectedKind.value = ''
  Object.assign(draft, emptyForm())
}

function selectNode(id: string) {
  if (!lf) return
  const data = lf.getNodeDataById(id) as unknown as RawNode | undefined
  if (!data) return
  selectedId.value = id
  selectedKind.value = kindOf(data.properties?.kind)
  Object.assign(draft, formFromProps(selectedKind.value, data.properties, nodeName(data)))
}

/** 属性面板 → 节点：类别固定，属性按当前类别的属性组写回 */
function applyDraft() {
  if (!lf || !selectedId.value || !selectedKind.value) return
  const model = lf.getNodeModelById(selectedId.value)
  if (!model) return
  lf.setProperties(selectedId.value, reshapeProps(model.properties, selectedKind.value, draft))
  if (model.text?.value !== draft.name) lf.updateText(selectedId.value, draft.name)
}

async function deleteSelected() {
  if (!lf || !selectedId.value) return
  const id = selectedId.value
  const name = draft.name.trim() || id
  // §4.1：模板改了不回溯在途实例，删节点是「该节点在 2.3 上变哑」的唯一成因，必须在确认文案里点明
  try {
    await ElMessageBox.confirm(
      `删除节点「${name}」及其相关连线。已被流程实例引用的节点删除后，`
      + `在途实例上该节点的状态行仍在，但名字会显示为空、也推进不了。`,
      '删除节点',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  lf.deleteNode(id)
  clearSelection()
}

// ===== 画布与工具栏 =====

function initCanvas() {
  if (!canvasRef.value) return
  lf = new LogicFlow({
    container: canvasRef.value,
    grid: { size: 20, visible: true, type: 'dot' },
    plugins: [MiniMap],
    // 插件配置按 pluginName 取键（MiniMap.pluginName = 'miniMap'，且这层叫 pluginsOptions 不是
    // pluginOptions）—— 写错不会报错，只是整套配置被静默忽略
    pluginsOptions: {
      // 设计稿没画缩略图，卡片 ③ 要求挂 MiniMap；默认右下角、可关
      miniMap: {
        width: 180, height: 120, showEdge: true,
        isShowHeader: true, headerTitle: '缩略图', isShowCloseIcon: true,
      },
    },
  })
  registerDesignElements(lf)
  // 实例在 new LogicFlow() 时就挂到 lf.extension 上（键 = pluginName），
  // 但 render 是 core 在 lf.render() 时才回调的，所以 show() 必须等首次渲染之后
  miniMap = lf.extension.miniMap as unknown as MiniMap

  if (paletteRef.value) {
    dnd = new DndPanel({ lf })
    dnd.setPatternItems(paletteItems(nodeKeyword.value))
    dnd.render(lf, paletteRef.value)
  }

  lf.on('node:click', ({ data }) => selectNode(data.id))
  // dnd-add 必须自己刷计数：history:change 是 debounce 100ms 才落栈的，连拖多个节点时
  // 每次拖拽都会把定时器往后推，角标能一直停在旧值（实测拖满 8 类仍显示「节点 0」）。
  // GraphModel.addNode 是先入栈后发事件，所以这里读到的已经是落位后的图。
  lf.on('node:dnd-add', ({ data }) => {
    refreshCount()
    selectNode(data.id)
  })
  lf.on('blank:click', () => clearSelection())
  // LogicFlow 只在 hover 或选中时渲染锚点，而它自己要到第一次 pointermove 才在 onDragStart 里选中源节点。
  // 从锚点拖出外形的那一瞬 hover 先掉，锚点子树被销毁，正在拖的实例成了孤儿——松手时它读到的
  // dragging 仍是 false，连线静默丢失（实测 edges 0）。提前在 mousedown 选中即可保住子树。
  lf.on('anchor:mousedown', ({ nodeModel }) => {
    lf?.graphModel.selectNodeById(nodeModel.id)
    selectNode(nodeModel.id)
  })
  lf.on('node:delete,edge:delete,node:add,edge:add,graph:rendered', () => refreshCount())
  lf.on('history:change', () => {
    if (!lf) return
    // S3 结论：lf.isEnableUndo() 在 2.2.5 里不存在，可用态只有 history.undoAble()/redoAble()
    canUndo.value = lf.history.undoAble()
    canRedo.value = lf.history.redoAble()
    refreshCount()
    // §6.3(b)：撤销可能把当前选中的节点撤没，右栏不能继续编辑一个不存在的节点
    if (selectedId.value && !lf.getNodeModelById(selectedId.value)) clearSelection()
  })
  lf.on('graph:transform', ({ transform }) => {
    zoomPct.value = Math.round((transform.SCALE_X ?? 1) * 100)
  })
}

function applyNodeFilter() {
  dnd?.setPatternItems(paletteItems(nodeKeyword.value))
}

function renderGraph(graph: RawGraph) {
  if (!lf) return
  // LogicFlow 的 render() 只把新图压成历史基线（History.watch 里 push 一条），旧模板的栈并不丢 ——
  // 切完模板按「上一步」会把上一个模板的图倒回画布，再保存就等于串了模板。清栈后让 watch 重新压基线。
  lf.history.undos.length = 0
  lf.history.redos.length = 0
  lf.render(deserializeGraph(graph) as unknown as Parameters<LogicFlow['render']>[0])
  clearSelection()
  issues.value = []
  canUndo.value = false
  canRedo.value = false
  refreshCount()
  resetBaseline()
  // 空模板不能 fitView：LogicFlow 对空图算出的缩放是 NaN，之后拖拽落点会换算到画布外
  if ((graph.nodes?.length ?? 0) > 0) nextTick(() => lf?.fitView())
  nextTick(() => miniMap?.show())
}

function zoomIn() { lf?.zoom(true) }
function zoomOut() { lf?.zoom(false) }
function fitCanvas() { lf?.fitView() }
function undo() { lf?.undo() }
function redo() { lf?.redo() }

// ===== 模板管理（卡片 ⑥：新建 / 保存 / 切换 + 回显）=====

async function reloadTemplates() {
  templates.value = await fetchFlowTemplates(tplKeyword.value)
}

/** 后端 message 全英文（契约 10400 的 duplicate node id 之类），前端只兜中文标题 + 附原文（§6.4 E4） */
function backendErrorText(e: unknown, fallback: string): string {
  const raw = (e as Error)?.message?.trim()
  if (!raw) return fallback
  return /[一-龥]/.test(raw) ? raw : `${fallback}，后端返回：${raw}`
}

function applyTemplateMeta(tpl: FlowTemplate) {
  currentId.value = tpl.templateId
  currentName.value = tpl.name
  currentVersion.value = tpl.version
  lastSavedAt.value = fmtTime(tpl.updatedAt)
  // 列表接口不带图数据（契约 api-contracts.ts:1082），下拉里的「N 节点」只能拿刚读到的详情回填这一条
  if (tpl.nodes?.length) {
    const cached = templates.value.find((t) => t.templateId === tpl.templateId)
    if (cached) {
      cached.nodes = tpl.nodes
      cached.edges = tpl.edges ?? []
    }
  }
}

async function switchTemplate(templateId: string) {
  if (!lf || templateId === currentId.value) return
  if (!(await confirmDiscard())) return
  loading.value = true
  try {
    const tpl = await fetchFlowTemplate(templateId)
    applyTemplateMeta(tpl)
    renderGraph({ nodes: tpl.nodes, edges: tpl.edges })
  } finally {
    loading.value = false
  }
}

async function newTemplate() {
  if (!(await confirmDiscard())) return
  let name = ''
  try {
    const res = await ElMessageBox.prompt('模板名称（1–64 字，不可与已有模板重名）', '新建流程模板', {
      confirmButtonText: '新建',
      cancelButtonText: '取消',
      inputPattern: /^\S.{0,62}\S$|^\S$/,
      inputErrorMessage: '请输入 1–64 个字符的模板名称',
    })
    name = res.value
  } catch {
    return
  }
  loading.value = true
  try {
    const tpl = await createFlowTemplateApi(name, { nodes: [], edges: [] })
    await reloadTemplates()
    applyTemplateMeta(tpl)
    renderGraph({ nodes: tpl.nodes, edges: tpl.edges })
    ElMessage.success(`已创建空模板「${tpl.name}」，从左侧拖入节点后点「保存」`)
  } catch (e) {
    ElMessage.error(backendErrorText(e, '新建模板失败'))
  } finally {
    loading.value = false
  }
}

async function saveTemplate() {
  if (!lf || !currentId.value) return
  // §6.2：后端只挡量程/自环/悬空边，环与不可达与「没出口」全是前端的责任 —— error 不放行
  const result = validateGraph(graphData(), currentName.value)
  issues.value = [...result.errors, ...result.warnings]
  markInvalid(issueNodeIds(result))
  if (result.errors.length) {
    ElMessage.error(`结构校验未通过：${result.errors.length} 处错误已在画布标红，右栏问题清单可点选定位`)
    return
  }
  if (result.warnings.length) {
    try {
      await ElMessageBox.confirm(
        `结构校验有 ${result.warnings.length} 条提示（不阻断保存）：${result.warnings[0].message}`,
        '仍要保存？',
        { type: 'warning', confirmButtonText: '仍要保存', cancelButtonText: '返回修改' },
      )
    } catch {
      return
    }
  }
  saving.value = true
  try {
    const graph = serializeGraph(graphData())
    const tpl = await updateFlowTemplateApi(currentId.value, graph)
    applyTemplateMeta(tpl)
    await reloadTemplates()
    // 保存后端上又回读了一次，比对数量就是「存进去没被吞字段」的最小证据（§4.3 第 5 步）
    const echo = await fetchFlowTemplate(tpl.templateId)
    // 顺序不能反：reloadTemplates() 会整体替换列表，详情回填必须发生在它之后（reload 之后列表项是新的对象）
    applyTemplateMeta(echo)
    clearIssues()
    refreshCount()
    resetBaseline()
    ElMessage.success(
      `流程模板已保存（v${tpl.version}）。节点 ${graph.nodes.length} / 连线 ${graph.edges.length}，`
      + `回读 ${echo.nodes.length} / ${echo.edges.length}。`,
    )
  } catch (e) {
    ElMessage.error(backendErrorText(e, '保存模板失败'))
  } finally {
    saving.value = false
  }
}

async function removeTemplate() {
  if (!lf || !currentId.value) return
  const target = templates.value.find((t) => t.templateId === currentId.value)
  try {
    await ElMessageBox.confirm(
      `确认删除模板「${currentName.value}」？已有流程实例的模板后端会拒绝删除（409）。`,
      '删除流程模板',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await deleteFlowTemplateApi(currentId.value)
    await reloadTemplates()
    // 模板本体都没了，画布上那点未保存改动既不能留也不该再问一次
    dirty.value = false
    const next = templates.value[0]
    if (next) {
      await switchTemplate(next.templateId)
    } else {
      currentId.value = ''
      currentName.value = ''
      currentVersion.value = 0
      lastSavedAt.value = ''
      renderGraph({ nodes: [], edges: [] })
    }
    ElMessage.success(`已删除模板${target ? `「${target.name}」` : ''}`)
  } catch (e) {
    ElMessage.error(backendErrorText(e, '删除模板失败'))
  }
}

onMounted(async () => {
  loading.value = true
  try {
    await nextTick()
    initCanvas()
    await reloadTemplates()
    const first = templates.value[0]
    if (first) {
      const tpl = await fetchFlowTemplate(first.templateId)
      applyTemplateMeta(tpl)
      renderGraph({ nodes: tpl.nodes, edges: tpl.edges })
    } else {
      renderGraph({ nodes: [], edges: [] })
    }
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  dnd?.destroy()
  dnd = null
  // 缩略图内部另建了一个 LogicFlow 实例，随主画布销毁前先自己收掉
  miniMap?.hide()
  miniMap = null
  lf?.destroy()
  lf = null
})
</script>

<style scoped>
.flow-designer { display: flex; flex-direction: column; }
.fd-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid #e8ecf0;
  flex-wrap: wrap;
}
.fd-topbar-left { display: flex; align-items: center; gap: 10px; }
.fd-topbar-meta { display: flex; align-items: center; gap: 16px; font-size: 12px; color: #64748b; }
.fd-topbar-meta b { color: #1e293b; font-weight: 600; }

.fd-body {
  display: flex;
  height: calc(100vh - 300px);
  min-height: 500px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  overflow: hidden;
  margin-top: 12px;
  background: #f8fafb;
}
.fd-palette {
  width: 200px;
  flex-shrink: 0;
  background: #fff;
  border-right: 1px solid #e2e8f0;
  display: flex;
  flex-direction: column;
}
.fd-palette-head { padding: 14px; border-bottom: 1px solid #e8ecf0; }
.fd-palette-title { font-size: 13px; font-weight: 600; color: #1e293b; margin-bottom: 10px; }
.fd-palette-panel { flex: 1; overflow-y: auto; padding: 8px; position: relative; }
/* LogicFlow 的 DndPanel 默认绝对定位在画布左上角，挂到左栏后要归位 */
.fd-palette-panel :deep(.lf-dndpanel) {
  position: static;
  background: transparent;
  box-shadow: none;
  border-radius: 0;
  margin: 0;
  padding: 0;
}
.fd-palette-panel :deep(.lf-dnd-item) { padding: 8px 4px; border-radius: 8px; }
.fd-palette-panel :deep(.lf-dnd-item:hover) { background: #f1f5f9; }
.fd-palette-panel :deep(.lf-dnd-text) { font-size: 12px; color: #1e293b; margin-top: 2px; }

.fd-canvas { flex: 1; position: relative; overflow: hidden; }
/* v-loading 遮罩淡出的那 300ms 仍铺满画布，而 LogicFlow 的 Dnd 用 elementFromPoint 判定落点在不在画布里，
   于是「加载刚结束就拖节点」会被判成画布外拖放 ⇒ 节点静默丢失。遮罩只管视觉，收起过程不吃指针。 */
.fd-canvas :deep(.el-loading-fade-leave-active) { pointer-events: none; }
.fd-canvas-inner { position: absolute; inset: 0; z-index: 1; }
.fd-float-bar {
  position: absolute;
  top: 14px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 10;
  display: flex;
  align-items: center;
  gap: 2px;
  background: #fff;
  border-radius: 24px;
  padding: 4px 12px;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.08);
  border: 1px solid #e8ecf0;
  /* 工具栏横在画布上沿，而 LogicFlow 判定拖放落点用 elementFromPoint：落在工具栏上的节点会被算成
     「画布外」而静默丢弃（同上面 v-loading 遮罩那条）。本体让指针穿透，只有按钮自己接事件。 */
  pointer-events: none;
}
.fd-float-bar > * { pointer-events: auto; }
.fd-zoom { font-size: 12px; color: #475569; min-width: 42px; text-align: center; }
/* 校验命中的节点标红：CSS 的 stroke 压得过 LogicFlow 写在形状上的描边属性，故不必改 model 样式。
   锚点也是 circle.lf-basic-shape，要排掉，否则选中态下锚点跟着变红。 */
.fd-canvas-inner :deep(g.lf-design-node.is-invalid .lf-basic-shape:not(.lf-node-anchor)) { stroke: #EF4444; }
.fd-dirty {
  font-style: normal;
  margin-left: 8px;
  padding: 1px 6px;
  border-radius: 8px;
  background: #FEF3C7;
  color: #B45309;
}
.fd-sep { width: 1px; height: 18px; background: #e2e8f0; margin: 0 4px; }
.fd-count {
  position: absolute;
  left: 12px;
  bottom: 10px;
  z-index: 10;
  /* 同悬浮工具栏：纯展示的角标别把落点挡住（LogicFlow 用 elementFromPoint 判落在不在画布上） */
  pointer-events: none;
  font-size: 12px;
  color: #64748b;
  background: rgba(255, 255, 255, 0.9);
  border: 1px solid #e8ecf0;
  border-radius: 6px;
  padding: 3px 8px;
}

.fd-tpl-search { padding: 10px 12px; border-bottom: 1px solid #f0f0f0; }
.fd-tpl-list { max-height: 220px; overflow-y: auto; }
.fd-tpl-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  font-size: 12px;
  cursor: pointer;
}
.fd-tpl-item b { font-weight: 600; color: #333; }
.fd-tpl-item span { color: #999; }
.fd-tpl-item:hover,
.fd-tpl-item.is-active { background: #f0f7ff; }
.fd-tpl-empty { padding: 14px; font-size: 12px; color: #94a3b8; text-align: center; }
</style>
