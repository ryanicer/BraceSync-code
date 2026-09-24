<template>
  <div class="abnormal-report">
    <!-- ① 检索条件（设计稿 异常报告.html 第 1 张卡） -->
    <div class="page-card">
      <div class="page-card-title">
        检索条件
        <span class="card-sub">数据来源：告警事件表（同「告警管理」页数据源）+ 后端异常报告汇总端点</span>
      </div>
      <div class="filter-row">
        <span class="filter-label">患者</span>
        <el-select
          v-model="patientId"
          filterable
          remote
          reserve-keyword
          :remote-method="searchPatients"
          :loading="patientLoading"
          placeholder="输入姓名或患者ID搜索"
          class="patient-select"
        >
          <el-option
            v-for="p in patients"
            :key="p.patientId"
            :label="`${p.patientId} · ${p.name}`"
            :value="p.patientId"
          />
        </el-select>
        <span class="filter-label">起始日期</span>
        <el-date-picker v-model="start" type="date" value-format="YYYY-MM-DD" placeholder="起始日期" clearable class="date-input" />
        <span class="filter-label">结束日期</span>
        <el-date-picker v-model="end" type="date" value-format="YYYY-MM-DD" placeholder="结束日期" clearable class="date-input" />
        <span class="filter-label">快捷范围</span>
        <span class="chips">
          <button
            v-for="q in quickRanges"
            :key="q.label"
            type="button"
            class="chip"
            :class="{ 'chip-on': activeQuick === q.label }"
            @click="applyQuick(q.label)"
          >{{ q.label }}</button>
        </span>
      </div>
      <div class="filter-row">
        <el-button @click="resetFilters">重置</el-button>
        <el-button type="primary" :loading="loading" @click="loadReport">查询</el-button>
        <el-button :loading="exporting" :disabled="!report" @click="handleExport">导出 CSV</el-button>
      </div>
      <el-alert type="info" :closable="false" show-icon class="scope-tip">
        <template #title>
          数据范围：可见患者集合 = 当前账号所属团队绑定的患者（后端按身份收窄）；本页只读，
          处理动作仍在「告警管理」页完成，避免双入口写入。
        </template>
      </el-alert>
    </div>

    <p v-if="notFoundTip" class="empty-tip">{{ notFoundTip }}</p>
    <p v-else-if="emptyTip" class="empty-tip">{{ emptyTip }}</p>

    <!-- ② 患者与设备（稿面 8 格，统计区间随查询联动） -->
    <div v-if="patient" class="page-card">
      <div class="page-card-title">患者与设备</div>
      <el-descriptions :column="4" border size="small">
        <el-descriptions-item label="患者 ID">{{ patient.patientId }}</el-descriptions-item>
        <el-descriptions-item label="姓名">{{ patient.name }}</el-descriptions-item>
        <el-descriptions-item label="性别 / 年龄">{{ genderText }} / {{ patient.age ?? '-' }}</el-descriptions-item>
        <el-descriptions-item label="诊断">{{ patient.diagnosis || '-' }}</el-descriptions-item>
        <el-descriptions-item label="绑定设备">{{ patient.deviceId || '未绑定' }}</el-descriptions-item>
        <el-descriptions-item label="绑定团队">{{ patient.teamName || teamNameOf(patient.teamId) }}</el-descriptions-item>
        <el-descriptions-item label="主治医生">{{ patient.doctorName || doctorNameOf(patient.doctorId) }}</el-descriptions-item>
        <el-descriptions-item label="统计区间">{{ start }} ~ {{ end }}（{{ spanDays }} 天）</el-descriptions-item>
      </el-descriptions>
    </div>

    <!-- ③ KPI 汇总（稿面 :238-259 五卡；第 4 卡「最高频采集点」缺读端点，本轮不做，见交件登记） -->
    <div v-if="report" class="kpi-row">
      <div class="page-card kpi-card">
        <div class="kpi-value">{{ kpi.total }}</div>
        <div class="kpi-label">异常总次数</div>
        <div class="kpi-note">日均 {{ kpi.dailyAvg }} 次</div>
      </div>
      <div class="page-card kpi-card kpi-warn">
        <div class="kpi-value">{{ kpi.pressureHigh }}</div>
        <div class="kpi-label">{{ alertTypeLabel('pressure_high') }}</div>
        <div class="kpi-note">占比 {{ kpi.pressureHighShare }}%</div>
      </div>
      <div class="page-card kpi-card kpi-sec">
        <div class="kpi-value">{{ kpi.deviceOffline }}</div>
        <div class="kpi-label">{{ alertTypeLabel('wear_interrupt') }}</div>
        <div class="kpi-note">稿面「最长单次时长」缺读端点</div>
      </div>
      <div class="page-card kpi-card kpi-ok">
        <div class="kpi-value">{{ kpi.unprocessed }}</div>
        <div class="kpi-label">未处理条数</div>
        <div class="kpi-note">处理率 {{ kpi.processedRate }}%</div>
      </div>
    </div>

    <!-- ④ 图表区（稿面 4 图；按日×类型堆叠、峰值 vs 阈值、4×5 采集点网格缺读端点，本轮只做两张） -->
    <div v-if="report" class="chart-row">
      <div class="page-card chart-card">
        <div class="page-card-title">异常趋势（按日）</div>
        <div class="chart-box">
          <Bar v-if="trendData" :data="trendData" :options="barOptions" />
        </div>
      </div>
      <div class="page-card chart-card">
        <div class="page-card-title">异常类型构成</div>
        <div class="chart-box">
          <Doughnut v-if="composeData" :data="composeData" :options="doughnutOptions" />
        </div>
      </div>
    </div>

    <!-- ⑤ 文字汇总（稿面固定 6 段中的 ①②⑤⑥；③④ 见交件登记） -->
    <div v-if="report" class="page-card">
      <div class="page-card-title">
        文字汇总
        <span class="card-sub">按模板自动生成，纯展示不可编辑</span>
      </div>
      <div class="summary-box">
        <p class="summary-head">{{ summaryHead }}</p>
        <p v-for="line in summary" :key="line" class="summary-line">{{ line }}</p>
        <p class="summary-foot">生成时间 {{ generatedAt }} · 生成人 {{ generator }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Chart as ChartJS, CategoryScale, LinearScale, BarElement, ArcElement, Tooltip, Legend } from 'chart.js'
