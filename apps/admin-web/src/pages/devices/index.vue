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
      <!-- T728：批量导出只对 admin 出现（网关 adminOnly + 服务端 assertAdminRole 同判据），
           限额与后端 MaxSecretExportBatch 同源，超限在前端先挡住，不发一次必 400 的往返 -->
      <el-button
        v-if="canSecret"
        :disabled="!selected.length"
        :loading="batchExporting"
        @click="exportSelectedSecrets"
      >批量下载密钥{{ selected.length ? `（${selected.length}）` : '' }}</el-button>
      <el-button type="primary" class="register-btn" @click="openRegisterDialog">注册设备</el-button>
    </div>

    <div class="page-card">
      <el-table :data="filteredList" size="small" v-loading="loading" @selection-change="onSelectionChange">
        <el-table-column v-if="canSecret" type="selection" width="42" />
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
        <el-table-column label="操作" :width="canSecret ? 210 : 80" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDetail(row)">详情</el-button>
            <el-button v-if="canSecret" link type="primary" size="small" @click="downloadSecret(row.deviceId)">下载密钥</el-button>
            <el-button v-if="canSecret" link type="warning" size="small" @click="askRegenerateSecret(row.deviceId)">重新生成</el-button>
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
        <el-alert
          v-if="canSecret"
          type="info"
          :closable="false"
          show-icon
          title="验签密钥将一次性展示"
          description="设备号首次注册会生成验签密钥，并在注册成功后一次性展示；该设备号若已存在，注册幂等、密钥不变更，本次不回显。"
        />
      </el-form>
      <template #footer>
        <el-button @click="registerVisible = false">取消</el-button>
        <el-button type="primary" :loading="registering" @click="submitRegister">注册</el-button>
      </template>
    </el-dialog>

    <!-- T728 一次性回显：注册与重生成共用这一扇窗（设计口径 = 卡面需求 1「回显仅一次，丢失走重新生成」）。
         关掉之后本页不再有取回入口，只能走「下载密钥」（每次留痕）或再重生一次（旧值即刻作废）。 -->
    <el-dialog v-model="secretVisible" :title="secretDialogTitle" width="560px" :close-on-click-modal="false">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="验签密钥仅此次展示，关闭后本页不可再看"
        description="请下载 .env 保存或立即复制转交烧录人员。如遗失，可在设备列表「重新生成」，旧密钥会即时失效，已按旧密钥烧录的设备需重新烧录后才能上报。"
      />
      <el-descriptions :column="1" border size="small" class="secret-desc">
        <el-descriptions-item label="设备ID">{{ secretPanel.deviceId }}</el-descriptions-item>
        <el-descriptions-item label="验签密钥">
          <div class="secret-line">
            <span class="secret-value">{{ secretPanel.secret }}</span>
            <el-button link type="primary" size="small" @click="copySecret">复制</el-button>
          </div>
        </el-descriptions-item>
        <el-descriptions-item v-if="secretPanel.secretVersion" label="密钥版本">
          v{{ secretPanel.secretVersion }}（本次重新生成，旧版本已失效）
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="downloadSecret(secretPanel.deviceId)">下载 .env</el-button>
        <el-button type="primary" @click="secretVisible = false">我已保存</el-button>
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
        <!-- 卡面验收 2 的落点写的是「设备详情 → 下载 secret 文件」，故抽屉里这一对入口与列表行内同源
             （共用 downloadSecret / askRegenerateSecret，不做两套语义） -->
        <div v-if="canSecret && detail" class="detail-secret-actions">
          <el-button size="small" @click="downloadSecret(detail.deviceId)">下载密钥（.env）</el-button>
          <el-button size="small" type="warning" @click="askRegenerateSecret(detail.deviceId)">重新生成</el-button>
        </div>
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
import { userErrorCopy } from '@bracesync/shared-utils'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Device } from '@bracesync/shared-types'
import {
  fetchDevices, fetchDeviceDetail, fetchDeviceBindings, registerDeviceApi, patientNameOf,
  downloadDeviceSecretFileApi, exportDeviceSecretsApi, regenerateDeviceSecretApi,
  type DeviceBindingRecord,
} from '../../api'
import { formatCstMonthDayTime } from '../../utils/formatTime'
import { useAuthStore } from '../../stores/auth'
import { canAccessDeviceSecret, MAX_SECRET_EXPORT_BATCH } from '../../utils/deviceSecretAccess'

const auth = useAuthStore()

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

// T728：验签密钥的三个入口（单台下载 / 批量下载 / 重新生成）+ 一次性回显窗
const canSecret = computed(() => canAccessDeviceSecret(auth.role))
const selected = ref<Device[]>([])
const batchExporting = ref(false)
const secretVisible = ref(false)
const secretPanel = ref<{ deviceId: string; secret: string; source: 'register' | 'regenerate'; secretVersion: number }>(
  { deviceId: '', secret: '', source: 'register', secretVersion: 0 },
)

