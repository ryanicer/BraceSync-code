<template>
  <div class="alerts">
    <el-tabs v-model="activeTab" class="alerts-tabs">
      <el-tab-pane label="告警列表" name="list">
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
              <template #default="{ row }">{{ row.patientName || patientNameOf(row.patientId) }}</template>
            </el-table-column>
            <el-table-column prop="deviceId" label="设备" width="130" />
            <el-table-column label="传感器" width="80">
              <template #default="{ row }">{{ row.sensorPoint || '-' }}</template>
            </el-table-column>
            <el-table-column label="阈值/实际" width="110">
              <template #default="{ row }">
                {{ row.thresholdValue != null ? `${formatAlertValue(row.type, row.thresholdValue)}/${formatAlertValue(row.type, row.actualValue)}` : '-' }}
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
      </el-tab-pane>

      <el-tab-pane label="告警规则配置" name="rules">
        <div class="page-card" v-loading="rulesLoading">
          <div class="card-title">按采集点设置告警阈值</div>
          <div class="rule-tip">💡 在 4×5 网格中点击格子勾选需监控的采集点，下方设置统一的压力阈值上下限。触发告警后将通知对应团队医生。</div>
          <div class="grid-area">
            <div class="alert-grid">
              <button
                v-for="p in rules.points"
                :key="p.pointId"
                type="button"
                class="grid-cell"
                :class="{ monitored: p.monitored }"
                :title="`${p.pointId} (${p.label}) — ${p.monitored ? '已勾选' : '未勾选'}，上限 ${p.effectiveUpperN}N / 下限 ${p.effectiveLowerN}N`"
                @click="handleCellClick(p)"
                @dblclick="openPointEditor(p)"
              >
                <span class="cell-id">{{ p.pointId }}</span>
                <span class="cell-label">{{ p.label }}</span>
              </button>
            </div>
            <div class="grid-legend">
              <span class="legend-item"><i class="legend-dot dot-on" />已勾选</span>
              <span class="legend-item"><i class="legend-dot dot-off" />未勾选</span>
            </div>
            <div class="grid-actions">
              <el-button size="small" @click="selectAllPoints">全选 (20点)</el-button>
              <el-button size="small" @click="deselectAllPoints">全不选</el-button>
              <el-button size="small" @click="selectByRow">按行选择</el-button>
            </div>
          </div>
          <div class="config-row config-header">
            <span>统一压力上限(N)</span>
            <span>统一压力下限(N)</span>
            <span>已选点位</span>
          </div>
          <div class="config-row">
            <span class="config-label">统一阈值</span>
            <el-input-number v-model="unifiedUpper" :min="1" :max="200" :precision="1" controls-position="right" />
            <el-input-number v-model="unifiedLower" :min="0" :max="200" :precision="1" controls-position="right" />
            <span class="selected-count">已选: {{ selectedCount }} / 20</span>
          </div>
          <div class="config-row per-point-row">
            <span class="config-label muted">各点位独立阈值 (双击网格格子可编辑)</span>
            <div class="point-chips">
              <span
                v-for="p in monitoredPoints"
                :key="p.pointId"
                class="point-chip"
                :title="`${p.pointId} 独立阈值（双击修改）`"
                @dblclick="openPointEditor(p)"
              >{{ p.pointId }} {{ p.effectiveUpperN }}/{{ p.effectiveLowerN }}N</span>
            </div>
          </div>
          <div class="rule-actions">
            <el-button type="primary" :loading="savingRules" @click="saveRules">保存规则</el-button>
            <el-button :disabled="savingRules" @click="resetRules">恢复默认</el-button>
          </div>
        </div>

        <div class="page-card">
          <div class="card-title">全局告警规则</div>
          <div class="config-row">
            <span class="config-label">设备离线阈值</span>
            <el-input-number v-model="globalRules.deviceOfflineMinutes" :min="1" :max="1440" :precision="0" controls-position="right" />
            <span class="unit">分钟</span>
          </div>
          <div class="config-row">
            <span class="config-label">佩戴时长下限</span>
            <el-input-number v-model="globalRules.dailyWearMinHours" :min="1" :max="24" :precision="1" controls-position="right" />
            <span class="unit">小时/天</span>
          </div>
          <div class="config-row">
            <span class="config-label">连续佩戴上限</span>
            <el-input-number v-model="globalRules.continuousWearMaxHours" :min="1" :max="24" :precision="1" controls-position="right" />
            <span class="unit">小时</span>
          </div>
          <div class="config-row">
            <span class="config-label">数据上报超时</span>
            <el-input-number v-model="globalRules.reportTimeoutMinutes" :min="1" :max="1440" :precision="0" controls-position="right" />
            <span class="unit">分钟</span>
          </div>
          <div class="rule-actions">
            <el-button type="primary" :loading="savingGlobal" @click="saveGlobalRules">保存全局规则</el-button>
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

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

    <!-- 独立阈值编辑（设计稿：双击网格格子可编辑；留空 = 跟随统一阈值） -->
    <el-dialog v-model="pointEditorVisible" :title="`设置 ${editing?.pointId ?? ''} 独立阈值`" width="380px">
      <p class="process-desc">留空 = 跟随统一阈值（当前统一上限 {{ unifiedUpper }}N / 下限 {{ unifiedLower }}N）</p>
      <el-form label-width="120px">
        <el-form-item label="独立压力上限(N)">
          <el-input-number v-model="editingUpper" :min="1" :max="200" :precision="1" controls-position="right" />
        </el-form-item>
        <el-form-item label="独立压力下限(N)">
          <el-input-number v-model="editingLower" :min="0" :max="200" :precision="1" controls-position="right" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pointEditorVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmPointEdit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Alert } from '@bracesync/shared-types'
