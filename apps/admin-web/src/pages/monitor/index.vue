<template>
  <div class="monitor">
    <!-- 顶部刷新栏（T322：数据侧采集时刻与前端拉取时刻分列，不再用拉取时刻冒充数据新鲜度） -->
    <div class="page-toolbar">
      <span class="realtime-tag" :class="`live-${liveState}`">
        <span class="realtime-dot" />
        {{ liveLabel }}
      </span>
      <span class="update-time">
        数据采集：{{ frameCollectedText }}<template v-if="frame.collectedAt !== null">（{{ frameAgeText }}）</template>
        <span class="time-split">|</span>
        本次拉取：{{ pullTime || '-' }}
      </span>
      <el-button size="small" type="primary" @click="refreshTick">立即刷新</el-button>
    </div>

    <!-- 患者选择卡片 -->
    <div class="page-card patient-card">
      <div class="card-title">患者选择</div>
      <div class="patient-bar">
        <el-select
          v-model="selectedPatientId"
          filterable
          placeholder="搜索患者姓名/ID..."
          style="min-width: 260px"
          @change="handlePatientChange"
        >
          <el-option
            v-for="p in patientOptions"
            :key="p.patientId"
            :label="`${p.name} · ${p.patientId}`"
            :value="p.patientId"
          />
        </el-select>
        <span :class="['status-indicator', `status-${snapshot?.status ?? 'offline'}`]">
          <span class="status-dot" />
          {{ statusLabel }}
        </span>
        <span class="device-hint">
          {{ selectedDevice ? '设备：' + selectedDevice : '未绑定设备' }}
        </span>
      </div>
    </div>

    <!-- 帧新鲜度告警条（T322：无帧 / 过期必须显式说，不能把陈旧值当实时展示） -->
    <div v-if="frameNotice" class="frame-notice" :class="`notice-${liveState}`" role="alert">
      {{ frameNotice }}
    </div>

    <!-- 患者摘要卡片（设计稿 实时监控.html:133-150） -->
    <div class="page-card peak-card">
      <div class="card-title">
        患者摘要
        <span class="realtime-tag small" :class="`live-${liveState}`">
          <span class="realtime-dot" />
          {{ liveState === 'fresh' ? '实时累计' : liveState === 'expired' ? '已过期' : '无实时帧' }}
        </span>
      </div>
      <div class="peak-grid">
        <div class="peak-cell">
          <div class="peak-label">今日累计佩戴时长</div>
          <div class="peak-num">{{ snapshot?.todayHours != null ? snapshot.todayHours.toFixed(1) + ' h' : '--' }}</div>
        </div>
        <div class="peak-cell peak-value">
          <div class="peak-label">当前最大压力</div>
          <!-- 帧派生值：无帧时不得显示 0.0 N（后端此刻给的是 seed 兜底），只能给占位 -->
          <div class="peak-num" :style="{ color: showFrame ? hmColor(curFrameValue, hmMaxN) : undefined }">
            {{ showFrame ? fmtN(curFrameValue) + ' N' : '--' }}
          </div>
        </div>
        <div class="peak-cell">
          <div class="peak-label">最大压力采集点</div>
          <div class="peak-text">{{ todayPeak ? todayPeak.pointId + ' (' + todayPeak.label + ')' : '--' }}</div>
        </div>
        <div class="peak-cell">
          <div class="peak-label">今日异常事件</div>
          <div class="peak-num" :style="{ color: (snapshot?.events ?? 0) > 0 ? '#ee5a24' : '#10ac84' }">
            {{ snapshot?.events ?? 0 }}
          </div>
        </div>
      </div>
    </div>

    <!-- 左右双栏：曲线 + 热力图 -->
    <div class="charts-row">
      <!-- 实时压力曲线 -->
      <div class="page-card chart-card">
        <div class="card-title">
          实时压力曲线
          <span class="realtime-tag small" :class="`live-${liveState}`">
            <span class="realtime-dot" />
            {{ liveState === 'fresh' ? '实时' : liveState === 'expired' ? '已过期' : '无实时帧' }}
          </span>
        </div>
        <div class="chart-container">
          <Line
            v-if="chartReady"
            ref="chartRef"
            :data="chartData"
            :options="chartOptions"
          />
          <!-- T322：曲线只画本轮真实帧，开局/无帧/过期都要说清楚，不给「有曲线」的错觉 -->
          <div v-if="chartNotice" class="chart-empty">{{ chartNotice }}</div>
        </div>
      </div>

      <!-- 4×5 热力图 -->
      <div class="page-card heatmap-card">
        <div class="card-title">
          采集点实时热力图
          <span class="realtime-tag small" :class="`live-${liveState}`">
            <span class="realtime-dot" />
            {{ liveState === 'fresh' ? '每秒刷新' : liveState === 'expired' ? '已过期' : '无实时帧' }}
          </span>
          <span v-if="frameStamp" class="hm-frame-stamp" title="本帧采集时刻（数据侧时间戳）">本帧 {{ frameStamp }}</span>
        </div>
        <div class="heatmap-wrap">
          <div class="hm-size-hint">压力片 4×5 网格 (40mm × 50mm)</div>
          <div v-if="!showFrame" class="hm-empty">{{ emptyFrameText }}</div>
          <div v-else class="hm-grid">
            <div v-for="row in heatmapRows" :key="'r'+row[0]?.row" class="hm-row">
              <div
                v-for="pt in row"
                :key="pt.pointId"
                :class="['hm-cell', { 'hm-cell-max': pt.isMax, 'hm-cell-pulse': pt.isMax && liveState === 'fresh' }]"
                :style="{ background: hmColor(pt.pressureValue, hmMaxN) }"
                :title="`${pt.pointId} (${pt.label}): ${fmtN(pt.pressureValue, 2)} N`"
                @click="selectHeatmapPoint(pt)"
              >
                <span class="hm-cell-id">{{ pt.pointId }}</span>
                <span class="hm-cell-val">{{ fmtN(pt.pressureValue) }}</span>
              </div>
            </div>
          </div>
          <div class="hm-legend">
            <span class="hm-lg-item"><span class="hm-swatch" style="background: #60a5fa" />低压</span>
            <span class="hm-lg-item"><span class="hm-swatch" style="background: #4ade80" />正常</span>
            <span class="hm-lg-item"><span class="hm-swatch" style="background: #facc15" />偏高</span>
            <span class="hm-lg-item"><span class="hm-swatch" style="background: #ef4444" />高压</span>
          </div>
          <div class="hm-detail">{{ heatmapDetail }}</div>
        </div>
      </div>
    </div>

    <!-- 采集点实时数值表 + 近期异常事件（设计稿 实时监控.html:151-170） -->
    <div class="bottom-row">
      <!-- 采集点表 -->
      <div class="page-card">
        <div class="card-title">
          采集点实时数值表
          <span v-if="liveState !== 'fresh'" class="tbl-note">{{ tableNote }}</span>
        </div>
        <div class="points-table-wrap">
          <table class="points-table">
            <thead>
              <tr>
                <th>采集点</th>
                <th>位置</th>
                <th>当前压力 (N)</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="pt in flatHeatmap" :key="pt.pointId" :class="{ 'point-max': pt.isMax }">
                <td>{{ pt.pointId }}</td>
                <td>{{ pt.label }}</td>
                <td :style="{ color: hmColor(pt.pressureValue, hmMaxN) }">{{ fmtN(pt.pressureValue) }}</td>
                <td>
                  <span class="status-dot" :class="pointStatus(pt.pressureValue)" />
                  {{ pointStatusLabel(pt.pressureValue) }}
                </td>
              </tr>
              <tr v-if="flatHeatmap.length === 0">
                <td colspan="4" class="empty-cell">{{ emptyFrameText }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 近期异常事件 -->
      <div class="page-card">
        <div class="card-title">近期异常事件</div>
        <div class="events-table-wrap">
          <table class="events-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>类型</th>
                <th>详情</th>
                <th>采集点</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="ev in snapshot?.alerts ?? []" :key="ev.alertId">
                <td>{{ fmtTime(ev.timestamp) }}</td>
                <td><span class="event-type" :class="eventTypeClass(ev.type)">{{ alertTypeLabel(ev.type) }}</span></td>
                <td>{{ ev.detail }}</td>
                <td>{{ ev.sensorPoint || '—' }}</td>
              </tr>
              <tr v-if="(snapshot?.alerts ?? []).length === 0">
                <td colspan="4" class="empty-cell">无异常事件</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, h, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
  type ChartOptions,
  type ChartData,
} from 'chart.js'
import type { Patient } from '@bracesync/shared-types'
import { alertTypeLabel } from '@bracesync/shared-utils'
import { fetchPatients, fetchPatientRealtime } from '../../api'
import type { RealtimeSnapshot, PressureHeatmapPoint } from '../../mock/patients'
import {
  FRAME_TAG_TEXT,
  FRAME_TTL_MS,
  formatClock,
  formatFrameAge,
  frameFreshness,
  type FrameFreshness,
} from '../../utils/frameFreshness'
import { normalizeFramePressure } from '../../utils/pressureValue'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)

