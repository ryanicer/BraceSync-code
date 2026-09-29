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
      <!-- T327 患者ID 二维码（设计稿 患者管理.html:186-199）：抽屉正文首位，在「基本信息」之前 -->
      <div v-if="detail" class="pid-card">
        <div class="pid-meta">
          <div class="label">患者ID</div>
          <div class="pid-value">{{ detail.patientId }}</div>
          <div class="pid-note">技师端「绑定」页扫码即自动填入患者ID，避免手输长ID出错。</div>
        </div>
        <div class="qr-box">
          <div class="qr-frame">
            <svg
              v-if="qrMatrix"
              width="144"
              height="144"
              :viewBox="`0 0 ${qrMatrix.size} ${qrMatrix.size}`"
              shape-rendering="crispEdges"
              role="img"
              aria-label="患者ID 二维码"
            >
              <path :d="qrMatrix.path" fill="#333333" />
            </svg>
          </div>
          <div class="qr-cap">扫描二维码录入患者ID</div>
        </div>
      </div>

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
        <!-- T372：异常报告按设计稿拆为独立页，抽屉只留跳转入口（稿「进入条件①」：进入后患者自动定位） -->
        <el-button @click="goAbnormalReport">异常报告</el-button>
        <!-- T432 运营自助三入口。依据分层：「编辑档案」有 PRD §7D.3 子功能「编辑患者弹窗」五项撑着，
             落点取抽屉（同节 :1113 明写「行内按钮形态属呈现差异、PRD 允许经弹窗承载」）；
             「改手机号」「解绑微信」在 PRD 与设计稿均无条文，属本卡新增（Boss 09-27 20:1x 诉求），
             稿面与 PRD 回写归口已登记在卡，本卡不改文档。 -->
        <el-button @click="openEditProfile">编辑档案</el-button>
        <el-button @click="openEditPhone">改手机号</el-button>
        <el-button type="danger" plain :loading="unbinding" @click="confirmUnbindWechat">解绑微信</el-button>
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

    <!-- T432 编辑档案弹窗。依据：PRD §7D.3:1119「编辑患者弹窗：修改姓名 / 性别 / 年龄 / 诊断 / Cobb 角度」，
         五项与后端 adminPatientEditRequest（admin_patient.go:140-146）白名单一一对应；
         白名单外的键会被 DisallowUnknownFields 判 400（同文件 :170-175），所以这里刻意不排团队 / 医生 / 状态。 -->
    <el-dialog v-model="editVisible" title="编辑档案" width="520px" :close-on-click-modal="false">
      <el-form label-width="80px">
        <el-form-item label="姓名">
          <el-input v-model="editForm.name" placeholder="请输入姓名" />
        </el-form-item>
        <el-form-item label="性别">
          <el-radio-group v-model="editForm.gender">
            <el-radio label="male">男</el-radio>
            <el-radio label="female">女</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="年龄">
          <el-input v-model="editForm.age" placeholder="请输入年龄" />
        </el-form-item>
        <el-form-item label="诊断">
          <el-input v-model="editForm.diagnosis" placeholder="请输入诊断" />
        </el-form-item>
        <el-form-item label="Cobb角">
          <el-input v-model="editForm.cobbAngle" placeholder="请输入Cobb角" />
        </el-form-item>
      </el-form>
      <!-- 后端对「字段缺席」和「字段值为空」是两套语义（nil=不改 / 传值则校验值域），
           而页面把原值预填进了输入框 ⇒ 用户清空某项既不是「不改」也存不进去，必须显式拦在这里 -->
      <div v-if="editErrors.length" class="form-errors">
        <div v-for="msg in editErrors" :key="msg" class="form-error">{{ msg }}</div>
      </div>
      <div class="dialog-note">只提交改动过的项；未改动的字段不会下发。</div>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!canSaveProfile" :loading="savingProfile" @click="confirmEditProfile">保存</el-button>
      </template>
    </el-dialog>

    <!-- T432 改手机号弹窗（PUT /admin/patients/:id/phone，admin_patient.go:61-135）。
         输入框刻意留空、不预填当前号码：患者域两个读接口（列表/详情）都不回 phone（shared-types/index.ts:20-23 T361/T491），
         拿到的恒为空串，预填等于把空值伪装成「原号」。
         也不做医护页那套「留空即不改」三态（utils/phoneField.ts）—— 本端点 validPhone 对空串判 400，
         只有「换成这个号」一种语义，没有清空通道。 -->
    <el-dialog v-model="phoneVisible" title="修改手机号" width="460px" :close-on-click-modal="false">
      <el-form ref="phoneFormRef" :model="phoneForm" :rules="phoneRules" label-width="80px">
        <el-form-item label="新手机号" prop="phone">
          <el-input v-model="phoneForm.phone" placeholder="请输入11位新手机号" maxlength="11" />
        </el-form-item>
        <el-form-item label="变更原因" prop="reason">
          <el-input v-model="phoneForm.reason" type="textarea" :rows="2" placeholder="例如：患者换号，本人来电申请" />
        </el-form-item>
      </el-form>
      <div class="dialog-note">原因随请求写入服务端审计日志（含改前/改后快照）。</div>
      <template #footer>
        <el-button @click="phoneVisible = false">取消</el-button>
        <el-button type="primary" :loading="savingPhone" @click="confirmPhone">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance } from 'element-plus'
