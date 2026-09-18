<template>
  <view class="page">
    <view class="page-header">
      <text class="back-link" @click="goBack">← 返回</text>
      <text class="page-title">WiFi 配置</text>
    </view>

    <!-- 配网状态步骤条 -->
    <view class="stepper">
      <template v-for="(label, i) in wifiSteps" :key="i">
        <view :class="['step', stepStatus(i)]">
          <view class="step-circle">
            <text v-if="stepStatus(i) !== 'done'">{{ i + 1 }}</text>
            <text v-else>✓</text>
          </view>
          <text class="step-label">{{ label }}</text>
        </view>
        <view v-if="i < wifiSteps.length - 1" :class="['step-line', { 'step-line-done': isStepDone(i) }]"></view>
      </template>
    </view>

    <!-- 成功态 -->
    <view v-if="wifiStatusCode === 9" class="section">
      <view class="success-card">
        <text class="success-icon">✓</text>
        <text class="success-title">配网成功</text>
        <text class="success-sub">WiFi 已连接，数据可达性验证通过</text>
        <text v-if="clearState === 'pending'" class="clear-tip">正在清除设备 WiFi…</text>
        <text v-else-if="clearState === 'success'" class="clear-tip clear-ok">设备 WiFi 已清除，可安全交付</text>
        <view v-else class="clear-failed">
          <text class="clear-tip clear-err">设备 WiFi 清除未完成</text>
          <view class="btn-outline-sm" @click="retryClear"><text>重新清除</text></view>
        </view>
        <text v-if="clearState === 'success'" class="auto-return-tip">3 秒后自动返回安装流程...</text>
      </view>
    </view>

    <!-- 错误态 -->
    <view v-else-if="errorCode !== null" class="section">
      <view class="error-card">
        <text class="error-icon">!</text>
        <text class="error-title">{{ errorMessage }}</text>
        <view class="btn-outline" @click="retryWifi"><text>重新配网</text></view>
        <view v-if="errorCode === -4" class="skip-hint">
          <text class="skip-text">WiFi 已连接，但暂时无法连接云端。</text>
          <view class="btn-outline-sm" @click="skipNetworkSetup"><text>先完成安装</text></view>
        </view>
      </view>
    </view>

    <!-- 配网前 + 配网中 -->
    <template v-else>
      <view class="section">
        <view class="card">
          <view class="notice">
            <text class="notice-icon">ℹ️</text>
            <text class="notice-text">请确保手机已连接到目标 WiFi 网络，或通过 BLE 将 WiFi 信息写入设备。</text>
          </view>

          <text class="section-title">WiFi 名称</text>
          <view class="manual-wifi">
            <text class="form-label">WiFi 名称 (SSID)</text>
            <input class="form-input" type="text" placeholder="请输入 WiFi 名称" v-model="manualSSID" />
          </view>
          <view class="wifi-hint">
            <text>仅支持 2.4GHz Wi-Fi 网络，不支持 5GHz（含 5G 频段的合一路由器请填 2.4G 那个网络名）</text>
            <text>请手动输入准确的 WiFi 名称与密码</text>
          </view>
          </view>
      </view>

      <view class="section">
        <view class="card">
          <view class="form-group">
            <text class="form-label">WiFi 密码</text>
            <view class="password-wrap">
              <input
                class="form-input password-input"
                :password="!showPassword"
                placeholder="请输入WiFi密码"
                v-model="password"
              />
              <text class="pwd-toggle" @click="showPassword = !showPassword">
                {{ showPassword ? '🙈' : '👁' }}
              </text>
            </view>
          </view>
          <view :class="['btn-primary', { 'btn-disabled': provisioning || !bleLinkUp }]" @click="startWifiConfig">
            <text>{{ provisioning ? '配置中...' : bleLinkUp ? '开始配网' : '设备连接已断开' }}</text>
          </view>
        </view>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onUnmounted } from 'vue'
import { useInstallStore } from '../../stores/install'
import { getProvisionKey } from '../../api/provision'
import { setDeviceWifi } from '../../api/device'
import { encryptWifiPayload } from '../../utils/aes-ctr'
import {
  connectDevice,
  registerBleStateListener,
  rescanOnce,
  writeWifiConfigV2,
  onWifiStatus,
  startMockWifiStatusSequence,
  stopMockWifiStatusSequence,
  sendWifiClear,
  waitForWifiClear,
} from '../../utils/ble'
import { pickReconnectTarget, RECONNECT_SCAN_MS } from '../../utils/ble-link'
import { bleLog } from '../../utils/ble-log'