// ====== 常量 ======
// F6：设计稿 实时监控.html:161 写「每秒刷新」，PRD §7D.2 写 2s —— 按四层关系以设计稿为准（T289 PM 23:39 指令）
const POLL_MS = 1000
const CHART_WINDOW = 30
// T296：色阶上界与偏高分界改由快照 heatmapMaxN / pressureHighN 下发（与告警引擎同源于 sys_configs）。
// 此前写死 60 / 45 / 30，是 T203「÷10」之前的量纲，真机亚牛顿数据在页面上恒贴底、色阶全冷。
const HM_MAX_FALLBACK = 6
const PRESS_HIGH_FALLBACK = 5
const BLUE = '#1a6db5'
const BLUE_ALPHA = 'rgba(26,109,181,0.08)'

// ====== 类型辅助 ======
interface PatientOption { patientId: string; name: string; deviceId: string | null }
type HistoryPoint = { t: string; v: number }
interface TodayPeak { value: number; pointId: string; label: string; time: string; dateKey: string }

// ====== 状态 ======
const patients = ref<PatientOption[]>([])
const selectedPatientId = ref<string>('')
const currentPatientId = ref<string>('') // 防竞态：正在请求的患者
const snapshot = ref<RealtimeSnapshot | null>(null)
// T322：帧新鲜度（后端数据侧）与拉取时刻（前端侧）分开存，二者再不同义混用
const frame = ref<FrameFreshness>({ state: 'none', collectedAt: null, ageMs: 0 })
const pullTime = ref('')
const pressureHistory = ref<HistoryPoint[]>([])
const heatmapSelected = ref<PressureHeatmapPoint | null>(null)
const chartReady = ref(false)
const chartRef = ref<InstanceType<typeof Line> | null>(null)
const todayPeak = ref<TodayPeak | null>(null)
const curFrameValue = ref(0)
let lastFrameAt: number | null | undefined // 同一帧每秒会被重读一次，曲线不能靠轮询把平线「推活」
let timer: ReturnType<typeof setInterval> | null = null

