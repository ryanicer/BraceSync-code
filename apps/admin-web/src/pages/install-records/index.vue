<template>
  <div class="install-records">
    <div class="page-toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索患者 / 设备ID / 技师"
        clearable
        class="search-input"
        @keyup.enter="handleSearch"
        @clear="handleSearch"
      />
      <el-button type="primary" @click="handleSearch">查询</el-button>
    </div>

    <div class="page-card install-list-card">
      <el-table :data="list" size="small" v-loading="loading">
        <el-table-column prop="installId" label="安装ID" width="110" />
        <el-table-column label="患者" width="110">
          <template #default="{ row }">{{ row.patientName || patientNameOf(row.patientId) }}</template>
        </el-table-column>
        <el-table-column prop="deviceId" label="设备ID" width="130" />
        <el-table-column label="技师" width="100">
          <template #default="{ row }">{{ row.techName || techNameOf(row.techId) }}</template>
        </el-table-column>
        <!-- 设计稿 安装记录.html:108 有「安装时间」列；列表 DTO（installListDTO）无 created_at，
             暂以 calibrate_time 承载，已登记契约偏差清单待后端补字段 -->
        <el-table-column label="安装时间" width="150">
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
              {{ row.wifiStatus === 'connected' ? '已连接' : '未连接' }}
            </el-tag>
          </template>
        </el-table-column>
        <!-- T289 9.3（设计稿 安装记录.html:108「校准」列，排在 WiFi 之后）：
             取代原「校准时间」列，取值由后端派生（installListDTO.calibStatus），前端不判阈值 -->
        <el-table-column label="校准状态" width="110">
          <template #default="{ row }">
            <el-tag :type="CALIB_TAG[row.calibStatus] ?? 'info'" size="small">
              {{ calibShortLabel(row.calibStatus) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="notes" label="备注" min-width="180" show-overflow-tooltip />
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openDetail(row)">详情</el-button>
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

    <!-- T289 9.1（设计稿 安装记录.html:168-197 详情抽屉）：20 点偏移值网格 + 安装备注。
         设计稿 :177「设备型号」行不做：installDetailDTO 无 model 字段，前端也不为此多拉一次设备列表 -->
    <el-drawer v-model="drawerVisible" title="安装详情" size="520px">
      <div v-if="detail" class="detail">
        <el-tag :type="CALIB_TAG[detail.calibStatus] ?? 'info'" size="small" class="detail-calib">
          {{ calibFullLabel(detail.calibStatus) }}
        </el-tag>
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="患者">
            {{ detail.patientName || patientNameOf(detail.patientId) }}
          </el-descriptions-item>
          <el-descriptions-item label="设备ID">{{ detail.deviceId }}</el-descriptions-item>
          <el-descriptions-item label="技师">{{ detail.techName || techNameOf(detail.techId) }}</el-descriptions-item>
          <el-descriptions-item label="安装时间">{{ formatTime(detail.createdAt) }}</el-descriptions-item>
          <el-descriptions-item label="WiFi">{{ detail.wifiStatus === 'connected' ? '已连接' : '未连接' }}</el-descriptions-item>
        </el-descriptions>

        <!-- 量纲按 T203 收口后的 N；设计稿 :185 的 kg/cm² 与越界点高亮（前端硬编 threshold=5）不照搬——
             PM 裁定阈值不硬编，后端只下发派生后的 calibStatus，未逐点给越界标记 -->
        <div class="section-title">传感器偏移值 (N)</div>
        <div v-if="detail.offsetValues.length" class="offset-grid">
          <div v-for="(v, i) in detail.offsetValues" :key="i" class="offset-cell">
            <div class="sensor-id">{{ sensorId(i) }}</div>
            <div class="sensor-val">{{ v.toFixed(2) }}</div>
          </div>
        </div>
        <p v-else class="offset-empty">尚未保存基线，无偏移值</p>

        <template v-if="detail.notes">
          <div class="section-title">安装备注</div>
          <div class="remark-box">{{ detail.notes }}</div>
        </template>
      </div>
      <el-skeleton v-else :rows="6" animated />
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { CalibStatus, InstallRecordRow, InstallRecordDetail } from '@bracesync/shared-types'
import { fetchInstallRecords, fetchInstallRecordDetail, patientNameOf, techNameOf } from '../../api'

const list = ref<InstallRecordRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const keyword = ref('')
const loading = ref(false)

const drawerVisible = ref(false)
const detail = ref<InstallRecordDetail | null>(null)

const CALIB_TAG: Record<string, 'success' | 'danger' | 'warning'> = {
  normal: 'success',
  abnormal: 'danger',
  uncalibrated: 'warning',
}

/** 设计稿 安装记录.html:140 列表用短词、:175 详情用全称 */
function calibShortLabel(status: CalibStatus): string {
  if (status === 'normal') return '正常'
  if (status === 'abnormal') return '异常'
  return '未校准'
}

function calibFullLabel(status: CalibStatus): string {
  if (status === 'normal') return '校准正常'
  if (status === 'abnormal') return '校准异常'
  return '未校准'
}

/** 设计稿 安装记录.html:189 偏移网格按 S01…S20 编号（其余页面同一 20 点用 P01…P20，已报 PM） */
function sensorId(index: number): string {
  return `S${String(index + 1).padStart(2, '0')}`
}

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

async function openDetail(row: InstallRecordRow) {
  detail.value = null
  drawerVisible.value = true
  try {
    const d = await fetchInstallRecordDetail(row.installId)
    // 真实模式 installDetailDTO 不回 patientName/techName（只有列表 DTO join 了姓名），
    // 而从列表页直接点详情时本地姓名字典也可能还没灌过 ⇒ 用被点行的姓名兜底，否则抽屉里显示 ID
    detail.value = {
      ...d,
      patientName: d.patientName || row.patientName,
      techName: d.techName || row.techName,
    }
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '详情加载失败')
    drawerVisible.value = false
  }
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
.detail-calib {
  margin-bottom: 12px;
}
.section-title {
  margin: 20px 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: #333;
}
/* 设计稿 安装记录.html:62 偏移网格为 5 列 × 4 行 */
.offset-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 8px;
}
.offset-cell {
  padding: 10px 8px;
  border-radius: 8px;
  background: #f8f9fa;
  text-align: center;
}
.sensor-id {
  font-size: 11px;
  color: #999;
}
.sensor-val {
  margin-top: 2px;
  font-size: 15px;
  font-weight: 600;
  color: #333;
}
.offset-empty {
  margin: 0;
  font-size: 13px;
  color: #909399;
}
.remark-box {
  padding: 12px;
  border-radius: 8px;
  background: #f8f9fa;
  font-size: 13px;
  color: #666;
  line-height: 1.6;
}
</style>