const installStore = useInstallStore()

const wifiSteps = ['收到', '连AP', '取IP', '探测', '成功']
const manualSSID = ref('')
const password = ref('')
const showPassword = ref(false)
const provisioning = ref(false)

// T218 A-2: 连接态监听——掉线给可执行提示并禁止提交，链路回来自动恢复
const bleLinkUp = ref(true)
const BLE_LINK_DOWN_TOAST = '设备连接已断开，请靠近设备后重试'
const BLE_RECONNECTING_TOAST = '设备连接已断开，正在重新连接…'

// T119: 最小重试间隔（ms）。防止连点导致高频 BLE 写入 / 重复申领。
// 取 3s：BLE 写入+设备处理约 1~2s，3s 足以让上一次操作落定，
// 同时用户改完密码后不会感到明显阻塞。
const MIN_PROVISION_INTERVAL = 3000

// T212: 配网超时兜底 60s（与患者端 PROVISION_TIMEOUT_MS 同值）。
// 正常链路实测 7–8s，但出现过一次约 50s 的长尾 ⇒ 留足余量。
// T218 A-5: 语义对齐患者端 armTimeout——60s 内**无任何推送**才算超时，每收一帧重新计时；
// 设备慢但正常（逐帧推进 0→1→2→3）不再被误判。
const PROVISION_TIMEOUT_MS = 60000
let lastProvisionAttempt = 0

// T218 A-4: 一轮下发的写入次数上限（患者端 WRITE_MAX_ATTEMPTS 同款）：
// 首写落在 API 层失败时，判死链路→原地重连→重订阅→领新 seq 重写一次，不再靠技师重新走流程
const WRITE_MAX_ATTEMPTS = 2

const wifiStatusCode = ref<number | null>(null)
const statusHistory: number[] = []
const errorCode = ref<number | null>(null)
const autoReturnTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const timeoutTimer = ref<ReturnType<typeof setTimeout> | null>(null)
let successProcessed = false
// T240 修复：配网成功（收到 9）后冻结 wifiStatusCode，
// 防止 clear 回执 B512 notify 0 把状态码打回 0、步骤条勾号消失。
let provisioningDone = false

// T240: 配网成功后清设备 WiFi 的子状态
const clearState = ref<'pending' | 'success' | 'failed'>('pending')
// 页面卸载后置 false，防止 clear settle 后对已卸载页面调 navigateBack/toast
let pageAlive = true

/** T218 A-5: 60s 无推送兜底表——每收一帧重新计时（armProvisionTimeout） */
function armProvisionTimeout() {
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  timeoutTimer.value = setTimeout(() => {
    if (wifiStatusCode.value !== 9) {
      handleTimeout()
    }
  }, PROVISION_TIMEOUT_MS)
}

// T218 A-2: 页面在栈顶期间监听连接态；卸载时解绑，把通知权还给下层页面
const offBleState = registerBleStateListener((deviceId, connected) => {
  bleLog.info(`wifi-config 连接态: connected=${connected} deviceId=${deviceId}`)
  if (connected) {
    installStore.setBleConnected(true)
    bleLinkUp.value = true
  } else {
    installStore.setBleDisconnected()
    bleLinkUp.value = false
    uni.showToast({ title: BLE_LINK_DOWN_TOAST, icon: 'none' })
  }
})

const errorMessage = computed(() => {
  const map: Record<number, string> = {
    [-1]: '密码错误，请检查 WiFi 密码',
    [-2]: '未找到 WiFi 网络，请检查 SSID',
    [-3]: '网络连接失败（DHCP），请检查路由器',
    [-4]: '云端暂不可达，设备将在后台持续重试（约每 5 分钟一次），请保持设备通电与 WiFi 环境',
  }
  return map[errorCode.value!] || '配网失败，请重试'
})

function stepStatus(i: number): string {
  // i: 0-4 对应 步骤1-5
  const code = wifiStatusCode.value
  if (code === null) return i === 0 ? 'active' : 'pending'
  if (code === 9) return 'done'
  if (code < 0) return i <= 3 ? 'done' : 'error'
  // 0/1/2/3
  if (i < code) return 'done'
  if (i === code) return 'active'
  return 'pending'
}
function isStepDone(i: number): boolean {
  const code = wifiStatusCode.value
  if (code === null) return false
  if (code === 9) return true
  if (code < 0) return i < 4
  return i < code
}