// ====== 计算属性 ======
const patientOptions = computed(() => patients.value)

/** 快照未到 = pending；到达后完全听 frameFreshness 的三态判定 */
const liveState = computed<'pending' | FrameFreshness['state']>(() =>
  snapshot.value === null ? 'pending' : frame.value.state,
)

const liveLabel = computed(() =>
  liveState.value === 'pending' ? '加载中' : FRAME_TAG_TEXT[liveState.value],
)

const showFrame = computed(() => liveState.value !== 'none' && liveState.value !== 'pending')

const frameCollectedText = computed(() => formatClock(frame.value.collectedAt) || '-')

const frameAgeText = computed(() => formatFrameAge(frame.value.ageMs))

const emptyFrameText = computed(() =>
  liveState.value === 'none' ? '无实时帧 · 不展示示例数据' : '帧已过期 · 无有效采集时刻',
)

const tableNote = computed(() =>
  liveState.value === 'expired'
    ? `以下为末次帧（${frameCollectedText.value}）数值，非当前实时`
    : emptyFrameText.value,
)

const frameNotice = computed(() => {
  if (liveState.value === 'expired') {
    const at = frameCollectedText.value === '-' ? '时刻未知' : `${frameCollectedText.value}（${frameAgeText.value}）`
    return `末次帧采集于 ${at}，已超过 ${FRAME_TTL_MS / 3600000} 小时有效期。下方压力、热力图与曲线均为末次帧数据，不代表患者当前状态。`
  }
  if (liveState.value === 'none') {
    return '该患者当前无实时帧（设备未上报或未绑定）。后端此时下发的热力图是 seed 示例兜底，页面不展示其数值。'
  }
  return ''
})

