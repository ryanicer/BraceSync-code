<template>
  <div class="teams">
    <!-- 团队列表模式 -->
    <div v-if="mode === 'list'">
      <div class="page-toolbar">
        <el-button type="success" @click="openCreate">新建团队</el-button>
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
        <el-button type="success" size="small" @click="openAddMember">添加成员</el-button>
      </div>
      <div class="page-card">
        <el-table :data="memberList" size="small" v-loading="memberLoading">
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
import type { Team, TeamMember, Doctor } from '@bracesync/shared-types'
import {
  fetchTeams, fetchDoctors,
  createTeamApi, updateTeamApi, deleteTeamApi,
  fetchTeamMembersApi, addTeamMemberApi, updateTeamMemberApi, removeTeamMemberApi,
} from '../../api'

const ROLE_OPTIONS = ['主任医师', '副主任医师', '主治医师', '住院医师', '护士', '康复师']

const mode = ref<'list' | 'members'>('list')
const teams = ref<Team[]>([])
const doctors = ref<Doctor[]>([])
const loading = ref(false)

// 团队成员管理
const currentTeam = ref<Team | null>(null)
const memberList = ref<TeamMember[]>([])
const memberLoading = ref(false)

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
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '删除失败')
  }
}

// 成员管理
async function openMembers(row: Team) {
  currentTeam.value = row
  mode.value = 'members'
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
