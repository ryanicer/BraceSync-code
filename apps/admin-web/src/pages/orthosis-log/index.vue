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
            <!-- T344 第 4 块：稿面（矫形日志.html #wsSendAdvice）只定形态与位置 = 未选患者禁用 + 点击占位提示。
                 模板消息无对外端点（卡内已报 PM），故本按钮本轮不触达患者。 -->
            <el-button class="send-advice-btn" type="primary" :disabled="!patientId" @click="sendAdvice">发送建议给患者</el-button>
          </div>

          <template v-if="patientId">
            <!-- T344 第 1 块 · 患者基本信息卡（稿面 #wsProfile，PRD §7D.8 视图② 7 字段 + 稿面另加患者ID） -->
            <div class="page-card profile-card">
              <div class="page-card-title">患者基本信息</div>
              <el-descriptions v-if="profile" :column="4" border size="small">
                <el-descriptions-item label="姓名">{{ profile.name }}</el-descriptions-item>
                <el-descriptions-item label="年龄">{{ profile.age !== null ? `${profile.age} 岁` : '—' }}</el-descriptions-item>
                <el-descriptions-item label="性别">{{ genderLabel(profile.gender) }}</el-descriptions-item>
                <el-descriptions-item label="诊断">{{ profile.diagnosis || '—' }}</el-descriptions-item>
                <el-descriptions-item label="Cobb 角度">{{ profile.cobbAngle !== null ? `${profile.cobbAngle}°` : '—' }}</el-descriptions-item>
                <el-descriptions-item label="绑定设备">{{ profile.deviceId || '—' }}</el-descriptions-item>
                <el-descriptions-item label="绑定团队">{{ profileTeamName }}</el-descriptions-item>
                <el-descriptions-item label="患者ID">{{ profile.patientId }}</el-descriptions-item>
              </el-descriptions>
              <el-empty v-else description="暂无患者档案" :image-size="60" />
            </div>
            <el-tabs v-model="activeTab" @tab-change="onWsTabChange">
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

              <!-- T344 第 2/3 块 · 数据视图（稿面 #wsData：Boss 2026-09-23 裁定问题 2 的第 2、3、4 项）
                   内层 Tab 顺序照稿面「追加末位不重排」。 -->
              <el-tab-pane label="数据视图" name="data">
                <div class="page-card chart-card" v-loading="wearLoading">
                  <div class="chart-head">
                    <span class="page-card-title">压力趋势图</span>
                    <el-radio-group v-model="wearRange" size="small" @change="onRangeChange">
                      <el-radio-button :value="7">7 天</el-radio-button>
                      <el-radio-button :value="14">14 天</el-radio-button>
                      <el-radio-button :value="30">30 天</el-radio-button>
                    </el-radio-group>
                  </div>
                  <div v-if="!seriesIsEmpty(wearSeries)" class="chart-container">
                    <Line :data="pressureChartData" :options="pressureOptions" />
                  </div>
                  <el-empty v-else-if="!wearLoading" description="暂无日佩戴统计" :image-size="60" />
                  <div class="axis-note">
                    纵轴 = 日均压力（N），横轴 = 日期（后端按 Asia/Shanghai 切日）。
                    <span v-if="thresholds">虚线为压力上限线，当前取系统配置 {{ thresholds.pressureHighThresholdN }}N（PRD §7D.12，不写死数值）。</span>
                    <span v-else>虚线为压力上限线，取值来自系统配置（PRD §7D.12），不写死数值。</span>
                  </div>
                </div>

                <div class="page-card chart-card" v-loading="wearLoading">
                  <div class="chart-head">
                    <span class="page-card-title">每日佩戴时长统计</span>
                    <span class="chart-unit">单位：小时</span>
                  </div>
                  <div v-if="!seriesIsEmpty(wearSeries)" class="chart-container">
                    <Bar :data="wearChartData" :options="wearOptions" />
                  </div>
                  <el-empty v-else-if="!wearLoading" description="暂无日佩戴统计" :image-size="60" />
                  <div class="axis-note">
                    虚线为佩戴目标线，
                    <span v-if="thresholds">当前取系统配置 {{ thresholds.dailyWearTargetHours }}h（PRD §7D.12）</span>
                    <span v-else>取值来自系统配置（PRD §7D.12）</span>
                    。时间范围跟随上方趋势图的 7/14/30 天选择。
                  </div>
                </div>

                <div class="page-card chart-card" v-loading="alertsLoading">
                  <div class="chart-head">
                    <span class="page-card-title">告警记录列表</span>
                    <span class="list-count">{{ alertCountText }}</span>
                  </div>
                  <el-table v-if="wsAlerts.length > 0" :data="wsAlerts" size="small">
                    <el-table-column label="时间" width="150">
                      <template #default="{ row }">{{ formatDateTime(row.timestamp) }}</template>
                    </el-table-column>
                    <el-table-column label="告警类型" width="150">
                      <template #default="{ row }">
                        <el-tag :type="severityType(row.type)" size="small">{{ alertTypeLabel(row.type) }}</el-tag>
                      </template>
                    </el-table-column>
                    <!-- 稿面 5 列含「等级」，但 Alert 契约无等级/严重度字段（shared-types index.ts:126-146），
                         PRD 也只在 §7D.8 视图② 提过一次、§7D.6 未定义 ⇒ 占位，不拿类型着色冒充（卡内已报 PM） -->
                    <el-table-column label="等级" width="90">
                      <template #default>—</template>
                    </el-table-column>
                    <el-table-column label="处理状态" width="110">
                      <template #default="{ row }">
                        <el-tag :type="processStatusType(row.processStatus)" size="small">
                          {{ processStatusLabel(row.processStatus) }}
                        </el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column prop="detail" label="说明" min-width="220" show-overflow-tooltip />
                  </el-table>
                  <el-empty v-else-if="!alertsLoading" description="该患者暂无告警记录" :image-size="60" />
                  <div class="axis-note">
                    告警类型词表按 PRD §7D.6 现行四类；「压力波动」已由 Boss 2026-09-23 裁定问题 4 砍除，
                    但历史行仍在库里，本页不隐藏它们（与「告警管理」页同一口径：只砍写入口，不删历史）。
                    「详情」「恢复态」两轴由「告警管理」页承载，此处不重复。
                  </div>
                </div>
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
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, BarElement,
  Tooltip, Legend, Filler, type ChartData, type ChartOptions,
} from 'chart.js'
import { Line, Bar } from 'vue-chartjs'
import { alertTypeLabel } from '@bracesync/shared-utils'
import type { Alert, FeelingLog, HealthReport, OrthosisPlan, Patient } from '@bracesync/shared-types'
import {
  fetchPatients, fetchPatientDetail, fetchTeams, fetchAlerts, fetchSystemSettings,
  fetchOrthosisPlans, saveOrthosisPlanApi,
  fetchFeelingLogs, fetchFeelingLogsAdmin, fetchPatientDailyWear, fetchHealthReports, replyFeelingLogApi,
  patientNameOf, teamNameOf,
} from '../../api'
import type { SystemSettings } from '../../mock/system'
import {
  alignWearSeries, constantLine, rangeForDays, seriesIsEmpty,
  type DailyWearDay, type WearRangeDays, type WearSeries,
} from '../../utils/workbenchData'
import { areaLabel } from '../../utils/feelingAreas'
import { useAuthStore } from '../../stores/auth'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, BarElement, Tooltip, Legend, Filler)

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