const chartNotice = computed(() => {
  if (liveState.value === 'none') return '无实时帧，曲线不绘制示例数据'
  if (liveState.value === 'expired') return '帧已过期，曲线不再推进（接 getPatientHistory 取历史帧为待办）'
  if (pressureHistory.value.length < 2) return '等待新帧…'
  return ''
})

const selectedDevice = computed(() => {
  const p = patients.value.find((x) => x.patientId === selectedPatientId.value)
  return p?.deviceId ?? ''
})

const statusLabel = computed(() => {
  const s = snapshot.value?.status
  if (s === 'online') return '佩戴中'
  if (s === 'abnormal') return '异常'
  if (s === 'offline') return '未佩戴'
  return '加载中'
})

// T296：渲染口径跟随后端下发值，缺字段（旧镜像/未部署）时回落到 T203 后的默认量纲
function positiveNum(v: unknown): number | null {
  return typeof v === 'number' && v > 0 && Number.isFinite(v) ? v : null
}
const hmMaxN = computed(() => positiveNum(snapshot.value?.heatmapMaxN) ?? HM_MAX_FALLBACK)
const pressHighN = computed(() => positiveNum(snapshot.value?.pressureHighN) ?? PRESS_HIGH_FALLBACK)

/** 压力值渲染：先按位数取整再归一 -0，避免亚阈值负值显示成「-0」这种非物理读数 */
function fmtN(v: number, digits = 1): string {
  const r = Number(v.toFixed(digits))
  return (Object.is(r, -0) ? 0 : r).toFixed(digits)
}

/** 本帧采集时刻（数据侧时间戳）+ 帧龄：热力图与设备逐帧日志对账的唯一凭据（T296/T322） */
const frameStamp = computed(() => {
  if (frame.value.collectedAt === null || liveState.value === 'pending') return ''
  return `${formatClock(frame.value.collectedAt)} 采集 · ${formatFrameAge(frame.value.ageMs)}`
})

/** 将 20 个 heatmap 点按 4 行分组 (每行 5 点，row 优先 P01-P20)；无帧时不渲染任何格子 */
const heatmapRows = computed<PressureHeatmapPoint[][]>(() => {
  if (!showFrame.value) return []
  const pts = snapshot.value?.pressureHeatmap ?? []
  if (pts.length !== 20) {
    // 兜底空行（避免渲染错误）
    const empty: PressureHeatmapPoint[] = Array.from({ length: 20 }, (_, i) => ({
      pointId: `P${String(i + 1).padStart(2, '0')}`,
      row: Math.floor(i / 5) + 1,
      col: (i % 5) + 1,
      label: `R${Math.floor(i / 5) + 1}C${(i % 5) + 1}`,
      pressureValue: 0,
      isMax: false,
    }))
    return [empty.slice(0, 5), empty.slice(5, 10), empty.slice(10, 15), empty.slice(15, 20)]
  }
  return [pts.slice(0, 5), pts.slice(5, 10), pts.slice(10, 15), pts.slice(15, 20)]
})

const heatmapDetail = computed(() => {
  if (!showFrame.value) return emptyFrameText.value
  const pts = snapshot.value?.pressureHeatmap ?? []
  const maxPt = pts.find((p) => p.isMax)
  const sel = heatmapSelected.value
  if (sel) {
    return `${sel.isMax ? '★ ' : ''}当前选中：${sel.pointId} (${sel.label}) · ${sel.pressureValue.toFixed(2)} N`
  }
  if (maxPt) {
    return `★ 压力最大点：${maxPt.pointId} (${maxPt.label}) · ${maxPt.pressureValue.toFixed(2)} N`
  }
  return '点击热力图格子查看点位数值'
})