const secretDialogTitle = computed(() =>
  secretPanel.value.source === 'register' ? `注册成功 · ${secretPanel.value.deviceId} 的验签密钥` : `已重新生成 · ${secretPanel.value.deviceId} 的验签密钥`,
)

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
  // T597：东八区公共出口（原 UTC 串切片早 8 小时）
  return formatCstMonthDayTime(iso)
}

async function loadData() {
  loading.value = true
  try {
    const res = await fetchDevices({ keyword: keyword.value || undefined })
    list.value = res.list
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载失败' }))
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
    const res = await registerDeviceApi({ deviceId, model: registerForm.value.model.trim() || undefined })
    // T268 既有行为不动：注册成功一律先给这条 toast（e2e/tests/admin-keypages.spec.ts:223 认它），
    // T728 的回显窗是叠加在上面的一层，不替代它。
    ElMessage.success('设备已注册')
    registerVisible.value = false
    await loadData()
    // T728：deviceSecret 键缺席 = 本次没有新建（幂等命中既有行，库里密钥没变）⇒ 不开回显窗，
    // 否则等于把一个与库里无关的串摆到界面上让人去烧录。要取已存在设备的密钥走下载端点。
    if (res.deviceSecret) {
      showOneTimeSecret(deviceId, res.deviceSecret, 'register')
    } else {
      ElMessage.info('该设备号已注册过，验签密钥未变更；如需取用请在列表点「下载密钥」')
    }
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '注册失败' }))
  } finally {
    registering.value = false
  }
}

// ===== T728 验签密钥取用面 =====

function onSelectionChange(rows: Device[]) {
  selected.value = rows
}

function showOneTimeSecret(deviceId: string, secret: string, source: 'register' | 'regenerate', secretVersion = 0) {
  secretPanel.value = { deviceId, secret, source, secretVersion }
  secretVisible.value = true
}

async function copySecret() {
  try {
    // 非安全上下文（http:// 直接访问 staging 机器）下 navigator.clipboard 是 undefined，
    // 这一句会同步抛 TypeError ⇒ 落进 catch 给手动复制的指引，不报成「复制失败」的错因不明
    await navigator.clipboard.writeText(secretPanel.value.secret)
    ElMessage.success('验签密钥已复制到剪贴板')
  } catch {
    ElMessage.warning('当前浏览器不允许写入剪贴板，请手动选中复制')
  }
}

/** 单台下载（行内按钮与回显窗里的「下载 .env」共用这条出口）：每次取用都在服务端留痕。 */
async function downloadSecret(deviceId: string) {
  try {
    await downloadDeviceSecretFileApi(deviceId)
    ElMessage.success(`已下载 ${deviceId}.env（本次取用已留痕）`)
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '密钥下载失败' }))
  }
}

async function exportSelectedSecrets() {
  const ids = selected.value.map((d) => d.deviceId)
  if (ids.length === 0) {
    ElMessage.warning('请先勾选要导出密钥的设备')
    return
  }
  if (ids.length > MAX_SECRET_EXPORT_BATCH) {
    ElMessage.warning(`单次最多导出 ${MAX_SECRET_EXPORT_BATCH} 台，请分次勾选（当前已选 ${ids.length} 台）`)
    return
  }
  batchExporting.value = true
  try {
    await exportDeviceSecretsApi(ids)
    ElMessage.success(`已导出 ${ids.length} 台设备的验签密钥（本次取用已留痕）`)
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '批量下载失败' }))
  } finally {
    batchExporting.value = false
  }
}

async function askRegenerateSecret(deviceId: string) {
  try {
    await ElMessageBox.confirm(
      `将重新生成设备 ${deviceId} 的验签密钥。旧密钥即刻失效：已按旧密钥烧录的设备必须重新烧录，否则上报会被拒（20401 签名错误）。确认继续？`,
      '重新生成验签密钥',
      { type: 'warning', confirmButtonText: '重新生成', cancelButtonText: '取消' },
    )
  } catch {
    return // 取消或关掉弹层都不发枪
  }
  try {
    const res = await regenerateDeviceSecretApi(deviceId)
    showOneTimeSecret(deviceId, res.secret, 'regenerate', res.secretVersion)
  } catch (e: unknown) {
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '重新生成失败' }))
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
    ElMessage.error(userErrorCopy(e, { scope: 'admin', fallback: '加载详情失败' }))
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
.detail-secret-actions {
  margin: 14px 0 4px;
  display: flex;
  gap: 8px;
}
/* T728 一次性回显：密钥是 64 位 hex，无空格 ⇒ 必须强制断行，否则弹窗被撑破、末尾字符看不见 */
.secret-desc {
  margin-top: 12px;
}
.secret-line {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.secret-value {
  flex: 1;
  word-break: break-all;
  font-family: 'SFMono-Regular', Consolas, monospace;
}
</style>
