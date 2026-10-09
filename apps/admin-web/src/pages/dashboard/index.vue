<template>
  <div class="dashboard">
    <!-- 周期切换：设计稿 数据概览.html:89-91 是原生 select 下拉（T245 §9 K10 收口），控件形态按设计稿走；
         设计稿把它画在页内顶栏右侧，本 SPA 顶栏是 MainLayout 共用区，不为单页塞插槽，留在页面工具栏 -->
    <div class="page-toolbar">
      <el-select v-model="period" class="period-select" @change="loadData">
        <el-option label="今日" value="today" />
        <el-option label="本周" value="week" />
        <el-option label="本月" value="month" />
      </el-select>
    </div>

    <!-- 6 KPI 卡片：自适应网格 -->
    <div class="kpi-grid">
      <div v-for="card in kpiCards" :key="card.label" :class="['kpi-card', 'kpi-' + card.color]">
        <div class="kpi-value">{{ card.value }}</div>
        <div class="kpi-label">{{ card.label }}</div>
        <div v-if="card.empty" class="kpi-note">暂无数据</div>
      </div>
    </div>

    <!-- 图表缺数据时走 el-empty「暂无数据」，空态口径同 orthosis-log:206-225 -->
    <!-- 趋势图表行：自适应双列/单列 -->
    <div class="chart-row">
      <div class="page-card chart-card">
        <el-tooltip :content="wearTrendTitle" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">{{ wearTrendTitle }}</div>
        </el-tooltip>
        <div v-if="wearTrendData" class="chart-container">
          <Line :data="wearTrendData" :options="lineOptions" />
        </div>
        <el-empty v-else description="暂无数据" :image-size="60" />
      </div>
      <div class="page-card chart-card">
        <el-tooltip :content="alertTrendTitle" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">{{ alertTrendTitle }}</div>
        </el-tooltip>
        <div v-if="alertTrendData" class="chart-container">
          <Bar :data="alertTrendData" :options="barOptions" />
        </div>
        <el-empty v-else description="暂无数据" :image-size="60" />
      </div>
    </div>

    <!-- 分布图表行 -->
    <div class="chart-row">
      <div class="page-card chart-card">
        <el-tooltip content="各团队管理患者数" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">各团队管理患者数</div>
        </el-tooltip>
        <div v-if="teamChartData" class="chart-container">
          <Bar :data="teamChartData" :options="barOptions" />
        </div>
        <el-empty v-else description="暂无数据" :image-size="60" />
      </div>
      <div class="page-card chart-card">
        <el-tooltip content="佩戴时长分布" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">佩戴时长分布</div>
        </el-tooltip>
        <div v-if="distributionData" class="chart-container">
          <Doughnut :data="distributionData" :options="doughnutOptions" />
        </div>
        <el-empty v-else description="暂无数据" :image-size="60" />
      </div>
    </div>

    <!-- 2 排行 -->
    <div class="chart-row">
      <div class="page-card chart-card">
        <el-tooltip content="团队佩戴达标排行" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">团队佩戴达标排行</div>
        </el-tooltip>
        <div class="table-scroll">
          <el-table :data="teamRanking" size="small">
            <el-table-column prop="rank" label="排名" width="70" />
            <el-table-column prop="teamName" label="团队" />
            <el-table-column prop="patientCount" label="患者数" width="90" />
            <el-table-column label="日均佩戴" width="100">
              <template #default="{ row }">{{ row.avgDailyWear }}h</template>
            </el-table-column>
            <el-table-column label="达标率" width="100">
              <template #default="{ row }">
                <el-tag :type="complianceTagType(row.complianceRate)" size="small">{{ row.complianceRate }}%</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
      <div class="page-card chart-card">
        <el-tooltip content="医生管理患者排行" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">医生管理患者排行</div>
        </el-tooltip>
        <div class="table-scroll">
          <el-table :data="doctorRanking" size="small">
            <el-table-column prop="rank" label="排名" width="70" />
            <el-table-column prop="doctorName" label="医生" width="100" />
            <el-table-column prop="teamName" label="团队" />
            <el-table-column prop="patientCount" label="管理患者" width="90" />
            <el-table-column label="达标率" width="100">
              <template #default="{ row }">
                <el-tag :type="complianceTagType(row.complianceRate)" size="small">{{ row.complianceRate }}%</el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Line, Bar, Doughnut } from 'vue-chartjs'