// 扁平化的 20 个采集点（设计稿 3.2 采集点表）；无帧时交给表内空态行，不铺 20 行 0.0
const flatHeatmap = computed<PressureHeatmapPoint[]>(() => {
  if (!showFrame.value) return []
  const pts = snapshot.value?.pressureHeatmap ?? []
  if (pts.length === 20) return pts
  return Array.from({ length: 20 }, (_, i) => ({
    pointId: `P${String(i + 1).padStart(2, '0')}`,
    row: Math.floor(i / 5) + 1,
    col: (i % 5) + 1,
    label: `R${Math.floor(i / 5) + 1}C${(i % 5) + 1}`,
    pressureValue: 0,
    isMax: false,
  }))
})

// 采集点压力状态（设计稿 3.2；分界与后端 PointStatus 同源：0.75× / 1.0× pressureHighN）
function pointStatus(v: number): string {
  if (v <= 0) return 'status-offline'
  if (v >= pressHighN.value) return 'status-danger'
  if (v >= 0.75 * pressHighN.value) return 'status-warn'
  return 'status-ok'
}
function pointStatusLabel(v: number): string {
  if (v <= 0) return '无信号'
  if (v >= pressHighN.value) return '偏高'
  if (v >= 0.75 * pressHighN.value) return '关注'
  return '正常'
}

// 时间格式化（HH:mm）
function fmtTime(iso: string): string {
  try {
    const d = new Date(iso)
    const h = String(d.getHours()).padStart(2, '0')
    const m = String(d.getMinutes()).padStart(2, '0')
    return `${h}:${m}`
  } catch {
    return iso
  }
}

// 告警类型标签走 shared-utils 的 ALERT_TYPE_LABELS（T289 2.6 全站唯一口径）；样式仍按设计稿 3.3
function eventTypeClass(type: string): string {
  const map: Record<string, string> = {
    pressure_high: 'ev-danger',
    wear_interrupt: 'ev-warn',
    pressure_fluctuation: 'ev-warn',
    sensor_drift: 'ev-info',
  }
  return map[type] ?? 'ev-info'
}

// ====== Chart.js 配置 ======
const chartData = computed<ChartData<'line'>>(() => ({
  labels: pressureHistory.value.map((d) => d.t),
  datasets: [
    {
      label: '压力 (N)',
      data: pressureHistory.value.map((d) => d.v),
      borderColor: BLUE,
      backgroundColor: BLUE_ALPHA,
      fill: true,
      tension: 0.3,
      pointRadius: 0,
      borderWidth: 2,
    },
  ],
}))

// 曲线纵轴与色阶共用快照下发的上界（写死会让亚牛顿真机数据整条线贴底，T296）
/** 纵轴刻度步长：向上取整到 1/2/5×10^k，避免出现 19.33N 这种刻度 */
function niceStep(span: number): number {
  const raw = span / 3
  const mag = 10 ** Math.floor(Math.log10(raw || 1))
  return Math.ceil(raw / mag) * mag
}

// 数据超出下发量程时抬高纵轴，否则曲线会被裁到画布外（Chart.js 不画越界段）
const chartYMax = computed(() => {
  const dataMax = pressureHistory.value.reduce((m, d) => Math.max(m, d.v), 0)
  const ceil = Math.max(hmMaxN.value, dataMax)
  return niceStep(ceil) * 3
})

const chartOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 200 },
  plugins: {
    legend: { display: false },
    tooltip: {
      mode: 'index',
      intersect: false,
      callbacks: {
        label: (c) => `压力：${fmtN(Number(c.parsed.y))} N`,
      },
    },
  },
  scales: {
    y: {
      min: 0,
      max: chartYMax.value,
      ticks: { stepSize: niceStep(chartYMax.value), callback: (v) => `${fmtN(Number(v))}N` },
      grid: { color: '#f0f0f0' },
    },
    x: {
      grid: { display: false },
      ticks: { maxTicksLimit: 8 },
    },
  },
}))

// ====== 工具函数 ======

/** 色阶映射：v/max 分四档 */
function hmColor(v: number, max: number): string {
  if (v < 0) v = 0
  const r = Math.min(v / max, 1)
  if (r < 0.25) return '#60a5fa'
  if (r < 0.5) return '#4ade80'
  if (r < 0.75) return '#facc15'
  return '#ef4444'
}

function selectHeatmapPoint(pt: PressureHeatmapPoint) {
  heatmapSelected.value = pt
}

