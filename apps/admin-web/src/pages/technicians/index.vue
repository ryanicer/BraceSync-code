<template>
  <div class="technicians">
    <div class="page-card">
      <div class="toolbar">
        <el-input
          v-model="keyword"
          placeholder="搜索姓名 / 手机号"
          clearable
          class="search-input"
          @keyup.enter="loadData"
          @clear="loadData"
        />
        <!-- T627 方案乙：类型过滤走服务端（?teamType=），分页端点必须先过滤再分页，
             否则「total 是全量、列的是过滤后」两数在同一页打脸。 -->
        <el-select
          v-model="teamTypeFilter"
          clearable
          placeholder="全部类型"
          class="type-filter"
          style="width: 130px"
          @change="onFilterChange"
        >
          <el-option label="医护团队" value="medical" />
          <el-option label="维护班组" value="maintenance" />
        </el-select>
        <el-button type="primary" @click="openCreate">新建技师</el-button>
      </div>
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column prop="name" label="姓名" width="100" />
        <el-table-column label="手机号" width="130">
          <template #default="{ row }">{{ phoneDisplay(row.phoneMasked) }}</template>
        </el-table-column>
        <el-table-column label="所属团队" width="140">
          <template #default="{ row }">{{ teamNameOf(row.teamId) }}</template>
        </el-table-column>
        <el-table-column label="归属类型" width="110">
          <template #default="{ row }">
            <el-tag :type="row.teamType === 'maintenance' ? 'success' : row.teamType === 'medical' ? 'info' : 'warning'" size="small">
              {{ teamTypeLabel(row.teamType) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="installCount" label="安装次数" width="90" />
        <el-table-column label="认证状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.authStatus === 'authorized' ? 'success' : 'warning'" size="small">
              {{ row.authStatus === 'authorized' ? '已认证' : '未认证' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'enabled' ? 'success' : 'info'" size="small">
              {{ row.status === 'enabled' ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="110">
          <template #default="{ row }">{{ row.createdAt ? row.createdAt.slice(0, 10) : '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" link type="warning" @click="askReset(row)">重置密码</el-button>
            <el-popconfirm
              :title="row.status === 'enabled' ? `确认禁用技师 ${row.name}？` : `确认启用技师 ${row.name}？`"
              @confirm="toggle(row)"
            >
              <template #reference>
                <el-button size="small" link :type="row.status === 'enabled' ? 'danger' : 'success'">
                  {{ row.status === 'enabled' ? '禁用' : '启用' }}
                </el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination
        class="pagination"
        v-model:current-page="page"
        :total="total"
        :page-size="pageSize"
        layout="total, prev, pager, next"
        @current-change="loadData"
      />
    </div>

    <!-- 新建 / 编辑模态框 -->
    <el-dialog v-model="formVisible" :title="editing ? '编辑技师' : '新建技师'" width="420px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="姓名" required>
          <el-input v-model="form.name" placeholder="技师姓名" maxlength="20" />
        </el-form-item>
        <el-form-item label="手机号" :required="!editing">
          <el-input v-model="form.phone" :placeholder="phoneHint" maxlength="11" />
        </el-form-item>
        <el-form-item label="所属团队" required>
          <el-select v-model="form.teamId" placeholder="请选择维护班组" style="width: 100%">
            <el-option
              v-for="t in teamOptions"
              :key="t.teamId"
              :label="t.label"
              :value="t.teamId"
            />
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
import { ref, computed, h, onMounted } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { PhoneState, Technician, Team, TeamType } from '@bracesync/shared-types'
import { PHONE_RE, phoneDisplay, phonePatch, phonePlaceholder } from '../../utils/phoneField'
import {
  fetchTechnicians, toggleTechnicianApi, teamNameOf,
  createTechnicianApi, updateTechnicianApi, resetTechnicianPasswordApi, fetchTeams,
} from '../../api'

const list = ref<Technician[]>([])
const teams = ref<Team[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const keyword = ref('')
/** T627：'' = 不过滤（api 层把空值剔出 query string），其余两值就是 shared-types 的 TeamType */
const teamTypeFilter = ref<'' | TeamType>('')

const formVisible = ref(false)
const editing = ref(false)
const submitting = ref(false)
const editingId = ref('')
const form = ref({ name: '', phone: '', teamId: '' })
/** 编辑态手机号的读侧状态（T361）：只决定占位文案，号码本身仍由列表列展示 */
const editingPhoneState = ref<PhoneState>('absent')
// T624：编辑态手机号可见且可编辑，占位文案沿用医护账号页那把共用尺（留空即不改）。
const phoneHint = computed(() => (editing.value ? phonePlaceholder(editingPhoneState.value) : '11 位手机号'))

async function loadData() {
  loading.value = true
  try {
    const res = await fetchTechnicians({
      page: page.value,
      pageSize: pageSize.value,
      teamType: teamTypeFilter.value || undefined,
    })
    let rows = res.list
    if (keyword.value.trim()) {
      const kw = keyword.value.trim().toLowerCase()
      rows = rows.filter((t) => t.name.toLowerCase().includes(kw) || t.phoneMasked.includes(kw))
    }
    list.value = rows
    total.value = res.total
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载失败' }))
  } finally {
    loading.value = false
  }
}

function onFilterChange() {
  page.value = 1
  loadData()
}

function teamTypeLabel(t?: TeamType | null): string {
  if (t === 'maintenance') return '维护班组'
  if (t === 'medical') return '医护团队'
  return '未知团队'
}

/** T627 R1：新建/编辑技师的归属只列维护班组。
 *  例外是编辑存量行——它当前挂在医护团队上（刷数之前的形状），服务端对「保存同团队」放行
 *  （类型判据只在 teamChanged 为真时咬），所以这一格得把它当前的团队也列出来并标明是存量；
 *  不列就是下拉里没有它 ⇒ 页面显示裸 ID，要么把人悄悄搬走，要么让一条本来合法的保存看起来坏了。 */
const teamOptions = computed(() => {
  const opts = teams.value
    .filter((t) => t.teamType === 'maintenance')
    .map((t) => ({ teamId: t.teamId, label: t.name }))
  const cur = teams.value.find((t) => t.teamId === form.value.teamId)
  if (cur && cur.teamType !== 'maintenance') {
    opts.push({ teamId: cur.teamId, label: `${cur.name}（存量：${teamTypeLabel(cur.teamType)}，保存同团队不改归属）` })
  }
  return opts
})

async function loadTeams() {
  try {
    teams.value = await fetchTeams()
  } catch {
    // 团队列表加载失败不阻塞
  }
}

async function toggle(row: Technician) {
  const action = row.status === 'enabled' ? 'disable' : 'enable'
  try {
    await toggleTechnicianApi(row.techId, action)
    row.status = action === 'enable' ? 'enabled' : 'disabled'
    ElMessage.success(action === 'enable' ? '已启用' : '已禁用')
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '操作失败' }))
  }
}

function openCreate() {
  editing.value = false
  editingId.value = ''
  form.value = { name: '', phone: '', teamId: '' }
  formVisible.value = true
}

function openEdit(row: Technician) {
  editing.value = true
  editingId.value = row.techId
  editingPhoneState.value = row.phoneState
  // T361 / T624：脱敏串不是「原值」，编辑框永不预填它——预填会让星号串有机会被当成号码写回。
  // 当前号码在列表行里展示；要换号必须在这里填 11 位新号，留空即保持库内号码不变。
  form.value = { name: row.name, phone: '', teamId: row.teamId }
  formVisible.value = true
}

async function submitForm() {
  if (!form.value.name.trim()) { ElMessage.warning('请填写姓名'); return }
  if (!editing.value && !PHONE_RE.test(form.value.phone)) {
    ElMessage.warning('请填写正确的 11 位手机号'); return
  }
  // 编辑态手机号选填：留空 = 不改库内号码（T361），填了就必须是合法新号。
  if (editing.value && form.value.phone && !PHONE_RE.test(form.value.phone)) {
    ElMessage.warning('手机号需为 11 位号码，或留空'); return
  }
  if (!form.value.teamId) { ElMessage.warning('请选择所属团队'); return }

  submitting.value = true
  try {
    if (editing.value) {
      await updateTechnicianApi(editingId.value, {
        name: form.value.name.trim(),
        phone: phonePatch(form.value.phone),
        teamId: form.value.teamId,
      })
      ElMessage.success('修改成功')
      formVisible.value = false
      loadData()
    } else {
      // 创建态的手机号是管理员刚填的明文，本就在页面手里 ⇒ 弹窗用它当「登录账号」
      const loginPhone = form.value.phone.trim()
      const res = await createTechnicianApi({
        name: form.value.name.trim(),
        phone: loginPhone,
        teamId: form.value.teamId,
      })
      formVisible.value = false
      await loadData()
      // T480：口令只在这一次响应里出现，用一次性弹窗替代原先那句无用的「创建成功」toast
      await showCredentials(res.account.techId, `登录手机号：${loginPhone}`, res.initialPassword, '创建成功')
    }
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '操作失败' }))
  } finally {
    submitting.value = false
  }
}

/**
 * 一次性凭据弹窗（T480，与医护账号页同规则）：关窗后页面上再没有入口可看这个口令。
 *
 * 账号行由调用方给可展示形态：创建时前端握着明文手机号；重置时读侧只有脱敏号
 * （T361 起服务端即已掩码）⇒ 重置那次必须显式说明「登录号＝建档手机号」，
 * 否则管理员拿着口令却不知道登哪个号。
 */
function showCredentials(techId: string, accountLine: string, password: string, title: string): Promise<void> {
  return ElMessageBox({
    title,
    message: h('div', { class: 'cred-box' }, [
      h('p', `技师编号：${techId}`),
      h('p', accountLine),
      h('p', `初始密码：${password}`),
      // T486：口令能用不等于管理员知道怎么用——技师端登录页只收手机号＋密码（无编号入口），
      // 不说这一句就会有人拿着编号去登。创建与重置两次都要说，故不随调用方分叉。
      h('p', { class: 'cred-hint' }, '技师使用手机号 + 密码登录（技师编号不能用于登录）。'),
      h('p', { class: 'cred-note' }, '仅此一次展示，关闭后不可再看。密码由系统随机生成，请当面 / 即时转交本人；如遗失，用列表行内「重置密码」按同一规则再生成一次。'),
    ]),
    confirmButtonText: '我已转交本人',
    // 关窗（X / Esc）不是操作失败：口令已经展示过了，拒绝对 Promise 无意义 ⇒ 吞掉，
    // 免得调用方的 catch 把「我点了 X」弹成「操作失败」。
  }).then(() => undefined).catch(() => undefined)
}

async function askReset(row: Technician) {
  try {
    await ElMessageBox.confirm(
      `将为 ${row.name}（${row.techId}） 重新随机生成登录密码，确认后一次性展示、旧密码即时失效。重置动作本身计入操作日志。`,
      '确认重置密码',
      { type: 'warning', confirmButtonText: '确认', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    const pwd = await resetTechnicianPasswordApi(row.techId)
    await showCredentials(
      row.techId,
      `登录手机号：${phoneDisplay(row.phoneMasked)}（建档时登记的号码，列表按 §9.2 脱敏展示）`,
      pwd,
      '重置成功',
    )
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '操作失败' }))
  }
}

onMounted(() => {
  loadTeams()
  loadData()
})
</script>

<style scoped>
.toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 14px;
}
.search-input {
  width: 240px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