import QRCode from 'qrcode'
import type { Patient, Team, Doctor } from '@bracesync/shared-types'
import {
  fetchPatients, fetchTeams, fetchDoctors, teamNameOf, doctorNameOf,
  createPatientApi, assignPatientTeamApi, batchBindPatientsApi,
  updatePatientPhoneApi, updatePatientProfileApi, unbindPatientWechatApi,
} from '../../api'
import type { BatchBindFailure, PatientProfilePatch } from '../../mock/patients'
import { PHONE_RE } from '../../utils/phoneField'

/** T269 D1：后端 /admin/patients 已 join 出团队名与医生名，优先用返回值显示 */
type PatientRow = Patient & { teamName?: string | null; doctorName?: string | null }

const router = useRouter()
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
const qrMatrix = ref<{ size: number; path: string } | null>(null)

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

// T432 编辑档案（PUT /admin/patients/:id）
const editVisible = ref(false)
const savingProfile = ref(false)
const editForm = ref({ name: '', gender: '' as '' | 'male' | 'female', age: '', diagnosis: '', cobbAngle: '' })
/** 打开弹窗时的档案快照 —— 「只发改过的键」与「不可清空」判定都以它为基准，不用实时表单自比 */
const editBase = ref<{ name: string; gender: string; age: string; diagnosis: string; cobbAngle: string } | null>(null)

// T432 改手机号（PUT /admin/patients/:id/phone）
const phoneVisible = ref(false)
const savingPhone = ref(false)
const unbinding = ref(false)
const phoneFormRef = ref<FormInstance>()
const phoneForm = ref({ phone: '', reason: '' })
const phoneRules = {
  phone: [
    { required: true, message: '请输入新手机号', trigger: 'blur' },
    { pattern: PHONE_RE, message: '手机号需为 11 位、以 1 开头的数字', trigger: 'blur' },
  ],
  // 后端不校验 reason（只写进审计日志，admin_patient.go:125-132），必填是页面自己加的：
  // 没有理由的改号在事后无从追责。
  reason: [{ required: true, message: '请填写变更原因', trigger: 'blur' }],
}

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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '未分配患者加载失败' }))
  } finally {
    batchLoading.value = false
  }
}

const canConfirmBatch = computed(
  () =>
    batchSelected.value.length > 0 &&
    batchSelected.value.every((r) => !!batchTargetTeam.value[r.patientId]),
)

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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载失败' }))
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  page.value = 1
  loadData()
}

/** 暗模块合成一条 SVG path：同一行内连续的暗模块并成一段水平描边（设计稿 markup 同形） */
function qrPathFrom(data: Uint8Array, size: number) {
  const seg: string[] = []
  for (let y = 0; y < size; y++) {
    let x = 0
    while (x < size) {
      if (!data[y * size + x]) {
        x++
        continue
      }
      let run = 0
      while (x + run < size && data[y * size + x + run]) run++
      seg.push(`M${x} ${y}h${run}v1h-${run}z`)
      x += run
    }
  }
  return seg.join('')
}

/**
 * 码内载荷 = 患者ID 明文本体（设计稿 患者管理.html:152 建议，不加 URL/scheme 前缀）。
 * 图形不留静区（margin 0），静区由 .qr-frame 的 8px 内白 + 白底卡片承载，见设计稿:150「尺寸」行。
 */
function showPatientQr(patientId: string) {
  const qr = QRCode.create(patientId, { errorCorrectionLevel: 'M' })
  qrMatrix.value = { size: qr.modules.size, path: qrPathFrom(qr.modules.data, qr.modules.size) }
}

function viewDetail(row: PatientRow) {
  detail.value = row
  drawerVisible.value = true
  qrMatrix.value = null
  showPatientQr(row.patientId)
}