import { Bar, Doughnut } from 'vue-chartjs'
import type { Patient } from '@bracesync/shared-types'
import { alertTypeLabel } from '@bracesync/shared-utils'
import { doctorNameOf, fetchAbnormalReport, exportAbnormalReportApi, fetchPatients, teamNameOf } from '../../api'
import type { AbnormalReport } from '../../mock/alerts'
import { kpiFromReport, lastNDays, monthToDate, rangeDays, stampText, summaryLines, type ReportKpi, type ReportRange } from '../../utils/abnormal-report'
import { useAuthStore } from '../../stores/auth'

ChartJS.register(CategoryScale, LinearScale, BarElement, ArcElement, Tooltip, Legend)

type PatientRow = Patient & { teamName?: string | null; doctorName?: string | null }

const route = useRoute()
const authStore = useAuthStore()

const patients = ref<PatientRow[]>([])
const patientLoading = ref(false)
const patientId = ref('')
const first = lastNDays(7)
const start = ref(first.start)
const end = ref(first.end)
const report = ref<AbnormalReport | null>(null)
const loading = ref(false)
const exporting = ref(false)
const generatedAt = ref('')

const QUICK = ['近 7 天', '本月至今', '近 30 天'] as const
type QuickLabel = (typeof QUICK)[number]
const quickRanges = QUICK.map((label) => ({ label }))
const activeQuick = ref('')

/**
 * 稿面「直接访问默认选中当前团队下异常数最多的患者」需要按异常数排序的患者列表，
 * 现读端点没有这个维度，本轮默认取可见列表首位（缺口已在卡内登记）。
 */
async function searchPatients(keyword = '') {
  patientLoading.value = true
  try {
    const res = await fetchPatients({ keyword: keyword || undefined, page: 1, pageSize: 20 })
    patients.value = res.list
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '患者列表加载失败')
  } finally {
    patientLoading.value = false
  }
}

const patient = computed(() => patients.value.find((p) => p.patientId === patientId.value) ?? null)
const genderText = computed(() => {
  const g = patient.value?.gender
  return g === 'male' ? '男' : g === 'female' ? '女' : '-'
})
const range = computed<ReportRange>(() => ({ start: start.value, end: end.value }))
const spanDays = computed(() => rangeDays(range.value))

const emptyTip = computed(() => {
  if (!report.value) return ''
  return report.value.total === 0 ? '所选区间内该患者无异常记录' : ''
})

/** 稿面「错误文案」：URL 指定的患者不在可见集合内时明写未找到，不悄悄换成别的患者 */
const notFound = ref(false)
const notFoundTip = computed(() => (notFound.value ? '未找到该患者的绑定记录' : ''))
const summaryHead = computed(() =>
  patient.value
    ? `区间异常汇总 · ${patient.value.name}（${patient.value.patientId}）· ${start.value} 至 ${end.value}`
    : '')

const kpi = computed<ReportKpi>(() =>
  report.value ? kpiFromReport(report.value, range.value) : {
    total: 0, dailyAvg: 0, pressureHigh: 0, pressureHighShare: 0,
    deviceOffline: 0, unprocessed: 0, processedRate: 0,
  })
const summary = computed(() => (report.value ? summaryLines(report.value, range.value) : []))

