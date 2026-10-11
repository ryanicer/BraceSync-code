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
            <!-- T653（T642 R2/R3 甲）：本页只保留 3 个平台参数。以下编辑位全部摘 UI 不删数据：
                 - 每日佩戴目标时长 → 唯一编辑位归「告警管理 · Tab2 全局规则 · 佩戴时长下限」（同键 wear_target_hours，示值 22）；
                 - 设备离线判定时间 / 传感器标定异常阈值 → 归「告警管理 · Tab2 全局规则」（同键 threshold_wear_interrupt_minutes /
                   threshold_sensor_drift）。form 字段全部保留并随 PUT 原值回传——后端 PUT 对零值恒写且 validateSettings
                   仍校验这些隐藏键（dailyWearTargetHours[1,24]/wearInterruptMinutes[10,720] 且 ≥2×间隔/
                   sensorDriftN[0.1,20]/pressureFluctuationPct[1,100]），缺键 = 解码 0 = 整页保存必 400。 -->
            <!-- T419 S-6：「压力波动幅度阈值」表单项按已停用口径下线（Boss 2026-09-23 裁定问题 4 砍类型，
                 PRD §7D.12 配置项明细 V3.21 已删该需求项）。只摘 UI：form 仍回显并随 PUT 原值回传，
                 键 threshold_pressure_fluctuation_pct 与 DB CHECK 未动（T430：PRD §7D.6 历史数据处置已拍 C
                 ＝历史行界面隐藏、数据不删，但本设置页两张表属配置面，PM 09-27 19:00 裁定不入本卡范围）。
                 🔴 不能连 form 的键一起删：后端 validateSettings 对该字段做 [1,100] 区间校验
                 （services/user-service/internal/handler/handler.go:2040），载荷缺键 ⇒ 解码成 0 ⇒ 整页保存必 400。
                 这条不对称由 apps/admin-web/test/settings-visible-terms.spec.ts 钉住。 -->
            <!-- T653：原「设备离线判定时间 / 传感器标定异常阈值」两编辑位摘 UI，归告警页 Tab2（同键）。 -->
            <el-form-item>
              <el-button type="primary" :loading="saving" @click="saveSettings">保存配置</el-button>
            </el-form-item>
          </el-form>
        </div>

        <!-- T653（T642 R2 甲）：原「压力阈值配置」整卡摘 UI——唯一编辑位归「告警管理 · Tab2」统一压力上下限
             （同键 threshold_pressure_high / threshold_pressure_low，患者端热力图继续消费，键与消费方均不动）。
             form.pressureHighThresholdN / pressureLowThresholdN 仍随 GET 回填、随 PUT 原值回传；
             normalUpperN 三档推导随卡一起下线（后端本就不落库）。 -->

        <!-- T289 12.4（设计稿 系统配置.html:103-137，表头 :107、20 行 :109-128、说明 :131-135）：医生默认阈值 20 点表（只读，单点在告警页网格改）。
             T633（Boss 2026-10-09 报单）：本卡与下面那块「WiFi 预设列表」一起隐藏（只关模板挂载）。
             本卡是只读回显且全点同值，真实编辑入口在「告警管理 · 告警规则配置」；它回显的 5/1 又未按 T203
             口径与生效配置同源，对用户是困惑。form 的键、pointDefaults 与后端 sys_configs 一律不动：
             本表回显的就是压力阈值卡那同一组键（threshold_pressure_high / low），本就没建第二份数据。 -->
        <div v-if="SHOW_LOW_VALUE_BLOCKS" class="page-card default-threshold-card">
          <div class="page-card-title">医生默认阈值</div>
          <p class="card-desc">
            全 20 个采集点（4×5 网格）逐点列出，未逐点改过时的回退默认值；单点独立阈值在
            <router-link to="/alerts" class="card-link">告警管理 · 告警规则配置</router-link>
            的网格内编辑。
          </p>
          <el-table :data="pointDefaults" size="small" class="point-table">
            <el-table-column prop="point" label="采集点" width="100" />
            <el-table-column prop="grid" label="网格位置" width="110" />
            <el-table-column label="默认上限(N)" width="130">
              <template #default="{ row }">{{ row.upper }}</template>
            </el-table-column>
            <el-table-column label="默认下限(N)" width="130">
              <template #default="{ row }">{{ row.lower }}</template>
            </el-table-column>
          </el-table>
          <ul class="threshold-notes">
            <li><b>数值来源</b>：全点统一默认，等同 sys_configs 的
              threshold_pressure_high={{ form.pressureHighThresholdN }} /
              threshold_pressure_low={{ pressureLowN }}（后端现值回显），
              与告警页 Tab2「统一压力上下限」是同一组键、不建第二份。</li>
            <li><b>点位命名口径</b>：采集点编号 P01–P20（零填充两位，同 alert_point_rules.point_id），
              网格位置 R行C列，换算 编号 =（行−1）×5 + 列。</li>
            <li><b>不列解剖名</b>：20 点的解剖名无来源文档，口径待裁，本表只按网格位置标识。</li>
            <li><b>量纲</b>：T203 已把压力类阈值统一 ÷10，本表按 N 呈现（设计稿样例的 45/10 为换算前旧值）。</li>
          </ul>
        </div>

        <!-- T633：微信小程序 WiFi 能力（startWifi / getWifiList / connectWifi 全系列）2024-12 起不可用，
             技师端扫不到也连不了这份列表，此块在当前技术条件下落不到端上 ⇒ 同样只关挂载：
             wifi_presets 键、form.wifiPresets 回显与 PUT 载荷都不动，接口就绪或配网方案定了再置回开关。 -->
        <div v-if="SHOW_LOW_VALUE_BLOCKS" class="page-card">
          <div class="page-card-title">WiFi 预设列表（技师端配网辅助）</div>
          <el-table :data="form.wifiPresets" size="small">
            <el-table-column prop="ssid" label="网络名称" min-width="200" />
            <el-table-column prop="password" label="密码（脱敏）" min-width="160" />
          </el-table>
          <p class="form-hint">WiFi 预设的新增/编辑待后端 sys_configs 写入接口就绪后开放。</p>
        </div>
      </el-tab-pane>

      <!-- T653（T642 R4 甲）：原「通知规则」「发送记录」两 Tab 摘 UI 退场（规则与记录的查看/维护改由
           msg-service 既有管理面承载，不在本系统配置页）；fetchNotifyRules/fetchNotificationLogs 的
           页面加载与相关状态一并摘除，不再对后端发这两枪。操作日志 Tab 保留。 -->

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
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { DEFAULT_THRESHOLDS } from '@bracesync/constants'
import { userErrorCopy } from '@bracesync/shared-utils'
import {
  fetchSystemSettings, saveSystemSettingsApi, fetchAuditLogsApi,
} from '../../api'
import type { SystemSettings, AuditLog } from '../../mock/system'
// T597：时间列一律走东八区公共出口（瘦身后操作日志时间列是本页唯一时间渲染点，仍守此口径）
import { formatCstDateTime as formatAuditTime } from '../../utils/formatTime'