/** T372：异常报告拆为独立页，抽屉只跳转并把患者带过去（设计稿「进入条件①」= 进入后自动定位） */
function goAbnormalReport() {
  if (!detail.value) return
  drawerVisible.value = false
  router.push({ path: '/abnormal-report', query: { patient: detail.value.patientId } })
}

function onBatchSelectionChange(rows: PatientRow[]) {
  batchSelected.value = rows
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '创建失败' }))
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '分配失败' }))
  } finally {
    assigning.value = false
  }
}

/**
 * 值域逐条对齐 buildAdminPatientEdit（admin_patient.go）：姓名 1-64 字符、年龄 0-150 整数、
 * Cobb 角 0-180、诊断 ≤255 字符。
 * 「留空」在姓名/年龄/Cobb 三个输入框里既不等于「不改」也存不进去：姓名库里是 NOT NULL，后端压根没有
 * 置空通道；年龄/Cobb 后端有显式置空通道（T450-②b 乙案的 clearFields），但弹窗没给这两档入口。
 * 页面又把原值预填进了输入框 ⇒ 用户清空它必须在点保存前就讲清楚，
 * 不能让运营以为改了、实际服务端收到的是缺键。
 */
const editErrors = computed<string[]>(() => {
  const base = editBase.value
  if (!base) return []
  const f = editForm.value
  const out: string[] = []
  const name = f.name.trim()
  const nameLen = [...name].length
  if (!name) out.push('姓名不可清空：档案编辑没有「置为空」通道，请填新姓名')
  else if (nameLen > 64) out.push('姓名不能超过 64 个字符')
  const age = f.age.trim()
  if (age === '') {
    if (base.age !== '') out.push('年龄不可清空：留空等于不修改，请填 0-150 的新年龄')
  } else if (!/^\d+$/.test(age) || Number(age) > 150) {
    out.push('年龄需为 0-150 的整数')
  }
  const cobb = f.cobbAngle.trim()
  const cobbNum = Number(cobb)
  if (cobb === '') {
    if (base.cobbAngle !== '') out.push('Cobb角不可清空：留空等于不修改，请填新的角度')
  } else if (!Number.isFinite(cobbNum) || cobbNum < 0 || cobbNum > 180) {
    out.push('Cobb角需为 0-180 之间的数字')
  }
  if ([...f.diagnosis].length > 255) out.push('诊断不能超过 255 个字符')
  return out
})

/**
 * 只发改过的键 —— 后端 adminPatientEditRequest 用指针区分「字段缺席(nil)=不改」与
 * 「显式传值=改」，且 gender/age 的空值形态会被值域校验判 400，所以全量下发既写坏没动的字段也存不进去。
 * 无改动时返回 null ⇒ 保存按钮置灰（空编辑后端判 400「no updatable fields」，buildAdminPatientEdit）。
 *
 * 清空诊断走 clearFields 而不是 diagnosis:''（T450-②b 乙案，PM 2026-09-28 17:41 拍）——
 * Alice 第 70 轮登记的缺陷正是「基线是 null，还原后是空串」：指针表达不出「改回 NULL」这一态，
 * 写空串会把库里原本的 NULL 漂成 ''，读侧两态就此塌成一态。
 */
const editPatch = computed<PatientProfilePatch | null>(() => {
  const base = editBase.value
  if (!base || editErrors.value.length > 0) return null
  const f = editForm.value
  const patch: PatientProfilePatch = {}
  const name = f.name.trim()
  if (name !== base.name.trim()) patch.name = name
  if (f.gender !== base.gender && f.gender) patch.gender = f.gender
  const age = f.age.trim()
  if (age !== base.age && age !== '') patch.age = Number(age)
  if (f.diagnosis !== base.diagnosis) {
    if (f.diagnosis.trim() === '') patch.clearFields = ['diagnosis']
    else patch.diagnosis = f.diagnosis
  }
  const cobb = f.cobbAngle.trim()
  if (cobb !== base.cobbAngle && cobb !== '') patch.cobbAngle = Number(cobb)
  return Object.keys(patch).length > 0 ? patch : null
})

const canSaveProfile = computed(() => editPatch.value !== null)

function openEditProfile() {
  const d = detail.value
  if (!d) return
  const snap: typeof editForm.value = {
    name: d.name ?? '',
    gender: d.gender ?? '',
    age: d.age == null ? '' : String(d.age),
    diagnosis: d.diagnosis ?? '',
    cobbAngle: d.cobbAngle == null ? '' : String(d.cobbAngle),
  }
  editBase.value = snap
  editForm.value = { ...snap }
  editVisible.value = true
}