const trendData = computed(() => {
  const r = report.value
  if (!r) return null
  return {
    labels: r.byDay.map((x) => x.key),
    datasets: [{ label: '异常次数', data: r.byDay.map((x) => x.count), backgroundColor: '#e74c3c' }],
  }
})
const composeData = computed(() => {
  const r = report.value
  if (!r || r.byType.length === 0) return null
  return {
    labels: r.byType.map((x) => alertTypeLabel(x.key)),
    datasets: [{ data: r.byType.map((x) => x.count), backgroundColor: ['#e74c3c', '#f39c12', '#3498db', '#9b59b6', '#1abc9c'] }],
  }
})
/** 无数据时 KPI 显示 0、图表显示空态文案、不隐藏卡片（稿面「区间规则」） */
const barOptions = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { display: false } },
  scales: { x: { title: { display: true, text: '日期' } }, y: { beginAtZero: true, title: { display: true, text: '次数' } } },
}
const doughnutOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { position: 'right' as const } } }

function queryOrNull(): ReportRange | null {
  if (!patientId.value) {
    ElMessage.warning('请选择患者')
    return null
  }
  if (!start.value || !end.value) {
    ElMessage.warning('请选择起止日期')
    return null
  }
  if (start.value > end.value) {
    ElMessage.warning('结束日期不能早于开始日期')
    return null
  }
  return { start: start.value, end: end.value }
}

function applyQuick(label: QuickLabel) {
  activeQuick.value = label
  const r = label === '本月至今' ? monthToDate() : lastNDays(label === '近 30 天' ? 30 : 7)
  start.value = r.start
  end.value = r.end
}

function resetFilters() {
  const r = lastNDays(7)
  start.value = r.start
  end.value = r.end
  activeQuick.value = '近 7 天'
  report.value = null
  notFound.value = false
  generatedAt.value = ''
}

async function loadReport() {
  const q = queryOrNull()
  if (!q) return
  notFound.value = false
  loading.value = true
  try {
    report.value = await fetchAbnormalReport({ patientId: patientId.value, ...q })
    generatedAt.value = stampText()
  } catch (e: unknown) {
    report.value = null
    ElMessage.error(e instanceof Error ? e.message : '汇总加载失败')
  } finally {
    loading.value = false
  }
}

async function handleExport() {
  const q = queryOrNull()
  if (!q) return
  exporting.value = true
  try {
    await exportAbnormalReportApi({ patientId: patientId.value, ...q })
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '导出失败')
  } finally {
    exporting.value = false
  }
}

const generator = computed(() => authStore.user?.name ?? '-')

onMounted(async () => {
  activeQuick.value = '近 7 天'
  const q = String(route.query.patient ?? '')
  await searchPatients(q || undefined)
  if (q) {
    notFound.value = !patients.value.some((p) => p.patientId === q)
    if (notFound.value) return
    patientId.value = q
  } else if (patients.value.length > 0) {
    patientId.value = patients.value[0].patientId
  }
  if (patientId.value) await loadReport()
})
</script>

<style scoped>
.abnormal-report { padding: 16px; display: flex; flex-direction: column; gap: 14px; }
.page-card-title { font-weight: 600; margin-bottom: 10px; }
.card-sub { font-weight: 400; font-size: 12px; color: #7f8c8d; margin-left: 8px; }
.filter-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 10px; }
.filter-label { font-size: 13px; color: #34495e; }
.patient-select { width: 240px; }
.date-input { width: 150px; }
.chips { display: inline-flex; gap: 6px; }
.chip { border: 1px solid #dcdfe6; background: #fff; border-radius: 12px; padding: 2px 10px; font-size: 12px; cursor: pointer; }
.chip-on { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.scope-tip { margin-bottom: 2px; }
.empty-tip { color: #7f8c8d; font-size: 13px; margin: 0; }
.kpi-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }
/* 稿面 :70-74 stat-card = 左侧 4px 色条区分口径（默认/警示/次级/成功） */
.kpi-card { border-left: 4px solid #1a6db5; }
.kpi-warn { border-left-color: #ee5a24; }
.kpi-sec { border-left-color: #f39c12; }
.kpi-ok { border-left-color: #10ac84; }
.kpi-value { font-size: 26px; font-weight: 600; color: #333; }
.kpi-label { font-size: 13px; color: #999; margin-top: 4px; }
.kpi-note { font-size: 11px; color: #bbb; margin-top: 6px; }
.chart-row { display: grid; grid-template-columns: 3fr 2fr; gap: 14px; }
/* 图表容器固定高度 + 相对定位，切断 Chart.js responsive 的 resize 死循环（同 dashboard 页教训） */
.chart-box { position: relative; height: 240px; }
/* 稿面 :83-87：汇总框有浅底描边，抬头用主色，生成时间右对齐 */
.summary-box { background: #fbfcfd; border: 1px solid #e8ecf0; border-radius: 8px; padding: 16px 18px; }
.summary-head { font-size: 13px; font-weight: 600; color: #1a6db5; margin: 0 0 8px; }
.summary-line { font-size: 13px; line-height: 2; margin: 0; color: #444; }
.summary-foot { font-size: 11px; color: #999; margin: 10px 0 0; text-align: right; }
@media (max-width: 900px) {
  .kpi-row { grid-template-columns: repeat(2, 1fr); }
  .chart-row { grid-template-columns: 1fr; }
}
</style>