const activeTab = ref('thresholds')
const loading = ref(false)
const saving = ref(false)

const form = reactive<SystemSettings>({
  collectIntervalSeconds: 60,
  retentionDays: 365,
  maxPatients: 10000,
  dailyWearTargetHours: 22,
  pressureHighThresholdN: DEFAULT_THRESHOLDS.PRESSURE_HIGH_N,
  // T289 12.4：GET 前须有值，否则低压框渲染成空（契约 :753 恒回数值、默认 1N；
  // 1 ≡ 迁移 000021 落库的 threshold_pressure_low 与后端 defaultUnifiedLowerN）
  pressureLowThresholdN: 1,
  pressureFluctuationPct: 30,
  wearInterruptMinutes: 60,
  sensorDriftN: 2.8,
  wifiPresets: [],
})

/**
 * T633：「WiFi 预设列表」与「医生默认阈值」两张卡的挂载开关（Boss 2026-10-09 裁定隐藏）。
 * 只关界面，不动数据与接口；将来配网方案确定后置回 true 即恢复，判据见模板同处注释。
 */
const SHOW_LOW_VALUE_BLOCKS = false

/** threshold_pressure_low 现值；后端 GET 恒回数值，null 只代表尚未加载。
 *  T653：压力阈值卡已摘 UI，本计算量只供 T633 隐藏的「医生默认阈值」卡文案引用（数据/键不动）。 */
const pressureLowN = computed(() => form.pressureLowThresholdN ?? null)

/** 医生默认阈值表：20 点全点统一默认（编号 =（行−1）×5 + 列，设计稿 系统配置.html:109-128） */
const pointDefaults = computed(() =>
  Array.from({ length: 20 }, (_, i) => ({
    point: `P${String(i + 1).padStart(2, '0')}`,
    grid: `R${Math.floor(i / 5) + 1}C${(i % 5) + 1}`,
    upper: form.pressureHighThresholdN,
    lower: pressureLowN.value ?? '—',
  })),
)

// ===== 操作日志（T253-12.3） =====
const auditLogs = ref<AuditLog[]>([])
const auditTotal = ref(0)
const auditPage = ref(1)
const auditPageSize = ref(20)
const auditDate = ref('')
const auditAction = ref('')
const auditOperator = ref('')
const loadingAudit = ref(false)

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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载操作日志失败' }))
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '保存失败' }))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  // T653：本页只剩平台参数 + 操作日志；通知规则/发送记录两 Tab 已摘，不再加载其数据。
  // 🔴 隐藏键（佩戴目标/设备离线/传感器/压力上下限/波动幅度）仍靠这次 GET 回填并随 PUT 原值回传，
  //    防后端零值恒写 + validateSettings 区间校验导致保存 400。
  loading.value = true
  try {
    const settings = await fetchSystemSettings()
    Object.assign(form, settings)
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载配置失败' }))
  } finally {
    loading.value = false
  }

  loadAuditLogs()
})
</script>

<style scoped>
.settings-form {
  max-width: 560px;
}
/* 与 .settings-form 同宽，但用独立类名：e2e 里 .settings-form 是「全局系统参数」卡的定位锚点 */
.pressure-tier-form {
  max-width: 560px;
}
.form-hint {
  font-size: 12px;
  color: #999;
  margin-left: 12px;
}
.card-desc {
  font-size: 12px;
  color: #888;
  margin: 0 0 14px;
}
.card-link {
  color: #1a6db5;
}
.point-table {
  max-width: 560px;
}
.threshold-notes {
  margin: 12px 0 0;
  padding-left: 18px;
  font-size: 12px;
  line-height: 1.75;
  color: #888;
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
