<template>
  <div class="flow-runtime">
    <!-- 顶部：模板关联信息（设计稿 Tab3 rp-template-bar） -->
    <div class="rp-template-bar">
      <div class="rp-bar-left">
        <div class="rp-title-row">
          <span class="rp-template-name">{{ templateName }}</span>
          <el-tag v-if="instance" :type="instance.status === 'running' ? 'primary' : 'success'" size="small">
            {{ instance.status === 'running' ? '运行中' : instance.status === 'completed' ? '已完成' : '已终止' }}
          </el-tag>
        </div>
        <div class="rp-info-row">
          <span>告警ID：<b>{{ alert?.alertId || '-' }}</b></span>
          <span>告警类型：<el-tag :type="alert?.type === 'pressure_high' || alert?.type === 'wear_interrupt' ? 'danger' : 'warning'" size="small">{{ alertTypeLabel(alert?.type) }}</el-tag></span>
          <span>触发时间：<b>{{ formatTime(alert?.timestamp) }}</b></span>
          <span>当前状态：<b :class="{ 'is-current': hasCurrent }">{{ currentSummary }}</b></span>
        </div>
      </div>
      <el-button size="small" plain :disabled="!instance" @click="viewTemplate">查看模板</el-button>
    </div>

    <!-- 未启动流程：选模板启动（契约：0 实例时先 startFlowInstance） -->
    <div v-if="!instance && !loading" class="page-card rp-start">
      <div class="card-title">该告警还没有处理流程</div>
      <el-select v-model="startTemplateId" placeholder="选择流程模板" class="rp-start-select">
        <el-option v-for="t in templates" :key="t.templateId" :label="`${t.name}（v${t.version}）`" :value="t.templateId" />
      </el-select>
      <el-button type="primary" :loading="starting" :disabled="!startTemplateId" @click="startInstance">启动流程</el-button>
      <span v-if="!templates.length" class="rp-start-empty">暂无可用模板，请先在流程模板设计器（T276）中建模板</span>
    </div>

    <div v-show="instance" class="rp-body">
      <!-- 左：只读画布 + 图例 -->
      <div class="rp-canvas-wrap">
        <div ref="canvasRef" class="rp-canvas" />
        <div class="rp-legend">
          <span class="rp-legend-item"><i class="rp-legend-dot" style="background:#10B981" />已完成</span>
          <span class="rp-legend-item"><i class="rp-legend-dot rp-legend-pulse" style="background:#3B82F6" />当前</span>
          <span class="rp-legend-item"><i class="rp-legend-dot" style="background:#CBD5E1" />待处理</span>
          <span class="rp-legend-sep">|</span>
          <span class="rp-legend-item"><i class="rp-legend-line" />被跳过</span>
        </div>
      </div>

      <!-- 右：操作面板 -->
      <div class="rp-op-panel">
        <div class="rp-op-header">
          <div class="rp-op-kicker">当前节点</div>
          <div class="rp-op-name">{{ selectedNodeName || '未选择节点' }}</div>
          <div class="rp-op-badge">
            <el-tag v-if="selectedState" :type="statusTagType(selectedState.status)" size="small">
              {{ FLOW_STATUS_LABEL[selectedState.status] }}
            </el-tag>
          </div>
          <div class="rp-op-info">
            <template v-if="selectedState && selectedState.status !== 'todo' && selectedState.status !== 'skipped'">
              <span>处理人：<b>{{ assigneeText }}</b></span>
              <span class="rp-op-sep">|</span>
              <span>时限：<b class="rp-deadline">{{ deadlineText }}</b></span>
            </template>
            <span v-else class="rp-muted">尚未到达此节点</span>
          </div>
        </div>

        <div class="rp-op-body">
          <!-- 当前节点：可操作 -->
          <template v-if="selectedState && selectedState.status === 'current'">
            <div class="rp-section">处理操作</div>
            <div class="rp-op-btn-row">
              <el-button
                v-for="a in ACTIONS" :key="a.value" size="small" :type="pendingAction === a.value ? a.primary ? 'success' : 'primary' : 'default'"
                :class="{ 'is-picked': pendingAction === a.value }"
                @click="pickAction(a.value)"
              >{{ a.label }}</el-button>
            </div>

            <el-form v-if="pendingAction === 'transfer'" label-width="0" class="rp-transfer">
              <el-select v-model="targetOperator" filterable allow-create default-first-option placeholder="转派给（选择或输入账号 ID）" size="small">
                <el-option v-for="d in doctors" :key="d.doctorId" :label="`${d.name}（${d.doctorId}）`" :value="d.doctorId" />
              </el-select>
            </el-form>

            <div v-if="branchOptions.length" class="rp-branch">
              <div class="rp-section">选择分支（判断节点有多条出边）</div>
              <el-checkbox-group v-model="pickedBranches" size="small">
                <el-checkbox v-for="b in branchOptions" :key="b.nodeId" :value="b.nodeId">{{ b.name }}</el-checkbox>
              </el-checkbox-group>
            </div>

            <div class="rp-section">处理意见</div>
            <el-input v-model="remark" type="textarea" :rows="3" maxlength="512" show-word-limit placeholder="请输入处理意见..." />

            <div class="op-upload" @click="fileInputRef?.click()">点击上传附件（截图 / 报告 / 记录）</div>
            <input ref="fileInputRef" type="file" class="rp-file" @change="handleUpload" />
            <div class="rp-upload-status">
              <span v-if="uploading">上传中…</span>
              <span v-for="a in attachments" :key="a.fileId" class="rp-attach">
                {{ a.name }}<el-button link size="small" @click="dropAttachment(a.fileId)">删除</el-button>
              </span>
            </div>

            <el-button type="primary" class="rp-submit" :loading="submitting" @click="submit">
              提交{{ ACTION_LABEL[pendingAction] }}
            </el-button>
          </template>

          <!-- 已完成节点：只读历史 -->
          <div v-else-if="selectedState && selectedState.status === 'done'" class="rp-readonly">
            <div class="rp-section">历史处理详情</div>
            <table class="rp-hist">
              <tbody>
                <tr><td class="rp-hist-k">处理人</td><td>{{ selectedState.operatorName || selectedState.operator || '-' }}</td></tr>
                <tr><td class="rp-hist-k">处理时间</td><td>{{ formatTime(selectedState.operatedAt) }}</td></tr>
                <tr><td class="rp-hist-k">处理意见</td><td>{{ selectedState.remark || '-' }}</td></tr>
                <tr><td class="rp-hist-k">附件</td><td>{{ selectedState.attachments?.length ? `${selectedState.attachments.length} 个` : '无' }}</td></tr>
              </tbody>
            </table>
          </div>

          <div v-else-if="selectedState" class="rp-muted rp-placeholder">
            此节点{{ selectedState.status === 'skipped' ? '已被跳过' : '尚未触发' }}，暂无处理记录
          </div>

          <!-- 时间线 -->
          <div class="rp-section rp-timeline-title">处理时间线</div>
          <el-timeline v-if="actions.length" class="rp-timeline">
            <el-timeline-item
              v-for="a in actions" :key="a.actionId"
              :timestamp="formatTime(a.createdAt)" placement="top"
              :type="a.nodeId === selectedNodeId ? 'primary' : 'info'"
            >
              <span class="rp-tl-actor">{{ a.operatorName || a.operator }}</span>
              <span class="rp-tl-node">{{ a.nodeName || a.nodeId }}</span>
              <span class="rp-tl-action">{{ a.actionLabel }}</span>
              <div v-if="a.remark" class="rp-tl-remark">{{ a.remark }}</div>
              <div v-if="a.attachments?.length" class="rp-tl-attach">附件 {{ a.attachments.length }} 个</div>
            </el-timeline-item>
          </el-timeline>
          <div v-else class="rp-muted">暂无处理记录</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Alert, Doctor } from '@bracesync/shared-types'
