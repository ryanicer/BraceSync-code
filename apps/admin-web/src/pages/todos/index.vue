<template>
  <div class="todos-page">
    <!-- ============ 列表页（无 instanceId；设计稿 医护待办列表.html） ============ -->
    <div v-if="!detailId" class="panel">
      <div class="toolbar">
        <input
          v-model.trim="keyword"
          type="text"
          class="t-search"
          placeholder="搜索患者姓名 / 患者ID…"
          @input="onSearchInput"
        >
      </div>
      <div class="pane-tabs">
        <button
          type="button"
          class="pane-tab"
          :class="{ active: pane === 'mine' }"
          @click="setPane('mine')"
        >待我处理 <span class="cnt">{{ mineCount }}</span></button>
        <button
          type="button"
          class="pane-tab"
          :class="{ active: pane === 'all' }"
          @click="setPane('all')"
        >全部告警 <span class="cnt">{{ allCount }}</span></button>
      </div>

      <table class="list">
        <thead>
          <tr>
            <th style="width:22%">患者</th>
            <th style="width:17%">告警类型</th>
            <th style="width:20%">触发时间</th>
            <th>当前节点</th>
            <th style="width:11%">状态</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in list"
            :key="row.instanceId"
            class="row-go"
            @click="goDetail(row.instanceId)"
          >
            <td>
              <span class="p-name">{{ row.patientName }}</span>
              <span class="p-pid">{{ row.patientId }}</span>
            </td>
            <td><span class="t-tag" :class="typeClass(row.alertType)">{{ row.alertTypeName }}</span></td>
            <td style="color:#475569">{{ formatCstDateTime(row.triggeredAt) }}</td>
            <td>
              <div v-if="row.node" class="node-cell">
                <span class="node-action">{{ row.node.name }}</span>
                <span class="node-sub">
                  <span class="chip" :class="row.node.tone">{{ row.node.leftText }}</span>
                </span>
              </div>
              <span v-else class="node-action node-done">已办结</span>
            </td>
            <td><span class="st-badge" :class="statusClass(row.processStatus)">{{ row.processStatusText }}</span></td>
          </tr>
        </tbody>
      </table>

      <div v-if="!list.length" class="empty">
        当前没有待你处理的告警<br>
        有新告警流转到需要医护处理的节点时会出现在这里（侧栏角标同步提醒）。
      </div>

      <!-- 分页（总条数 > 一页时） -->
      <div v-if="total > pageSize" class="pager">
        <el-pagination
          layout="prev, pager, next"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          @current-change="onPageChange"
        />
      </div>
    </div>

    <!-- ============ 详情页（有 instanceId；设计稿 医护待办详情.html） ============ -->
    <div v-else class="detail-wrap">
      <div class="detail-top">
        <a class="back" @click="goBack">← 返回待办列表</a>
      </div>

      <p v-if="detailError" class="detail-error">{{ detailError }}</p>
      <template v-else-if="detail">
        <!-- ① 患者条：只有姓名 + patient_id -->
        <div class="card patient-bar">
          <span class="p-name">{{ detail.patientName }}</span>
          <span class="p-pid">{{ detail.patientId }}</span>
        </div>

        <!-- ② 告警信息卡 -->
        <div class="card alert-card">
          <div class="alert-head">
            <span class="t-tag" :class="typeClass(detail.alertType)">{{ detail.alertTypeName }}</span>
            <span class="alert-id">{{ detail.alertId }}</span>
            <span class="alert-time">触发时间 {{ formatCstDateTime(detail.triggeredAt) }}</span>
          </div>
          <div class="compare">
            <div class="box">
              <div class="cap">{{ thresholdCap }}</div>
              <div class="val">{{ thresholdVal }}</div>
            </div>
            <div class="box actual">
              <div class="cap">{{ actualCap }}</div>
              <div class="val">{{ actualVal }}</div>
            </div>
          </div>
          <div v-if="detail.detail" class="alert-detail">
            {{ detail.detail }}
            <div v-if="detail.sensorPoint">采集点：<b>{{ detail.sensorPoint }}</b></div>
          </div>
          <div class="alert-sub">设备：{{ detail.deviceId }}</div>
          <div v-if="detail.resolved" class="recovered-note">
            ✓ 告警已于 {{ formatCstDateTime(detail.resolvedAt) }} 恢复上报
          </div>
        </div>

        <!-- ③ 流程进度一行式（画布折叠） -->
        <div class="card flow-card">
          <div class="flow-line">
            <span v-if="detail.node">
              {{ detail.templateName }} · 第 <span class="step">{{ detail.step }}/{{ detail.total }}</span> 步
              · 当前「<span class="step">{{ detail.node.name }}</span>」
              <span class="node-limit">{{ detail.node.limitText }}</span>
              <span class="chip" :class="detail.node.tone">{{ detail.node.leftText }}</span>
            </span>
            <span v-else>
              {{ detail.templateName }} · 第 <span class="step">{{ detail.total }}/{{ detail.total }}</span> 步
              · <span class="flow-done">流程已办结</span>
            </span>
            <button type="button" class="canvas-toggle" @click="canvasOpen = !canvasOpen">
              {{ canvasOpen ? '收起完整流程画布 ▴' : '展开完整流程画布 ▾' }}
            </button>
          </div>
          <div v-if="canvasOpen" class="chain-wrap">
            <div class="chain">
              <div
                v-for="s in detail.steps"
                :key="s.nodeId"
                class="chain-node"
                :class="s.status"
              >
                <div class="chain-dot" :class="s.status">{{ s.status === 'done' ? '✓' : '' }}</div>
                <div class="chain-name">{{ s.name }}</div>
                <div class="chain-role">{{ s.role }}</div>
              </div>
            </div>
            <div class="chain-foot">完整画布只读；节点编排在「告警管理 · 流程配置」维护。</div>
          </div>
        </div>

        <!-- ④ 下一步操作（R8 节点指引两层兜底） -->
        <div class="card">
          <div v-if="detail.node" class="node-note">
            📋 <b>{{ detail.node.name }}</b>：{{ nodeNote }}
          </div>
          <div class="actions">
            <template v-if="detail.node">
              <div class="action-btns">
                <button type="button" class="btn btn-confirm" @click="pickAction('confirm')">确认处理</button>
                <button type="button" class="btn btn-reject" @click="pickAction('reject')">跳过</button>
                <button type="button" class="btn" @click="pickAction('transfer')">转派</button>
              </div>
              <div v-if="pickedAction" class="action-form">
                <select v-if="pickedAction === 'transfer'" v-model="transferTarget" class="af-input">
                  <option value="">选择转派的医护账号…</option>
                  <option value="doctor_li">李医师（演示账号）</option>
                  <option value="doctor_wang">王护士（演示账号）</option>
                </select>
                <textarea
                  v-model.trim="actionRemark"
                  maxlength="512"
                  class="af-input af-textarea"
                  placeholder="处理意见（不超过 512 字，选填）"
                />
                <button
                  type="button"
                  class="submit"
                  :disabled="actionSubmitting"
                  @click="submitAction"
                >{{ submitText }}</button>
              </div>
            </template>
            <span v-else class="done-note">流程已办结，无待执行操作</span>
          </div>
        </div>

        <!-- 处理记录（折叠） -->
        <div class="card records-card" :class="{ open: recordsOpen }">
          <button type="button" class="records-toggle" @click="recordsOpen = !recordsOpen">
            <span>处理记录</span>
            <span class="r-count">（{{ detail.records.length }}）</span>
            <span class="arrow">▾</span>
          </button>
          <div v-if="recordsOpen" class="records-body">
            <div class="timeline">
              <div v-for="r in detail.records" :key="r.actionId" class="tl-item">
                <div class="tl-time">{{ formatCstMonthDayTime(r.createdAt) }}</div>
                <div class="tl-text">
                  <span class="tl-actor">{{ r.operatorName || r.operator || '系统' }}</span>
                  — {{ recordText(r) }}
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
      <p v-else class="detail-loading">加载中…</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { formatAlertValue, userErrorCopy } from '@bracesync/shared-utils'