/** 曲线点只由真实帧产生：横轴用帧采集时刻（数据侧），不用拉取时刻（T322） */
function pushHistory(val: number, atMs: number | null) {
  pressureHistory.value.push({ t: formatClock(atMs ?? Date.now()), v: val })
  if (pressureHistory.value.length > CHART_WINDOW) {
    pressureHistory.value.shift()
  }
}

function resetHistory() {
  pressureHistory.value = []
  heatmapSelected.value = null
  todayPeak.value = null
  curFrameValue.value = 0
  lastFrameAt = undefined
}

// ====== 数据加载 ======
async function loadPatients() {
  try {
    const res = await fetchPatients({ page: 1, pageSize: 50 })
    patients.value = res.list.map((p: Patient) => ({
      patientId: p.patientId,
      name: p.name,
      deviceId: p.deviceId,
    }))
    // 默认选第一个有 deviceId 的患者，若全无则选第一个
    const firstWithDevice = patients.value.find((p) => p.deviceId)
    selectedPatientId.value = firstWithDevice?.patientId ?? patients.value[0]?.patientId ?? ''
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载患者列表失败')
  }
}

async function refreshTick() {
  const pid = selectedPatientId.value
  if (!pid) return
  const pullAt = Date.now()
  currentPatientId.value = pid
  heatmapSelected.value = null
  try {
    const snap = await fetchPatientRealtime(pid)
    // 竞态防护：请求返回时若患者已切换则丢弃
    if (currentPatientId.value !== pid) return
    // 校准减基线后的负读数在这一层归零：热力图 / 采集点表 / 由它取最大值的曲线共用同一份数值
    snapshot.value = normalizeFramePressure(snap)
    pullTime.value = formatClock(pullAt)
    frame.value = frameFreshness(snap.pressureRecords, pullAt)
    // 同一帧每秒都会被重新读到：只有换了帧才允许推进曲线与峰值（T322 防「平线被轮询推活」）
    const isNewFrame = frame.value.collectedAt !== lastFrameAt
    lastFrameAt = frame.value.collectedAt

    // ===== T079 逐帧 max：一律以 heatmap 20 点最大值为基准，弃用 snap.maxPressure =====
    const hm = snap.pressureHeatmap ?? []
    const curMaxPt = hm.reduce<PressureHeatmapPoint | null>((max, p) => {
      if (!max || p.pressureValue > max.pressureValue) return p
      return max
    }, null)
    const curV = curMaxPt?.pressureValue ?? 0
    // 无帧时后端给的是 seed 兜底值，不参与任何显示与统计
    curFrameValue.value = frame.value.state === 'none' ? 0 : curV

    // ===== 今日峰值累计（跨日自动重置、仅 curV > 0 才写入，避免 0N 占位） =====
    const dateKey = `${new Date(pullAt).getFullYear()}-${String(new Date(pullAt).getMonth() + 1).padStart(2, '0')}-${String(new Date(pullAt).getDate()).padStart(2, '0')}`
    if (todayPeak.value && todayPeak.value.dateKey !== dateKey) {
      // 跨日：清零昨日峰值
      todayPeak.value = null
    }
    const peakTime = formatClock(frame.value.collectedAt ?? pullAt)
    // 无帧患者的 heatmap 是 seed 兜底 ⇒ 不许进峰值，否则「最大压力采集点」会显示一个示例点位号
    if (isNewFrame && frame.value.state !== 'none' && curV > 0 && curV > (todayPeak.value?.value ?? -1) && curMaxPt) {
      todayPeak.value = {
        value: curV,
        pointId: curMaxPt.pointId,
        label: curMaxPt.label,
        time: peakTime,
        dateKey,
      }
    }

    // 曲线只收「未过期的新帧」；过期/无帧时宁可不画，也不制造在动的样子
    if (isNewFrame && frame.value.state === 'fresh') pushHistory(curV, frame.value.collectedAt)
  } catch (e: unknown) {
    if (currentPatientId.value === pid) {
      ElMessage.error(e instanceof Error ? e.message : '实时数据刷新失败')
    }
  }
}

function handlePatientChange(pid: string) {
  if (!pid) return
  // 重置历史，立即刷新一次（不等待下一轮轮询）
  resetHistory()
  void refreshTick()
}

