<template>
  <div class="settings">
    <el-tabs v-model="activeTab">
      <!-- 阈值与系统参数 -->
      <el-tab-pane label="阈值与参数" name="thresholds">
        <div class="page-card" v-loading="loading">
          <div class="page-card-title">全局系统参数（PRD §7D.12，默认值对齐 @bracesync/constants）</div>
          <el-form label-width="220px" class="settings-form">
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
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { NotifyRule, NotificationRecord, NotifyChannel, NotifyTarget, AlertType } from '@bracesync/shared-types'
import {
  fetchSystemSettings, saveSystemSettingsApi, fetchNotifyRules,
  updateNotifyRuleApi, fetchNotificationLogs, patientNameOf,
} from '../../api'
import type { SystemSettings } from '../../mock/system'

const activeTab = ref('thresholds')
const loading = ref(false)
const loadingRules = ref(false)
const loadingLogs = ref(false)
const saving = ref(false)
const notifyRules = ref<NotifyRule[]>([])
const notificationLogs = ref<NotificationRecord[]>([])

const form = reactive<SystemSettings>({
  dailyWearTargetHours: 22,
  pressureHighThresholdN: 45,
  pressureFluctuationPct: 30,
  wearInterruptMinutes: 60,
  sensorDriftN: 2.8,
  wifiPresets: [],
})

function alertTypeLabel(type: string): string {
  const map: Record<string, string> = {
    pressure_high: '压力偏高',
    wear_interrupt: '佩戴中断',
    pressure_fluctuation: '压力波动',
    sensor_drift: '传感器漂移',
  }
  return map[type] || type
}

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
</style>