async function confirmEditProfile() {
  const patch = editPatch.value
  if (!detail.value || !patch) return
  savingProfile.value = true
  try {
    const result = await updatePatientProfileApi(detail.value.patientId, patch)
    // 写响应是 PatientDTO，不带列表 join 出的团队名/医生名 ⇒ 叠加而非整体替换，避免抽屉退化成显示编号
    detail.value = { ...detail.value, ...result }
    ElMessage.success('档案已保存')
    editVisible.value = false
    loadData()
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '保存失败' }))
  } finally {
    savingProfile.value = false
  }
}

function openEditPhone() {
  if (!detail.value) return
  phoneForm.value = { phone: '', reason: '' }
  phoneFormRef.value?.clearValidate()
  phoneVisible.value = true
}

async function confirmPhone() {
  if (!detail.value || !phoneFormRef.value) return
  // 逐字段校验：与 confirmCreate 同因（避免 strict mode 下多元素报错）
  const validPhone = await phoneFormRef.value.validateField('phone').then(() => true).catch(() => false)
  if (!validPhone) return
  const validReason = await phoneFormRef.value.validateField('reason').then(() => true).catch(() => false)
  if (!validReason) return
  savingPhone.value = true
  try {
    await updatePatientPhoneApi(
      detail.value.patientId,
      phoneForm.value.phone.trim(),
      phoneForm.value.reason.trim(),
    )
    ElMessage.success('手机号已更新')
    phoneVisible.value = false
    loadData()
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '修改失败' }))
  } finally {
    savingPhone.value = false
  }
}

/**
 * 后端是无条件 `SET wx_openid = NULL`（pg.go:168-171），对从未绑过微信的患者亦回 200，
 * 而患者域读侧没有 openid 字段可判两态 ⇒ 文案不断言「该患者已绑定微信」，只讲解绑后的后果
 * （「重新绑定手机号」一支已由 wxLogin 未绑定分支 10601+bindToken 核实，handler.go:648-665）。
 */
async function confirmUnbindWechat() {
  const d = detail.value
  if (!d) return
  try {
    await ElMessageBox.confirm(
      `确认解绑患者 ${d.patientId} 的微信？解绑后该患者再用微信登录会进入「重新绑定手机号」流程。此操作会写入审计日志。`,
      '解绑微信',
      { type: 'warning', confirmButtonText: '确认解绑', cancelButtonText: '取消' },
    )
  } catch {
    return // 取消或关掉弹层都不发请求
  }
  unbinding.value = true
  try {
    await unbindPatientWechatApi(d.patientId)
    ElMessage.success('已解绑微信')
    loadData()
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '解绑失败' }))
  } finally {
    unbinding.value = false
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '批量分配失败' }))
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
/* T327 患者ID 二维码卡片：尺寸/配色照设计稿 患者管理.html:59-67，左文本右码、顶部对齐 */
.pid-card {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 16px;
  padding: 16px;
  border: 1px solid #e8ecf0;
  border-radius: 12px;
  background: #fcfdff;
}
.pid-meta {
  flex: 1;
  min-width: 0;
}
.pid-meta .label {
  font-size: 12px;
  color: #999;
}
.pid-value {
  margin-top: 4px;
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 0.5px;
  color: #333;
  word-break: break-all;
}
.pid-note {
  margin-top: 10px;
  font-size: 11px;
  line-height: 1.7;
  color: #999;
}
.qr-box {
  width: 160px;
  flex-shrink: 0;
}
/* 外框 160×160 含 8px 内白（静区，不可裁），内里图形 144×144 */
.qr-frame {
  box-sizing: border-box;
  width: 160px;
  height: 160px;
  padding: 8px;
  border: 1px solid #ddd;
  border-radius: 8px;
  background: #fff;
}
.qr-cap {
  margin-top: 6px;
  font-size: 11px;
  line-height: 1.5;
  text-align: center;
  color: #666;
}
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
  /* T432：抽屉内动作从 2 个增到 5 个，420px 抽屉一行放不下 ⇒ 换行右对齐。
     EP 默认给相邻按钮加 margin-left:12px，换行后行首那颗会被顶出 12px，故由 gap 统一控制间距。 */
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}
.drawer-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}
.form-errors {
  margin: 0 0 12px 80px;
}
.form-error {
  font-size: 12px;
  line-height: 1.7;
  color: var(--el-color-danger);
}
.dialog-note {
  margin-left: 80px;
  font-size: 12px;
  line-height: 1.7;
  color: #999;
}
.batch-team-select {
  width: 180px;
}
/* 设计稿 患者管理.html:108 确认分配按钮 margin-top:12px */
.batch-submit {
  margin-top: 12px;
}
</style>