// ====== 生命周期 ======
watch(selectedPatientId, (id, oldId) => {
  if (id && id !== oldId) {
    handlePatientChange(id)
  }
})

onMounted(async () => {
  await loadPatients()
  // 让 vue-chartjs 先挂载，避免首次 render 报错
  chartReady.value = true
  await nextTick()
  if (selectedPatientId.value) {
    await refreshTick()
  }
  timer = setInterval(() => {
    void refreshTick()
  }, POLL_MS)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})

// 为了避免 h unused 警告（vue-chartjs 某些版本 TS 要求）
void h
</script>

<style scoped>
.monitor {
  min-height: 100%;
  padding: 16px 0;
}

/* ===== 顶部刷新栏 ===== */
.page-toolbar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 0 20px 12px;
}
.update-time {
  font-size: 13px;
  color: #999;
  flex: 1;
}
.time-split {
  margin: 0 8px;
  color: #d8dee9;
}
.realtime-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #10ac84;
  font-weight: 500;
}
.realtime-tag.small { font-size: 11px; margin-left: 10px; }
.realtime-dot {
  width: 6px; height: 6px; border-radius: 50%;
  background: #10ac84;
  animation: rtPulse 1.5s infinite;
}
/* T322：非实时态不得继续闪绿点 —— 脉冲本身就是「数据在动」的暗示 */
.realtime-tag.live-expired { color: #b45309; }
.realtime-tag.live-expired .realtime-dot { background: #f59e0b; animation: none; }
.realtime-tag.live-none { color: #64748b; }
.realtime-tag.live-none .realtime-dot { background: #cbd5e1; animation: none; }
.realtime-tag.live-pending { color: #94a3b8; }
.realtime-tag.live-pending .realtime-dot { background: #e2e8f0; animation: none; }

/* T322 帧新鲜度告警条 */
.frame-notice {
  margin: 0 20px 14px;
  padding: 10px 14px;
  border-radius: 10px;
  font-size: 13px;
  line-height: 1.6;
}
.notice-expired {
  background: #fff7ed;
  border: 1px solid #fed7aa;
  color: #9a3412;
}
.notice-none {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  color: #475569;
}
@keyframes rtPulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.4; transform: scale(0.8); }
}

/* ===== 通用卡片 ===== */
.page-card {
  background: #fff;
  border-radius: 12px;
  padding: 18px 20px;
  margin: 0 20px 16px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}
.card-title {
  font-size: 15px;
  font-weight: 600;
  color: #333;
  margin-bottom: 14px;
  display: flex;
  align-items: center;
}
/* 采集点表在过期/无帧时给一句口径说明（列名保持设计稿四列不动） */
.tbl-note {
  margin-left: auto;
  font-size: 12px;
  font-weight: 400;
  color: #b45309;
}

/* T296：本帧采集时刻（数据侧时间戳），供与设备逐帧日志对账 */
.hm-frame-stamp {
  margin-left: auto;
  font-size: 12px;
  font-weight: 400;
  color: #64748b;
}

/* ===== 患者选择 ===== */
.patient-card .patient-bar {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}
.status-indicator {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
}
.status-indicator .status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}
.status-indicator.status-online { color: #10ac84; }
.status-indicator.status-online .status-dot { background: #10ac84; animation: rtPulse 2s infinite; }
.status-indicator.status-abnormal { color: #ee5a24; }
.status-indicator.status-abnormal .status-dot { background: #ee5a24; animation: rtPulse 1s infinite; }
.status-indicator.status-offline { color: #999; }
.status-indicator.status-offline .status-dot { background: #ccc; }
.device-hint {
  font-size: 12px;
  color: #888;
}

/* ===== 双栏布局 ===== */
.charts-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}
@media (max-width: 1100px) {
  .charts-row { grid-template-columns: 1fr; }
}

/* ===== 曲线卡片 ===== */
.chart-card .chart-container {
  position: relative;
  width: 100%;
  height: 280px;
}
/* 无帧/过期时盖住画布区，不给「曲线在跑」的观感 */
.chart-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 12px;
  text-align: center;
  font-size: 13px;
  color: #64748b;
  background: rgba(255, 255, 255, 0.9);
}

/* ===== 热力图卡片 ===== */
.heatmap-card .heatmap-wrap {
  text-align: center;
}
.hm-empty {
  padding: 48px 0;
  border: 1px dashed #e2e8f0;
  border-radius: 10px;
  font-size: 13px;
  color: #94a3b8;
}
.hm-size-hint {
  font-size: 11px;
  color: #999;
  margin-bottom: 10px;
}
.hm-grid {
  display: inline-flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
}
.hm-row {
  display: flex;
  gap: 5px;
}
.hm-cell {
  width: 54px;
  height: 54px;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
  border: 2px solid transparent;
  color: #fff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.25);
  position: relative;
  user-select: none;
}
.hm-cell:hover {
  transform: scale(1.08);
  z-index: 1;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.18);
}
.hm-cell-max::after {
  content: '★';
  position: absolute;
  top: 1px;
  right: 4px;
  font-size: 10px;
  color: #fff;
  text-shadow: 0 0 3px rgba(0, 0, 0, 0.6);
}
.hm-cell-id {
  font-size: 10px;
  font-weight: 600;
  opacity: 0.9;
  line-height: 1;
}
.hm-cell-val {
  font-size: 14px;
  font-weight: 700;
  line-height: 1.2;
  margin-top: 2px;
}
.hm-cell-pulse {
  animation: hmPulse 1.2s ease-in-out infinite;
}
@keyframes hmPulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(238, 90, 36, 0.5); }
  50% { box-shadow: 0 0 0 6px rgba(238, 90, 36, 0); }
}
.hm-legend {
  display: flex;
  justify-content: center;
  gap: 18px;
  margin-top: 12px;
  font-size: 11px;
  color: #888;
}
.hm-lg-item {
  display: flex;
  align-items: center;
  gap: 4px;
}
.hm-swatch {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  flex-shrink: 0;
}
.hm-detail {
  margin-top: 10px;
  font-size: 12px;
  color: #ee5a24;
  font-weight: 500;
  min-height: 18px;
}

/* ===== 今日峰值卡片 ===== */
.peak-card .peak-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
}
@media (max-width: 768px) {
  .peak-card .peak-grid { grid-template-columns: repeat(2, 1fr); }
}
.peak-cell {
  background: #f8fafc;
  border-radius: 10px;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 6px;
  border: 1px solid #eef2f7;
}
.peak-label {
  font-size: 12px;
  color: #94a3b8;
  font-weight: 500;
  letter-spacing: 0.3px;
}
.peak-num {
  font-size: 28px;
  font-weight: 700;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}
.peak-text {
  font-size: 16px;
  font-weight: 600;
  color: #334155;
  line-height: 1.2;
}
.peak-cell.peak-value {
  background: linear-gradient(135deg, #f8fafc 0%, #eef5ff 100%);
  border-color: #dbeafe;
}

/* ====== 底部：采集点表 + 异常事件 ====== */
.bottom-row {
  display: grid;
  grid-template-columns: 1.2fr 1fr;
  gap: 16px;
}
@media (max-width: 992px) {
  .bottom-row { grid-template-columns: 1fr; }
}
.points-table-wrap, .events-table-wrap {
  max-height: 360px;
  overflow-y: auto;
}
.points-table, .events-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.points-table th, .points-table td,
.events-table th, .events-table td {
  padding: 8px 10px;
  text-align: left;
  border-bottom: 1px solid #f0f2f5;
}
.points-table thead th, .events-table thead th {
  position: sticky;
  top: 0;
  background: #f8fafc;
  color: #64748b;
  font-weight: 600;
  font-size: 12px;
  z-index: 1;
}
.points-table tbody tr:hover, .events-table tbody tr:hover {
  background: #f8fafc;
}
.points-table tbody tr.point-max {
  background: #fef3c7;
  font-weight: 600;
}
.empty-cell {
  text-align: center;
  color: #cbd5e1;
  padding: 24px 0;
}
.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  vertical-align: middle;
}
.status-ok { background: #10ac84; }
.status-warn { background: #facc15; }
.status-danger { background: #ef4444; }
.status-offline { background: #cbd5e1; }
.event-type {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 500;
}
.ev-danger { background: #fee2e2; color: #b91c1c; }
.ev-warn { background: #fef3c7; color: #92400e; }
.ev-info { background: #dbeafe; color: #1e40af; }
</style>