import {
  Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement,
  BarElement, ArcElement, Tooltip, Legend, Filler,
} from 'chart.js'
import type { DashboardKPI, TeamRanking, DoctorRanking } from '@bracesync/shared-types'
import { userErrorCopy } from '@bracesync/shared-utils'
import {
  fetchDashboardKPI, fetchWearTrend, fetchAlertTrend, fetchTeamRanking,
  fetchDoctorRanking, fetchWearDistribution,
} from '../../api'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, BarElement, ArcElement, Tooltip, Legend, Filler)

const BLUE = '#1a6db5'
const BLUE_ALPHA = 'rgba(26,109,181,0.1)'
const PALETTE = ['#1a6db5', '#2E86DE', '#10AC84', '#EE5A24', '#F39C12', '#8E44AD']

const period = ref<'today' | 'week' | 'month'>('today')

// 两条趋势端点只吃 days（后端 T489 未扩 period），前端按派发单口径映射：
// today/week 都是近 7 日（维持现状），month 30 日 —— 与后端 periodWindow 的 7/30 自然日窗口对齐。
const trendDaysParam = computed(() => (period.value === 'month' ? 30 : 7))

// 设计稿 数据概览.html:130,134 的标题是「今日」态的静态快照；默认态下这两个串与稿面逐字相同，
// 只有切到本月才会变成「近30天」，避免选了三十年窗口还挂着「近7天」。
const wearTrendTitle = computed(() => `近${trendDaysParam.value}天日均佩戴时长`)
const alertTrendTitle = computed(() => `近${trendDaysParam.value}天告警趋势`)

const kpi = ref<DashboardKPI | null>(null)
const wearTrend = ref<{ date: string; avgHours: number | null }[]>([])
const alertTrend = ref<{ date: string; count: number }[]>([])
const teamRanking = ref<TeamRanking[]>([])
const doctorRanking = ref<DoctorRanking[]>([])
const distribution = ref<{ range: string; count: number }[]>([])

interface KpiCard {
  label: string
  value: string
  color: string
  empty?: boolean
}

// 🔴 T636：后端把「窗口内查不到」回成 null（不再 COALESCE 成 0），这里只负责把它渲染成空态。
// 单位跟着消失：0h / 0% 是「有人在戴但时长为零」的读数，null 只能是「—」+「暂无数据」。
const NO_DATA = '—'

const kpiCards = computed<KpiCard[]>(() => {
  if (!kpi.value) return []
  const avgWearHours = kpi.value.avgWearHours
  const deviceOnlineRate = kpi.value.deviceOnlineRate
  return [
    { label: '累计患者', value: String(kpi.value.totalPatients), color: 'primary' },
    { label: '今日活跃佩戴', value: String(kpi.value.todayActiveWear), color: 'success' },
    { label: '今日告警次数', value: String(kpi.value.todayAlerts), color: 'warning' },
    {
      label: '平均佩戴时长',
      value: avgWearHours === null ? NO_DATA : `${avgWearHours}h`,
      color: 'info',
      empty: avgWearHours === null,
    },
    {
      label: '设备在线率',
      value: deviceOnlineRate === null ? NO_DATA : `${deviceOnlineRate}%`,
      color: 'accent',
      empty: deviceOnlineRate === null,
    },
    { label: '本月新增患者', value: String(kpi.value.monthNewPatients), color: 'secondary' },
  ]
})

// 全 null ⇒ 这张图没有任何真实数据可画，画出来是一条贴着 0 的假线；整块换成「暂无数据」占位。
// 有行有 null 混排时仍出图，缺行日由 null 断线（spanGaps: false）。
const wearTrendData = computed(() => {
  if (wearTrend.value.length === 0) return null
  if (wearTrend.value.every((d) => d.avgHours === null)) return null
  return {
    labels: wearTrend.value.map((d) => d.date),
    datasets: [{
      label: '日均佩戴时长(h)',
      data: wearTrend.value.map((d) => d.avgHours),
      borderColor: BLUE,
      backgroundColor: BLUE_ALPHA,
      fill: true,
      spanGaps: false,
    }],
  }
})

const alertTrendData = computed(() => {
  if (alertTrend.value.length === 0) return null
  return {
    labels: alertTrend.value.map((d) => d.date),
    datasets: [{
      label: '告警次数',
      data: alertTrend.value.map((d) => d.count),
      backgroundColor: '#EE5A24',
      borderRadius: 6,
    }],
  }
})

// 各团队管理患者数取团队排行而非 GET /teams：设计稿 数据概览.html:237,239 这张图的 labels/data
// 就是同页「团队佩戴达标排行」表的团队名与患者数列（154-162），且 team-ranking 在 staff 读权限内
// （/teams 是 admin 专属，医护角色打它必 403）。
const teamChartData = computed(() => {
  if (teamRanking.value.length === 0) return null
  return {
    labels: teamRanking.value.map((t) => t.teamName),
    datasets: [{
      label: '患者数',
      data: teamRanking.value.map((t) => t.patientCount),
      backgroundColor: PALETTE,
      borderRadius: 6,
    }],
  }
})