function goBack() {
  uni.navigateBack()
}

async function startWifiConfig() {
  // T119: 最小重试间隔节流，避免连点高频写设备
  const now = Date.now()
  if (now - lastProvisionAttempt < MIN_PROVISION_INTERVAL) {
    const waitSec = Math.ceil((MIN_PROVISION_INTERVAL - (now - lastProvisionAttempt)) / 1000)
    uni.showToast({ title: `操作过于频繁，请${waitSec}秒后重试`, icon: 'none' })
    return
  }
  lastProvisionAttempt = now

  // T218 A-2: 链路已断禁止提交（提示可执行：靠近设备；恢复后按钮自动可用）
  if (!bleLinkUp.value) {
    uni.showToast({ title: BLE_LINK_DOWN_TOAST, icon: 'none' })
    return
  }

  const ssid = manualSSID.value
  if (!ssid) {
    uni.showToast({ title: '请选择或输入 WiFi 网络', icon: 'none' })
    return
  }
  if (!password.value) {
    uni.showToast({ title: '请输入 WiFi 密码', icon: 'none' })
    return
  }
  provisioning.value = true
  wifiStatusCode.value = null
  errorCode.value = null
  successProcessed = false
  provisioningDone = false
  statusHistory.length = 0

  try {
    // T218 A-3: 下发前确保链路可用——断链不再要求技师退出重走
    if (!(await ensureLinkForProvision())) {
      uni.showToast({ title: BLE_LINK_DOWN_TOAST, icon: 'none' })
      provisioning.value = false
      return
    }
    let bleMac = installStore.bleDeviceId || installStore.deviceId

    // 1. 申领 provision-key（真实 API，一轮一次；provision_key 原文不入日志）
    const { provision_key_hex } = await getProvisionKey(installStore.deviceId)

    // 2. 监听配网状态（T109: 传入 BLE MAC 以订阅 B512 Notify）
    //    T216: 必须先订阅再写 B511 —— 设备在收到配置后立刻回 0/1，
    //    订阅晚于写入就会把首帧丢掉（患者端 wifi-setup 一直是这个顺序）。
    const statusListener = (code: number) => {
      // T240 修复：成功后不再让 B512 帧覆盖状态码（clear 的 notify 0 会把勾打没）
      if (!provisioningDone) {
        wifiStatusCode.value = code
        statusHistory.push(code)
      }
      if (code === 9) handleSuccess(ssid)
      else if (code < 0) handleError(code)
      // T218 A-5: 每收一帧重新计时——60s 无推送才算超时
      else if (!provisioningDone) armProvisionTimeout()
    }

    // 3. BLE 写入加密配置（T109: 用 BLE MAC，非后端设备 ID）
    //    T218 A-4: 整段可重试——链路在写入途中自断时微信回的是 API 层 fail
    //    （writeBLECharacteristicValue:fail:system error:Inner error.），不是设备失败码
    let writeErr: Error | null = null
    for (let attempt = 1; attempt <= WRITE_MAX_ATTEMPTS; attempt++) {
      writeErr = null
      try {
        onWifiStatus(statusListener, bleMac)

        // T216: seq 每轮自增，仅当将要超出固件候选窗上限（64）时回绕为 1。
        // 🔴 重试同样领新 seq（重放同一 IV ＝ CTR keystream 复用）
        const seq = installStore.nextWifiSeq()
        const encrypted = await encryptWifiPayload(ssid, password.value, provision_key_hex, seq)
        await writeWifiConfigV2(bleMac, encrypted)
        bleLog.info(`B511 写入完成，等待设备推送 attempt=${attempt} seq=${seq} payloadBytes=${encrypted.length / 2}`)
        break
      } catch (e) {
        writeErr = e as Error
        // 与"设备回失败码"区分开：这条只在 API 层写失败时出现，设备侧的 -1..-4 走 handleError
        bleLog.error(`B511 下发失败（API 层，非设备失败码）attempt=${attempt} msg=${writeErr.message} bleConnected=${installStore.bleConnected}`)
        // 主动判死：写 fail 多半就是链路已断。仍标着已连 ⇒ 下一次 ensureLink 会跳过重连、原地再失败一次
        installStore.setBleConnected(false)
        bleLinkUp.value = false
        if (attempt === WRITE_MAX_ATTEMPTS) break
        if (!(await ensureLinkForProvision())) break
        // 重扫可能命中另一广播名对应的 MAC，后续写入以 store 里最新的为准
        bleMac = installStore.bleDeviceId || installStore.deviceId
        bleLog.warn(`T218 重试已续上链路，领新 seq 重写 attempt=${attempt + 1} bleMac=${bleMac}`)
      }
    }
    if (writeErr) throw writeErr

    // H5 mock：启动状态机序列
    // T089-MOCK: 真机由硬件 WiFi Status Notify 驱动
    startMockWifiStatusSequence()

    // T218 A-5: 写入完成后起表——60s 无任何推送才超时（收到帧即续期，见 statusListener）
    armProvisionTimeout()
  } catch (e) {
    uni.showToast({ title: e instanceof Error ? e.message : '配网失败', icon: 'none' })
    provisioning.value = false
  }
}

