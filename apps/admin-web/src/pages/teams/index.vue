<template>
  <div class="teams">
    <!-- 团队列表模式 -->
    <div v-if="mode === 'list'">
      <div class="page-toolbar">
        <el-button type="success" @click="openCreate">新建团队</el-button>
        <!-- T658（Boss 12:2x 澄清选 A）：维护班组在团队管理页列表隐藏（技师归类 T627 后端仍用，
             技师编辑页归属下拉不过本页），类型筛选下拉随之退场；新建团队弹窗的类型选项保留维护班组。
             统计卡仍是全量口径，必须把差异写明，否则就是 T385 修过的「卡数与各行之和对不上」。 -->
        <span class="filter-hint">列表仅显示医护团队；维护班组（技师归类 T627）已隐藏，上方统计卡仍为全量口径</span>
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
        <!-- T658：列表只渲染医护团队；维护班组（TEAM04/TEAM05 等）不在本页出现 -->
        <el-table :data="visibleTeams" size="small" v-loading="loading">
          <el-table-column prop="teamId" label="团队编号" width="120" />
          <el-table-column prop="name" label="团队名称" min-width="160" />
          <el-table-column label="类型" width="100">
            <template #default="{ row }">
              <el-tag :type="row.teamType === 'maintenance' ? 'success' : 'info'" size="small">
                {{ row.teamType === 'maintenance' ? '维护班组' : '医护团队' }}
              </el-tag>
            </template>
          </el-table-column>
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
      <!-- T289 5.3：设计稿 团队管理.html:154-158 成员工具条 = 搜索成员 + 职称筛选 + 添加成员
           T456（T442 T-1 裁 A）：该下拉的选项与行内值都是 doctors.title ⇒ 标签按内容语义叫「职称」，
           稿面侧同一处由 T457（Peter）订正。 -->
      <div class="member-toolbar">
        <el-input v-model="memberKeyword" placeholder="搜索成员" clearable class="member-search" />
        <el-select v-model="memberRoleFilter" placeholder="全部职称" clearable class="member-role-filter">
          <el-option v-for="r in memberTitleOptions" :key="r" :label="r" :value="r" />
        </el-select>
        <el-button type="success" size="small" @click="openAddMember">添加成员</el-button>
      </div>
      <div class="page-card">
        <el-table :data="filteredMembers" size="small" v-loading="memberLoading">
          <el-table-column prop="name" label="姓名" width="110" />
          <!-- T456：契约把 doctors.title 放在 role、把 department 放在 title
               （pg.go:1582-1583 / mock-org.ts 同源），列头按内容语义命名，绑定不动。 -->
          <el-table-column label="职称" width="110">
            <template #default="{ row }">{{ row.role ?? '-' }}</template>
          </el-table-column>
          <el-table-column label="科室" width="110">
            <template #default="{ row }">{{ row.title ?? '-' }}</template>
          </el-table-column>
          <el-table-column label="手机号" width="130">
            <template #default="{ row }">{{ phoneDisplay(row.phoneMasked) }}</template>
          </el-table-column>
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
          <!-- T658：成员「编辑」入口移除（职称唯一维护入口=医护账号页），只保留移除；
               启用/停用在医护账号页维护，本页不设该动作。 -->
          <el-table-column label="操作" width="100" fixed="right">
            <template #default="{ row }">
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
            <el-option v-for="d in teamLeaderOptions" :key="d.doctorId" :label="d.name" :value="d.doctorId" />
          </el-select>
        </el-form-item>
        <el-form-item label="类型">
          <el-select v-model="teamForm.teamType" :disabled="isTeamEdit" style="width: 100%">
            <el-option label="医护团队" value="medical" />
            <el-option label="维护班组" value="maintenance" />
          </el-select>
          <!-- 文案给的是页面上真实存在的那条约束：新建技师的下拉只出维护班组（技师页 teamOptions），
               负责人一格仍是必填且候选只有医生（本弹窗的 leader 下拉）——服务端这条 T059 起就在，本卡没动它 -->
          <div class="form-hint">
            {{ isTeamEdit
              ? '类型创建后不可改：改类型等于把整支团队的成员腿换边，属另一格动作'
              : '医护团队放医生，维护班组放技师；新建技师时下拉只出维护班组。负责人仍必填且在医生里选（这条 T059 起就在，不随类型放宽）' }}
          </div>
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
    <!-- T736：职称唯一维护入口=医护账号页（Boss 拍板），本弹窗只选成员，不改职称 -->
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
      </el-form>
      <template #footer>
        <el-button @click="addMemberVisible = false">取消</el-button>
        <el-button type="primary" :loading="addMemberSaving" @click="confirmAddMember">确认添加</el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance } from 'element-plus'