import { formatAlertValue } from '@bracesync/shared-utils'
import {
  fetchAlerts, processAlertApi, patientNameOf,
  fetchAlertRules, saveAlertPointRulesApi, resetAlertPointRulesApi, saveAlertGlobalRulesApi,
} from '../../api'
import type { AlertPointRule, AlertGlobalRules } from '../../mock/alerts'

const activeTab = ref('list')

// ===== Tab1 告警列表 =====
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

function formatTime(iso: string | null | undefined): string {
  if (typeof iso !== 'string' || iso.length < 16) return '-'
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

// ===== Tab2 告警规则配置 =====
const rulesLoading = ref(false)
const savingRules = ref(false)
const savingGlobal = ref(false)
const rules = ref<{ unifiedUpperN: number; unifiedLowerN: number; points: AlertPointRule[]; globalRules: AlertGlobalRules }>({
  unifiedUpperN: 45,
  unifiedLowerN: 10,
  points: [],
  globalRules: { deviceOfflineMinutes: 30, dailyWearMinHours: 18, continuousWearMaxHours: 23, reportTimeoutMinutes: 5 },
})
const unifiedUpper = ref(45)
const unifiedLower = ref(10)
const globalRules = ref<AlertGlobalRules>({ ...rules.value.globalRules })
const pointEditorVisible = ref(false)
const editing = ref<AlertPointRule | null>(null)
const editingUpper = ref<number | null>(null)
const editingLower = ref<number | null>(null)
// 单击切换勾选 / 双击编辑独立阈值：单击延迟分派，双击取消定时器
let cellClickTimer: number | null = null

const selectedCount = computed(() => rules.value.points.filter((p) => p.monitored).length)
const monitoredPoints = computed(() => rules.value.points.filter((p) => p.monitored))

async function loadRules() {
  rulesLoading.value = true
  try {
    const res = await fetchAlertRules()
    applyRules(res)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载告警规则失败')
  } finally {
    rulesLoading.value = false
  }
}

function applyRules(res: { unifiedUpperN: number; unifiedLowerN: number; points: AlertPointRule[]; globalRules: AlertGlobalRules }) {
  rules.value = res
  unifiedUpper.value = res.unifiedUpperN
  unifiedLower.value = res.unifiedLowerN
  globalRules.value = { ...res.globalRules }
}

function handleCellClick(p: AlertPointRule) {
  if (cellClickTimer !== null) return
  cellClickTimer = window.setTimeout(() => {
    cellClickTimer = null
    p.monitored = !p.monitored
  }, 250)
}

function openPointEditor(p: AlertPointRule) {
  if (cellClickTimer !== null) {
    window.clearTimeout(cellClickTimer)
    cellClickTimer = null
  }
  editing.value = p
  editingUpper.value = p.upperN
  editingLower.value = p.lowerN
  pointEditorVisible.value = true
}

function confirmPointEdit() {
  const p = editing.value
  if (!p) return
  const effUpper = editingUpper.value ?? unifiedUpper.value
  const effLower = editingLower.value ?? unifiedLower.value
  if (effUpper <= effLower) {
    ElMessage.error('压力上限必须大于下限')
    return
  }
  p.upperN = editingUpper.value
  p.lowerN = editingLower.value
  p.effectiveUpperN = effUpper
  p.effectiveLowerN = effLower
  pointEditorVisible.value = false
}

function selectAllPoints() {
  rules.value.points.forEach((p) => { p.monitored = true })
}

function deselectAllPoints() {
  rules.value.points.forEach((p) => { p.monitored = false })
}

async function selectByRow() {
  try {
    const { value } = await ElMessageBox.prompt('按行号勾选 (1~4)，仅勾选该行', '按行选择', {
      inputValue: '1',
      inputPattern: /^[1-4]$/,
      inputErrorMessage: '请输入 1~4 的行号',
      confirmButtonText: '确定',
      cancelButtonText: '取消',
    })
    const row = Number(value)
    rules.value.points.forEach((p) => { p.monitored = p.row === row })
  } catch {
    // 取消
  }
}

async function saveRules() {
  if (unifiedUpper.value <= unifiedLower.value) {
    ElMessage.error('统一压力上限必须大于下限')
    return
  }
  const bad = rules.value.points.find((p) => p.monitored && (p.upperN ?? unifiedUpper.value) <= (p.lowerN ?? unifiedLower.value))
  if (bad) {
    ElMessage.error(`点位 ${bad.pointId} 生效上限必须大于下限`)
    return
  }
  savingRules.value = true
  try {
    const res = await saveAlertPointRulesApi({
      unifiedUpperN: unifiedUpper.value,
      unifiedLowerN: unifiedLower.value,
      points: rules.value.points.map((p) => ({
        pointId: p.pointId,
        monitored: p.monitored,
        upperN: p.upperN,
        lowerN: p.lowerN,
      })),
    })
    applyRules(res)
    ElMessage.success('保存成功')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    savingRules.value = false
  }
}

async function resetRules() {
  savingRules.value = true
  try {
    const res = await resetAlertPointRulesApi()
    applyRules(res)
    ElMessage.success('已恢复默认')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '恢复默认失败')
  } finally {
    savingRules.value = false
  }
}