/* ===== T218 A-3: 链路保障（患者端 wifi-setup ensureLinkForProvision 平移） ===== */

/** 距本页最近一次建连成功的秒数（从未建连返回 -1）：用于量化固件空闲保活窗口 */
let linkUpAtMs = 0
function secsSinceLinkUp(): number {
  return linkUpAtMs ? Math.round((Date.now() - linkUpAtMs) / 1000) : -1
}

async function tryLink(mac: string): Promise<boolean> {
  try {
    const ok = await connectDevice(mac)
    installStore.setBleConnected(ok, mac)
    if (ok) {
      bleLinkUp.value = true
      linkUpAtMs = Date.now()
    }
    bleLog.info(`原地重连结果 deviceId=${mac} ok=${ok}`)
    return ok
  } catch (e) {
    bleLog.warn(`原地重连异常 deviceId=${mac}`, e instanceof Error ? e.message : String(e))
    return false
  }
}

/**
 * 下发前确保链路可用：直连已存 MAC → 不成再一次性重扫（等设备重新广播）→ 连上即回 true。
 * 技师端没有设备广播名可比对（bind 页只存 MAC），expectedName 传空走 BSYNC 兜底，
 * 与患者端"重扫兜底第一台 BSYNC"同一行为。
 */
async function ensureLinkForProvision(): Promise<boolean> {
  if (installStore.bleConnected && installStore.bleDeviceId) return true
  if (!installStore.bleDeviceId) {
    // 从未建立过链路（如调试直连本页）：无从重连，放行走原流程，写失败由失败路径兜底
    bleLog.warn(`下发前无 BLE MAC 可重连（未绑定？） secsSinceLinkUp=${secsSinceLinkUp()}`)
    return true
  }
  bleLog.warn(`下发前链路已断，尝试原地重连 bleDeviceId=${installStore.bleDeviceId} secsSinceLinkUp=${secsSinceLinkUp()}`)
  uni.showLoading({ title: BLE_RECONNECTING_TOAST, mask: true })
  try {
    if (await tryLink(installStore.bleDeviceId)) return true

    const found = await rescanOnce(RECONNECT_SCAN_MS)
    const target = pickReconnectTarget(found, '')
    if (!target) {
      bleLog.warn(`原地重连失败：重扫窗口内无可用广播 found=${found.length} windowMs=${RECONNECT_SCAN_MS}`)
      return false
    }
    bleLog.info(`重扫命中 name=${target.name} deviceId=${target.deviceId}`)
    return await tryLink(target.deviceId)
  } finally {
    uni.hideLoading()
  }
}

// T240 修复：设备在收到 9 后会连发多次 9（实测 3 次，约 2s 窗口期），
// 期间写入 0x02 会被固件忽略 → clear 超时。先等 1.5s 让 9 突冲落定再发 clear。
const CLEAR_SETTLE_DELAY_MS = 1500

// T240: 清掉刚配的 WiFi —— 设备出厂态交付患者。
// 发 clear (B513 0x02) → 等 B512 notify 0（≤10s）。返回 false 表示未完成。
// 超时自动重试一次（实测首次常因设备仍在发 9 而被忽略，重试即成功）。
async function doWifiClear(): Promise<boolean> {
  const bleMac = installStore.bleDeviceId || installStore.deviceId
  const attemptOnce = async (): Promise<boolean> => {
    try {
      await sendWifiClear(bleMac)
      return await waitForWifiClear(10000)
    } catch (e) {
      bleLog.error('sendWifiClear 异常', e instanceof Error ? e.message : String(e))
      return false
    }
  }
  // 首次：等 9 突冲落定
  await new Promise((r) => setTimeout(r, CLEAR_SETTLE_DELAY_MS))
  const firstOk = await attemptOnce()
  if (firstOk) return true
  // 自动重试一次（不再额外等待——设备此时已空闲）
  bleLog.warn('T240 clear 首次未收到回执，自动重试一次')
  return await attemptOnce()
}

