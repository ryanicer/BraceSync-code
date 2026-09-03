<template>
  <div class="monitor">
    <!-- 顶部刷新栏 -->
    <div class="page-toolbar">
      <span class="realtime-tag">
        <span class="realtime-dot" />
        实时同步中
      </span>
      <span class="update-time">最近更新：{{ lastUpdated || '-' }}</span>
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

    <!-- 今日峰值卡片 -->
    <div class="page-card peak-card">
      <div class="card-title">
        今日峰值
        <span class="realtime-tag small">
          <span class="realtime-dot" />
          实时累计
        </span>
      </div>
      <div class="peak-grid">
        <div class="peak-cell peak-value">
          <div class="peak-label">峰值压力</div>
          <div class="peak-num" :style="{ color: hmColor(todayPeak?.value ?? 0, HM_MAX_N) }">
            {{ todayPeak ? todayPeak.value.toFixed(1) + ' N' : '--' }}
          </div>
        </div>
        <div class="peak-cell">
          <div class="peak-label">最大点位</div>
          <div class="peak-text">{{ todayPeak ? todayPeak.pointId + ' (' + todayPeak.label + ')' : '--' }}</div>
        </div>
        <div class="peak-cell">
          <div class="peak-label">发生时间</div>
          <div class="peak-text">{{ todayPeak?.time ?? '--' }}</div>
        </div>
        <div class="peak-cell">
          <div class="peak-label">当前帧值</div>
          <div class="peak-text" :style="{ color: hmColor(curFrameValue, HM_MAX_N) }">
            {{ curFrameValue.toFixed(1) }} N
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
          <span class="realtime-tag small">
            <span class="realtime-dot" />
            实时
          </span>
        </div>
        <div class="chart-container">
          <Line
            v-if="chartReady"
            ref="chartRef"
            :data="chartData"
            :options="chartOptions"
          />
        </div>
      </div>

      <!-- 4×5 热力图 -->
      <div class="page-card heatmap-card">
        <div class="card-title">
          采集点实时热力图
          <span class="realtime-tag small">
            <span class="realtime-dot" />
            每 2s 刷新
          </span>
        </div>
        <div class="heatmap-wrap">
          <div class="hm-size-hint">压力片 4×5 网格 (40mm × 50mm)</div>
          <div class="hm-grid">
            <div v-for="row in heatmapRows" :key="'r'+row[0]?.row" class="hm-row">
              <div
                v-for="pt in row"
                :key="pt.pointId"
                :class="['hm-cell', { 'hm-cell-max': pt.isMax, 'hm-cell-pulse': pt.isMax }]"
                :style="{ background: hmColor(pt.pressureValue, HM_MAX_N) }"
                :title="`${pt.pointId} (${pt.label}): ${pt.pressureValue.toFixed(1)} N`"
                @click="selectHeatmapPoint(pt)"
              >
                <span class="hm-cell-id">{{ pt.pointId }}</span>
                <span class="hm-cell-val">{{ pt.pressureValue.toFixed(0) }}</span>
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
import { fetchPatients, fetchPatientRealtime } from '../../api'
import type { RealtimeSnapshot, PressureHeatmapPoint } from '../../mock/patients'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)

// ====== 常量 ======
const POLL_MS = 2000
const CHART_WINDOW = 30
const HM_MAX_N = 60
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
const lastUpdated = ref('')
const pressureHistory = ref<HistoryPoint[]>([])
const heatmapSelected = ref<PressureHeatmapPoint | null>(null)
const chartReady = ref(false)
const chartRef = ref<InstanceType<typeof Line> | null>(null)
const todayPeak = ref<TodayPeak | null>(null)
const curFrameValue = ref(0)
let timer: ReturnType<typeof setInterval> | null = null

// ====== 计算属性 ======
const patientOptions = computed(() => patients.value)

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

/** 将 20 个 heatmap 点按 4 行分组 (每行 5 点，row 优先 P01-P20) */
const heatmapRows = computed<PressureHeatmapPoint[][]>(() => {
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

const chartOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 200 },
  plugins: {
    legend: { display: false },
    tooltip: {
      mode: 'index',
      intersect: false,
      callbacks: {
        label: (c) => `压力：${Number(c.parsed.y).toFixed(1)} N`,
      },
    },
  },
  scales: {
    y: {
      min: 0,
      max: HM_MAX_N,
      ticks: { stepSize: 20, callback: (v) => `${v}N` },
      grid: { color: '#f0f0f0' },
    },
    x: {
      grid: { display: false },
      ticks: { maxTicksLimit: 8 },
    },
  },
}

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

function pushHistory(val: number) {
  const now = new Date()
  const t = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`
  pressureHistory.value.push({ t, v: val })
  if (pressureHistory.value.length > CHART_WINDOW) {
    pressureHistory.value.shift()
  }
}

function resetHistory() {
  pressureHistory.value = []
  heatmapSelected.value = null
  todayPeak.value = null
  curFrameValue.value = 0
}

/** 构造 30 点初始历史：以当前值为基准，平滑正弦曲线 */
function initHistory(base: number) {
  const arr: HistoryPoint[] = []
  const now = Date.now()
  for (let i = CHART_WINDOW - 1; i >= 0; i--) {
    const d = new Date(now - i * POLL_MS)
    const t = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`
    const v = Math.max(0, base + Math.sin(i * 0.35) * 5 + (Math.random() * 4 - 2))
    arr.push({ t, v: Math.round(v * 10) / 10 })
  }
  pressureHistory.value = arr
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
  currentPatientId.value = pid
  heatmapSelected.value = null
  try {
    const snap = await fetchPatientRealtime(pid)
    // 竞态防护：请求返回时若患者已切换则丢弃
    if (currentPatientId.value !== pid) return
    snapshot.value = snap

    // ===== T079 逐帧 max：一律以 heatmap 20 点最大值为基准，弃用 snap.maxPressure =====
    const hm = snap.pressureHeatmap ?? []
    const curMaxPt = hm.reduce<PressureHeatmapPoint | null>((max, p) => {
      if (!max || p.pressureValue > max.pressureValue) return p
      return max
    }, null)
    const curV = curMaxPt?.pressureValue ?? 0
    curFrameValue.value = curV

    // ===== 今日峰值累计（跨日自动重置、仅 curV > 0 才写入，避免 0N 占位） =====
    const now = new Date()
    const dateKey = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`
    if (todayPeak.value && todayPeak.value.dateKey !== dateKey) {
      // 跨日：清零昨日峰值
      todayPeak.value = null
    }
    if (curV > 0 && curV > (todayPeak.value?.value ?? -1) && curMaxPt) {
      todayPeak.value = {
        value: curV,
        pointId: curMaxPt.pointId,
        label: curMaxPt.label,
        time: timeStr,
        dateKey,
      }
    }

    if (pressureHistory.value.length === 0) {
      initHistory(curV)
    } else {
      pushHistory(curV)
    }
    lastUpdated.value = timeStr
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

/* ===== 热力图卡片 ===== */
.heatmap-card .heatmap-wrap {
  text-align: center;
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
</style>
