<template>
  <div class="orthosis-log">
    <div class="page-toolbar">
      <span class="toolbar-label">选择患者：</span>
      <el-select v-model="patientId" placeholder="选择患者" class="patient-select" @change="loadPatientData">
        <el-option v-for="p in patients" :key="p.patientId" :label="`${p.name}（${p.patientId}）`" :value="p.patientId" />
      </el-select>
      <el-tag v-if="auth.role === 'doctor'" type="info" effect="plain">医生工作台：仅本团队患者（PRD §7D.11）</el-tag>
    </div>

    <template v-if="patientId">
      <el-tabs v-model="activeTab">
        <!-- 矫形方案 -->
        <el-tab-pane label="矫形方案" name="plans">
          <div class="page-card">
            <div class="page-card-title">方案调整</div>
            <el-input v-model="newPlanContent" type="textarea" :rows="3" placeholder="填写新版矫形方案（如：佩戴目标、加压区、复查计划）" />
            <el-button type="primary" class="save-btn" :loading="savingPlan" :disabled="!newPlanContent.trim()" @click="savePlan">
              保存新方案
            </el-button>
          </div>
          <div class="page-card">
            <div class="page-card-title">历史方案（{{ plans.length }}）</div>
            <el-timeline v-if="plans.length > 0">
              <el-timeline-item v-for="plan in plans" :key="plan.planId" :timestamp="`${plan.version} · ${plan.createdAt.slice(0, 10)}`">
                {{ plan.content }}
              </el-timeline-item>
            </el-timeline>
            <el-empty v-else description="暂无方案记录" :image-size="60" />
          </div>
        </el-tab-pane>

        <!-- 佩戴感受日志 -->
        <el-tab-pane label="佩戴感受" name="feelings">
          <div class="page-card">
            <el-table :data="feelings" size="small">
              <el-table-column prop="logDate" label="日期" width="120" />
              <el-table-column label="舒适度" width="160">
                <template #default="{ row }">
                  <el-rate :model-value="row.comfortScore" disabled allow-half />
                </template>
              </el-table-column>
              <el-table-column label="不适部位" width="160">
                <template #default="{ row }">
                  {{ row.discomfortAreas.length > 0 ? row.discomfortAreas.map(areaLabel).join('、') : '-' }}
                </template>
              </el-table-column>
              <el-table-column prop="notes" label="患者备注" min-width="180" />
              <el-table-column label="医生回复" min-width="160">
                <template #default="{ row }">{{ row.replyContent || '未回复' }}</template>
              </el-table-column>
            </el-table>
            <el-empty v-if="feelings.length === 0" description="暂无感受日志" :image-size="60" />
          </div>
        </el-tab-pane>

        <!-- 健康报告 -->
        <el-tab-pane label="健康报告" name="reports">
          <div class="page-card" v-for="report in reports" :key="report.reportId">
            <div class="report-header">
              <span class="report-title">
                {{ report.reportType === 'weekly' ? '周报' : '月报' }}：{{ report.periodStart }} ~ {{ report.periodEnd }}
              </span>
              <el-tag :type="trendTagType(report.trendJudgment)" size="small">{{ trendLabel(report.trendJudgment) }}</el-tag>
            </div>
            <el-descriptions :column="2" border size="small" class="report-desc">
              <el-descriptions-item label="佩戴达标率">{{ report.wearComplianceRate }}%</el-descriptions-item>
              <el-descriptions-item label="平均压力">{{ report.avgPressure }}N</el-descriptions-item>
              <el-descriptions-item label="医生建议" :span="2">{{ report.suggestion }}</el-descriptions-item>
            </el-descriptions>
          </div>
          <el-empty v-if="reports.length === 0" description="暂无健康报告" :image-size="60" />
        </el-tab-pane>
      </el-tabs>
    </template>
    <el-empty v-else description="请选择患者开始诊断评估" class="empty-placeholder" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Patient, OrthosisPlan, FeelingLog, HealthReport } from '@bracesync/shared-types'
import { fetchPatients, fetchOrthosisPlans, saveOrthosisPlanApi, fetchFeelingLogs, fetchHealthReports } from '../../api'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const patients = ref<Patient[]>([])
const patientId = ref('')
const activeTab = ref('plans')
const plans = ref<OrthosisPlan[]>([])
const feelings = ref<FeelingLog[]>([])
const reports = ref<HealthReport[]>([])
const newPlanContent = ref('')
const savingPlan = ref(false)

function areaLabel(area: string): string {
  const map: Record<string, string> = { neck: '颈部', thoracic: '胸段', lumbar: '腰段', pelvis: '骨盆' }
  return map[area] ?? area
}

function trendLabel(trend: HealthReport['trendJudgment']): string {
  const map: Record<HealthReport['trendJudgment'], string> = { up: '趋势向好', flat: '保持平稳', down: '趋势下行' }
  return map[trend] ?? trend
}

function trendTagType(trend: HealthReport['trendJudgment']): 'success' | 'info' | 'danger' {
  if (trend === 'up') return 'success'
  if (trend === 'down') return 'danger'
  return 'info'
}

async function loadPatientData() {
  if (!patientId.value) return
  try {
    const [plansRes, feelingsRes, reportsRes] = await Promise.all([
      fetchOrthosisPlans(patientId.value),
      fetchFeelingLogs(patientId.value),
      fetchHealthReports(patientId.value),
    ])
    plans.value = plansRes
    feelings.value = feelingsRes
    reports.value = reportsRes
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  }
}

async function savePlan() {
  savingPlan.value = true
  try {
    const saved = await saveOrthosisPlanApi(patientId.value, newPlanContent.value.trim())
    if (saved) plans.value = [saved, ...plans.value]
    newPlanContent.value = ''
    ElMessage.success('方案已保存')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    savingPlan.value = false
  }
}

onMounted(async () => {
  try {
    const res = await fetchPatients({ page: 1, pageSize: 50 })
    patients.value = res.list
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载患者列表失败')
  }
})
</script>

<style scoped>
.toolbar-label {
  font-size: 13px;
  color: #666;
}
.patient-select {
  width: 240px;
}
.save-btn {
  margin-top: 12px;
}
.report-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.report-title {
  font-size: 15px;
  font-weight: 600;
  color: #333;
}
.report-desc {
  margin-bottom: 8px;
}
.empty-placeholder {
  margin-top: 80px;
}
</style>