// T240: clear 失败后技师手动重试
async function retryClear() {
  clearState.value = 'pending'
  const clearOk = await doWifiClear()
  if (!pageAlive) return
  if (clearOk) {
    clearState.value = 'success'
    // 不主动 closeBLEConnection：设备清完 WiFi 会自行重启掉链，
    // 主动断开会让 install 页校准的 B513 订阅链路代次错乱、零收帧。
    autoReturnTimer.value = setTimeout(() => {
      uni.navigateBack()
    }, 3000)
  } else {
    clearState.value = 'failed'
    uni.showToast({ title: '设备 WiFi 清除未完成，请靠近设备后重试', icon: 'none' })
  }
}

async function handleSuccess(ssid: string) {
  // 固件会 Notify 两次 9（防 BLE 漏收），第二次直接忽略，不重复跳转/回写
  if (successProcessed) return
  successProcessed = true
  provisioningDone = true
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  provisioning.value = false
  clearState.value = 'pending'

  // 云端 WiFi 状态回写（mock 先行）
  try {
    await setDeviceWifi(installStore.deviceId, ssid)
  } catch (e) {
    uni.showToast({ title: 'WiFi 状态同步失败', icon: 'none' })
  }

  installStore.setWifiStatus('connected')
  installStore.updateWifiStatusCode(9)

  // T240: 配网成功后必须清掉刚配的 WiFi，设备出厂态交付患者。
  // 仅在 clear 完成（或失败提示）后才决定是否自动返回，禁止 3s 窗口抢先返回。
  const clearOk = await doWifiClear()
  if (!pageAlive) return

  if (clearOk) {
    clearState.value = 'success'
    // 不主动 closeBLEConnection：设备清完 WiFi 会自行重启掉链，
    // 主动断开会让 install 页校准的 B513 订阅链路代次错乱、零收帧。
    autoReturnTimer.value = setTimeout(() => {
      uni.navigateBack()
    }, 3000)
  } else {
    clearState.value = 'failed'
    uni.showToast({ title: '设备 WiFi 清除未完成，请靠近设备后重试', icon: 'none' })
  }
}

function handleError(code: number) {
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  provisioning.value = false
  errorCode.value = code
}

function handleTimeout() {
  bleLog.warn(`${PROVISION_TIMEOUT_MS / 1000}s 无推送超时，状态历史`, [...statusHistory])
  // P2-5: 迟到状态 9 回转——继续监听，不立即标失败
  // 这里给提示，但保留 statusListener（未移除），迟到状态 9 仍可触发 handleSuccess
  uni.showToast({ title: '设备无响应，请靠近设备后重试', icon: 'none' })
}

function retryWifi() {
  wifiStatusCode.value = null
  errorCode.value = null
  password.value = ''
}

function skipNetworkSetup() {
  // -4 状态：本地标记「已跳过」，返回 install（不落库）
  installStore.setWifiStatus('connected')
  installStore.setNetworkSkipped(true)
  uni.navigateBack()
}

onUnmounted(() => {
  pageAlive = false
  if (autoReturnTimer.value) clearTimeout(autoReturnTimer.value)
  if (timeoutTimer.value) clearTimeout(timeoutTimer.value)
  stopMockWifiStatusSequence()
  offBleState()
})
</script>

