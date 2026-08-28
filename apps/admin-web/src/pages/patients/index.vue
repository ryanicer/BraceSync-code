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
      <el-button type="warning" :disabled="selectedRows.length === 0" @click="openBatchBind">批量绑定</el-button>
    </div>

    <div class="page-card">
      <el-table :data="list" size="small" v-loading="loading" @row-click="viewDetail" @selection-change="onSelectionChange">
        <el-table-column type="selection" width="40" />
        <el-table-column prop="patientId" label="患者ID" width="110" />
        <el-table-column prop="name" label="姓名" width="100" />
        <el-table-column label="性别" width="70">
          <template #default="{ row }">{{ row.gender === 'male' ? '男' : row.gender === 'female' ? '女' : '-' }}</template>
        </el-table-column>
        <el-table-column prop="age" label="年龄" width="70" />
        <el-table-column label="诊断" min-width="180">
          <template #default="{ row }">{{ row.diagnosis || '-' }}</template>
        </el-table-column>
        <el-table-column label="Cobb角" width="90">
          <template #default="{ row }">{{ row.cobbAngle ? row.cobbAngle + '°' : '-' }}</template>
        </el-table-column>
        <el-table-column label="团队" width="130">
          <template #default="{ row }">{{ teamNameOf(row.teamId) }}</template>
        </el-table-column>
        <el-table-column label="主治医生" width="110">
          <template #default="{ row }">{{ doctorNameOf(row.doctorId) }}</template>
        </el-table-column>
        <el-table-column label="设备" width="130">
          <template #default="{ row }">{{ row.deviceId || '未绑定' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'warning'" size="small">
              {{ row.status === 'active' ? '活跃' : '待分配' }}
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

    <!-- 患者详情抽屉 -->
    <el-drawer v-model="drawerVisible" :title="detail ? `${detail.name}（${detail.patientId}）` : ''" size="420px">
      <el-descriptions v-if="detail" :column="1" border size="small">
        <el-descriptions-item label="性别">{{ detail.gender === 'male' ? '男' : detail.gender === 'female' ? '女' : '-' }}</el-descriptions-item>
        <el-descriptions-item label="年龄">{{ detail.age ?? '-' }}</el-descriptions-item>
        <el-descriptions-item label="诊断">{{ detail.diagnosis || '-' }}</el-descriptions-item>
        <el-descriptions-item label="Cobb角">{{ detail.cobbAngle ? detail.cobbAngle + '°' : '-' }}</el-descriptions-item>
        <el-descriptions-item label="所属团队">{{ teamNameOf(detail.teamId) }}</el-descriptions-item>
        <el-descriptions-item label="主治医生">{{ doctorNameOf(detail.doctorId) }}</el-descriptions-item>
        <el-descriptions-item label="绑定设备">{{ detail.deviceId || '未绑定' }}</el-descriptions-item>
        <el-descriptions-item label="建档时间">{{ formatDate(detail.createdAt) }}</el-descriptions-item>
      </el-descriptions>
      <div v-if="detail" class="drawer-actions">
        <el-button type="primary" @click="openAssignTeam">分配团队</el-button>
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
        <el-form-item label="设备ID">
          <el-input v-model="createForm.deviceId" placeholder="请输入设备ID" />
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

    <!-- 批量绑定弹窗 -->
    <el-dialog v-model="batchVisible" title="批量绑定" width="480px" :close-on-click-modal="false">
      <p class="batch-desc">已选 {{ selectedRows.length }} 位患者，请选择目标团队：</p>
      <el-form label-width="80px">
        <el-form-item label="目标团队">
          <el-select v-model="batchTeamId" placeholder="请选择团队">
            <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="batchVisible = false">取消</el-button>
        <el-button type="primary" :loading="batching" @click="confirmBatch">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance } from 'element-plus'
import type { Patient, Team, Doctor } from '@bracesync/shared-types'
import {
  fetchPatients, fetchTeams, fetchDoctors, teamNameOf, doctorNameOf,
  createPatientApi, assignPatientTeamApi, batchBindPatientsApi,
} from '../../api'

const list = ref<Patient[]>([])
const teams = ref<Team[]>([])
const doctors = ref<Doctor[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const keyword = ref('')
const teamFilter = ref('')
const loading = ref(false)
const drawerVisible = ref(false)
const detail = ref<Patient | null>(null)
const selectedRows = ref<Patient[]>([])

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
  deviceId: '',
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

// 批量绑定
const batchVisible = ref(false)
const batching = ref(false)
const batchTeamId = ref('')

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

function viewDetail(row: Patient, column?: { type?: string }) {
  // 点击 selection 列的 checkbox 不触发详情抽屉
  if (column?.type === 'selection') return
  detail.value = row
  drawerVisible.value = true
}

function onSelectionChange(rows: Patient[]) {
  selectedRows.value = rows
}

// 新建患者
function openCreate() {
  createForm.value = {
    name: '', phone: '', age: '', diagnosis: '',
    cobbAngle: '', deviceId: '', teamId: '', doctorId: '', gender: '',
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
      deviceId: createForm.value.deviceId || null,
      teamId: createForm.value.teamId || null,
      doctorId: createForm.value.doctorId || null,
    })
    ElMessage.success('创建成功')
    createVisible.value = false
    loadData()
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
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '分配失败')
  } finally {
    assigning.value = false
  }
}

// 批量绑定
function openBatchBind() {
  batchTeamId.value = ''
  batchVisible.value = true
}

async function confirmBatch() {
  if (selectedRows.value.length === 0 || !batchTeamId.value) return
  batching.value = true
  try {
    const ids = selectedRows.value.map((r) => r.patientId)
    const result = await batchBindPatientsApi(ids, batchTeamId.value)
    if (result.failedCount > 0) {
      ElMessage.warning(`成功 ${result.successCount} 条，失败 ${result.failedCount} 条`)
    } else {
      ElMessage.success(`批量绑定成功 ${result.successCount} 条`)
    }
    batchVisible.value = false
    loadData()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '批量绑定失败')
  } finally {
    batching.value = false
  }
}

onMounted(async () => {
  loadData()
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
.batch-desc {
  margin: 0 0 12px;
  color: #333;
  font-size: 13px;
}
</style>
