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

    <!-- 6 KPI 卡片 -->
    <el-row :gutter="16" class="kpi-row">
      <el-col :span="4" v-for="card in kpiCards" :key="card.label">
        <div :class="['kpi-card', 'kpi-' + card.color]">
          <div class="kpi-value">{{ card.value }}</div>
          <div class="kpi-label">{{ card.label }}</div>
        </div>
      </el-col>
    </el-row>

    <!-- 趋势图表行 -->
    <el-row :gutter="16">
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title">近7天日均佩戴时长</div>
          <Line v-if="wearTrendData" :data="wearTrendData" :options="lineOptions" class="chart-canvas" />
        </div>
      </el-col>
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title">近7天告警趋势</div>
          <Bar v-if="alertTrendData" :data="alertTrendData" :options="barOptions" class="chart-canvas" />
        </div>
      </el-col>
    </el-row>

    <!-- 分布图表行 -->
    <el-row :gutter="16">
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title">各团队管理患者数</div>
          <Bar v-if="teamChartData" :data="teamChartData" :options="barOptions" class="chart-canvas" />
        </div>
      </el-col>
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title">佩戴时长分布</div>
          <Doughnut v-if="distributionData" :data="distributionData" :options="doughnutOptions" class="chart-canvas" />
        </div>
      </el-col>
    </el-row>

    <!-- 2 排行 -->
    <el-row :gutter="16">
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title">团队佩戴达标排行</div>
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
      </el-col>
      <el-col :span="12">
        <div class="page-card">
          <div class="page-card-title">医生管理患者排行</div>
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
      </el-col>
    </el-row>
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
.kpi-row {
  margin-bottom: 16px;
}
.kpi-card {
  background: #fff;
  border-radius: 12px;
  padding: 20px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}
.kpi-value {
  font-size: 28px;
  font-weight: 600;
  color: #333;
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
.chart-canvas {
  height: 280px;
}
.dashboard :deep(.el-row) {
  margin-bottom: 16px;
}
</style>
