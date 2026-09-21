<template>
  <div class="settings">
    <el-tabs v-model="activeTab">
      <!-- 阈值与系统参数 -->
      <el-tab-pane label="阈值与参数" name="thresholds">
        <div class="page-card" v-loading="loading">
          <div class="page-card-title">全局系统参数（PRD §7D.12，默认值对齐 @bracesync/constants）</div>
          <el-form label-width="220px" class="settings-form">
            <el-form-item label="数据采集间隔（秒）">
              <el-input-number v-model="form.collectIntervalSeconds" :min="60" :max="3600" :step="60" :step-strictly="true" />
              <span class="form-hint">须为 60 的整数倍</span>
            </el-form-item>
            <el-form-item label="数据保留天数">
              <el-input-number v-model="form.retentionDays" :min="1" :max="3650" />
            </el-form-item>
            <el-form-item label="最大患者数">
              <el-input-number v-model="form.maxPatients" :min="1" :max="1000000" />
            </el-form-item>
            <el-form-item label="每日佩戴目标时长（h）">
              <el-input-number v-model="form.dailyWearTargetHours" :min="1" :max="24" />
            </el-form-item>
            <el-form-item label="压力偏高阈值（N）">
              <el-input-number v-model="form.pressureHighThresholdN" :min="1" :max="200" />
            </el-form-item>
            <el-form-item label="压力波动幅度阈值（%）">
              <el-input-number v-model="form.pressureFluctuationPct" :min="1" :max="100" />
            </el-form-item>
            <el-form-item label="佩戴中断判定时间（分钟）">
              <el-input-number v-model="form.wearInterruptMinutes" :min="10" :max="720" />
              <span class="form-hint">必须 ≥ 2×采集间隔</span>
            </el-form-item>
            <el-form-item label="传感器漂移告警阈值（N）">
              <el-input-number v-model="form.sensorDriftN" :min="0.1" :max="20" :step="0.1" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="saving" @click="saveSettings">保存配置</el-button>
            </el-form-item>
          </el-form>
        </div>

        <div class="page-card">
          <div class="page-card-title">WiFi 预设列表（技师端配网辅助）</div>
          <el-table :data="form.wifiPresets" size="small">
            <el-table-column prop="ssid" label="网络名称" min-width="200" />
            <el-table-column prop="password" label="密码（脱敏）" min-width="160" />
          </el-table>
          <p class="form-hint">WiFi 预设的新增/编辑待后端 sys_configs 写入接口就绪后开放。</p>
        </div>
      </el-tab-pane>

      <!-- 通知规则 -->
      <el-tab-pane label="通知规则" name="notify-rules">
        <div class="page-card">
          <div class="page-card-title">告警通知规则（msg-service，契约 getNotifyRules）</div>
          <el-table :data="notifyRules" size="small" v-loading="loadingRules">
            <el-table-column label="告警类型" width="130">
              <template #default="{ row }">{{ alertTypeLabel(row.type) }}</template>
            </el-table-column>
            <el-table-column label="通知渠道" min-width="180">
              <template #default="{ row }">
                <el-checkbox-group :model-value="row.channels" @change="(val: string[]) => updateChannels(row, val)">
                  <el-checkbox value="wechat">微信</el-checkbox>
                  <el-checkbox value="sms">短信</el-checkbox>
                </el-checkbox-group>
              </template>
            </el-table-column>
            <el-table-column label="通知对象" min-width="260">
              <template #default="{ row }">
                <el-checkbox-group :model-value="row.notifyTargets" @change="(val: string[]) => updateTargets(row, val)">
                  <el-checkbox value="patient">患者</el-checkbox>
                  <el-checkbox value="doctor">医生</el-checkbox>
                  <el-checkbox value="tech">技师</el-checkbox>
                  <el-checkbox value="ops">运营</el-checkbox>
                </el-checkbox-group>
              </template>
            </el-table-column>
            <el-table-column label="最近更新" width="160">
              <template #default="{ row }">{{ row.updatedAt ? row.updatedAt.slice(0, 10) : '-' }}</template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <!-- 发送记录 -->
      <el-tab-pane label="发送记录" name="notification-logs">
        <div class="page-card">
          <el-table :data="notificationLogs" size="small" v-loading="loadingLogs">
            <el-table-column prop="recordId" label="记录ID" width="100" />
            <el-table-column label="患者" width="100">
              <template #default="{ row }">{{ patientNameOf(row.patientId) }}</template>
            </el-table-column>
            <el-table-column label="告警类型" width="110">
              <template #default="{ row }">{{ row.alertType ? alertTypeLabel(row.alertType) : '非告警' }}</template>
            </el-table-column>
            <el-table-column label="渠道" width="80">
              <template #default="{ row }">{{ row.channel === 'wechat' ? '微信' : '短信' }}</template>
            </el-table-column>
            <el-table-column prop="content" label="内容" min-width="240" show-overflow-tooltip />
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="logStatusType(row.status)" size="small">{{ logStatusLabel(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="发送时间" width="150">
              <template #default="{ row }">{{ row.sentAt ? formatTime(row.sentAt) : '-' }}</template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <!-- 操作日志（T253-12.3，设计稿 系统配置.html:168-193 Tab3） -->
      <el-tab-pane label="操作日志" name="audit-logs">
        <div class="page-card">
          <div class="audit-toolbar">
            <el-date-picker
              v-model="auditDate"
              type="date"
              placeholder="全部日期"
              value-format="YYYY-MM-DD"
              clearable
              class="audit-date"
              @change="handleAuditSearch"
            />
            <el-select v-model="auditAction" placeholder="全部操作" clearable class="audit-action" @change="handleAuditSearch">
              <el-option label="登录" value="login" />
              <el-option label="数据修改" value="data_modify" />
              <el-option label="配置变更" value="config_change" />
              <el-option label="权限变更" value="permission_change" />
            </el-select>
            <el-input
              v-model="auditOperator"
              placeholder="搜索操作人员..."
              clearable
              class="audit-operator"
              @keyup.enter="handleAuditSearch"
              @clear="handleAuditSearch"
            />
            <el-button type="primary" @click="handleAuditSearch">查询</el-button>
          </div>
          <el-table :data="auditLogs" size="small" v-loading="loadingAudit">
            <el-table-column label="时间" width="170">
              <template #default="{ row }">{{ formatAuditTime(row.ts) }}</template>
            </el-table-column>
            <el-table-column label="操作人员" width="140">
              <template #default="{ row }">{{ row.operatorName || row.operatorId || '-' }}</template>
            </el-table-column>
            <el-table-column label="IP地址" width="140">
              <template #default="{ row }">{{ row.ip || '-' }}</template>
            </el-table-column>
            <el-table-column label="操作类型" width="110">
              <template #default="{ row }">
                <el-tag :type="auditActionType(row.action)" size="small">{{ row.actionLabel || row.action }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="description" label="操作描述" min-width="280" show-overflow-tooltip />
          </el-table>
          <el-pagination
            class="pagination"
            v-model:current-page="auditPage"
            :total="auditTotal"
            :page-size="auditPageSize"
            layout="total, prev, pager, next"
            @current-change="loadAuditLogs"
          />
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { NotifyRule, NotificationRecord, NotifyChannel, NotifyTarget, AlertType } from '@bracesync/shared-types'
import { alertTypeLabel } from '@bracesync/shared-utils'
import {
  fetchSystemSettings, saveSystemSettingsApi, fetchNotifyRules,
  updateNotifyRuleApi, fetchNotificationLogs, patientNameOf, fetchAuditLogsApi,
} from '../../api'
import type { SystemSettings, AuditLog } from '../../mock/system'

const activeTab = ref('thresholds')
const loading = ref(false)
const loadingRules = ref(false)
const loadingLogs = ref(false)
const saving = ref(false)
const notifyRules = ref<NotifyRule[]>([])
const notificationLogs = ref<NotificationRecord[]>([])

const form = reactive<SystemSettings>({
  collectIntervalSeconds: 60,
  retentionDays: 365,
  maxPatients: 10000,
  dailyWearTargetHours: 22,
  pressureHighThresholdN: 45,
  pressureFluctuationPct: 30,
  wearInterruptMinutes: 60,
  sensorDriftN: 2.8,
  wifiPresets: [],
})

function logStatusLabel(status: NotificationRecord['status']): string {
  const map: Record<NotificationRecord['status'], string> = { pending: '待发送', sent: '已发送', failed: '失败', degraded: '降级短信' }
  return map[status] ?? status
}

function logStatusType(status: NotificationRecord['status']): 'info' | 'success' | 'danger' | 'warning' {
  if (status === 'sent') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'degraded') return 'warning'
  return 'info'
}

function formatTime(iso: string): string {
  return `${iso.slice(5, 10)} ${iso.slice(11, 16)}`
}

// ===== 操作日志（T253-12.3） =====
const auditLogs = ref<AuditLog[]>([])
const auditTotal = ref(0)
const auditPage = ref(1)
const auditPageSize = ref(20)
const auditDate = ref('')
const auditAction = ref('')
const auditOperator = ref('')
const loadingAudit = ref(false)

function formatAuditTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function auditActionType(action: string): 'primary' | 'warning' | 'success' | 'danger' | 'info' {
  const map: Record<string, 'primary' | 'warning' | 'success' | 'danger' | 'info'> = {
    data_modify: 'primary',
    config_change: 'warning',
    login: 'success',
    permission_change: 'danger',
    data_read: 'info',
  }
  return map[action] ?? 'info'
}

async function loadAuditLogs() {
  loadingAudit.value = true
  try {
    const res = await fetchAuditLogsApi({
      date: auditDate.value || undefined,
      action: auditAction.value || undefined,
      operator: auditOperator.value || undefined,
      page: auditPage.value,
      pageSize: auditPageSize.value,
    })
    auditLogs.value = res.list
    auditTotal.value = res.total
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载操作日志失败')
  } finally {
    loadingAudit.value = false
  }
}

function handleAuditSearch() {
  auditPage.value = 1
  loadAuditLogs()
}

async function saveSettings() {
  saving.value = true
  try {
    await saveSystemSettingsApi({ ...form })
    ElMessage.success('配置已保存')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

async function updateChannels(row: NotifyRule, channels: string[]) {
  try {
    await updateNotifyRuleApi(row.type as AlertType, { channels: channels as NotifyChannel[] })
    row.channels = channels as NotifyChannel[]
    ElMessage.success('通知渠道已更新')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '更新失败')
  }
}

async function updateTargets(row: NotifyRule, targets: string[]) {
  try {
    await updateNotifyRuleApi(row.type as AlertType, { notifyTargets: targets as NotifyTarget[] })
    row.notifyTargets = targets as NotifyTarget[]
    ElMessage.success('通知对象已更新')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '更新失败')
  }
}

onMounted(async () => {
  loading.value = true
  try {
    const settings = await fetchSystemSettings()
    Object.assign(form, settings)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '加载配置失败')
  } finally {
    loading.value = false
  }

  loadingRules.value = true
  try {
    notifyRules.value = await fetchNotifyRules()
  } catch {
    // 通知规则加载失败不阻塞其他 tab
  } finally {
    loadingRules.value = false
  }

  loadingLogs.value = true
  try {
    const res = await fetchNotificationLogs({ page: 1, pageSize: 20 })
    notificationLogs.value = res.list
  } catch {
    // 发送记录加载失败不阻塞其他 tab
  } finally {
    loadingLogs.value = false
  }

  loadAuditLogs()
})
</script>

<style scoped>
.settings-form {
  max-width: 560px;
}
.form-hint {
  font-size: 12px;
  color: #999;
  margin-left: 12px;
}
.audit-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.audit-date {
  width: 150px;
}
.audit-action {
  width: 140px;
}
.audit-operator {
  width: 200px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