import { alertTypeLabel } from '@bracesync/shared-utils'
import LogicFlow from '@logicflow/core'
import '@logicflow/core/dist/index.css'
import {
  fetchFlowInstancesByAlert, fetchFlowInstanceActions, fetchFlowNodeStates, fetchFlowTemplate,
  fetchFlowTemplates, startFlowInstanceApi, submitFlowNodeActionApi,
  type FlowInstance, type FlowNodeAction, type FlowNodeState, type FlowTemplate, type FlowActionType,
} from '../../../api/flow'
import { fetchDoctors, presignFile, uploadFileDirect, completeUpload } from '../../../api'
import { registerFlowElements } from './canvas'
import { buildRuntimeGraph, deadlineOf, FLOW_STATUS_LABEL, type FlowStatus } from './flowGraph'

const props = defineProps<{ alert: Alert | null }>()

const ACTIONS: { value: FlowActionType; label: string; primary: boolean }[] = [
  { value: 'confirm', label: '确认处理', primary: true },
  { value: 'reject', label: '驳回', primary: false },
  { value: 'transfer', label: '转派', primary: false },
  { value: 'urge', label: '加急', primary: false },
]
const ACTION_LABEL: Record<FlowActionType, string> = {
  confirm: '处理', reject: '（驳回）', transfer: '（转派）', urge: '（加急）',
}