import {
  fetchTodos, fetchTodoDetail,
  type FlowTodoDetail, type FlowTodoItem,
} from '../../api'
import { submitFlowNodeActionApi } from '../../api/flow'
import { formatCstDateTime, formatCstMonthDayTime } from '../../utils/formatTime'

const route = useRoute()
const router = useRouter()

// ============ 列表页状态 ============
const pane = ref<'mine' | 'all'>('mine')
const keyword = ref('')
const page = ref(1)
const pageSize = ref(20)
const list = ref<FlowTodoItem[]>([])
const total = ref(0)
const mineCount = ref(0)
const allCount = ref(0)

/** T596 同式：取数序号防竞态（换页签/搜索的迟到响应直接丢弃） */
let listSeq = 0

async function loadList() {
  const seq = ++listSeq
  try {
    const res = await fetchTodos({ pane: pane.value, keyword: keyword.value || undefined, page: page.value, pageSize: pageSize.value })
    if (seq !== listSeq) return
    list.value = res.list
    total.value = res.total
    mineCount.value = res.mineCount
    allCount.value = res.allCount
  } catch (e: unknown) {
    if (seq !== listSeq) return
    list.value = []
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '待办列表加载失败' }))
  }
}

function setPane(p: 'mine' | 'all') {
  if (pane.value === p) return
  pane.value = p
  page.value = 1
  void loadList()
}