async function saveGlobalRules() {
  savingGlobal.value = true
  try {
    const res = await saveAlertGlobalRulesApi({ ...globalRules.value })
    applyRules(res)
    ElMessage.success('全局规则保存成功')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    savingGlobal.value = false
  }
}

onMounted(() => {
  loadData()
  loadRules()
})
</script>

<style scoped>
.alerts-tabs {
  margin-bottom: 8px;
}
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
.card-title {
  font-size: 16px;
  font-weight: 600;
  color: #333;
  margin-bottom: 16px;
}
.rule-tip {
  background: #f0f7ff;
  padding: 12px;
  border-radius: 6px;
  margin-bottom: 12px;
  font-size: 12px;
  color: #666;
}
.grid-area {
  margin-bottom: 16px;
  text-align: center;
}
.alert-grid {
  display: inline-grid;
  grid-template-columns: repeat(5, 64px);
  gap: 4px;
}
.grid-cell {
  width: 64px;
  height: 54px;
  border: none;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
  background: #e2e8f0;
  color: #94a3b8;
  padding: 0;
}
.grid-cell.monitored {
  background: #2563eb;
  color: #fff;
}
.grid-cell:hover {
  transform: scale(1.06);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
  z-index: 1;
}
.cell-id {
  font-size: 10px;
  font-weight: 600;
  line-height: 1.2;
}
.cell-label {
  font-size: 12px;
  font-weight: 700;
  line-height: 1.2;
}
.grid-legend {
  display: flex;
  justify-content: center;
  gap: 16px;
  margin-top: 8px;
  font-size: 11px;
  color: #999;
}
.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.legend-dot {
  width: 14px;
  height: 14px;
  border-radius: 3px;
}
.dot-on {
  background: #2563eb;
}
.dot-off {
  background: #e2e8f0;
}
.grid-actions {
  margin-top: 8px;
  display: flex;
  gap: 8px;
  justify-content: center;
}
.config-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
  font-size: 13px;
  color: #333;
}
.config-row:last-of-type {
  border-bottom: none;
}
.config-header {
  font-weight: 600;
}
.config-header span:first-child {
  min-width: 160px;
}
.config-header span:nth-child(2) {
  min-width: 160px;
}
.config-label {
  min-width: 100px;
  font-weight: 500;
}
.config-label.muted {
  min-width: 220px;
  font-size: 12px;
  color: #999;
  font-weight: 400;
}
.selected-count {
  font-size: 12px;
  color: #2563eb;
}
.per-point-row {
  border-top: 1px solid #e2e8f0;
  margin-top: 4px;
}
.point-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.point-chip {
  display: inline-flex;
  align-items: center;
  background: #f0f7ff;
  padding: 2px 8px;
  border-radius: 12px;
  font-size: 11px;
  cursor: pointer;
  color: #2563eb;
}
.rule-actions {
  margin-top: 16px;
  display: flex;
  gap: 8px;
}
.unit {
  font-size: 12px;
  color: #666;
}
</style>