const loading = ref(false)
const starting = ref(false)
const submitting = ref(false)
const uploading = ref(false)

const instance = ref<FlowInstance | null>(null)
const template = ref<FlowTemplate | null>(null)
const states = ref<FlowNodeState[]>([])
const actions = ref<FlowNodeAction[]>([])
const templates = ref<FlowTemplate[]>([])
const startTemplateId = ref('')
const doctors = ref<Doctor[]>([])

const selectedNodeId = ref('')
const pendingAction = ref<FlowActionType>('confirm')
const targetOperator = ref('')
const pickedBranches = ref<string[]>([])
const remark = ref('')
const attachments = ref<{ fileId: string; name: string }[]>([])

const canvasRef = ref<HTMLDivElement | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
let lf: LogicFlow | null = null

const templateName = computed(() => instance.value?.templateName || template.value?.name || '未绑定流程模板')
const hasCurrent = computed(() => states.value.some((s) => s.status === 'current'))
const currentSummary = computed(() => {
  if (!instance.value) return '-'
  if (instance.value.status === 'completed') return '已完成'
  return hasCurrent.value ? '处理中' : '等待推进'
})

const selectedState = computed(() => states.value.find((s) => s.nodeId === selectedNodeId.value) ?? null)
const selectedTemplateNode = computed(() => template.value?.nodes.find((n) => n.id === selectedNodeId.value) ?? null)
const selectedNodeName = computed(() => selectedTemplateNode.value?.text?.value || selectedNodeId.value)
const assigneeText = computed(() => {
  const s = selectedState.value
  // 实例的 assignee 只有 transfer 之后才有值（契约 :1026-1028：后端不解析模板属性），
  // 所以「处理人初值」必须从模板节点的 assigneeRole 自取 —— 键名与 T285 §2.3 / 契约 :1197 同口径。
  const n = selectedTemplateNode.value?.properties as { assigneeRole?: string } | undefined
  return s?.assigneeName || s?.assignee || n?.assigneeRole || '-'
})
const deadlineText = computed(() => deadlineOf(selectedTemplateNode.value?.properties))

const branchOptions = computed(() => {
  const s = selectedState.value
  if (!s || s.nextNodeIds.length < 2) return []
  return s.nextNodeIds.map((id) => ({
    nodeId: id,
    name: template.value?.nodes.find((n) => n.id === id)?.text?.value || id,
  }))
})

function formatTime(iso?: string | null): string {
  if (typeof iso !== 'string' || iso.length < 16) return '-'
  return `${iso.slice(0, 10)} ${iso.slice(11, 19)}`
}

function statusTagType(status: FlowStatus): 'success' | 'primary' | 'info' | 'warning' {
  if (status === 'done') return 'success'
  if (status === 'current') return 'primary'
  if (status === 'skipped') return 'warning'
  return 'info'
}

