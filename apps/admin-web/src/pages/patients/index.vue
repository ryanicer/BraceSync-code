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
    </div>

    <div class="page-card">
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
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Patient, Team } from '@bracesync/shared-types'
import { fetchPatients, fetchTeams, teamNameOf, doctorNameOf } from '../../api'

const list = ref<Patient[]>([])
const teams = ref<Team[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const keyword = ref('')
const teamFilter = ref('')
const loading = ref(false)
const drawerVisible = ref(false)
const detail = ref<Patient | null>(null)

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

function viewDetail(row: Patient) {
  detail.value = row
  drawerVisible.value = true
}

onMounted(async () => {
  loadData()
  try {
    teams.value = await fetchTeams()
  } catch {
    // 团队筛选失败不阻塞列表
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
</style>
