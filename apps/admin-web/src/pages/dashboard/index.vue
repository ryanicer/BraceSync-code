<template>
  <div class="dashboard">
    <!-- 周期切换 -->
    <div class="page-toolbar">
      <el-radio-group v-model="period" @change="loadData">
        <el-radio-button value="today">今日</el-radio-button>
        <el-radio-button value="week">本周</el-radio-button>
        <el-radio-button value="month">本月</el-radio-button>
      </el-radio-group>
    </div>

    <!-- 6 KPI 卡片：自适应网格 -->
    <div class="kpi-grid">
      <div v-for="card in kpiCards" :key="card.label" :class="['kpi-card', 'kpi-' + card.color]">
        <div class="kpi-value">{{ card.value }}</div>
        <div class="kpi-label">{{ card.label }}</div>
      </div>
    </div>

    <!-- 趋势图表行：自适应双列/单列 -->
    <div class="chart-row">
      <div class="page-card chart-card">
        <el-tooltip content="近7天日均佩戴时长" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">近7天日均佩戴时长</div>
        </el-tooltip>
        <div class="chart-container">
          <Line v-if="wearTrendData" :data="wearTrendData" :options="lineOptions" />
        </div>
      </div>
      <div class="page-card chart-card">
        <el-tooltip content="近7天告警趋势" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">近7天告警趋势</div>
        </el-tooltip>
        <div class="chart-container">
          <Bar v-if="alertTrendData" :data="alertTrendData" :options="barOptions" />
        </div>
      </div>
    </div>

    <!-- 分布图表行 -->
    <div class="chart-row">
      <div class="page-card chart-card">
        <el-tooltip content="各团队管理患者数" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">各团队管理患者数</div>
        </el-tooltip>
        <div class="chart-container">
          <Bar v-if="teamChartData" :data="teamChartData" :options="barOptions" />
        </div>
      </div>
      <div class="page-card chart-card">
        <el-tooltip content="佩戴时长分布" placement="top" :show-after="300">
          <div class="page-card-title card-title-ellipsis">佩戴时长分布</div>
        </el-tooltip>
        <div class="chart-container">
          <Doughnut v-if="distributionData" :data="distributionData" :options="doughnutOptions" />
        </div>
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
import type { DashboardKPI, TeamRanking, DoctorRanking, Team } from '@bracesync/shared-types'
import {
  fetchDashboardKPI, fetchWearTrend, fetchAlertTrend, fetchTeamRanking,
  fetchDoctorRanking, fetchWearDistribution, fetchTeams,
} from '../../api'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, BarElement, ArcElement, Tooltip, Legend, Filler)

const BLUE = '#1a6db5'
const BLUE_ALPHA = 'rgba(26,109,181,0.1)'
const PALETTE = ['#1a6db5', '#2E86DE', '#10AC84', '#EE5A24', '#F39C12', '#8E44AD']

const period = ref<'today' | 'week' | 'month'>('today')
const kpi = ref<DashboardKPI | null>(null)
const wearTrend = ref<{ date: string; avgHours: number }[]>([])
const alertTrend = ref<{ date: string; count: number }[]>([])
const teamRanking = ref<TeamRanking[]>([])
const doctorRanking = ref<DoctorRanking[]>([])
const distribution = ref<{ range: string; count: number }[]>([])
const teams = ref<Team[]>([])

interface KpiCard {
  label: string
  value: string
  color: string
}

const kpiCards = computed<KpiCard[]>(() => {
  if (!kpi.value) return []
  return [
    { label: '累计患者', value: String(kpi.value.totalPatients), color: 'primary' },
    { label: '今日活跃佩戴', value: String(kpi.value.todayActiveWear), color: 'success' },
    { label: '今日告警次数', value: String(kpi.value.todayAlerts), color: 'warning' },
    { label: '平均佩戴时长', value: `${kpi.value.avgWearHours}h`, color: 'info' },
    { label: '设备在线率', value: `${kpi.value.deviceOnlineRate}%`, color: 'accent' },
    { label: '本月新增患者', value: String(kpi.value.monthNewPatients), color: 'secondary' },
  ]
})

const wearTrendData = computed(() => {
  if (wearTrend.value.length === 0) return null
  return {
    labels: wearTrend.value.map((d) => d.date),
    datasets: [{
      label: '日均佩戴时长(h)',
      data: wearTrend.value.map((d) => d.avgHours),
      borderColor: BLUE,
      backgroundColor: BLUE_ALPHA,
      fill: true,
      tension: 0.4,
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

const teamChartData = computed(() => {
  if (teams.value.length === 0) return null
  return {
    labels: teams.value.map((t) => t.name),
    datasets: [{
      label: '患者数',
      data: teams.value.map((t) => t.patientCount),
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

async function loadData() {
  try {
    const [kpiRes, wearRes, alertRes, teamRes, doctorRes, distRes, teamsRes] = await Promise.all([
      fetchDashboardKPI(period.value),
      fetchWearTrend(7),
      fetchAlertTrend(7),
      fetchTeamRanking(),
      fetchDoctorRanking(),
      fetchWearDistribution(),
      fetchTeams(),
    ])
    kpi.value = kpiRes
    wearTrend.value = wearRes
    alertTrend.value = alertRes
    teamRanking.value = teamRes
    doctorRanking.value = doctorRes
    distribution.value = distRes
    teams.value = teamsRes
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  }
}

onMounted(loadData)
</script>

<style scoped>
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