/** 契约装配顺序：实例 → 模板 → 节点状态 → 时间线 */
async function load() {
  const alertId = props.alert?.alertId
  if (!alertId) { reset(); return }
  loading.value = true
  try {
    const list = await fetchFlowInstancesByAlert(alertId)
    if (!list.length) {
      instance.value = null
      template.value = null
      states.value = []
      actions.value = []
      templates.value = await fetchFlowTemplates()
      startTemplateId.value = templates.value[0]?.templateId || ''
      return
    }
    instance.value = list[0]
    template.value = await fetchFlowTemplate(instance.value.templateId)
    await refresh()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载流程失败')
  } finally {
    loading.value = false
  }
}

/** 操作成功后只重拉状态与时间线（契约第 5 步） */
async function refresh() {
  if (!instance.value) return
  const [nodeStates, timeline] = await Promise.all([
    fetchFlowNodeStates(instance.value.instanceId),
    fetchFlowInstanceActions(instance.value.instanceId),
  ])
  states.value = nodeStates
  actions.value = timeline
  await nextTick()
  paint()
  if (!selectedNodeId.value || !states.value.some((s) => s.nodeId === selectedNodeId.value)) {
    selectNode(states.value.find((s) => s.status === 'current')?.nodeId || template.value?.nodes[0]?.id || '')
  }
}

function ensureLogicFlow(): LogicFlow | null {
  if (lf || !canvasRef.value) return lf
  lf = new LogicFlow({
    container: canvasRef.value,
    grid: { size: 20, visible: true, type: 'dot' },
    isSilentMode: true,
    stopScrollPage: false,
    edgeType: 'polyline',
  })
  registerFlowElements(lf)
  lf.on('node:click', ({ data }: { data: { id: string } }) => selectNode(data.id))
  return lf
}

function paint() {
  if (!template.value) return
  const graph = buildRuntimeGraph(template.value, states.value)
  const flow = ensureLogicFlow()
  if (!flow) return
  // LogicFlow 的 properties 类型只声明了标量，这里要挂 style 对象，故在渲染边界做一次转换
  flow.render(graph as unknown as Parameters<LogicFlow['render']>[0])
  // 已执行连线：绿色 + 流动动画（动画态取色见 FlowEdgeModel）
  graph.edges.filter((e) => e.properties.executed).forEach((e) => {
    flow.getEdgeModelById(e.id)?.openEdgeAnimation()
  })
  flow.fitView(24, 24)
}

function selectNode(nodeId: string) {
  selectedNodeId.value = nodeId
  pendingAction.value = 'confirm'
  pickedBranches.value = []
  targetOperator.value = ''
}

function pickAction(action: FlowActionType) {
  pendingAction.value = action
}

async function startInstance() {
  if (!props.alert || !startTemplateId.value) return
  starting.value = true
  try {
    instance.value = await startFlowInstanceApi(startTemplateId.value, props.alert.alertId)
    template.value = await fetchFlowTemplate(instance.value.templateId)
    await refresh()
    ElMessage.success('流程已启动')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '启动流程失败')
  } finally {
    starting.value = false
  }
}

async function submit() {
  const inst = instance.value
  const nodeId = selectedNodeId.value
  if (!inst || !nodeId) return
  const action = pendingAction.value
  if (action === 'transfer' && !targetOperator.value.trim()) {
    ElMessage.warning('转派需要选择目标处理人')
    return
  }
  if (branchOptions.value.length && action === 'confirm' && !pickedBranches.value.length) {
    ElMessage.warning('判断节点请先选择走哪条分支')
    return
  }
  submitting.value = true
  try {
    await submitFlowNodeActionApi(inst.instanceId, nodeId, {
      action,
      ...(remark.value.trim() ? { remark: remark.value.trim() } : {}),
      ...(attachments.value.length ? { attachments: attachments.value.map((a) => a.fileId) } : {}),
      ...(action === 'transfer' ? { targetOperator: targetOperator.value.trim() } : {}),
      ...(action === 'confirm' && branchOptions.value.length ? { nextNodeIds: pickedBranches.value } : {}),
    })
    remark.value = ''
    attachments.value = []
    pickedBranches.value = []
    targetOperator.value = ''
    await refresh()
    // 设计稿 rpHandleAction：推进后面板跟到新的当前节点；转派/加急不推进，停在原节点
    const stillCurrent = states.value.some((s) => s.nodeId === nodeId && s.status === 'current')
    if (!stillCurrent) {
      const next = states.value.find((s) => s.status === 'current')
      if (next) selectNode(next.nodeId)
    }
    ElMessage.success('处理已提交')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '提交失败')
  } finally {
    submitting.value = false
  }
}