function onPageChange(p: number) {
  page.value = p
  void loadList()
}

/** 搜索防抖（静态稿是 oninput 即时刷，这里用 300ms 防抖压请求） */
let searchTimer: ReturnType<typeof setTimeout> | undefined
function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    page.value = 1
    void loadList()
  }, 300)
}

function goDetail(instanceId: string) {
  router.push({ path: '/todos', query: { instanceId } })
}

// ============ 详情页状态 ============
const detailId = computed(() => {
  const q = route.query.instanceId
  return typeof q === 'string' && q ? q : ''
})

const detail = ref<FlowTodoDetail | null>(null)
const detailError = ref('')
const canvasOpen = ref(false)
const recordsOpen = ref(false)
const pickedAction = ref<'' | 'confirm' | 'reject' | 'transfer'>('')
const actionRemark = ref('')
const transferTarget = ref('')
const actionSubmitting = ref(false)

async function loadDetail(id: string) {
  detail.value = null
  detailError.value = ''
  canvasOpen.value = false
  recordsOpen.value = false
  pickedAction.value = ''
  actionRemark.value = ''
  transferTarget.value = ''
  try {
    detail.value = await fetchTodoDetail(id)
  } catch (e: unknown) {
    detailError.value = userErrorCopy(e, { scope: 'admin', fallback: '待办加载失败' })
  }
}

watch(detailId, (id) => {
  if (id) void loadDetail(id)
})

function goBack() {
  router.push({ path: '/todos' })
}

// ============ 详情派生展示 ============

/** 阈值/实际值 cap 文案：本地 map + 兜底（值本体用仓库唯一真源 formatAlertValue） */
const CAPS: Record<string, { threshold: string; actual: string }> = {
  pressure_high: { threshold: '告警阈值（统一上限）', actual: '触发值（采集点实测）' },
  wear_duration_short: { threshold: '佩戴时长下限', actual: '当日累计' },
  sensor_drift: { threshold: '标定漂移阈值', actual: '空载漂移实测' },
  wear_interrupt: { threshold: '离线时长上限', actual: '本次离线' },
}

const thresholdCap = computed(() => (detail.value ? CAPS[detail.value.alertType]?.threshold ?? '阈值' : ''))
const actualCap = computed(() => (detail.value ? CAPS[detail.value.alertType]?.actual ?? '实际值' : ''))