import type { Team, TeamMember, TeamStats, Doctor, TeamType } from '@bracesync/shared-types'
import { phoneDisplay } from '../../utils/phoneField'
import { titleOptions } from '../../utils/medicalTitles'
import {
  fetchTeams, fetchDoctors, fetchTeamStats,
  createTeamApi, updateTeamApi, deleteTeamApi,
  fetchTeamMembersApi, addTeamMemberApi, removeTeamMemberApi,
  fetchTeamDetailApi,
} from '../../api'

const mode = ref<'list' | 'members'>('list')
const teams = ref<Team[]>([])
/**
 * T658：团队管理页只显示医护团队（Boss 12:2x 选 A）。维护班组仍由后端保留、技师编辑页归属下拉
 * 仍按 T627 取维护班组，两条腿都不经过本页，故这里只做前端展示收口。
 */
const visibleTeams = computed(() => teams.value.filter((t) => t.teamType !== 'maintenance'))
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
/** T736：筛选项走 T360 职称词表（预置 ∪ 成员真实 title），清掉本页硬编码角色数组 */
const memberTitleOptions = computed(() => titleOptions(doctors.value))

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
const teamForm = ref({ teamId: '', name: '', leader: '', description: '', teamType: 'medical' as TeamType })
const teamRules = {
  name: [{ required: true, message: '请输入团队名称', trigger: 'blur' }],
}
/** 编辑态判据就是「有没有带 teamId」，与 confirmSaveTeam 走 PUT 还是 POST 同一枚条件 */
const isTeamEdit = computed(() => !!teamForm.value.teamId)

/**
 * T685：负责人下拉候选。以全量医生为源，并保证「当前负责人」恒在候选内——
 * 真实后端详情会带回 leaderName，若该 doctorId 不在候选集（列表/医生投影缺列，T279 同源），
 * 用详情 leaderName 补一条，避免 el-select 无匹配 option 而显示空。
 */
const leaderCandidates = ref<{ doctorId: string; name: string }[]>([])
const teamLeaderOptions = computed(() => {
  const base = doctors.value.map((d) => ({ doctorId: d.doctorId, name: d.name }))
  const ids = new Set(base.map((o) => o.doctorId))
  for (const extra of leaderCandidates.value) {
    if (!ids.has(extra.doctorId)) base.push(extra)
  }
  return base
})

// 添加成员
const addMemberVisible = ref(false)
const addMemberSaving = ref(false)
const addMemberForm = ref({ memberId: '' })

function formatDate(iso: string): string {
  return iso ? iso.slice(0, 10) : '-'
}

