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
      <el-button type="primary" class="register-btn" @click="openRegisterDialog">注册设备</el-button>
    </div>

    <div class="page-card">
      <el-table :data="filteredList" size="small" v-loading="loading">
        <el-table-column prop="deviceId" label="设备ID" width="150" />
        <el-table-column prop="model" label="型号" width="120" />
        <el-table-column label="患者" width="110">
          <template #default="{ row }">{{ row.patientName || patientNameOf(row.patientId) }}</template>
        </el-table-column>
        <el-table-column label="绑定时间" width="100">
          <template #default="{ row }">{{ row.bindTime ? row.bindTime.slice(0, 10) : '-' }}</template>
        </el-table-column>
        <el-table-column prop="firmwareVersion" label="固件版本" width="90">
          <template #default="{ row }">{{ row.firmwareVersion || '-' }}</template>
        </el-table-column>
        <el-table-column label="连接的WiFi" min-width="120">
          <template #default="{ row }">
            <span v-if="row.wifiSsid">{{ row.wifiSsid }}</span>
            <span v-else style="color:#999">未连接</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后上报" width="150">
          <template #default="{ row }">{{ formatDateTime(row.lastReportAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="80" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 注册设备弹窗（设计稿 设备管理.html 顶栏按钮；字段对齐 device-service registerRequest，注册幂等） -->
    <el-dialog v-model="registerVisible" title="注册设备" width="440px">
      <el-form label-width="80px">
        <el-form-item label="设备ID" required>
          <el-input v-model="registerForm.deviceId" maxlength="48" placeholder="4-48 位，字母/数字/-/_" />
        </el-form-item>
        <el-form-item label="型号">
          <el-input v-model="registerForm.model" placeholder="PRS-ML05-RC" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="registerVisible = false">取消</el-button>
        <el-button type="primary" :loading="registering" @click="submitRegister">注册</el-button>
      </template>
    </el-dialog>

    <!-- 设备详情抽屉（GET /devices/:id + GET /devices/:id/bindings） -->
    <el-drawer v-model="detailVisible" title="设备详情" size="520px">
      <div v-loading="detailLoading">
        <el-descriptions v-if="detail" :column="1" border size="small">
          <el-descriptions-item label="设备ID">{{ detail.deviceId }}</el-descriptions-item>
          <el-descriptions-item label="型号">{{ detail.model }}</el-descriptions-item>
          <el-descriptions-item label="固件版本">{{ detail.firmwareVersion || '-' }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="statusTagType(detail.status)" size="small">{{ statusLabel(detail.status) }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="患者">{{ detail.patientId ? (detail.patientName || patientNameOf(detail.patientId)) : '未绑定' }}</el-descriptions-item>
          <el-descriptions-item label="绑定时间">{{ formatDateTime(detail.bindTime) }}</el-descriptions-item>
          <el-descriptions-item label="连接的WiFi">
            <span v-if="detail.wifiSsid">{{ detail.wifiSsid }}</span>
            <span v-else style="color:#999">未连接</span>
          </el-descriptions-item>
          <el-descriptions-item label="最后上报">{{ formatDateTime(detail.lastReportAt) }}</el-descriptions-item>
        </el-descriptions>
        <div class="binding-title">绑定历史</div>
        <el-table :data="bindings" size="small" border class="binding-table">
          <el-table-column label="绑定时间" width="100">
            <template #default="{ row }">{{ row.bindAt.slice(0, 10) }}</template>
          </el-table-column>
          <el-table-column prop="patientId" label="患者ID" min-width="110" />
          <el-table-column label="解绑时间" width="100">
            <template #default="{ row }">{{ row.unbindAt ? row.unbindAt.slice(0, 10) : '-' }}</template>
          </el-table-column>
          <el-table-column label="原因" width="90">
            <template #default="{ row }">{{ row.reason || '-' }}</template>
          </el-table-column>
          <el-table-column prop="operatorId" label="操作人" width="90" />
        </el-table>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { Device } from '@bracesync/shared-types'
import { fetchDevices, fetchDeviceDetail, fetchDeviceBindings, registerDeviceApi, patientNameOf, type DeviceBindingRecord } from '../../api'

const list = ref<Device[]>([])
const keyword = ref('')
const statusFilter = ref('')
const loading = ref(false)

// T268: 注册设备 + 详情抽屉
const registerVisible = ref(false)
const registering = ref(false)
const registerForm = ref({ deviceId: '', model: 'PRS-ML05-RC' })
const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref<Device | null>(null)
const bindings = ref<DeviceBindingRecord[]>([])

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

function openRegisterDialog() {
  registerForm.value = { deviceId: '', model: 'PRS-ML05-RC' }
  registerVisible.value = true
}

async function submitRegister() {
  const deviceId = registerForm.value.deviceId.trim()
  if (!/^[A-Za-z0-9_-]{4,48}$/.test(deviceId)) {
    ElMessage.warning('设备ID须为 4-48 位字母/数字/-/_')
    return
  }
  registering.value = true
  try {
    await registerDeviceApi({ deviceId, model: registerForm.value.model.trim() || undefined })
    ElMessage.success('设备已注册')
    registerVisible.value = false
    await loadData()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '注册失败')
  } finally {
    registering.value = false
  }
}

async function openDetail(row: Device) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = row
  bindings.value = []
  try {
    const [dev, hist] = await Promise.all([fetchDeviceDetail(row.deviceId), fetchDeviceBindings(row.deviceId)])
    detail.value = { ...dev, patientName: dev.patientName ?? row.patientName }
    bindings.value = hist
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载详情失败')
  } finally {
    detailLoading.value = false
  }
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
.register-btn {
  margin-left: auto;
}
.binding-title {
  font-size: 13px;
  font-weight: 600;
  margin: 16px 0 8px;
}
</style>
