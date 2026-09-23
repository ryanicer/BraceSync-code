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
        <el-button type="primary" @click="openCreate">新建技师</el-button>
      </div>
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column prop="name" label="姓名" width="100" />
        <el-table-column prop="phoneMasked" label="手机号" width="130" />
        <el-table-column label="所属团队" width="140">
          <template #default="{ row }">{{ teamNameOf(row.teamId) }}</template>
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
        <el-table-column label="操作" width="160" fixed="right">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openEdit(row)">编辑</el-button>
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
        <el-form-item label="手机号" required>
          <el-input
            v-model="form.phone"
            :placeholder="phoneHint"
            maxlength="11"
            :disabled="editing"
          />
        </el-form-item>
        <el-form-item label="所属团队" required>
          <el-select v-model="form.teamId" placeholder="请选择团队" style="width: 100%">
            <el-option
              v-for="t in teams"
              :key="t.teamId"
              :label="t.name"
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
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { PhoneState, Technician, Team } from '@bracesync/shared-types'
import { PHONE_PLACEHOLDER, PHONE_RE } from '../../utils/phoneField'
import {
  fetchTechnicians, toggleTechnicianApi, teamNameOf,
  createTechnicianApi, updateTechnicianApi, fetchTeams,
} from '../../api'

const list = ref<Technician[]>([])
const teams = ref<Team[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const loading = ref(false)
const keyword = ref('')

const formVisible = ref(false)
const editing = ref(false)
const submitting = ref(false)
const editingId = ref('')
const form = ref({ name: '', phone: '', teamId: '' })
/** 编辑态手机号的读侧状态（T361）：决定这格禁用输入框显示什么，而不是把脱敏串当可编辑原值 */
const editingPhoneState = ref<PhoneState>('masked')
// 技师编辑态的手机号是禁用框（设计稿 技师管理.html:245 编辑流程不改号码），
// 所以文案不能复用医护账号页那句「留空即不修改」—— 这里根本没有可填的入口。
const phoneHint = computed(() => {
  if (!editing.value) return '11 位手机号'
  if (editingPhoneState.value === 'unreadable') return `号码读取失败（${PHONE_PLACEHOLDER}）`
  if (editingPhoneState.value === 'absent') return '未登记手机号'
  return '编辑时不可修改手机号'
})

async function loadData() {
  loading.value = true
  try {
    const res = await fetchTechnicians({ page: page.value, pageSize: pageSize.value })
    let rows = res.list
    if (keyword.value.trim()) {
      const kw = keyword.value.trim().toLowerCase()
      rows = rows.filter((t) => t.name.toLowerCase().includes(kw) || t.phoneMasked.includes(kw))
    }
    list.value = rows
    total.value = res.total
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
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
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
  // T361：脱敏串不是「原值」。只在服务端确实读得到号码（masked）时把它作为只读展示回填；
  // absent/unreadable 一律空串，避免星号串被当成号码再次写回。
  form.value = {
    name: row.name,
    phone: row.phoneState === 'masked' ? row.phoneMasked : '',
    teamId: row.teamId,
  }
  formVisible.value = true
}

async function submitForm() {
  if (!form.value.name.trim()) { ElMessage.warning('请填写姓名'); return }
  if (!editing.value && !PHONE_RE.test(form.value.phone)) {
    ElMessage.warning('请填写正确的 11 位手机号'); return
  }
  if (!form.value.teamId) { ElMessage.warning('请选择所属团队'); return }

  submitting.value = true
  try {
    if (editing.value) {
      await updateTechnicianApi(editingId.value, { name: form.value.name.trim(), teamId: form.value.teamId })
      ElMessage.success('修改成功')
    } else {
      await createTechnicianApi({
        name: form.value.name.trim(),
        phone: form.value.phone,
        teamId: form.value.teamId,
      })
      ElMessage.success('创建成功')
    }
    formVisible.value = false
    loadData()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '操作失败')
  } finally {
    submitting.value = false
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
