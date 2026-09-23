<template>
  <div class="orthosis-log">
    <!-- T289 8.1（PM 裁定）：设计稿的跨患者日志列表为默认视图，PRD §7D.11 的「选择患者」工作台并存保留 -->
    <el-tabs v-model="viewMode" class="view-tabs">
      <el-tab-pane label="日志列表" name="logs">
        <div class="view-logs">
          <div class="page-card log-filter-card">
            <div class="page-card-title">筛选条件</div>
            <div class="filter-bar">
              <el-input
                v-model="filters.keyword"
                placeholder="搜索患者姓名 / ID..."
                clearable
                class="log-search"
                @keyup.enter="applyFilters"
                @clear="applyFilters"
              />
              <span class="filter-label">日期范围：</span>
              <el-date-picker v-model="filters.startDate" type="date" value-format="YYYY-MM-DD" placeholder="开始日期" class="date-input" @change="applyFilters" />
              <span class="filter-label">至</span>
              <el-date-picker v-model="filters.endDate" type="date" value-format="YYYY-MM-DD" placeholder="结束日期" class="date-input" @change="applyFilters" />
              <el-select v-model="filters.feeling" placeholder="全部感受" clearable class="feeling-filter" @change="applyFilters">
                <el-option label="贴合" value="fitted" />
                <el-option label="不适" value="discomfort" />
              </el-select>
              <el-button @click="resetFilters">重置</el-button>
            </div>
          </div>

          <div class="page-card log-list-card">
            <div class="list-header">
              <span class="page-card-title">日志列表</span>
              <span class="list-count">共 {{ total }} 条记录</span>
            </div>
            <el-table :data="logs" size="small" v-loading="logsLoading" @row-click="openDetail">
              <el-table-column label="患者" width="140">
                <template #default="{ row }">
                  <div class="patient-name">{{ row.patientName || patientNameOf(row.patientId) }}</div>
                  <div class="patient-id">{{ row.patientId }}</div>
                </template>
              </el-table-column>
              <el-table-column prop="logDate" label="日期" width="120" />
              <el-table-column label="佩戴感受" width="110">
                <template #default="{ row }">
                  <el-tag :type="FEELING_TAG[row.feeling ?? 'none'] ?? 'info'" size="small">
                    {{ feelingLabel(row.feeling) }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="notes" label="备注" min-width="240" show-overflow-tooltip />
              <!-- 设计稿 矫形日志.html:127 有「提交时间」列；契约 FeelingLogDTO 未下发 created_at
                   （DB feeling_logs.created_at 存在）⇒ 后端补字段前只显示占位，不用 logDate 冒充 -->
              <el-table-column label="提交时间" width="160">
                <template #default="{ row }">{{ row.createdAt ? formatDateTime(row.createdAt) : '—' }}</template>
              </el-table-column>
              <el-table-column label="操作" width="90">
                <template #default="{ row }">
                  <el-button size="small" link type="primary" @click.stop="openDetail(row)">查看</el-button>
                </template>
              </el-table-column>
            </el-table>
            <el-pagination
              class="pagination"
              v-model:current-page="page"
              :total="total"
              :page-size="pageSize"
              layout="total, prev, pager, next"
              @current-change="loadLogs"
            />
          </div>
        </div>
      </el-tab-pane>

      <el-tab-pane label="患者工作台" name="workspace">
        <div class="view-workspace">
          <div class="page-toolbar">
            <span class="toolbar-label">选择患者：</span>
            <el-select v-model="patientId" placeholder="选择患者" class="patient-select" @change="loadPatientData">
              <el-option v-for="p in patients" :key="p.patientId" :label="`${p.name}（${p.patientId}）`" :value="p.patientId" />
            </el-select>
            <el-tag v-if="auth.role === 'doctor'" type="info" effect="plain">医护工作台：仅本团队患者（PRD §7D.11）</el-tag>
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
                <div class="page-card workspace-feelings-card">
                  <el-table :data="feelings" size="small">
                    <el-table-column prop="logDate" label="日期" width="120" />
                    <!-- T289 8.2：两档（贴合/不适）为 T256 #3 起写入口径，星级 comfortScore 仅历史兼容 -->
                    <el-table-column label="佩戴感受" width="110">
                      <template #default="{ row }">
                        <el-tag :type="FEELING_TAG[row.feeling ?? 'none'] ?? 'info'" size="small">
                          {{ feelingLabel(row.feeling) }}
                        </el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column label="舒适度" width="160">
                      <template #default="{ row }">
                        <el-rate v-if="row.comfortScore !== null" :model-value="row.comfortScore" disabled allow-half />
                        <span v-else>—</span>
                      </template>
                    </el-table-column>
                    <el-table-column label="不适部位" width="160">
                      <template #default="{ row }">
                        {{ row.discomfortAreas.length > 0 ? row.discomfortAreas.map(areaLabel).join('、') : '-' }}
                      </template>
                    </el-table-column>
                    <el-table-column prop="notes" label="患者备注" min-width="180" />
                    <el-table-column label="医生回复" min-width="200">
                      <template #default="{ row }">
                        <div v-if="row.replyContent" class="reply-content">{{ row.replyContent }}</div>
                        <div v-else>
                          <el-input
                            v-model="replyDrafts[row.logId]"
                            type="textarea"
                            :rows="2"
                            placeholder="输入回复内容..."
                            size="small"
                          />
                          <el-button
                            size="small"
                            type="primary"
                            :loading="replyingId === row.logId"
                            :disabled="!replyDrafts[row.logId]?.trim()"
                            style="margin-top: 6px"
                            @click="submitReply(row)"
                          >回复</el-button>
                        </div>
                      </template>
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
      </el-tab-pane>
    </el-tabs>

    <!-- 设计稿 矫形日志.html:204-221 日志详情弹窗 -->
    <el-dialog v-model="detailVisible" title="矫形日志详情" width="560px">
      <el-descriptions v-if="current" :column="1" border size="small">
        <el-descriptions-item label="患者姓名">{{ current.patientName || patientNameOf(current.patientId) }}</el-descriptions-item>
        <el-descriptions-item label="患者ID">{{ current.patientId }}</el-descriptions-item>
        <el-descriptions-item label="日期">{{ current.logDate }}</el-descriptions-item>
        <el-descriptions-item label="佩戴感受">{{ feelingLabel(current.feeling) }}</el-descriptions-item>
        <el-descriptions-item label="提交时间">{{ current.createdAt ? formatDateTime(current.createdAt) : '—' }}</el-descriptions-item>
      </el-descriptions>
      <div v-if="current" class="detail-note">
        <div class="detail-note-label">备注内容</div>
        <div class="detail-note-body">{{ current.notes || '—' }}</div>
      </div>
      <template #footer><el-button @click="detailVisible = false">关闭</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Patient, OrthosisPlan, FeelingLog, HealthReport } from '@bracesync/shared-types'
import {
  fetchPatients, fetchOrthosisPlans, saveOrthosisPlanApi,
  fetchFeelingLogs, fetchFeelingLogsAdmin, fetchHealthReports, replyFeelingLogApi,
  patientNameOf,
} from '../../api'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const viewMode = ref<'logs' | 'workspace'>('logs')
const patients = ref<Patient[]>([])
const patientId = ref('')
const activeTab = ref('plans')
const plans = ref<OrthosisPlan[]>([])
const feelings = ref<FeelingLog[]>([])
const reports = ref<HealthReport[]>([])
const newPlanContent = ref('')
const savingPlan = ref(false)
const replyDrafts = ref<Record<string, string>>({})
const replyingId = ref<string | null>(null)

// T289 8.1 跨患者日志流
const logs = ref<FeelingLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const logsLoading = ref(false)
const filters = reactive({ keyword: '', startDate: '', endDate: '', feeling: '' as '' | 'fitted' | 'discomfort' })
const detailVisible = ref(false)
const current = ref<FeelingLog | null>(null)

const FEELING_TAG: Record<string, 'success' | 'warning' | 'info'> = {
  fitted: 'success',
  discomfort: 'warning',
  none: 'info',
}

function feelingLabel(feeling: FeelingLog['feeling']): string {
  if (feeling === 'fitted') return '贴合'
  if (feeling === 'discomfort') return '不适'
  return '未评'
}

function formatDateTime(iso: string): string {
  return `${iso.slice(0, 10)} ${iso.slice(11, 16)}`
}

async function loadLogs() {
  logsLoading.value = true
  try {
    const res = await fetchFeelingLogsAdmin({
      keyword: filters.keyword || undefined,
      startDate: filters.startDate || undefined,
      endDate: filters.endDate || undefined,
      feeling: filters.feeling || undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    logs.value = res.list
    total.value = res.total
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    logsLoading.value = false
  }
}

function applyFilters() {
  page.value = 1
  loadLogs()
}

function resetFilters() {
  filters.keyword = ''
  filters.startDate = ''
  filters.endDate = ''
  filters.feeling = ''
  applyFilters()
}

function openDetail(row: FeelingLog) {
  current.value = row
  detailVisible.value = true
}

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

// T247 8.3: 医生回复感受日志
async function submitReply(row: FeelingLog) {
  const content = replyDrafts.value[row.logId]?.trim()
  if (!content) return
  replyingId.value = row.logId
  try {
    await replyFeelingLogApi(row.logId, content)
    row.replyContent = content
    row.replyTime = new Date().toISOString()
    delete replyDrafts.value[row.logId]
    ElMessage.success('回复成功')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '回复失败')
  } finally {
    replyingId.value = null
  }
}

onMounted(async () => {
  loadLogs()
  try {
    const res = await fetchPatients({ page: 1, pageSize: 50 })
    patients.value = res.list
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载患者列表失败')
  }
})
</script>

<style scoped>
.view-tabs {
  margin-bottom: 8px;
}
.filter-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.filter-label {
  font-size: 13px;
  color: #666;
}
.log-search {
  width: 200px;
}
.date-input {
  width: 150px;
}
.feeling-filter {
  width: 140px;
}
.list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.list-count {
  font-size: 12px;
  color: #999;
}
.patient-name {
  font-weight: 600;
}
.patient-id {
  font-size: 12px;
  color: #999;
}
.pagination {
  margin-top: 12px;
  justify-content: flex-end;
}
.detail-note {
  margin-top: 16px;
}
.detail-note-label {
  font-size: 13px;
  color: #999;
  margin-bottom: 8px;
}
.detail-note-body {
  padding: 12px;
  border-radius: 8px;
  background: #f8f9fa;
  font-size: 13px;
  line-height: 1.6;
}
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
.reply-content {
  color: #1a6db5;
  font-size: 13px;
  line-height: 1.5;
}
</style>
