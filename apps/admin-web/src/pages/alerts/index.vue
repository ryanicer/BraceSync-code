<template>
  <div class="alerts">
    <div class="page-toolbar">
      <el-select v-model="typeFilter" placeholder="全部类型" clearable class="filter-select" @change="handleSearch">
        <el-option label="压力偏高" value="pressure_high" />
        <el-option label="佩戴中断" value="wear_interrupt" />
        <el-option label="压力波动" value="pressure_fluctuation" />
        <el-option label="传感器漂移" value="sensor_drift" />
      </el-select>
      <el-select v-model="statusFilter" placeholder="全部状态" clearable class="filter-select" @change="handleSearch">
        <el-option label="待处理" value="pending" />
        <el-option label="已处理" value="processed" />
      </el-select>
      <el-button type="primary" @click="handleSearch">查询</el-button>
    </div>

    <div class="page-card">
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column label="类型" width="110">
          <template #default="{ row }">
            <el-tag :type="severityType(row.type)" size="small">{{ alertTypeLabel(row.type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="detail" label="详情" min-width="240" show-overflow-tooltip />
        <el-table-column label="患者" width="110">
          <template #default="{ row }">{{ patientNameOf(row.patientId) }}</template>
        </el-table-column>
        <el-table-column prop="deviceId" label="设备" width="130" />
        <el-table-column label="传感器" width="80">
          <template #default="{ row }">{{ row.sensorPoint || '-' }}</template>
        </el-table-column>
        <el-table-column label="阈值/实际" width="110">
          <template #default="{ row }">
            {{ row.thresholdValue ? `${row.thresholdValue}/${row.actualValue}N` : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="时间" width="140">
          <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
        </el-table-column>
        <el-table-column label="处理状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.processStatus === 'pending' ? 'warning' : 'primary'" size="small">
              {{ row.processStatus === 'pending' ? '待处理' : '已处理' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="恢复态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.resolvedStatus === 'active' ? 'danger' : 'success'" size="small" effect="plain">
              {{ row.resolvedStatus === 'active' ? '进行中' : '已恢复' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="90" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.processStatus === 'pending'"
              size="small"
              link
              type="primary"
              @click="openProcess(row)"
            >处理</el-button>
            <span v-else class="processed-by">{{ row.processedBy || '-' }}</span>
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

    <!-- 处理对话框（复用 T019B processAlert 流程） -->
    <el-dialog v-model="processVisible" title="处理告警" width="420px">
      <template v-if="current">
        <p class="process-desc">{{ alertTypeLabel(current.type) }}：{{ current.detail }}</p>
        <el-input v-model="processNote" type="textarea" :rows="3" placeholder="处理备注（如：已通知患者调整佩戴位置）" />
      </template>
      <template #footer>
        <el-button @click="processVisible = false">取消</el-button>
        <el-button type="primary" :loading="processing" @click="confirmProcess">确认处理</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Alert } from '@bracesync/shared-types'
import { fetchAlerts, processAlertApi, patientNameOf } from '../../api'

const list = ref<Alert[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const typeFilter = ref('')
const statusFilter = ref('')
const loading = ref(false)
const processVisible = ref(false)
const processing = ref(false)
const processNote = ref('')
const current = ref<Alert | null>(null)

function alertTypeLabel(type: string): string {
  const map: Record<string, string> = {
    pressure_high: '压力偏高',
    wear_interrupt: '佩戴中断',
    pressure_fluctuation: '压力波动',
    sensor_drift: '传感器漂移',
  }
  return map[type] || type
}

function severityType(type: string): 'danger' | 'warning' {
  if (type === 'pressure_high' || type === 'wear_interrupt') return 'danger'
  return 'warning'
}

function formatTime(iso: string): string {
  return `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
}

async function loadData() {
  loading.value = true
  try {
    const res = await fetchAlerts({
      type: typeFilter.value || undefined,
      status: statusFilter.value || undefined,
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

function openProcess(alert: Alert) {
  current.value = alert
  processNote.value = ''
  processVisible.value = true
}

async function confirmProcess() {
  if (!current.value) return
  processing.value = true
  try {
    await processAlertApi(current.value.alertId)
    // mock 模式本地更新；真实模式由后端落库后列表刷新
    current.value.processStatus = 'processed'
    current.value.processedAt = new Date().toISOString()
    current.value.processNote = processNote.value || null
    ElMessage.success('处理成功')
    processVisible.value = false
    loadData()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '处理失败')
  } finally {
    processing.value = false
  }
}

onMounted(loadData)
</script>

<style scoped>
.filter-select {
  width: 150px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
.process-desc {
  margin: 0 0 12px;
  color: #333;
  font-size: 13px;
}
.processed-by {
  font-size: 12px;
  color: #999;
}
</style>