const thresholdVal = computed(() =>
  detail.value && detail.value.thresholdValue != null
    ? formatAlertValue(detail.value.alertType, detail.value.thresholdValue, { prefix: '>' })
    : '-',
)
const actualVal = computed(() =>
  detail.value && detail.value.actualValue != null
    ? formatAlertValue(detail.value.alertType, detail.value.actualValue)
    : '-',
)

/** R8 节点指引两层兜底：有 note 用之，无则默认句（与后端 model.FlowTodoDefaultNote 同值） */
const nodeNote = computed(() => detail.value?.node?.note || '请核对该告警并按流程处置')

function typeClass(t: string): string {
  if (t === 'pressure_high') return 't-pressure'
  if (t === 'sensor_drift') return 't-sensor'
  if (t === 'wear_duration_short' || t === 'wear_interrupt') return 't-wear'
  return 't-wear'
}

function statusClass(s: string): string {
  if (s === 'processing') return 'st-run'
  if (s === 'pending') return 'st-todo'
  return 'st-done'
}

/** 处理记录一行文本：actionLabel +（备注）；转派带目标账号 */
function recordText(r: { actionLabel: string; remark: string | null; targetOperator: string | null }): string {
  let text = r.actionLabel
  if (r.remark) text += `：${r.remark}`
  if (r.targetOperator) text += `（转派给 ${r.targetOperator}）`
  return text
}

const submitText = computed(() => {
  if (pickedAction.value === 'confirm') return '提交确认'
  if (pickedAction.value === 'reject') return '提交跳过'
  return '提交转派'
})

function pickAction(a: 'confirm' | 'reject' | 'transfer') {
  pickedAction.value = pickedAction.value === a ? '' : a
  actionRemark.value = ''
  transferTarget.value = ''
}

async function submitAction() {
  const d = detail.value
  const action = pickedAction.value
  if (!d || !d.node || !action) return
  if (action === 'transfer' && !transferTarget.value) {
    ElMessage.warning('请选择转派目标账号')
    return
  }
  actionSubmitting.value = true
  try {
    await submitFlowNodeActionApi(d.instanceId, d.node.nodeId, {
      action,
      remark: actionRemark.value || undefined,
      targetOperator: action === 'transfer' ? transferTarget.value : undefined,
    })
    ElMessage.success('已提交')
    // 提交后刷新详情（流程可能已推进/办结）
    await loadDetail(d.instanceId)
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '操作提交失败' }))
  } finally {
    actionSubmitting.value = false
  }
}

// ============ 入口 ============
onMounted(() => {
  if (detailId.value) {
    void loadDetail(detailId.value)
  } else {
    void loadList()
  }
})
</script>