<style scoped>
.page { padding-bottom: 120rpx; }
.page-header { padding: 80rpx 48rpx 16rpx; }
.back-link { font-size: 28rpx; color: #2563EB; display: block; margin-bottom: 16rpx; }
.page-title { font-size: 40rpx; font-weight: 600; color: #1e293b; display: block; }
.section { padding: 0 40rpx; margin-top: 24rpx; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.04); }

/* 步骤条 */
.stepper { display: flex; align-items: center; justify-content: center; padding: 24rpx 48rpx; }
.step { display: flex; flex-direction: column; align-items: center; position: relative; z-index: 1; }
.step-circle { width: 52rpx; height: 52rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 24rpx; font-weight: 600; color: #fff; transition: all 0.3s; }
.step-done .step-circle { background: #10B981; }
.step-active .step-circle { background: #3B82F6; }
.step-pending .step-circle { background: #e5e7eb; color: #9ca3af; }
.step-error .step-circle { background: #ef4444; }
.step-label { font-size: 20rpx; margin-top: 6rpx; font-weight: 500; }
.step-done .step-label { color: #10B981; }
.step-active .step-label { color: #3B82F6; }
.step-pending .step-label { color: #9ca3af; }
.step-error .step-label { color: #ef4444; }
.step-line { flex: 1; height: 4rpx; margin: 0 6rpx 34rpx; background: #e5e7eb; }
.step-line-done { background: #10B981; }

.notice { display: flex; align-items: flex-start; gap: 12rpx; padding: 20rpx; background: #f0f9ff; border-radius: 12rpx; margin-bottom: 20rpx; }
.notice-icon { font-size: 28rpx; }
.notice-text { font-size: 24rpx; color: #0369a1; line-height: 1.5; }
.section-title { font-size: 28rpx; font-weight: 500; color: #1e293b; margin-bottom: 16rpx; display: block; }
.manual-wifi { margin-top: 16rpx; }
.wifi-hint { display: flex; flex-direction: column; gap: 8rpx; margin-top: 20rpx; padding: 20rpx 24rpx; background: #fff7ed; border-radius: 12rpx; }
.wifi-hint text { font-size: 22rpx; color: #b45309; line-height: 1.5; }
.form-group { margin-bottom: 20rpx; }
.form-label { font-size: 26rpx; color: #64748b; display: block; margin-bottom: 12rpx; }
.form-input { width: 100%; height: 76rpx; line-height: 76rpx; padding: 0 24rpx; border: 1rpx solid #e2e8f0; border-radius: 12rpx; font-size: 28rpx; color: #1e293b; background: #f8fafc; box-sizing: border-box; }
.password-wrap { display: flex; align-items: center; border: 1rpx solid #e2e8f0; border-radius: 12rpx; background: #f8fafc; padding-right: 20rpx; }
.password-input { border: none; }
.pwd-toggle { font-size: 28rpx; padding: 0 8rpx; }
.btn-primary { width: 100%; padding: 24rpx 0; background: #2563EB; border-radius: 16rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-disabled { opacity: 0.5; }
.btn-outline { width: 100%; padding: 22rpx 0; background: #fff; border: 2rpx solid #3B82F6; border-radius: 16rpx; text-align: center; margin-top: 20rpx; }
.btn-outline text { color: #3B82F6; font-size: 28rpx; font-weight: 500; }
.btn-outline-sm { padding: 16rpx 0; background: #fff; border: 2rpx solid #f59e0b; border-radius: 12rpx; text-align: center; margin-top: 16rpx; }
.btn-outline-sm text { color: #b45309; font-size: 26rpx; }

.success-card { background: #f0fdf4; border: 1rpx solid #bbf7d0; border-radius: 24rpx; padding: 64rpx 32rpx; text-align: center; }
.success-icon { display: inline-flex; width: 120rpx; height: 120rpx; border-radius: 50%; background: #10B981; color: #fff; font-size: 64rpx; align-items: center; justify-content: center; }
.success-title { display: block; font-size: 36rpx; font-weight: 600; color: #166534; margin-top: 24rpx; }
.success-sub { display: block; font-size: 26rpx; color: #047857; margin-top: 12rpx; }
.clear-tip { display: block; font-size: 26rpx; margin-top: 24rpx; color: #6b7280; }
.clear-ok { color: #047857; }
.clear-err { color: #b91c1c; }
.clear-failed { margin-top: 24rpx; }
.auto-return-tip { display: block; font-size: 24rpx; color: #6b7280; margin-top: 24rpx; }

.error-card { background: #fef2f2; border: 1rpx solid #fecaca; border-radius: 24rpx; padding: 48rpx 32rpx; text-align: center; }
.error-icon { display: inline-flex; width: 80rpx; height: 80rpx; border-radius: 50%; background: #ef4444; color: #fff; font-size: 48rpx; align-items: center; justify-content: center; }
.error-title { display: block; font-size: 30rpx; color: #991b1b; margin: 24rpx 0; line-height: 1.5; }
.skip-hint { margin-top: 32rpx; }
.skip-text { display: block; font-size: 24rpx; color: #b45309; line-height: 1.5; }
</style>

