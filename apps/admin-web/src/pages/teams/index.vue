<template>
  <div class="teams">
    <!-- 团队列表模式 -->
    <div v-if="mode === 'list'">
      <div class="page-toolbar">
        <el-button type="success" @click="openCreate">新建团队</el-button>
      </div>

      <!-- T289 5.1：设计稿 团队管理.html:88-92 四张统计卡，数据源 GET /api/v1/admin/teams/stats（T256 #1） -->
      <div class="stats-grid">
        <div class="stat-card primary">
          <div class="value">{{ stats?.teamCount ?? '-' }}</div>
          <div class="label">团队总数</div>
        </div>
        <div class="stat-card success">
          <div class="value">{{ stats?.memberCount ?? '-' }}</div>
          <div class="label">成员总数</div>
        </div>
        <div class="stat-card purple">
          <div class="value">{{ stats?.managedPatientCount ?? '-' }}</div>
          <div class="label">管理患者</div>
        </div>
        <div class="stat-card sky">
          <div class="value">{{ stats?.unassignedPatientCount ?? '-' }}</div>
          <!-- PRD §7D.4:1104：第四张卡的「待分配」= 尚无团队归属（team_id 为空），与患者状态无关，
               须在卡片说明里点明，避免与已禁用的「待分配」状态文案混淆 -->
          <div class="label-row">
            <div class="label">待分配患者</div>
            <el-tooltip content="统计口径：尚未分配团队（team_id 为空）的患者数，与患者登录状态无关" placement="top">
              <span class="label-hint">ⓘ</span>
            </el-tooltip>
          </div>
        </div>
      </div>

      <div class="page-card">
        <el-table :data="teams" size="small" v-loading="loading">
          <el-table-column prop="teamId" label="团队编号" width="120" />
          <el-table-column prop="name" label="团队名称" min-width="160" />
          <el-table-column label="负责人" width="110">
            <template #default="{ row }">{{ row.leaderName ?? '-' }}</template>
          </el-table-column>
          <el-table-column prop="memberCount" label="成员数" width="90" />
          <el-table-column prop="patientCount" label="管理患者数" width="120" />
          <el-table-column label="创建时间" width="120">
            <template #default="{ row }">{{ row.createdAt ? formatDate(row.createdAt) : '-' }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 'deleted' ? 'info' : 'success'" size="small">
                {{ row.status === 'deleted' ? '已删除' : '活跃' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="220" fixed="right">
            <template #default="{ row }">
              <el-button size="small" link type="primary" @click="openMembers(row)">成员</el-button>
              <el-button size="small" link type="primary" @click="openEdit(row)">编辑</el-button>
              <el-button size="small" link type="danger" @click="confirmDelete(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <!-- 成员管理模式 -->
    <div v-else class="member-panel">
      <div class="member-panel-header">
        <span class="member-panel-title">团队: {{ currentTeam?.name }} - 成员管理</span>
        <el-button size="small" @click="mode = 'list'">返回</el-button>
      </div>
      <!-- T289 5.3：设计稿 团队管理.html:154-158 成员工具条 = 搜索成员 + 全部角色筛选 + 添加成员 -->
      <div class="member-toolbar">
        <el-input v-model="memberKeyword" placeholder="搜索成员" clearable class="member-search" />
        <el-select v-model="memberRoleFilter" placeholder="全部角色" clearable class="member-role-filter">
          <el-option v-for="r in ROLE_OPTIONS" :key="r" :label="r" :value="r" />
        </el-select>
        <el-button type="success" size="small" @click="openAddMember">添加成员</el-button>
      </div>
      <div class="page-card">
        <el-table :data="filteredMembers" size="small" v-loading="memberLoading">
          <el-table-column prop="name" label="姓名" width="110" />
          <el-table-column label="角色" width="110">
            <template #default="{ row }">{{ row.role ?? '-' }}</template>
          </el-table-column>
          <el-table-column label="职称" width="110">
            <template #default="{ row }">{{ row.title ?? '-' }}</template>
          </el-table-column>
          <el-table-column prop="phoneMasked" label="手机号" width="130" />
          <el-table-column prop="patientCount" label="负责患者数" width="110" />
          <el-table-column label="加入时间" width="120">
            <template #default="{ row }">{{ formatDate(row.joinTime) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 'enabled' ? 'success' : 'info'" size="small">
                {{ row.status === 'enabled' ? '启用' : '禁用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="160" fixed="right">
            <template #default="{ row }">
              <el-button size="small" link type="primary" @click="openEditMember(row)">编辑</el-button>
              <el-button size="small" link type="danger" @click="confirmRemoveMember(row)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <!-- 新建/编辑团队弹窗 -->
    <el-dialog v-model="teamDialogVisible" :title="teamDialogTitle" width="480px" :close-on-click-modal="false">
      <el-form ref="teamFormRef" :model="teamForm" :rules="teamRules" label-width="80px">
        <el-form-item label="团队名称" prop="name">
          <el-input v-model="teamForm.name" placeholder="请输入团队名称" maxlength="50" />
        </el-form-item>
        <el-form-item label="负责人" prop="leader">
          <el-select v-model="teamForm.leader" placeholder="请选择负责人" filterable>
            <el-option v-for="d in doctors" :key="d.doctorId" :label="d.name" :value="d.doctorId" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="teamForm.description" placeholder="请输入团队描述" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="teamDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="teamSaving" @click="confirmSaveTeam">保存</el-button>
      </template>
    </el-dialog>

    <!-- 添加成员弹窗 -->
    <el-dialog v-model="addMemberVisible" title="添加成员" width="480px" :close-on-click-modal="false">
      <el-form label-width="80px">
        <el-form-item label="成员">
          <el-select v-model="addMemberForm.memberId" placeholder="请选择成员" filterable>
            <el-option
              v-for="m in candidateMembers"
              :key="m.id"
              :label="m.name"
              :value="m.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="addMemberForm.role" placeholder="请选择角色" clearable>
            <el-option v-for="r in ROLE_OPTIONS" :key="r" :label="r" :value="r" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addMemberVisible = false">取消</el-button>
        <el-button type="primary" :loading="addMemberSaving" @click="confirmAddMember">确认添加</el-button>
      </template>
    </el-dialog>

    <!-- 编辑成员弹窗 -->
    <el-dialog v-model="editMemberVisible" title="编辑成员" width="420px" :close-on-click-modal="false">
      <el-form label-width="80px">
        <el-form-item label="成员">
          <el-input :model-value="editMemberForm.name" disabled />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="editMemberForm.role" placeholder="请选择角色" clearable>
            <el-option v-for="r in ROLE_OPTIONS" :key="r" :label="r" :value="r" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editMemberVisible = false">取消</el-button>
        <el-button type="primary" :loading="editMemberSaving" @click="confirmEditMember">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance } from 'element-plus'
import type { Team, TeamMember, TeamStats, Doctor } from '@bracesync/shared-types'
import {
  fetchTeams, fetchDoctors, fetchTeamStats,
  createTeamApi, updateTeamApi, deleteTeamApi,
  fetchTeamMembersApi, addTeamMemberApi, updateTeamMemberApi, removeTeamMemberApi,
} from '../../api'

const ROLE_OPTIONS = ['主任医师', '副主任医师', '主治医师', '住院医师', '护士', '康复师']

const mode = ref<'list' | 'members'>('list')
const teams = ref<Team[]>([])
const doctors = ref<Doctor[]>([])
const loading = ref(false)
// T289 5.1：团队统计卡
const stats = ref<TeamStats | null>(null)

// 团队成员管理
const currentTeam = ref<Team | null>(null)
const memberList = ref<TeamMember[]>([])
const memberLoading = ref(false)
// T289 5.3：成员搜索 + 角色筛选（前端过滤，成员列表为全量返回，无分页）
const memberKeyword = ref('')
const memberRoleFilter = ref('')

const filteredMembers = computed(() => {
  const kw = memberKeyword.value.trim().toLowerCase()
  return memberList.value.filter((m) => {
    if (memberRoleFilter.value && m.role !== memberRoleFilter.value) return false
    if (!kw) return true
    return `${m.name}`.toLowerCase().includes(kw) || `${m.phoneMasked ?? ''}`.toLowerCase().includes(kw)
  })
})

// 新建/编辑团队
const teamDialogVisible = ref(false)
const teamDialogTitle = ref('新建团队')
const teamSaving = ref(false)
const teamFormRef = ref<FormInstance>()
const teamForm = ref({ teamId: '', name: '', leader: '', description: '' })
const teamRules = {
  name: [{ required: true, message: '请输入团队名称', trigger: 'blur' }],
}

// 添加成员
const addMemberVisible = ref(false)
const addMemberSaving = ref(false)
const addMemberForm = ref({ memberId: '', role: '' })

// 编辑成员
const editMemberVisible = ref(false)
const editMemberSaving = ref(false)
const editMemberForm = ref({ memberId: '', memberType: 'doctor' as 'doctor' | 'technician', name: '', role: '' })

function formatDate(iso: string): string {
  return iso ? iso.slice(0, 10) : '-'
}

async function loadTeams() {
  loading.value = true
  try {
    teams.value = await fetchTeams()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
  loadStats()
}

/** 统计卡失败不弹错：卡片显示 '-'，列表本身仍可用（避免同一次进页两条 ElMessage） */
async function loadStats() {
  try {
    stats.value = await fetchTeamStats()
  } catch {
    stats.value = null
  }
}

// 候选成员：不在当前团队的 doctors/technicians（按 id 去重）
const candidateMembers = computed(() => {
  const inTeam = new Set(memberList.value.map((m) => m.memberId))
  const list: { id: string; name: string }[] = []
  for (const d of doctors.value) {
    if (!inTeam.has(d.doctorId)) {
      list.push({ id: d.doctorId, name: `${d.name}（医生）` })
    }
  }
  return list
})

// 新建/编辑团队
function openCreate() {
  teamDialogTitle.value = '新建团队'
  teamForm.value = { teamId: '', name: '', leader: '', description: '' }
  teamFormRef.value?.clearValidate()
  teamDialogVisible.value = true
}

function openEdit(row: Team) {
  teamDialogTitle.value = '编辑团队'
  teamForm.value = {
    teamId: row.teamId,
    name: row.name,
    leader: row.leader ?? '',
    description: row.description ?? '',
  }
  teamFormRef.value?.clearValidate()
  teamDialogVisible.value = true
}

async function confirmSaveTeam() {
  if (!teamFormRef.value) return
  const valid = await teamFormRef.value.validate().then(() => true).catch(() => false)
  if (!valid) return
  // leader 手动校验（避免与 name 校验同时触发多 .el-form-item__error 导致 E2E strict mode 违规）
  if (!teamForm.value.leader) {
    ElMessage.warning('请选择负责人')
    return
  }
  teamSaving.value = true
  try {
    const input = {
      name: teamForm.value.name.trim(),
      leader: teamForm.value.leader,
      description: teamForm.value.description || undefined,
    }
    if (teamForm.value.teamId) {
      // 编辑：乐观更新本地行
      const result = await updateTeamApi(teamForm.value.teamId, input)
      const idx = teams.value.findIndex((t) => t.teamId === teamForm.value.teamId)
      if (idx >= 0) teams.value[idx] = { ...teams.value[idx], ...result }
      ElMessage.success('更新成功')
    } else {
      // 新建：乐观 push 到列表首条
      const result = await createTeamApi(input)
      teams.value.unshift(result)
      ElMessage.success('创建成功')
    }
    teamDialogVisible.value = false
    loadStats() // 团队总数/成员总数随增删改变化，统计卡重新拉取
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    teamSaving.value = false
  }
}

// 删除团队
async function confirmDelete(row: Team) {
  try {
    await ElMessageBox.confirm('确定删除该团队？', '删除确认', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return // 取消
  }
  try {
    await deleteTeamApi(row.teamId)
    // 乐观移除本地行
    teams.value = teams.value.filter((t) => t.teamId !== row.teamId)
    ElMessage.success('删除成功')
    loadStats()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '删除失败')
  }
}

// 成员管理
async function openMembers(row: Team) {
  currentTeam.value = row
  mode.value = 'members'
  memberKeyword.value = ''
  memberRoleFilter.value = ''
  await loadMembers(row.teamId)
}

async function loadMembers(teamId: string) {
  memberLoading.value = true
  try {
    const res = await fetchTeamMembersApi(teamId)
    memberList.value = [...res.doctors, ...res.technicians]
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载成员失败')
  } finally {
    memberLoading.value = false
  }
}

// 添加成员
function openAddMember() {
  addMemberForm.value = { memberId: '', role: '' }
  addMemberVisible.value = true
}

async function confirmAddMember() {
  if (!currentTeam.value || !addMemberForm.value.memberId) {
    ElMessage.warning('请选择成员')
    return
  }
  addMemberSaving.value = true
  try {
    const memberId = addMemberForm.value.memberId
    // 候选成员只含 doctor，memberType 固定 doctor（candidateMembers 仅 doctors）
    const result = await addTeamMemberApi(currentTeam.value.teamId, {
      memberType: 'doctor',
      memberId,
      role: addMemberForm.value.role || undefined,
    })
    // 乐观 push 到成员表
    memberList.value.push(result)
    ElMessage.success('添加成功')
    addMemberVisible.value = false
    loadStats()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '添加失败')
  } finally {
    addMemberSaving.value = false
  }
}

// 编辑成员
function openEditMember(row: TeamMember) {
  editMemberForm.value = {
    memberId: row.memberId,
    memberType: row.memberType,
    name: row.name,
    role: row.role ?? '',
  }
  editMemberVisible.value = true
}

async function confirmEditMember() {
  if (!currentTeam.value) return
  editMemberSaving.value = true
  try {
    const result = await updateTeamMemberApi(
      currentTeam.value.teamId,
      editMemberForm.value.memberId,
      { memberType: editMemberForm.value.memberType, role: editMemberForm.value.role || undefined },
    )
    // 乐观更新本地行
    const idx = memberList.value.findIndex((m) => m.memberId === editMemberForm.value.memberId)
    if (idx >= 0) memberList.value[idx] = result
    ElMessage.success('更新成功')
    editMemberVisible.value = false
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '更新失败')
  } finally {
    editMemberSaving.value = false
  }
}

// 移除成员
async function confirmRemoveMember(row: TeamMember) {
  if (!currentTeam.value) return
  try {
    await ElMessageBox.confirm(`确定移除成员 ${row.name}？`, '移除确认', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return // 取消
  }
  try {
    await removeTeamMemberApi(currentTeam.value.teamId, row.memberId, row.memberType)
    // 乐观移除本地行（幂等）
    memberList.value = memberList.value.filter((m) => m.memberId !== row.memberId)
    ElMessage.success('移除成功')
    loadStats()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '移除失败')
  }
}

onMounted(async () => {
  loadTeams()
  try {
    doctors.value = await fetchDoctors()
  } catch {
    // 医生列表加载失败不阻塞
  }
})
</script>

<style scoped>
.page-toolbar {
  margin-bottom: 12px;
}
/* T289 5.1：统计卡样式取设计稿 团队管理.html:37-43 */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}
.stat-card {
  background: #fff;
  border-radius: 12px;
  padding: 20px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}
.stat-card .value {
  font-size: 28px;
  font-weight: 600;
}
.stat-card .label {
  font-size: 13px;
  color: #999;
  margin-top: 4px;
}
.label-row {
  display: flex;
  align-items: center;
  gap: 4px;
}
.stat-card.primary {
  border-left: 4px solid #1a6db5;
}
.stat-card.success {
  border-left: 4px solid #10ac84;
}
.stat-card.purple {
  border-left: 4px solid #9c27b0;
}
.stat-card.sky {
  border-left: 4px solid #2e86de;
}
.label-hint {
  cursor: help;
  color: #bbb;
}
@media (max-width: 1023px) {
  .stats-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
.member-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.member-search {
  width: 200px;
}
.member-role-filter {
  width: 150px;
}
.member-panel-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.member-panel-title {
  font-size: 15px;
  font-weight: 600;
}
</style>