<style scoped>
.todos-page { padding: 16px; }
.panel { background:#fff;border-radius:12px;box-shadow:0 2px 8px rgba(0,0,0,.06); }
.toolbar { display:flex;align-items:center;gap:10px;padding:14px 18px;border-bottom:1px solid #f0f3f6; }
.t-search { border:1px solid #dde4ec;border-radius:8px;padding:8px 12px;font-size:13px;width:260px; }

.pane-tabs { display:flex;border-bottom:2px solid #e8ecf0; }
.pane-tab { padding:11px 24px;font-size:14px;border:none;background:none;color:#666;cursor:pointer;border-bottom:2px solid transparent;margin-bottom:-2px; }
.pane-tab.active { color:#1a6db5;border-bottom-color:#1a6db5;font-weight:600; }
.pane-tab .cnt { font-size:12px;background:#eef2f6;border-radius:9px;padding:1px 7px;margin-left:5px; }
.pane-tab.active .cnt { background:#e8f2fc;color:#1a6db5; }

table.list { width:100%;border-collapse:collapse;font-size:13px; }
table.list th { background:#f8fafc;color:#667085;font-weight:600;text-align:left;padding:11px 14px;border-bottom:1px solid #e8ecf0;font-size:12px; }
table.list td { padding:13px 14px;border-bottom:1px solid #f0f3f6;vertical-align:middle; }
tr.row-go { cursor:pointer; }
tr.row-go:hover td { background:#f8fafd; }
.p-name { font-weight:600;color:#1e293b; }
.p-pid { font-size:12px;color:#94a3b8;font-weight:400;margin-left:6px; }
.t-tag { font-size:11px;border-radius:4px;padding:2px 8px;white-space:nowrap;font-weight:500;display:inline-block; }
.t-pressure { color:#c0392b;background:#fdecea; }
.t-sensor { color:#6c3483;background:#f4ecf7; }
.t-wear { color:#1a6db5;background:#e8f2fc; }
.node-cell { display:flex;flex-direction:column;gap:4px; }
.node-action { font-size:13px;font-weight:600;color:#1e293b; }
.node-action.node-done { color:#10AC84; }
.node-sub { display:flex;gap:5px;flex-wrap:wrap; }
.chip { display:inline-block;border-radius:10px;padding:1px 8px;font-size:11px;font-weight:500; }
.chip.over { background:#fdecea;color:#e74c3c; }
.chip.due { background:#fff0e8;color:#EE5A24; }
.chip.ok { background:#e8f5e9;color:#10AC84; }
.st-badge { font-size:11px;border-radius:4px;padding:3px 10px;white-space:nowrap; }
.st-run { background:#fff3e0;color:#F39C12; }
.st-todo { background:#e3f2fd;color:#2E86DE; }
.st-done { background:#e8f5e9;color:#10AC84; }
.empty { padding:50px 0 60px;text-align:center;color:#999;font-size:13px;line-height:2; }
.pager { display:flex;justify-content:flex-end;padding:14px 18px; }

/* ===== 详情页 ===== */
.detail-wrap { max-width:1060px; }
.detail-top { display:flex;align-items:center;margin-bottom:10px; }
.back { font-size:13px;color:#1a6db5;cursor:pointer;text-decoration:none; }
.detail-error { color:#e74c3c;font-size:13px; }
.detail-loading { color:#999;font-size:13px; }
.card { background:#fff;border-radius:12px;box-shadow:0 2px 8px rgba(0,0,0,.06);margin-bottom:14px; }
.patient-bar { display:flex;align-items:baseline;gap:12px;flex-wrap:wrap;padding:16px 22px; }
.patient-bar .p-name { font-size:18px;font-weight:700;color:#1e293b; }
.patient-bar .p-pid { color:#94a3b8;font-size:13px; }

.alert-card { padding:18px 22px; }
.alert-head { display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:12px; }
.alert-id { font-size:12px;color:#94a3b8; }
.alert-time { margin-left:auto;font-size:12px;color:#94a3b8; }
.compare { display:grid;grid-template-columns:1fr 1fr;gap:14px;margin-bottom:10px; }
.compare .box { border:1.5px solid #e8ecf0;border-radius:10px;padding:15px 18px;text-align:center; }
.compare .cap { font-size:12px;color:#94a3b8;margin-bottom:6px; }
.compare .val { font-size:25px;font-weight:700;color:#1e293b; }
.compare .box.actual { border-color:#f5c2bd;background:#fff8f7; }
.compare .box.actual .val { color:#e74c3c; }
.alert-detail { font-size:13px;color:#475569;line-height:1.8; }
.alert-sub { font-size:12px;color:#94a3b8;margin-top:8px;line-height:1.8; }
.recovered-note { font-size:12px;color:#0f8a5f;margin-top:8px; }

.flow-card { padding:0; }
.flow-line { display:flex;align-items:center;gap:12px;flex-wrap:wrap;font-size:14px;padding:15px 22px; }
.flow-line .step { font-weight:600;color:#1e293b; }
.flow-line .node-limit { color:#64748b;font-size:12px; }
.flow-line .flow-done { color:#10AC84;font-weight:600; }
.canvas-toggle { margin-left:auto;font-size:13px;color:#1a6db5;cursor:pointer;background:none;border:none; }
.chain-wrap { border-top:1px solid #eef1f5;padding:16px 12px 12px;overflow-x:auto; }
.chain { display:flex;align-items:flex-start;min-width:max-content; }
.chain-node { flex:0 0 auto;min-width:104px;text-align:center;position:relative;padding:0 10px; }
.chain-dot { width:30px;height:30px;border-radius:50%;margin:0 auto 6px;display:flex;align-items:center;justify-content:center;color:#fff;font-size:12px;font-weight:700; }
.chain-dot.done { background:#10B981; }
.chain-dot.current { background:#3B82F6;box-shadow:0 0 0 4px rgba(59,130,246,.18); }
.chain-dot.todo { background:#CBD5E1; }
.chain-dot.skipped { background:#fff;border:2px dashed #CBD5E1;color:#CBD5E1; }
.chain-node:not(:last-child)::after { content:'';position:absolute;top:15px;left:calc(50% + 20px);width:calc(100% - 40px);height:2px;background:#e2e8f0; }
.chain-node.done:not(:last-child)::after { background:#10B981; }
.chain-name { font-size:12px;font-weight:600;color:#333;line-height:1.4; }
.chain-role { font-size:11px;color:#94a3b8;margin-top:2px; }
.chain-foot { font-size:11px;color:#94a3b8;padding:2px 12px 10px; }

.node-note { font-size:13px;color:#475569;background:#f0f7ff;border:1px solid #d0e3f5;border-radius:8px;padding:11px 15px;margin:18px 22px 14px;line-height:1.7; }
.actions { padding:0 22px 18px; }
.action-btns { display:flex;gap:10px;flex-wrap:wrap; }
.btn { border-radius:8px;padding:10px 20px;font-size:14px;cursor:pointer;border:1px solid #d4dce5;background:#fff;color:#444;min-width:104px; }
.btn-confirm { background:#10B981;color:#fff;border-color:#10B981; }
.btn-reject { background:#e74c3c;color:#fff;border-color:#e74c3c; }
.action-form { margin-top:14px;border-top:1px dashed #e8ecf0;padding-top:14px;max-width:500px; }
.af-input { width:100%;border:1px solid #dde4ec;border-radius:8px;padding:9px 11px;font-size:13px;font-family:inherit;box-sizing:border-box; }
select.af-input { margin-bottom:10px; }
.af-textarea { min-height:60px;margin-bottom:10px;resize:vertical; }
.submit { border:none;border-radius:8px;padding:9px 24px;font-size:13px;font-weight:600;color:#fff;background:#1a6db5;cursor:pointer; }
.submit:disabled { opacity:.6;cursor:not-allowed; }
.done-note { font-size:13px;color:#0f8a5f;background:#e8f5e9;border:1px solid #bfe8cf;border-radius:8px;padding:11px 15px;display:inline-block; }

.records-card { padding:0; }
.records-toggle { width:100%;text-align:left;background:none;border:none;padding:13px 22px;font-size:14px;font-weight:600;color:#333;cursor:pointer;display:flex;align-items:center;gap:8px; }
.records-toggle .r-count { color:#94a3b8;font-weight:400;font-size:13px; }
.records-toggle .arrow { margin-left:auto;color:#94a3b8;transition:transform .2s; }
.records-card.open .arrow { transform:rotate(180deg); }
.records-body { padding:0 22px 16px; }
.timeline { padding-left:16px;border-left:2px solid #e8ecf0; }
.tl-item { position:relative;padding:0 0 12px 14px; }
.tl-item::before { content:'';position:absolute;left:-21px;top:4px;width:10px;height:10px;border-radius:50%;background:#1a6db5;border:2px solid #fff;box-shadow:0 0 0 1px #e8ecf0; }
.tl-time { font-size:11px;color:#1a6db5;font-weight:600; }
.tl-text { font-size:12px;color:#555;margin-top:2px;line-height:1.7; }
.tl-actor { color:#333;font-weight:600; }
</style>