// ===== T344 工作台区块 =====
/** 患者基本信息卡（稿面 #wsProfile）：后端 join 出 teamName 时优先用它，否则查组织字典 */
type PatientRow = Patient & { teamName?: string | null }
const profile = ref<PatientRow | null>(null)
const wearRange = ref<WearRangeDays>(7)
const wearRows = ref<DailyWearDay[]>([])
const wearLoading = ref(false)
const wsAlerts = ref<Alert[]>([])
const alertTotal = ref(0)
const alertsLoading = ref(false)
/** 稿面两条虚线的取值：一律来自 §7D.12 系统配置，不写死 */
const thresholds = ref<Pick<SystemSettings, 'dailyWearTargetHours' | 'pressureHighThresholdN'> | null>(null)
/** 数据视图按需加载：首屏不进工作台不该打这三个请求 */
const dataLoaded = ref(false)
/** 稿面 #wsAlertBody 取数上限：后端 alerts 分页 pageSize ≤ 100 */
const ALERT_PAGE_SIZE = 100
/** 「最近 N 条」的 N：稿面样例 3 行、未定 N，取工作台一屏可读量 20 条 */
const ALERT_SHOW_COUNT = 20

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
  dataLoaded.value = false
  profile.value = null
  wearRows.value = []
  wsAlerts.value = []
  alertTotal.value = 0
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
  loadProfile()
  // 内层 Tab 停在数据视图时切患者：该页内容当场就要有数
  if (activeTab.value === 'data') ensureDataView()
}

