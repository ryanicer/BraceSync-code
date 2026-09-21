<template>
  <div class="patients">
    <div class="page-toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索姓名 / 患者ID"
        clearable
        class="search-input"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-select v-model="teamFilter" placeholder="全部团队" clearable class="team-select" @change="handleSearch">
        <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
      </el-select>
      <el-button type="primary" @click="handleSearch">查询</el-button>
      <el-button type="success" @click="openCreate">添加患者</el-button>
    </div>

    <div class="page-card patient-list-card">
      <!-- 4.2（设计稿 患者管理.html:88）：列表 thead 无复选框列，批量分配改由下方独立卡片承载 -->
      <el-table :data="list" size="small" v-loading="loading" @row-click="viewDetail">
        <el-table-column prop="patientId" label="患者ID" width="110" />
        <el-table-column prop="name" label="姓名" width="100" />
        <el-table-column label="性别" width="70">
          <template #default="{ row }">{{ row.gender === 'male' ? '男' : row.gender === 'female' ? '女' : '-' }}</template>
        </el-table-column>
        <el-table-column prop="age" label="年龄" width="70" />
        <el-table-column label="诊断" min-width="180">
          <template #default="{ row }">{{ row.diagnosis || '-' }}</template>
        </el-table-column>
        <!-- 设计稿 患者管理.html:88 无以下两列，PRD §7D.3 有；T245 明令多出列不自行判删。
             插在诊断之后，让设计稿的 绑定设备 / 绑定团队 / 状态 保持相邻原序。 -->
        <el-table-column label="Cobb角" width="90">
          <template #default="{ row }">{{ row.cobbAngle ? row.cobbAngle + '°' : '-' }}</template>
        </el-table-column>
        <el-table-column label="主治医生" width="110">
          <template #default="{ row }">{{ row.doctorName || doctorNameOf(row.doctorId) }}</template>
        </el-table-column>
        <el-table-column label="绑定设备" width="130">
          <template #default="{ row }">{{ row.deviceId || '未绑定' }}</template>
        </el-table-column>
        <el-table-column label="绑定团队" width="130">
          <template #default="{ row }">{{ row.teamName || teamNameOf(row.teamId) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <!-- 设计稿 患者管理.html:92 仍写「活跃」，属稿面未回写且与 PRD §7D.3:1065/:1320 的
                 「可登录 / 不可登录」两态口径冲突 ⇒ 按 PM 09-22 01:03 裁定 ① 统一两态，不与同列「不可登录」混搭。 -->
            <el-tag :type="row.status === 'active' ? 'success' : 'warning'" size="small">
              {{ row.status === 'active' ? '可登录' : '不可登录' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        class="pagination"
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="loadData"
      />
    </div>

    <!-- T289 4.2 批量患者-团队绑定（设计稿 患者管理.html:98-108 WB-09）：
         独立卡片 + 逐行「分配至」下拉 + 确认分配。卡片只列未分配团队的患者 = F2
         （PRD §7D.3:1070「勾选未分配团队的患者」限定）。 -->
    <div class="page-card batch-bind-card">
      <div class="page-card-title">批量患者-团队绑定</div>
      <el-table
        :data="unassignedList"
        size="small"
        v-loading="batchLoading"
        class="batch-table"
        @selection-change="onBatchSelectionChange"
      >
        <el-table-column type="selection" width="40" />
        <el-table-column prop="patientId" label="患者ID" width="120" />
        <el-table-column prop="name" label="姓名" width="120" />
        <el-table-column label="当前团队" width="120">
          <template #default><el-tag type="info" size="small">未分配</el-tag></template>
        </el-table-column>
        <el-table-column label="分配至" min-width="200">
          <template #default="{ row }">
            <el-select
              v-model="batchTargetTeam[row.patientId]"
              placeholder="选择团队"
              size="small"
              class="batch-team-select"
            >
              <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
            </el-select>
          </template>
        </el-table-column>
        <template #empty>暂无未分配团队的患者</template>
      </el-table>
      <el-button
        type="primary"
        class="batch-submit"
        :disabled="!canConfirmBatch"
        :loading="batching"
        @click="confirmBatch"
      >确认分配</el-button>
    </div>

    <!-- 患者详情抽屉 -->
    <el-drawer v-model="drawerVisible" :title="detail ? `${detail.name}（${detail.patientId}）` : ''" size="420px">
      <el-descriptions v-if="detail" :column="1" border size="small">
        <el-descriptions-item label="性别">{{ detail.gender === 'male' ? '男' : detail.gender === 'female' ? '女' : '-' }}</el-descriptions-item>
        <el-descriptions-item label="年龄">{{ detail.age ?? '-' }}</el-descriptions-item>
        <el-descriptions-item label="诊断">{{ detail.diagnosis || '-' }}</el-descriptions-item>
        <el-descriptions-item label="Cobb角">{{ detail.cobbAngle ? detail.cobbAngle + '°' : '-' }}</el-descriptions-item>
        <el-descriptions-item label="所属团队">{{ detail.teamName || teamNameOf(detail.teamId) }}</el-descriptions-item>
        <el-descriptions-item label="主治医生">{{ detail.doctorName || doctorNameOf(detail.doctorId) }}</el-descriptions-item>
        <el-descriptions-item label="绑定设备">{{ detail.deviceId || '未绑定' }}</el-descriptions-item>
        <el-descriptions-item label="建档时间">{{ formatDate(detail.createdAt) }}</el-descriptions-item>
      </el-descriptions>
      <div v-if="detail" class="drawer-actions">
        <el-button type="primary" @click="openAssignTeam">分配团队</el-button>
      </div>

      <!-- T300 异常报告最小入口：按患者 + 日期范围汇总，并导出同口径 CSV 明细 -->
      <div v-if="detail" class="abnormal-report">
        <div class="report-head">
          <span class="report-title">异常报告</span>
          <el-date-picker
            v-model="reportRange"
            type="daterange"
            value-format="YYYY-MM-DD"
            size="small"
            unlink-panels
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            class="report-range"
          />
        </div>
        <div class="report-actions">
          <el-button size="small" :loading="reportLoading" @click="loadReport">查询汇总</el-button>
          <el-button size="small" type="primary" :loading="exporting" @click="handleExportReport">导出 CSV</el-button>
        </div>
        <template v-if="report">
          <p class="report-total">
            共 {{ report.total }} 条
            <span v-for="s in report.byStatus" :key="s.key">
              · {{ processStatusLabel(s.key) }} {{ s.count }}
            </span>
          </p>
          <el-table :data="report.byType" size="small" border empty-text="该区间无异常">
            <el-table-column label="异常类型">
              <template #default="{ row }">{{ abnormalTypeLabel(row.key) }}</template>
            </el-table-column>
            <el-table-column prop="count" label="次数" width="70" align="right" />
          </el-table>
          <el-table :data="report.byDay" size="small" border :max-height="200" class="report-days" empty-text="该区间无异常">
            <el-table-column prop="key" label="日期" />
            <el-table-column prop="count" label="次数" width="70" align="right" />
          </el-table>
        </template>
      </div>
    </el-drawer>

    <!-- 新建患者弹窗 -->
    <el-dialog v-model="createVisible" title="新建患者" width="520px" :close-on-click-modal="false">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="80px">
        <el-form-item label="姓名" prop="name">
          <el-input v-model="createForm.name" placeholder="请输入姓名" />
        </el-form-item>
        <el-form-item label="手机号" prop="phone">
          <el-input v-model="createForm.phone" placeholder="请输入手机号" maxlength="11" />
        </el-form-item>
        <el-form-item label="年龄">
          <el-input v-model="createForm.age" placeholder="请输入年龄" />
        </el-form-item>
        <el-form-item label="诊断">
          <el-input v-model="createForm.diagnosis" placeholder="请输入诊断" />
        </el-form-item>
        <el-form-item label="团队">
          <el-select v-model="createForm.teamId" placeholder="请选择团队" clearable>
            <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
          </el-select>
        </el-form-item>
        <el-form-item label="性别">
          <el-radio-group v-model="createForm.gender">
            <el-radio label="male">男</el-radio>
            <el-radio label="female">女</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Cobb角">
          <el-input v-model="createForm.cobbAngle" placeholder="请输入Cobb角" />
        </el-form-item>
        <el-form-item label="医生">
          <el-select v-model="createForm.doctorId" placeholder="请选择医生" clearable>
            <el-option v-for="d in doctors" :key="d.doctorId" :label="d.name" :value="d.doctorId" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="confirmCreate">确定</el-button>
      </template>
    </el-dialog>

    <!-- 分配团队弹窗 -->
    <el-dialog v-model="assignVisible" title="分配团队" width="420px" :close-on-click-modal="false">
      <el-form label-width="80px">
        <el-form-item label="目标团队">
          <el-select v-model="assignTeamId" placeholder="请选择团队">
            <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="assignVisible = false">取消</el-button>
        <el-button type="primary" :loading="assigning" @click="confirmAssign">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance } from 'element-plus'
import type { Patient, Team, Doctor } from '@bracesync/shared-types'
import type { AbnormalReport } from '../../mock/alerts'
import {
  fetchPatients, fetchTeams, fetchDoctors, teamNameOf, doctorNameOf,
  createPatientApi, assignPatientTeamApi, batchBindPatientsApi,
  fetchAbnormalReport, exportAbnormalReportApi,
} from '../../api'
import type { BatchBindFailure } from '../../mock/patients'

/** T269 D1：后端 /admin/patients 已 join 出团队名与医生名，优先用返回值显示 */
type PatientRow = Patient & { teamName?: string | null; doctorName?: string | null }

const list = ref<PatientRow[]>([])
const teams = ref<Team[]>([])
const doctors = ref<Doctor[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const keyword = ref('')
const teamFilter = ref('')
const loading = ref(false)
const drawerVisible = ref(false)
const detail = ref<PatientRow | null>(null)

// 新建患者
const createVisible = ref(false)
const creating = ref(false)
const createFormRef = ref<FormInstance>()
const createForm = ref({
  name: '',
  phone: '',
  age: '',
  diagnosis: '',
  cobbAngle: '',
  teamId: '',
  doctorId: '',
  gender: '' as '' | 'male' | 'female',
})
const createRules = {
  name: [{ required: true, message: '请输入姓名', trigger: 'blur' }],
  phone: [{ required: true, message: '请输入手机号', trigger: 'blur' }],
}

// 分配团队
const assignVisible = ref(false)
const assigning = ref(false)
const assignTeamId = ref('')

// 批量分配（4.2 独立卡片）
const batchLoading = ref(false)
const batching = ref(false)
const unassignedList = ref<PatientRow[]>([])
const batchSelected = ref<PatientRow[]>([])
const batchTargetTeam = ref<Record<string, string>>({})

/**
 * 契约 api-contracts.ts:53 明写 patients 分页 pageSize 上限 100（超限 400 code=10400；
 * staging 实测 101 即拒），所以只能按页扫，不能一次要 200。
 * 扫描上限 10 页 = 1000 人；再要更大范围得后端补 unassigned 过滤参数（已登记契约偏差清单）。
 */
const UNASSIGNED_SCAN_PAGE_SIZE = 100
const UNASSIGNED_SCAN_MAX_PAGES = 10

async function loadUnassigned() {
  batchLoading.value = true
  try {
    const acc: PatientRow[] = []
    let totalCount = 0
    for (let p = 1; p <= UNASSIGNED_SCAN_MAX_PAGES; p++) {
      const res = await fetchPatients({ page: p, pageSize: UNASSIGNED_SCAN_PAGE_SIZE })
      totalCount = res.total
      acc.push(...res.list)
      if (res.list.length === 0 || acc.length >= totalCount) break
    }
    unassignedList.value = acc.filter((x) => !x.teamId)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '未分配患者加载失败')
  } finally {
    batchLoading.value = false
  }
}

const canConfirmBatch = computed(
  () =>
    batchSelected.value.length > 0 &&
    batchSelected.value.every((r) => !!batchTargetTeam.value[r.patientId]),
)

// T300 异常报告（详情抽屉内）
const reportRange = ref<[string, string] | null>(defaultReportRange())
const report = ref<AbnormalReport | null>(null)
const reportLoading = ref(false)
const exporting = ref(false)

/** 默认看最近 7 天（含今天） */
function defaultReportRange(): [string, string] {
  const end = new Date()
  const start = new Date(end.getTime() - 6 * 86400000)
  return [dayText(start), dayText(end)]
}

function dayText(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function formatDate(iso: string): string {
  return iso.slice(0, 10)
}

async function loadData() {
  loading.value = true
  try {
    const res = await fetchPatients({
      keyword: keyword.value || undefined,
      teamId: teamFilter.value || undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    list.value = res.list
    total.value = res.total
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  page.value = 1
  loadData()
}

function viewDetail(row: PatientRow) {
  detail.value = row
  report.value = null
  drawerVisible.value = true
  loadReport()
}

function onBatchSelectionChange(rows: PatientRow[]) {
  batchSelected.value = rows
}

// T300 异常报告
function reportQueryOrNull() {
  // Element Plus 清空 daterange 会把 v-model 置为 null，必须先兜底再解构，否则下面的空值提示永远走不到
  const [start, end] = reportRange.value ?? []
  if (!detail.value) return null
  if (!start || !end) {
    ElMessage.warning('请选择日期范围')
    return null
  }
  if (start > end) {
    ElMessage.warning('结束日期不能早于开始日期')
    return null
  }
  return { patientId: detail.value.patientId, start, end }
}

async function loadReport() {
  const q = reportQueryOrNull()
  if (!q) return
  reportLoading.value = true
  try {
    report.value = await fetchAbnormalReport(q)
  } catch (e: unknown) {
    report.value = null
    ElMessage.error(e instanceof Error ? e.message : '汇总加载失败')
  } finally {
    reportLoading.value = false
  }
}

async function handleExportReport() {
  const q = reportQueryOrNull()
  if (!q) return
  exporting.value = true
  try {
    await exportAbnormalReportApi(q)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '导出失败')
  } finally {
    exporting.value = false
  }
}

function abnormalTypeLabel(key: string): string {
  const map: Record<string, string> = {
    pressure_high: '压力偏高',
    pressure_fluctuation: '压力波动',
    wear_interrupt: '佩戴中断',
    sensor_drift: '传感器漂移',
    wear_duration_short: '佩戴时长不足',
  }
  return map[key] || key
}

function processStatusLabel(key: string): string {
  const map: Record<string, string> = { pending: '待处理', processing: '处理中', processed: '已处理' }
  return map[key] || key
}

// 新建患者
function openCreate() {
  createForm.value = {
    name: '', phone: '', age: '', diagnosis: '',
    cobbAngle: '', teamId: '', doctorId: '', gender: '',
  }
  createFormRef.value?.clearValidate()
  createVisible.value = true
}

async function confirmCreate() {
  if (!createFormRef.value) return
  // 逐字段校验：只显示第一个无效字段的错误（避免 strict mode 多元素）
  const validName = await createFormRef.value.validateField('name').then(() => true).catch(() => false)
  if (!validName) return
  const validPhone = await createFormRef.value.validateField('phone').then(() => true).catch(() => false)
  if (!validPhone) return
  creating.value = true
  try {
    await createPatientApi({
      name: createForm.value.name,
      phone: createForm.value.phone,
      gender: createForm.value.gender || null,
      age: createForm.value.age ? Number(createForm.value.age) : null,
      diagnosis: createForm.value.diagnosis || null,
      cobbAngle: createForm.value.cobbAngle ? Number(createForm.value.cobbAngle) : null,
      teamId: createForm.value.teamId || null,
      doctorId: createForm.value.doctorId || null,
    })
    ElMessage.success('创建成功')
    createVisible.value = false
    loadData()
    loadUnassigned()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建失败')
  } finally {
    creating.value = false
  }
}

// 分配团队
function openAssignTeam() {
  assignTeamId.value = detail.value?.teamId ?? ''
  assignVisible.value = true
}

async function confirmAssign() {
  if (!detail.value || !assignTeamId.value) return
  assigning.value = true
  try {
    const result = await assignPatientTeamApi(detail.value.patientId, assignTeamId.value)
    detail.value = { ...result }
    ElMessage.success('分配成功')
    assignVisible.value = false
    loadData()
    loadUnassigned()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '分配失败')
  } finally {
    assigning.value = false
  }
}

// 批量分配（4.2）
async function confirmBatch() {
  if (!canConfirmBatch.value) return
  batching.value = true
  try {
    // 契约 POST /admin/patients/batch-bind 单次只接受一个 teamId ⇒ 按目标团队分组逐组提交
    const byTeam = new Map<string, string[]>()
    for (const row of batchSelected.value) {
      const teamId = batchTargetTeam.value[row.patientId]
      const ids = byTeam.get(teamId)
      if (ids) ids.push(row.patientId)
      else byTeam.set(teamId, [row.patientId])
    }
    let successCount = 0
    const failures: BatchBindFailure[] = []
    for (const [teamId, ids] of byTeam) {
      const result = await batchBindPatientsApi(ids, teamId)
      successCount += result.successCount
      failures.push(...result.failures)
    }
    if (failures.length > 0) {
      const reasons = failures.map((f) => `${f.patientId}：${f.reason}`).join('；')
      ElMessage.warning(`成功 ${successCount} 条，失败 ${failures.length} 条（${reasons}）`)
    } else {
      ElMessage.success(`批量分配成功 ${successCount} 条`)
    }
    batchSelected.value = []
    batchTargetTeam.value = {}
    await Promise.all([loadData(), loadUnassigned()])
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '批量分配失败')
  } finally {
    batching.value = false
  }
}

onMounted(async () => {
  loadData()
  loadUnassigned()
  try {
    teams.value = await fetchTeams()
  } catch {
    // 团队筛选失败不阻塞列表
  }
  try {
    doctors.value = await fetchDoctors()
  } catch {
    // 医生列表加载失败不阻塞
  }
})
</script>

<style scoped>
.search-input {
  width: 220px;
}
.team-select {
  width: 160px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
.drawer-actions {
  margin-top: 16px;
  text-align: right;
}
.batch-team-select {
  width: 180px;
}
/* 设计稿 患者管理.html:108 确认分配按钮 margin-top:12px */
.batch-submit {
  margin-top: 12px;
}
.abnormal-report {
  margin-top: 20px;
  padding-top: 12px;
  border-top: 1px solid #ebeef5;
}
.report-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.report-title {
  font-size: 14px;
  font-weight: 600;
}
.report-range {
  width: 100%;
}
.report-actions {
  margin: 10px 0 12px;
  text-align: right;
}
.report-total {
  margin: 0 0 8px;
  font-size: 12px;
  color: #606266;
}
.report-days {
  margin-top: 8px;
}
</style>