/** 附件复用 file-service 预签名直传通道（契约：attachments 存 fileId，不建外键） */
async function handleUpload(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  uploading.value = true
  try {
    const head = new Uint8Array(await file.slice(0, 8).arrayBuffer())
    const presign = await presignFile({
      fileName: file.name,
      contentType: file.type || 'application/octet-stream',
      fileType: 'review_report',
      ownerType: 'alert',
      ownerId: props.alert?.alertId,
      fileHeader: btoa(String.fromCharCode(...head)),
    })
    await uploadFileDirect(presign.uploadUrl, file, file.type || 'application/octet-stream')
    await completeUpload(presign.fileId)
    attachments.value.push({ fileId: presign.fileId, name: file.name })
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? `附件上传失败：${e.message}` : '附件上传失败')
  } finally {
    uploading.value = false
  }
}

function dropAttachment(fileId: string) {
  attachments.value = attachments.value.filter((a) => a.fileId !== fileId)
}

function viewTemplate() {
  ElMessage.info('流程模板设计器在 T276 交付，本卡（2.3 运行态）只读展示模板图结构')
}

function reset() {
  instance.value = null
  template.value = null
  states.value = []
  actions.value = []
  selectedNodeId.value = ''
  remark.value = ''
  attachments.value = []
}

onMounted(async () => {
  await load()
  try {
    doctors.value = await fetchDoctors()
  } catch {
    doctors.value = []
  }
})
watch(() => props.alert?.alertId, () => { reset(); void load() })
onBeforeUnmount(() => {
  lf?.destroy()
  lf = null
})
</script>