/** 稿面 #wsProfile 取数：GET /api/v1/admin/patients/:patientId（患者管理详情同源，不新建端点） */
async function loadProfile() {
  try {
    profile.value = await fetchPatientDetail(patientId.value) as PatientRow | null
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载患者档案失败')
  }
}

async function ensureDataView() {
  if (dataLoaded.value) return
  dataLoaded.value = true
  await Promise.all([loadWearSeries(), loadWsAlerts()])
}

function onWsTabChange(name: string | number) {
  if (name === 'data') ensureDataView()
}

async function loadWearSeries() {
  if (!patientId.value) return
  wearLoading.value = true
  try {
    const { start, end } = rangeForDays(wearRange.value)
    const [rows, settings] = await Promise.all([
      fetchPatientDailyWear(patientId.value, start, end),
      thresholds.value ? Promise.resolve(null) : fetchSystemSettings(),
    ])
    if (settings) {
      thresholds.value = {
        dailyWearTargetHours: settings.dailyWearTargetHours,
        pressureHighThresholdN: settings.pressureHighThresholdN,
      }
    }
    wearRows.value = rows
  } catch (e: unknown) {
    dataLoaded.value = false
    ElMessage.error(e instanceof Error ? e.message : '加载日佩戴统计失败')
  } finally {
    wearLoading.value = false
  }
}

async function loadWsAlerts() {
  if (!patientId.value) return
  alertsLoading.value = true
  try {
    const res = await fetchAlerts({ patientId: patientId.value, page: 1, pageSize: ALERT_PAGE_SIZE })
    // 稿面 wsAlertCount 注：「实现按最近 N 条取数」⇒ 不做时间窗，只按时间倒序截断。
    // 倒序在前端兜一次：告警页也这么排，且不依赖后端返回序（后端无 order 参数）。
    wsAlerts.value = [...res.list]
      .sort((x, y) => (x.timestamp < y.timestamp ? 1 : -1))
      .slice(0, ALERT_SHOW_COUNT)
    alertTotal.value = res.total
  } catch (e: unknown) {
    dataLoaded.value = false
    ElMessage.error(e instanceof Error ? e.message : '加载告警记录失败')
  } finally {
    alertsLoading.value = false
  }
}

/** 区间切换只影响两张图：稿面 wsSetRange（矫形日志.html:625）只重绘趋势与时长，告警列表是「最近 N 条」不随区间变 */
function onRangeChange() {
  loadWearSeries()
}

const wearSeries = computed<WearSeries>(() => alignWearSeries(wearRows.value, rangeForDays(wearRange.value)))

const pressureChartData = computed<ChartData<'line'>>(() => ({
  labels: wearSeries.value.dates,
  datasets: [
    {
      label: '日均压力（N）',
      data: wearSeries.value.avgPressure,
      borderColor: '#409eff',
      backgroundColor: 'rgba(64, 158, 255, 0.12)',
      fill: true,
      tension: 0.25,
      spanGaps: false,
    },
    {
      label: `压力上限线（${thresholds.value?.pressureHighThresholdN ?? '—'}N）`,
      data: constantLine(thresholds.value?.pressureHighThresholdN ?? 0, wearSeries.value.dates.length),
      borderColor: '#f56c6c',
      borderDash: [6, 4],
      pointRadius: 0,
      fill: false,
    },
  ],
}))

