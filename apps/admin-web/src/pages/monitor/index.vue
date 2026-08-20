<template>
  <div class="monitor">
    <div class="page-toolbar">
      <el-tag type="info" effect="plain">每 30s 自动刷新</el-tag>
      <span class="update-time">最近更新：{{ lastUpdated || '-' }}</span>
      <el-button size="small" @click="loadAll">立即刷新</el-button>
    </div>

    <div class="page-card">
      <el-table :data="rows" size="small" v-loading="loading">
        <el-table-column prop="patientId" label="患者ID" width="110" />
        <el-table-column prop="name" label="姓名" width="100" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.snapshot?.status)" size="small">
              {{ statusLabel(row.snapshot?.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="今日佩戴" width="110">
          <template #default="{ row }">
            {{ row.snapshot ? row.snapshot.todayHours.toFixed(1) + 'h' : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="最大压力" width="120">
          <template #default="{ row }">
            <span :class="{ 'pressure-warn': (row.snapshot?.maxPressure ?? 0) > 45 }">
              {{ row.snapshot ? row.snapshot.maxPressure + 'N' : '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="压力峰值点" width="110">
          <template #default="{ row }">{{ row.snapshot?.maxPoint || '-' }}</template>
        </el-table-column>
        <el-table-column label="今日异常事件" width="120">
          <template #default="{ row }">
            <el-tag v-if="(row.snapshot?.events ?? 0) > 0" type="danger" size="small">{{ row.snapshot.events }} 次</el-tag>
            <span v-else>0</span>
          </template>
        </el-table-column>
        <el-table-column label="设备" width="140">
          <template #default="{ row }">{{ row.deviceId || '未绑定' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button size="small" link type="primary" :disabled="!row.snapshot" @click="viewDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 详情抽屉 -->
    <el-drawer v-model="drawerVisible" :title="detailRow ? `${detailRow.name} 实时快照` : ''" size="420px">
      <template v-if="detailRow?.snapshot">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="状态">{{ statusLabel(detailRow.snapshot.status) }}</el-descriptions-item>
          <el-descriptions-item label="今日佩戴时长">{{ detailRow.snapshot.todayHours.toFixed(1) }}h</el-descriptions-item>
          <el-descriptions-item label="最大压力">{{ detailRow.snapshot.maxPressure }}N（{{ detailRow.snapshot.maxPoint }}）</el-descriptions-item>
          <el-descriptions-item label="今日异常事件">{{ detailRow.snapshot.events }} 次</el-descriptions-item>
          <el-descriptions-item label="最新帧上报">
            {{ detailRow.snapshot.pressureRecords[0]?.timestamp ?? '-' }}
          </el-descriptions-item>
        </el-descriptions>
        <el-alert
          v-if="detailRow.snapshot.status === 'abnormal'"
          title="设备状态异常，请检查设备故障码或联系技师"
          type="warning"
          :closable="false"
          class="detail-alert"
        />
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import type { Patient } from '@bracesync/shared-types'
import { fetchPatients, fetchPatientRealtime } from '../../api'
import type { RealtimeSnapshot } from '../../mock/patients'

interface MonitorRow {
  patientId: string
  name: string
  deviceId: string | null
  snapshot: RealtimeSnapshot | null
}

const POLL_INTERVAL = 30_000

const rows = ref<MonitorRow[]>([])
const loading = ref(false)
const lastUpdated = ref('')
const drawerVisible = ref(false)
const detailRow = ref<MonitorRow | null>(null)
let timer: ReturnType<typeof setInterval> | null = null

function statusLabel(status?: string): string {
  if (status === 'online') return '在线'
  if (status === 'abnormal') return '异常'
  if (status === 'offline') return '离线'
  return '加载中'
}

function statusTagType(status?: string): 'success' | 'danger' | 'info' {
  if (status === 'online') return 'success'
  if (status === 'abnormal') return 'danger'
  return 'info'
}

async function loadAll() {
  loading.value = true
  try {
    if (rows.value.length === 0) {
      const res = await fetchPatients({ page: 1, pageSize: 50 })
      rows.value = res.list.map((p: Patient) => ({
        patientId: p.patientId,
        name: p.name,
        deviceId: p.deviceId,
        snapshot: null,
      }))
    }
    // 并发拉取实时快照（getPatientRealtime 契约，data-service Redis 快照）
    await Promise.all(rows.value.map(async (row) => {
      if (!row.deviceId) return
      try {
        row.snapshot = await fetchPatientRealtime(row.patientId)
      } catch {
        // 单个患者失败不阻塞整页
      }
    }))
    const now = new Date()
    lastUpdated.value = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}

function viewDetail(row: MonitorRow) {
  detailRow.value = row
  drawerVisible.value = true
}

onMounted(() => {
  loadAll()
  // 30s 轮询（PRD §7D.2）
  timer = setInterval(loadAll, POLL_INTERVAL)
})

onBeforeUnmount(() => {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
})
</script>

<style scoped>
.update-time {
  font-size: 13px;
  color: #999;
}
.pressure-warn {
  color: #EE5A24;
  font-weight: 600;
}
.detail-alert {
  margin-top: 16px;
}
</style>
