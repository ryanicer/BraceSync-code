<template>
  <div class="devices">
    <div class="page-toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索设备ID / 患者ID"
        clearable
        class="search-input"
        @keyup.enter="loadData"
        @clear="loadData"
      />
      <el-select v-model="statusFilter" placeholder="全部状态" clearable class="status-select" @change="applyFilter">
        <el-option label="在线" value="online" />
        <el-option label="离线" value="offline" />
        <el-option label="异常" value="abnormal" />
        <el-option label="未绑定" value="unbound" />
      </el-select>
      <el-button type="primary" @click="loadData">查询</el-button>
    </div>

    <div class="page-card">
      <el-table :data="filteredList" size="small" v-loading="loading">
        <el-table-column prop="deviceId" label="设备ID" width="140" />
        <el-table-column prop="model" label="型号" width="130" />
        <el-table-column prop="firmwareVersion" label="固件版本" width="100" />
        <el-table-column label="绑定患者" width="120">
          <template #default="{ row }">{{ patientNameOf(row.patientId) }}</template>
        </el-table-column>
        <el-table-column label="WiFi" min-width="140">
          <template #default="{ row }">{{ row.wifiSsid || '-' }}</template>
        </el-table-column>
        <el-table-column label="绑定时间" width="120">
          <template #default="{ row }">{{ row.bindTime ? row.bindTime.slice(0, 10) : '-' }}</template>
        </el-table-column>
        <el-table-column label="最后上报" width="160">
          <template #default="{ row }">{{ formatDateTime(row.lastReportAt) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Device } from '@bracesync/shared-types'
import { fetchDevices, patientNameOf } from '../../api'

const list = ref<Device[]>([])
const keyword = ref('')
const statusFilter = ref('')
const loading = ref(false)

const filteredList = computed(() => {
  if (!statusFilter.value) return list.value
  return list.value.filter((d) => d.status === statusFilter.value)
})

function statusLabel(status: Device['status']): string {
  const map: Record<Device['status'], string> = { online: '在线', offline: '离线', abnormal: '异常', unbound: '未绑定' }
  return map[status] ?? status
}

function statusTagType(status: Device['status']): 'success' | 'danger' | 'warning' | 'info' {
  if (status === 'online') return 'success'
  if (status === 'abnormal') return 'danger'
  if (status === 'offline') return 'warning'
  return 'info'
}

function formatDateTime(iso: string | null): string {
  if (!iso) return '-'
  return `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
}

async function loadData() {
  loading.value = true
  try {
    const res = await fetchDevices({ keyword: keyword.value || undefined })
    list.value = res.list
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function applyFilter() {
  // 状态筛选为前端过滤，无需请求
}

onMounted(loadData)
</script>

<style scoped>
.search-input {
  width: 220px;
}
.status-select {
  width: 140px;
}
</style>
