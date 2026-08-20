<template>
  <div class="install-records">
    <div class="page-toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索安装ID / 设备ID / 患者ID"
        clearable
        class="search-input"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-button type="primary" @click="handleSearch">查询</el-button>
    </div>

    <div class="page-card">
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column prop="installId" label="安装ID" width="110" />
        <el-table-column prop="deviceId" label="设备" width="130" />
        <el-table-column label="患者" width="110">
          <template #default="{ row }">{{ patientNameOf(row.patientId) }}</template>
        </el-table-column>
        <el-table-column label="技师" width="100">
          <template #default="{ row }">{{ techNameOf(row.techId) }}</template>
        </el-table-column>
        <el-table-column label="校准时间" width="150">
          <template #default="{ row }">{{ formatTime(row.calibrateTime) }}</template>
        </el-table-column>
        <el-table-column label="基线" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.baselineId" type="success" size="small">已保存</el-tag>
            <el-tag v-else type="warning" size="small">待保存</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="WiFi" width="100">
          <template #default="{ row }">
            <el-tag :type="row.wifiStatus === 'connected' ? 'success' : 'info'" size="small" effect="plain">
              {{ row.wifiStatus === 'connected' ? '已配网' : '未配网' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="notes" label="备注" min-width="180" show-overflow-tooltip />
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { InstallRecord } from '@bracesync/shared-types'
import { fetchInstallRecords, patientNameOf, techNameOf } from '../../api'

const list = ref<InstallRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const keyword = ref('')
const loading = ref(false)

function formatTime(iso: string): string {
  return `${iso.slice(0, 10)} ${iso.slice(11, 16)}`
}

async function loadData() {
  loading.value = true
  try {
    const res = await fetchInstallRecords({
      keyword: keyword.value || undefined,
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

onMounted(loadData)
</script>

<style scoped>
.search-input {
  width: 260px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
