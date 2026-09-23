<template>
  <div class="medical-accounts">
    <el-alert type="info" :closable="false" class="page-notice">
      <p>
        医护账号 = <b>能登录本运营后台</b>的机构用户，由运营管理员创建并分配团队（PRD §7D.10）。
      </p>
      <p>
        与「技师管理」不同端（技师登录小程序、不登录后台）；可访问页面由「权限控制 §7D.11 角色矩阵」决定，
        本页只管<b>人 ↔ 职称 ↔ 团队</b>。
      </p>
      <p>
        职称（主任医师 / 主治医师 / 康复师 / 护士，落 doctors.title）≠ 登录角色；登录角色只有 3 个
        （运营管理员 / 医护 / 客服），<b>本类账号的登录角色固定为「医护」</b>。
      </p>
    </el-alert>

    <div class="page-card">
      <div class="toolbar">
        <el-input
          v-model="keyword"
          placeholder="搜索姓名 / 科室 / 登录账号"
          clearable
          class="search-input"
        />
        <el-select v-model="teamFilter" class="filter-select" placeholder="全部团队">
          <el-option label="全部团队" value="" />
          <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
        </el-select>
        <el-select v-model="titleFilter" class="filter-select" placeholder="全部职称">
          <el-option label="全部职称" value="" />
          <el-option v-for="t in titleChoices" :key="t" :label="t" :value="t" />
        </el-select>
        <el-button type="primary" @click="openCreate">+ 新建医护账号</el-button>
        <span class="count-hint">共 {{ list.length }} 个账号（启用 {{ enabledCount }} / 禁用 {{ list.length - enabledCount }}）</span>
      </div>

      <!-- 10 列按设计稿 :141；宽度合计控制在 1280 视口内，不做右侧固定列（固定列会盖住状态/创建时间） -->
      <el-table :data="list" size="small" v-loading="loading" empty-text="无匹配的医护账号">
        <el-table-column prop="name" label="姓名" width="80" />
        <el-table-column label="登录账号" width="95">
          <template #default="{ row }">{{ row.username || DASH }}</template>
        </el-table-column>
        <el-table-column label="手机号" width="100">
          <template #default="{ row }">{{ row.phoneMasked || DASH }}</template>
        </el-table-column>
        <el-table-column prop="department" label="科室" min-width="80" />
        <el-table-column label="所属团队" min-width="100">
          <template #default="{ row }">{{ row.teamId ? teamNameOf(row.teamId) : DASH }}</template>
        </el-table-column>
        <el-table-column label="职称" width="95">
          <template #default="{ row }">
            <el-tag :class="`title-tag title-${titleTone(row.title)}`" size="small">{{ row.title }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="patientCount" label="管理患者数" width="90" />
        <el-table-column label="状态" width="65">
          <template #default="{ row }">
            <el-tag :type="row.status === 'enabled' ? 'success' : 'info'" size="small">
              {{ row.status === 'enabled' ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="90">
          <template #default="{ row }">{{ row.createdAt ? row.createdAt.slice(0, 10) : DASH }}</template>
        </el-table-column>
        <el-table-column label="操作" width="155">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" link type="warning" @click="askReset(row)">重置密码</el-button>
            <el-button
              size="small"
              link
              :type="row.status === 'enabled' ? 'danger' : 'success'"
              @click="askToggle(row)"
            >
              {{ row.status === 'enabled' ? '禁用' : '启用' }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 设计稿 :146：手机号服务端即已脱敏；管理患者数 = 主诊患者数，不是团队患者总数 -->
      <p class="table-hint">
        手机号按 §9.2 患者隐私口径脱敏展示。<b>「管理患者数」= 以该医生为主诊医生的患者数</b>
        （事实源 patients.primary_doctor_id），<b>不是</b>其所属团队的患者总数。
      </p>
    </div>

    <el-dialog v-model="formVisible" :title="editing ? '编辑医护账号' : '新建医护账号'" width="520px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="姓名" required>
          <el-input v-model="form.name" placeholder="医护姓名" maxlength="20" />
        </el-form-item>
        <el-form-item label="手机号">
          <el-input v-model="form.phone" :placeholder="phoneHint" maxlength="11" />
          <span class="form-help">
            选填：登录账号由系统生成，手机号不承担登录职责；列表按 §9.2 脱敏展示。
            编辑时此处不回显原号 —— 留空即保持库内号码不变，要换号请填 11 位新号。
          </span>
        </el-form-item>
        <el-form-item label="登录账号">
          <el-input :model-value="acctHint" readonly disabled />
          <span class="form-help">系统自动生成、运营不可输入：规则 = doc + 5 位序号，全系统唯一。</span>
        </el-form-item>
        <el-form-item v-if="!editing" label="初始密码">
          <el-input model-value="系统随机生成，创建成功后一次性展示" readonly disabled />
          <span class="form-help">创建成功后在结果弹窗一次性展示账号与密码，页面不保留、不可二次查看。</span>
        </el-form-item>
        <el-form-item label="科室" required>
          <el-input v-model="form.department" placeholder="如：脊柱外科" maxlength="20" />
        </el-form-item>
        <el-form-item label="所属团队" required>
          <el-select v-model="form.teamId" placeholder="请选择团队" style="width: 100%">
            <el-option v-for="t in teams" :key="t.teamId" :label="t.name" :value="t.teamId" />
          </el-select>
        </el-form-item>
        <el-form-item label="职称" required>
          <el-select v-model="form.title" placeholder="请选择职称" style="width: 100%">
            <el-option v-for="t in titleChoices" :key="t" :label="t" :value="t" />
          </el-select>
          <span class="form-help">职称落 doctors.title，均为医护岗位职称、可扩展；职称 ≠ 登录角色，本页账号的后台登录角色固定为「医护」。</span>
        </el-form-item>
        <el-form-item label="初始状态">
          <el-select v-model="form.status" style="width: 100%">
            <el-option label="启用" value="enabled" />
            <el-option label="禁用（暂不开放登录）" value="disabled" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">
          {{ editing ? '保存修改' : '确认创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { PhoneState, Team } from '@bracesync/shared-types'
import { fetchTeams, teamNameOf } from '../../api'
import { PHONE_RE, phonePatch, phonePlaceholder } from '../../utils/phoneField'
import {
  createMedicalAccountApi,
  fetchMedicalAccounts,
  resetMedicalPasswordApi,
  setMedicalAccountStatusApi,
  updateMedicalAccountApi,
  type MedicalAccount,
  type UpdateMedicalAccountInput,
} from '../../api/medicalAccount'
import { titleOptions } from '../../utils/medicalTitles'

/** §9.2 / 设计稿 :299：空值一律显示横杠，不留空白格 */
const DASH = '—'

const rows = ref<MedicalAccount[]>([])
const teams = ref<Team[]>([])
const loading = ref(false)
const keyword = ref('')
const teamFilter = ref('')
const titleFilter = ref('')

const formVisible = ref(false)
const editing = ref(false)
const submitting = ref(false)
const editingId = ref('')
/**
 * 编辑态手机号的读侧状态（T361）：只用于挑占位提示文案。
 * 🔴 输入框永不预填 row.phoneMasked —— 那串是可展示脱敏值不是可编辑原值，
 * 一旦预填，「清空它」就会被服务端按写语义理解成「删掉真号」（不可回滚）。
 */
const editingPhoneState = ref<PhoneState>('absent')
/** 编辑态原状态：服务端 PUT 不收 status，只有真改过才另发 /status */
const originalStatus = ref<'enabled' | 'disabled'>('enabled')
const form = ref({ name: '', phone: '', department: '', teamId: '', title: '', status: 'enabled' as 'enabled' | 'disabled' })

/** 手机号输入框占位提示：三态各一句，全部是「提示」而非「值」 */
const phoneHint = computed(() => phonePlaceholder(editingPhoneState.value))

const list = computed(() => {
  const kw = keyword.value.trim()
  return rows.value.filter((r) =>
    (!kw || r.name.includes(kw) || r.department.includes(kw) || r.username.includes(kw))
    && (!teamFilter.value || r.teamId === teamFilter.value)
    && (!titleFilter.value || r.title === titleFilter.value),
  )
})

const enabledCount = computed(() => list.value.filter((r) => r.status === 'enabled').length)

/**
 * 职称下拉词表 = 预置 4 项 ∪ 当前列表出现过的职称（T360）。
 * 取未过滤的 rows 而非 list：选了「副主任医师」后再输关键字，词表不能把自己筛掉。
 * 设计稿 :306/:308 两处下拉读同一个数组 ⇒ 筛选与编辑同源，这里只算一次。
 */
const titleChoices = computed(() => titleOptions(rows.value))

/** 设计稿 :226/:360：新建态提示「系统自动生成」，编辑态提示「不可改」 */
const acctHint = computed(() => {
  const username = editing.value ? (rows.value.find((r) => r.doctorId === editingId.value)?.username ?? '') : ''
  return editing.value
    ? `${username}（系统生成，不可改）`
    : '（系统自动生成）'
})

/** 设计稿 :309 titleBadge 配色：主任医师紫 / 主治医师蓝 / 康复师橙 / 其余灰 */
function titleTone(title: string): 'purple' | 'blue' | 'orange' | 'gray' {
  if (title === '主任医师') return 'purple'
  if (title === '主治医师') return 'blue'
  if (title === '康复师') return 'orange'
  return 'gray'
}

async function loadData() {
  loading.value = true
  try {
    rows.value = await fetchMedicalAccounts()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

async function loadTeams() {
  try {
    teams.value = await fetchTeams()
  } catch {
    // 团队字典失败不阻塞列表渲染
  }
}

function openCreate() {
  editing.value = false
  editingId.value = ''
  editingPhoneState.value = 'absent'
  originalStatus.value = 'enabled'
  form.value = { name: '', phone: '', department: '', teamId: '', title: '', status: 'enabled' }
  formVisible.value = true
}

function openEdit(row: MedicalAccount) {
  editing.value = true
  editingId.value = row.doctorId
  editingPhoneState.value = row.phoneState
  originalStatus.value = row.status
  form.value = {
    name: row.name,
    // T361：留空 = 不改动手机号（要改必须填 11 位新号；页面无「清除手机号」控件）
    phone: '',
    department: row.department,
    teamId: row.teamId ?? '',
    title: row.title,
    status: row.status,
  }
  formVisible.value = true
}

function validateForm(): boolean {
  if (!form.value.name.trim()) { ElMessage.warning('请填写姓名'); return false }
  if (!form.value.department.trim()) { ElMessage.warning('请填写科室'); return false }
  if (!form.value.teamId) { ElMessage.warning('请选择所属团队'); return false }
  if (!form.value.title) { ElMessage.warning('请选择职称'); return false }
  // 手机号选填（设计稿 :221）：填了就必须是合法新号，留空一律按「不改」处理（T361）
  if (form.value.phone && !PHONE_RE.test(form.value.phone)) {
    ElMessage.warning('手机号需为 11 位号码，或留空')
    return false
  }
  return true
}

async function submitForm() {
  if (!validateForm()) return
  submitting.value = true
  try {
    if (editing.value) {
      const patch: UpdateMedicalAccountInput = {
        name: form.value.name.trim(),
        phone: phonePatch(form.value.phone),
        department: form.value.department.trim(),
        teamId: form.value.teamId,
        title: form.value.title,
      }
      // 服务端 PUT 不收 status ⇒ 状态真改过才另发 /status（设计稿 :378 编辑态可改状态）
      if (form.value.status !== originalStatus.value) patch.status = form.value.status
      await updateMedicalAccountApi(editingId.value, patch)
      formVisible.value = false
      await loadData()
      ElMessage.success('修改成功')
    } else {
      const res = await createMedicalAccountApi({
        name: form.value.name.trim(),
        phone: form.value.phone,
        department: form.value.department.trim(),
        teamId: form.value.teamId,
        title: form.value.title,
        status: form.value.status,
      })
      formVisible.value = false
      await loadData()
      await showCredentials(res.account.username, res.initialPassword, '创建成功')
    }
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  } finally {
    submitting.value = false
  }
}

/** 一次性凭据弹窗：关闭后页面无任何入口可再看（设计稿 :388-397） */
function showCredentials(username: string, password: string, title: string): Promise<void> {
  return ElMessageBox({
    title,
    message: h('div', { class: 'cred-box' }, [
      h('p', `登录账号：${username}`),
      h('p', `初始密码：${password}`),
      h('p', { class: 'cred-note' }, '仅此一次展示，关闭后不可再看。密码由系统随机生成，请当面 / 即时转交本人；如遗失，用列表行内「重置密码」按同一规则再生成一次。'),
    ]),
    confirmButtonText: '我已转交本人',
  }).then(() => undefined)
}

async function askToggle(row: MedicalAccount) {
  const disabling = row.status === 'enabled'
  const text = disabling
    ? `禁用后 ${row.name}（${row.username}） 将无法登录运营后台，其历史操作记录保留不变。在途数据归属口径待 PRD 确认。`
    : `启用后 ${row.name}（${row.username}） 可按其角色权限重新登录后台。`
  try {
    await ElMessageBox.confirm(text, disabling ? '确认禁用' : '确认启用', {
      type: 'warning',
      confirmButtonText: '确认',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await setMedicalAccountStatusApi(row.doctorId, disabling ? 'disabled' : 'enabled')
    await loadData()
    ElMessage.success(disabling ? '已禁用' : '已启用')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  }
}

async function askReset(row: MedicalAccount) {
  try {
    await ElMessageBox.confirm(
      `将为 ${row.name}（${row.username}） 重新随机生成初始密码，确认后一次性展示、旧密码即时失效。重置动作本身计入操作日志。`,
      '确认重置密码',
      { type: 'warning', confirmButtonText: '确认', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    const pwd = await resetMedicalPasswordApi(row.doctorId)
    await showCredentials(row.username, pwd, '重置成功')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  }
}

onMounted(() => {
  loadTeams()
  loadData()
})
</script>

<style scoped>
.page-notice {
  margin-bottom: 14px;
}
.page-notice p {
  margin: 2px 0;
  line-height: 1.6;
}
.toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 14px;
}
.search-input {
  width: 240px;
}
.filter-select {
  width: 150px;
}
.count-hint {
  margin-left: auto;
  font-size: 13px;
  color: #7f8c8d;
}
.table-hint {
  margin-top: 12px;
  font-size: 12px;
  color: #7f8c8d;
}
.form-help {
  display: block;
  font-size: 12px;
  color: #909399;
  line-height: 1.5;
}
.title-tag {
  border: none;
}
.title-purple {
  background: #f3e8ff;
  color: #6c3483;
}
.title-blue {
  background: #eaf2fd;
  color: #1a6db5;
}
.title-orange {
  background: #fdf1e3;
  color: #d6741e;
}
.title-gray {
  background: #f0f2f5;
  color: #606266;
}
</style>