async function loadTeams() {
  loading.value = true
  try {
    // 全量拉取，维护班组的隐藏在 visibleTeams 展示层收口（T658）
    teams.value = await fetchTeams({})
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载失败' }))
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

/**
 * 候选成员：仅医护账号（doctors），且不在当前团队内。
 * T658 ①（Boss 拍板，医护与技师/运维禁混编）：候选源只取 doctors，
 * 技师/运维账号不出现；技师归维护班组走技师编辑页（T627），不进本弹窗。
 */
const candidateMembers = computed(() => {
  const inTeam = new Set(memberList.value.map((m) => m.memberId))
  const list: { id: string; name: string }[] = []
  for (const d of doctors.value) {
    if (!inTeam.has(d.doctorId)) {
      // T671：后缀显示真实职称（Doctor.title，T456 字段换位契约），不再硬编码「（医生）」；
      // T658 已移除职称编辑，存量账号职称可为空——空就只显名字，数据真实形态优于假后缀。
      list.push({ id: d.doctorId, name: d.title ? `${d.name}（${d.title}）` : d.name })
    }
  }
  return list
})

// 新建/编辑团队
function openCreate() {
  teamDialogTitle.value = '新建团队'
  leaderCandidates.value = []
  teamForm.value = { teamId: '', name: '', leader: '', description: '', teamType: 'medical' }
  teamFormRef.value?.clearValidate()
  teamDialogVisible.value = true
}

async function openEdit(row: Team) {
  teamDialogTitle.value = '编辑团队'
  leaderCandidates.value = []
  teamForm.value = {
    teamId: row.teamId,
    name: row.name,
    leader: row.leader ?? '',
    description: row.description ?? '',
    teamType: row.teamType,
  }
  teamFormRef.value?.clearValidate()
  teamDialogVisible.value = true
  // T685：编辑弹窗负责人从团队详情端点取（权威源），纠正列表概要投影可能缺列导致的「不回显」（T279 旧账同源）。
  try {
    const detail = await fetchTeamDetailApi(row.teamId)
    teamForm.value = {
      teamId: detail.teamId,
      name: detail.name,
      leader: detail.leader ?? '',
      description: detail.description ?? '',
      teamType: detail.teamType,
    }
    // 当前负责人若不在候选集（医生列表/投影缺该 doctorId），并入候选，保证下拉渲染出名字而非空白
    if (detail.leader && !doctors.value.some((d) => d.doctorId === detail.leader)) {
      leaderCandidates.value = [{ doctorId: detail.leader, name: detail.leaderName ?? detail.leader }]
    } else {
      leaderCandidates.value = []
    }
  } catch {
    // 详情读失败：保留列表回填值，不清空负责人，也不阻断编辑
  }
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
      // 编辑：乐观更新本地行。PUT 不发 teamType——类型创建后不可改（服务端 UpdateTeamRequestDTO 没这一枚，
      // 改类型等于把整支团队的成员腿换边，属另一格动作，见 T627 卡内登记）
      const result = await updateTeamApi(teamForm.value.teamId, input)
      const idx = teams.value.findIndex((t) => t.teamId === teamForm.value.teamId)
      if (idx >= 0) teams.value[idx] = { ...teams.value[idx], ...result }
      ElMessage.success('更新成功')
    } else {
      // 新建：乐观 push 到列表首条
      const result = await createTeamApi({ ...input, teamType: teamForm.value.teamType })
      teams.value.unshift(result)
      ElMessage.success('创建成功')
    }
    teamDialogVisible.value = false
    loadStats() // 团队总数/成员总数随增删改变化，统计卡重新拉取
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '保存失败' }))
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '删除失败' }))
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载成员失败' }))
  } finally {
    memberLoading.value = false
  }
}

// 添加成员
function openAddMember() {
  addMemberForm.value = { memberId: '' }
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
    // 候选成员只含 doctor，memberType 固定 doctor（candidateMembers 仅 doctors）；
    // T736：不再提交 role，成员职称沿用医护账号页维护的 doctors.title
    const result = await addTeamMemberApi(currentTeam.value.teamId, {
      memberType: 'doctor',
      memberId,
    })
    // 乐观 push 到成员表
    memberList.value.push(result)
    ElMessage.success('添加成功')
    addMemberVisible.value = false
    loadStats()
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '添加失败' }))
  } finally {
    addMemberSaving.value = false
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '移除失败' }))
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
/* T627：筛选态提示与表单口径说明（两者都是「页面读数与另一处口径不同」时必须有的一句白话） */
.filter-hint {
  margin-left: 10px;
  font-size: 12px;
  color: #909399;
}
.form-hint {
  font-size: 12px;
  line-height: 1.5;
  color: #909399;
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