const distributionData = computed(() => {
  if (distribution.value.length === 0) return null
  return {
    labels: distribution.value.map((d) => d.range),
    datasets: [{
      data: distribution.value.map((d) => d.count),
      backgroundColor: ['#EE5A24', '#F39C12', '#2E86DE', BLUE, '#10AC84'],
      borderWidth: 0,
    }],
  }
})

const lineOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }
const barOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }
const doughnutOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { position: 'bottom' as const } } }

function complianceTagType(rate: number): 'success' | 'primary' | 'warning' {
  if (rate >= 90) return 'success'
  if (rate >= 80) return 'primary'
  return 'warning'
}

// 局部失败不清盘：Promise.all 是「一个 reject 全盘弃」，T348 现场就是被一个非核心请求的 403
// 带走了 6 张 KPI、4 张图和 2 张排行表。改为逐项落盘，失败项聚成一条提示。
function applySettled<T>(res: PromiseSettledResult<T>, set: (value: T) => void, errors: string[]) {
  if (res.status === 'fulfilled') {
    set(res.value)
    return
  }
  errors.push(userErrorCopy(res.reason, { scope: 'admin', fallback: '部分数据加载失败' }))
}

async function loadData() {
  const days = trendDaysParam.value
  const [kpiRes, wearRes, alertRes, teamRankRes, doctorRes, distRes] = await Promise.allSettled([
    fetchDashboardKPI(period.value),
    fetchWearTrend(days),
    fetchAlertTrend(days),
    fetchTeamRanking(period.value),
    fetchDoctorRanking(period.value),
    fetchWearDistribution(period.value),
  ])
  const errors: string[] = []
  applySettled(kpiRes, (v) => { kpi.value = v }, errors)
  applySettled(wearRes, (v) => { wearTrend.value = v }, errors)
  applySettled(alertRes, (v) => { alertTrend.value = v }, errors)
  applySettled(teamRankRes, (v) => { teamRanking.value = v }, errors)
  applySettled(doctorRes, (v) => { doctorRanking.value = v }, errors)
  applySettled(distRes, (v) => { distribution.value = v }, errors)
  if (errors.length > 0) ElMessage.error(errors.join('；'))
}

onMounted(loadData)
</script>

<style scoped>
.period-select {
  width: 150px;
}

/* KPI 卡片自适应网格：≥1280px 6列，768-1279px 3列，<768px 2列 */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}

.kpi-card {
  background: #fff;
  border-radius: 12px;
  padding: 20px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  min-width: 0;
}

.kpi-value {
  font-size: 28px;
  font-weight: 600;
  color: #333;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kpi-label {
  font-size: 13px;
  color: #999;
  margin-top: 4px;
}

/* T636 空态副文案：值为「—」时补一句「暂无数据」，避免只留符号被读成 0 */
.kpi-note {
  font-size: 12px;
  color: #bbb;
  margin-top: 2px;
}

.kpi-primary { border-left: 4px solid #1a6db5; }
.kpi-success { border-left: 4px solid #10AC84; }
.kpi-warning { border-left: 4px solid #EE5A24; }
.kpi-info { border-left: 4px solid #2E86DE; }
.kpi-accent { border-left: 4px solid #8E44AD; }
.kpi-secondary { border-left: 4px solid #F39C12; }

/* 图表行：双列网格 */
.chart-row {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}

.chart-card {
  margin-bottom: 0;
  min-width: 0;
}

/* 卡片标题单行省略：窄屏下避免长标题撑高卡片 */
.card-title-ellipsis {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: block;
}

/* 图表容器：固定高度 + 相对定位，切断 Chart.js resize 死循环
   Chart.js responsive:true 会监听容器尺寸，若容器大小又受 canvas 影响会无限循环 */
.chart-container {
  position: relative;
  width: 100%;
  height: 280px;
}

/* 表格横向滚动兜底 */
.table-scroll {
  overflow-x: auto;
}

/* 平板断点（768-1279px）：KPI 3列，图表仍双列 */
@media (max-width: 1279px) and (min-width: 768px) {
  .kpi-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

/* 移动端断点（<768px）：KPI 2列，图表单列 */
@media (max-width: 767px) {
  .kpi-grid {
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
  }
  .kpi-card {
    padding: 14px;
  }
  .kpi-value {
    font-size: 22px;
  }
  .kpi-label {
    font-size: 12px;
  }
  .chart-row {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .chart-container {
    height: 220px;
  }
}
</style>