/**
 * 柱图里叠一条虚线目标线（chart.js 混合图）。TS 侧 ChartData<'bar'> 不接受
 * type: 'line' 的数据集，故整对象做一次窄化断言，运行时由 chart.js 按 dataset.type 分派。
 */
const wearChartData = computed(() => ({
  labels: wearSeries.value.dates,
  datasets: [
    {
      label: '佩戴时长（小时）',
      data: wearSeries.value.wearHours,
      backgroundColor: 'rgba(64, 158, 255, 0.55)',
    },
    {
      type: 'line' as const,
      label: `佩戴目标线（${thresholds.value?.dailyWearTargetHours ?? '—'}h）`,
      data: constantLine(thresholds.value?.dailyWearTargetHours ?? 0, wearSeries.value.dates.length),
      borderColor: '#f56c6c',
      borderDash: [6, 4],
      pointRadius: 0,
      fill: false,
    },
  ],
}) as unknown as ChartData<'bar'>)

const pressureOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { position: 'bottom' } },
  scales: { y: { beginAtZero: false, title: { display: true, text: 'N' } } },
}

const wearOptions: ChartOptions<'bar'> = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { position: 'bottom' } },
  scales: { y: { beginAtZero: true, title: { display: true, text: '小时' } } },
}

const alertCountText = computed(() => {
  if (alertTotal.value === 0) return '共 0 条'
  if (alertTotal.value <= ALERT_SHOW_COUNT) return `共 ${alertTotal.value} 条`
  return `共 ${alertTotal.value} 条，显示最近 ${ALERT_SHOW_COUNT} 条`
})

/** 档案卡 8 格用同一个占位符：teamNameOf 的 '-' 是列表页列内的旧口径，不适用于本卡 */
const profileTeamName = computed(() => {
  const p = profile.value
  if (!p) return '—'
  const name = p.teamName || (p.teamId ? teamNameOf(p.teamId) : '')
  return name && name !== '-' ? name : '—'
})

function genderLabel(gender: Patient['gender']): string {
  if (gender === 'male') return '男'
  if (gender === 'female') return '女'
  return '未填'
}

/** 与「告警管理」页同词表（该页函数是页内局部实现，未收口到共享层，收口属另一张卡的范围） */
function severityType(type: string): 'danger' | 'warning' {
  if (type === 'pressure_high' || type === 'wear_interrupt') return 'danger'
  return 'warning'
}

function processStatusLabel(status: string): string {
  return { pending: '待处理', processing: '处理中', processed: '已处理' }[status] || status
}

function processStatusType(status: string): 'warning' | 'primary' | 'success' {
  if (status === 'processed') return 'success'
  return status === 'processing' ? 'primary' : 'warning'
}

/** 稿面 #wsSendAdvice：模板消息端点尚未建（卡内已报 PM），本轮只给占位提示 */
function sendAdvice() {
  ElMessage.info('发送建议给患者的模板消息通道待后端建端点（见 T344 卡内登记）')
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
  // T350 第 8 轮打回项 2：GET /api/v1/teams 在网关是 admin 专属（rbac.go:97 adminOnlyPatterns），
  // 医护 token 打过去必 403（Ella T345 观察项 R-teams）。这一枪只是填组织字典给基本信息卡兜底，
  // 而团队名本就由 GET /admin/patients/:id 的 teamName 字段带出（user-service patientSelect LEFT JOIN teams），
  // 故按角色发：非 admin 不发（同 T348 对 dashboard 页的处置——网关的 403 是正确行为，前端不照打）。
  if (auth.role === 'admin') fetchTeams().catch(() => undefined)
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
.send-advice-btn {
  margin-left: auto;
}
.profile-card :deep(.el-descriptions__label) {
  width: 96px;
}
.chart-card {
  margin-bottom: 16px;
}
.chart-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.chart-unit {
  font-size: 12px;
  color: #999;
}
.chart-container {
  position: relative;
  width: 100%;
  height: 240px;
}
.axis-note {
  margin-top: 10px;
  font-size: 12px;
  color: #999;
  line-height: 1.6;
}
</style>