<style scoped>
.rp-template-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 20px;
  background: #fff;
  border-radius: 10px;
  margin-bottom: 12px;
  box-shadow: 0 1px 4px rgba(0, 0, 0, .04);
}
.rp-title-row { display: flex; align-items: center; gap: 10px; }
.rp-template-name { font-weight: 600; font-size: 15px; color: #1e293b; }
.rp-info-row { display: flex; gap: 20px; margin-top: 6px; font-size: 12px; color: #64748b; flex-wrap: wrap; align-items: center; }
.rp-info-row b { color: #1e293b; }
.rp-info-row .is-current { color: #3B82F6; font-weight: 600; }

.rp-start { display: flex; align-items: center; gap: 12px; }
.rp-start-select { width: 260px; }
.rp-start-empty { font-size: 12px; color: #94a3b8; }

.rp-body { display: flex; gap: 12px; height: calc(100vh - 300px); min-height: 520px; }
.rp-canvas-wrap { flex: 2.5; position: relative; background: #F8FAFB; border: 1px solid #e2e8f0; border-radius: 10px; overflow: hidden; }
.rp-canvas { position: absolute; inset: 0; }

.rp-legend {
  position: absolute; top: 12px; left: 12px; z-index: 10;
  background: rgba(255, 255, 255, .92); border: 1px solid #e2e8f0; border-radius: 8px;
  padding: 8px 12px; font-size: 11px; color: #475569; display: flex; flex-wrap: wrap; gap: 8px;
}
.rp-legend-item { display: flex; align-items: center; gap: 4px; }
.rp-legend-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
.rp-legend-pulse { animation: rpPulse 1.6s ease-in-out infinite; }
.rp-legend-line { width: 14px; height: 0; border-top: 2px dashed #CBD5E1; flex-shrink: 0; }
.rp-legend-sep { color: #CBD5E1; margin: 0 4px; }

.rp-op-panel { width: 360px; flex-shrink: 0; background: #fff; border: 1px solid #e2e8f0; border-radius: 10px; display: flex; flex-direction: column; overflow: hidden; }
.rp-op-header { padding: 16px 18px; border-bottom: 1px solid #e8ecf0; }
.rp-op-kicker { font-size: 12px; color: #94a3b8; margin-bottom: 2px; }
.rp-op-name { font-size: 15px; font-weight: 600; color: #1e293b; }
.rp-op-badge { margin-top: 8px; }
.rp-op-info { margin-top: 8px; font-size: 12px; color: #64748b; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.rp-op-info b { color: #1e293b; }
.rp-op-sep { color: #CBD5E1; }
.rp-deadline { color: #EE5A24 !important; }
.rp-op-body { flex: 1; overflow-y: auto; padding: 16px 18px; }

.rp-section { font-size: 12px; font-weight: 600; color: #64748b; margin: 10px 0 6px; }
.rp-op-btn-row { display: flex; flex-wrap: wrap; gap: 8px; }
.rp-op-btn-row .is-picked { box-shadow: 0 0 0 2px rgba(59, 130, 246, .35); }
.rp-transfer { margin-top: 8px; }
.rp-branch { margin-top: 4px; }
.op-upload {
  margin-top: 10px; padding: 10px; border: 1px dashed #cbd5e1; border-radius: 6px;
  font-size: 12px; color: #64748b; text-align: center; cursor: pointer; background: #f8fafc;
}
.rp-file { display: none; }
.rp-upload-status { font-size: 11px; color: #999; margin-top: 4px; display: flex; flex-direction: column; gap: 2px; }
.rp-attach { display: flex; align-items: center; gap: 6px; }
.rp-submit { width: 100%; margin-top: 14px; }

.rp-readonly { margin-bottom: 8px; }
.rp-hist { width: 100%; font-size: 12px; border-collapse: collapse; }
.rp-hist td { padding: 5px 8px; border-bottom: 1px solid #f1f5f9; }
.rp-hist-k { color: #94a3b8; width: 64px; }
.rp-placeholder { padding: 20px 0; text-align: center; }
.rp-muted { color: #94a3b8; font-size: 12px; }
.rp-timeline-title { margin-top: 18px; }
.rp-timeline { margin-top: 4px; }
.rp-tl-actor { font-weight: 600; color: #1e293b; font-size: 12px; }
.rp-tl-node { color: #64748b; font-size: 12px; margin-left: 6px; }
.rp-tl-action { color: #3B82F6; font-size: 12px; margin-left: 6px; }
.rp-tl-remark { font-size: 12px; color: #64748b; margin-top: 2px; }
.rp-tl-attach { font-size: 11px; color: #94a3b8; }
</style>

<style>
/* LogicFlow 画布内的节点状态色（节点由自定义 model 打上 is-<status> 类名） */
.lf-flow-node.is-done .lf-basic-shape { fill: #D1FAE5; stroke: #10B981; stroke-width: 2; }
.lf-flow-node.is-done .lf-node-text-content, .lf-flow-node.is-done text { fill: #065F46; }
.lf-flow-node.is-current .lf-basic-shape { fill: #DBEAFE; stroke: #3B82F6; stroke-width: 2; animation: rpNodePulse 1.6s ease-in-out infinite; }
.lf-flow-node.is-current text { fill: #1E40AF; }
.lf-flow-node.is-todo .lf-basic-shape { fill: #F1F5F9; stroke: #CBD5E1; stroke-width: 2; }
.lf-flow-node.is-todo text { fill: #94A3B8; }
.lf-flow-node.is-skipped .lf-basic-shape { fill: #F1F5F9; stroke: #CBD5E1; stroke-width: 2; stroke-dasharray: 6 4; opacity: .6; }
.lf-flow-node.is-skipped text { fill: #94A3B8; opacity: .6; }
@keyframes rpNodePulse {
  0%, 100% { filter: drop-shadow(0 0 0 rgba(59, 130, 246, .0)); }
  50% { filter: drop-shadow(0 0 6px rgba(59, 130, 246, .75)); }
}
</style>
