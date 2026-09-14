<template>
  <view class="wifi-setup">
    <!-- 01-entry -->
    <WifiEntry
      v-if="view === 'entry'"
      ref="entryRef"
      :device-name="deviceLabel"
      :network-text="networkText"
      :bluetooth-on="bluetoothOn"
      :location-on="locationOn"
      @start="onEntryStart"
      @enable-bluetooth="onEnableBluetooth"
      @grant-location="onGrantLocation"
    />

    <!-- 02-scan -->
    <WifiScan
      v-else-if="view === 'scan'"
      :devices="devices"
      :scanning="scanning"
      @select="onSelectDevice"
      @retry="startScan"
      @contact="goContact('nodevice', 'scan')"
    />

    <!-- 03-connect -->
    <WifiConnect
      v-else-if="view === 'connect'"
      ref="connectRef"
      :phase="connectPhase"
      :device-name="deviceLabel"
      :connected="bleLinkUp"
      @next="connectPhase = 'form'"
      @start="onStartProvision"
    />

    <!-- 04-progress -->
    <WifiProgress
      v-else-if="view === 'progress'"
      :code="provisionCode"
      :device-name="deviceLabel"
      @cancel="onCancelProvision"
    />

    <!-- 05-success -->
    <WifiSuccess
      v-else-if="view === 'success'"
      :device-label="deviceLabel"
      :network-name="enteredSsid"
      @primary="goMonitor"
      @secondary="goDevicePage"
    />

    <!-- 06a–06e -->
    <WifiFailure
      v-else-if="view === 'failure'"
      :type="failureType"
      @primary="onFailurePrimary"
      @secondary="onFailureSecondary"
      @contact="goContact(failureType, 'failure')"
    />

    <!-- 07-contact -->
    <WifiContact
      v-else
      :issue-type="contactIssue"
      :device-label="deviceLabel"
      :occurred-at="occurredAt"
      :submit-state="feedbackState"
      @open-chat="onOpenChat"
      @back="onContactBack"
    />
  </view>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { onLoad, onShow } from '@dcloudio/uni-app'
import { useDeviceStore } from '../../stores/device'
import { useAuthStore } from '../../stores/auth'
import { request } from '../../utils/request'
import { getProvisionKey } from '../../api/provision'
import { reportDeviceWifi } from '../../api/device'
import { submitWifiFailureFeedback, buildFailureContent, formatFeedbackTime } from '../../api/feedback'
import { encryptWifiPayload } from '../../utils/aes-ctr'
import { logger } from '../../utils/logger'
import {
  closeBLEConnection,
  connectDevice,
  ensureLocationPermission,
  isBluetoothReady,
  isLocationAuthed,
  onWifiStatus,
  registerBleStateListener,
  startBleScan,
  startMockWifiStatusSequence,
  stopBleScan,
  stopMockWifiStatusSequence,
  writeWifiConfigV2,
  type ScannedDevice,
} from '../../utils/ble'
import {
  PROVISION_TIMEOUT_MS,
  broadcastNameOf,
  isBsyncDevice,
  normalizeWifiName,
} from '../../utils/wifi-state'
import {
  BLE_DISCONNECTED_TOAST,
  CONNECT,
  CONTACT,
  ENTRY,
  FAILURE_COMMON,
  FAILURE_KEY_BY_CODE,
  FAILURES,
  PROGRESS,
  SCAN,
  SUCCESS,
  type ContactIssue,
  type FailureKey,
} from '../../utils/wifi-copy'
import WifiEntry from '../../components/wifi-setup/WifiEntry.vue'
import WifiScan from '../../components/wifi-setup/WifiScan.vue'
import WifiConnect from '../../components/wifi-setup/WifiConnect.vue'
import WifiProgress from '../../components/wifi-setup/WifiProgress.vue'
import WifiSuccess from '../../components/wifi-setup/WifiSuccess.vue'
import WifiFailure from '../../components/wifi-setup/WifiFailure.vue'
import WifiContact from '../../components/wifi-setup/WifiContact.vue'

/** 视图名与设计稿文件一一对应（01/02/03/04/05/06a-e/07） */
type View = 'entry' | 'scan' | 'connect' | 'progress' | 'success' | 'failure' | 'contact'

const NAV_TITLES: Record<View, string> = {
  entry: ENTRY.navTitle,
  scan: SCAN.navTitle,
  connect: CONNECT.navTitle,
  progress: PROGRESS.navTitle,
  success: SUCCESS.navTitle,
  failure: FAILURE_COMMON.navTitle,
  contact: CONTACT.navTitle,
}

/** BLE 扫描时长：设计稿 02 为持续搜索，6s 后收敛到结果/空态 */
const SCAN_DURATION_MS = 6000
/** T119 防重入节流窗口 */
const MIN_PROVISION_INTERVAL = 3000

const deviceStore = useDeviceStore()
const authStore = useAuthStore()

const entryRef = ref<InstanceType<typeof WifiEntry> | null>(null)
const connectRef = ref<InstanceType<typeof WifiConnect> | null>(null)

const view = ref<View>('entry')
const connectPhase = ref<'connecting' | 'form'>('connecting')
const bleLinkUp = ref(false)
const bluetoothOn = ref(false)
const locationOn = ref(false)
const devices = ref<ScannedDevice[]>([])
const scanning = ref(false)
const provisionCode = ref<number | null>(null)
const failureType = ref<FailureKey>('timeout')
const contactIssue = ref<ContactIssue>('timeout')
const contactBack = ref<View>('failure')
const occurredAt = ref('')
const feedbackState = ref<'pending' | 'ok' | 'failed'>('pending')
const enteredSsid = ref('')

/** 云端 device_id（provision-key / wifi 回写用）与 BLE 侧 deviceId（建连/写入用）来源分离 */
const cloudDeviceId = ref('')
const bleDeviceId = ref('')
const scannedName = ref('')

let enteredPwd = ''
let successHandled = false
let timeoutTimer: ReturnType<typeof setTimeout> | null = null
let lastAttemptAt = 0
let resumePending: 'bluetooth' | 'location' | '' = ''
let deviceReady = false
// #ifdef H5
let mockSequence: number[] | undefined
// #endif

/** 患者端设备标识只到广播名（PRD §7A.9：不展示完整 device_id） */
const deviceLabel = computed(() => scannedName.value || broadcastNameOf(cloudDeviceId.value))

const networkText = computed(() => {
  const ssid = deviceStore.currentDevice?.wifiSsid
  if (deviceStore.wifiStatus === 'connected' || (ssid && ssid !== '-')) return ENTRY.networkConnected
  return ENTRY.networkUnconnected
})

watch(
  view,
  (v) => {
    logger.info('[T192] view →', { view: v })
    uni.setNavigationBarTitle({ title: NAV_TITLES[v] })
  },
  { immediate: true }
)

onLoad(async (options) => {
  logger.info('[T192] onLoad', { options: options ?? {} })
  // #ifdef H5
  const raw = (options as { mock?: string } | undefined)?.mock
  if (raw) {
    const parsed = raw.split(',').map((n) => Number(n)).filter((n) => !Number.isNaN(n))
    if (parsed.length) mockSequence = parsed
  }
  // #endif
  registerBleStateListener((deviceId, connected) => {
    logger.info('[T192] BLE 连接态变化', { deviceId, connected, view: view.value })
    deviceStore.setBleConnected(connected)
    if (connected) bleLinkUp.value = true
    else bleLinkUp.value = false
    if (connected || view.value !== 'progress' || successHandled) return
    // 配网途中断连：不再有状态推送，按超时分支给可执行处置
    stopProvisionTimer()
    stopMockWifiStatusSequence()
    failureType.value = 'timeout'
    deviceStore.setWifiStatus('failed')
    view.value = 'failure'
    uni.showToast({ title: BLE_DISCONNECTED_TOAST, icon: 'none' })
  })
  deviceReady = await ensureCloudDevice()
})

onShow(async () => {
  const [bt, loc] = await Promise.all([isBluetoothReady(), isLocationAuthed()])
  bluetoothOn.value = bt
  locationOn.value = loc
  logger.info('[T192] onShow 前置态', {
    bluetoothOn: bt,
    locationOn: loc,
    view: view.value,
    resumePending,
    deviceReady,
  })
  if (view.value !== 'entry' || !resumePending) return
  if ((resumePending === 'bluetooth' && bt) || (resumePending === 'location' && loc)) {
    const which = resumePending
    resumePending = ''
    logger.info('[T192] 授权回来，自动续跑扫描', { which })
    uni.showToast({ title: which === 'bluetooth' ? ENTRY.btReadyToast : ENTRY.locReadyToast, icon: 'none' })
    await nextTick()
    goScan()
  }
})

/** 云端 device_id：优先设备页已加载的绑定设备，否则按快照补一次（不写占位值） */
async function ensureCloudDevice(): Promise<boolean> {
  cloudDeviceId.value = deviceStore.currentDevice?.deviceId || ''
  if (cloudDeviceId.value) {
    logger.info('[T192] 云端 device_id 来自 store', { deviceId: cloudDeviceId.value })
    return true
  }

  const patientId = authStore.patientId
  if (patientId) {
    try {
      const snap = await request<{ deviceId?: string | null }>({
        url: `/api/v1/patients/${patientId}/realtime`,
        method: 'GET',
      })
      cloudDeviceId.value = snap?.deviceId || ''
      logger.info('[T192] 云端 device_id 来自快照', { patientId, deviceId: cloudDeviceId.value })
    } catch (e) {
      logger.error('[T192] 设备快照加载失败', { patientId, msg: (e as Error)?.message })
    }
  }
  if (!cloudDeviceId.value) {
    logger.warn('[T192] 无云端设备，退回设备页', { patientId: patientId || '(空)' })
    uni.showToast({ title: ENTRY.noDeviceToast, icon: 'none' })
    setTimeout(() => uni.switchTab({ url: '/pages/device/index', fail: () => uni.navigateBack() }), 800)
  }
  return !!cloudDeviceId.value
}

/* ===== 01-entry ===== */

function onEntryStart() {
  logger.info('[T192] 01 开始配网点击', {
    deviceReady,
    bluetoothOn: bluetoothOn.value,
    locationOn: locationOn.value,
  })
  if (!deviceReady) {
    uni.showToast({ title: ENTRY.noDeviceToast, icon: 'none' })
    return
  }
  if (!bluetoothOn.value) {
    resumePending = 'bluetooth'
    entryRef.value?.showBlockingModal('bluetooth')
    return
  }
  if (!locationOn.value) {
    resumePending = 'location'
    entryRef.value?.showBlockingModal('location')
    return
  }
  goScan()
}

function onEnableBluetooth() {
  resumePending = 'bluetooth'
  // #ifdef MP-WEIXIN
  const sysBt = (uni as unknown as { openSystemBluetoothSetting?: (o: { fail?: (e?: unknown) => void }) => void })
    .openSystemBluetoothSetting
  logger.info('[T192] 01 去开蓝牙', { apiAvailable: typeof sysBt === 'function' })
  if (sysBt) {
    try {
      sysBt({
        fail: (err) => {
          logger.error('[T192] openSystemBluetoothSetting fail', { err: (err as Error)?.message ?? err })
          uni.showToast({ title: ENTRY.btSettingsToast, icon: 'none' })
        },
      })
      return
    } catch (e) {
      logger.error('[T192] openSystemBluetoothSetting 抛出', { msg: (e as Error)?.message })
    }
  }
  // #endif
  uni.showToast({ title: ENTRY.btSettingsToast, icon: 'none' })
}

async function onGrantLocation() {
  resumePending = 'location'
  locationOn.value = await ensureLocationPermission()
  logger.info('[T192] 01 位置授权返回', { granted: locationOn.value })
}

/* ===== 02-scan ===== */

function goScan() {
  logger.info('[T192] → 02 扫描')
  view.value = 'scan'
  startScan()
}

function startScan() {
  stopBleScan()
  devices.value = []
  scanning.value = true
  logger.info('[T192] startBleScan', { durationMs: SCAN_DURATION_MS })
  startBleScan(
    (dev) => {
      if (!isBsyncDevice(dev.name)) return
      if (devices.value.some((d) => d.deviceId === dev.deviceId)) return
      logger.info('[T192] 扫描到设备', { name: dev.name, deviceId: dev.deviceId, rssi: dev.RSSI })
      devices.value.push(dev)
    },
    (found) => {
      scanning.value = false
      logger.info('[T192] 扫描窗口结束', { found })
    },
    SCAN_DURATION_MS
  )
}

async function onSelectDevice(dev: ScannedDevice) {
  logger.info('[T192] 02 选中设备，建 BLE 连接', { name: dev.name, deviceId: dev.deviceId })
  stopBleScan()
  scanning.value = false
  scannedName.value = dev.name
  bleDeviceId.value = dev.deviceId
  bleLinkUp.value = false
  deviceStore.bleDeviceId = dev.deviceId
  deviceStore.bleName = dev.name
  view.value = 'connect'
  connectPhase.value = 'connecting'

  // 发现设备 → 建立 BLE 连接 → 才允许进入凭据输入（T182 缺失的前置）
  // connectDevice 会以 reject 表达超时/连接失败/服务与特征发现失败，必须在此兜住，
  // 否则安卓上未处理的 rejection 既无提示也不上报（wx.onUnhandledRejection 安卓不支持）
  let connected = false
  try {
    connected = await connectDevice(dev.deviceId)
  } catch (e) {
    logger.error('[T192] BLE 建连失败', { name: dev.name, deviceId: dev.deviceId, msg: (e as Error)?.message ?? String(e) })
  }
  logger.info('[T192] BLE 建连结果', { deviceId: dev.deviceId, connected })
  if (!connected) {
    uni.showToast({ title: CONNECT.connectFailedToast, icon: 'none' })
    goScan()
    return
  }
  deviceStore.setBleConnected(true)
  bleLinkUp.value = true
}

/* ===== 03-connect ===== */

async function onStartProvision() {
  const now = Date.now()
  if (now - lastAttemptAt < MIN_PROVISION_INTERVAL) {
    logger.info('[T192] 03 节流拦截', { sinceLastMs: now - lastAttemptAt })
    uni.showToast({ title: CONNECT.throttleToast, icon: 'none' })
    return
  }

  const creds = connectRef.value?.getCredentials() ?? { ssid: '', pwd: '' }
  const ssid = normalizeWifiName(creds.ssid)
  if (!ssid) {
    logger.info('[T192] 03 ssid 为空，拦截')
    uni.showToast({ title: CONNECT.ssidRequiredToast, icon: 'none' })
    return
  }
  if (!creds.pwd) {
    logger.info('[T192] 03 密码为空，拦截', { ssid })
    uni.showToast({ title: CONNECT.pwdRequiredToast, icon: 'none' })
    return
  }
  enteredSsid.value = ssid
  enteredPwd = creds.pwd
  lastAttemptAt = now
  await runProvision()
}

/* ===== 04-progress ===== */

async function runProvision() {
  logger.info('[T192] runProvision 入口', {
    bleDeviceId: bleDeviceId.value,
    cloudDeviceId: cloudDeviceId.value,
    bleConnected: deviceStore.bleConnected,
    ssid: enteredSsid.value,
  })
  if (!bleDeviceId.value || !deviceStore.bleConnected) {
    logger.warn('[T192] 前置校验失败：BLE 未连', {
      bleDeviceId: bleDeviceId.value,
      bleConnected: deviceStore.bleConnected,
    })
    uni.showToast({ title: CONNECT.connectFailedToast, icon: 'none' })
    goScan()
    return
  }

  view.value = 'progress'
  provisionCode.value = null
  successHandled = false
  deviceStore.setWifiStatus('configuring')

  try {
    uni.showLoading({ title: CONNECT.sendingToast, mask: true })
    // provision_key 原文不入日志（实时日志后台可见）
    const { provision_key_hex } = await getProvisionKey(cloudDeviceId.value)
    uni.hideLoading()
    logger.info('[T192] provision-key 就绪', { keyBytes: provision_key_hex.length / 2 })

    // 先订阅 B512 Notify，再写 B511，否则首帧状态（0/1）会丢
    onWifiStatus(handleStatus, bleDeviceId.value)

    const payload = encryptWifiPayload(enteredSsid.value, enteredPwd, provision_key_hex, deviceStore.nextWifiSeq())
    await writeWifiConfigV2(bleDeviceId.value, payload)
    logger.info('[T192] B511 写入完成，等待设备推送', { payloadBytes: payload.length / 2 })
    armTimeout()
    // #ifdef H5
    startMockWifiStatusSequence(mockSequence)
    // #endif
  } catch (e) {
    uni.hideLoading()
    stopMockWifiStatusSequence()
    stopProvisionTimer()
    logger.error('[T192] 配网下发失败', { msg: (e as Error)?.message ?? String(e), ssid: enteredSsid.value })
    uni.showToast({ title: CONNECT.keyFailToast, icon: 'none' })
    connectPhase.value = 'form'
    view.value = 'connect'
  }
}

function handleStatus(code: number) {
  logger.info('[T192] 收到 B512 状态推送', { code })
  provisionCode.value = code
  deviceStore.updateWifiStatusCode(code)

  if (code === 9) {
    void onProvisionSuccess()
    return
  }
  if (code < 0) {
    stopProvisionTimer()
    stopMockWifiStatusSequence()
    failureType.value = FAILURE_KEY_BY_CODE[code] ?? 'timeout'
    logger.warn('[T192] 设备回失败码', { code, failureType: failureType.value })
    deviceStore.setWifiStatus('failed')
    view.value = 'failure'
    return
  }
  if (code === 3) uni.showToast({ title: PROGRESS.nearlyDoneToast, icon: 'none' })
  armTimeout() // PRD §7A.9：15s「无推送」超时，每收一帧重新计时
}

function armTimeout() {
  stopProvisionTimer()
  timeoutTimer = setTimeout(() => {
    if (successHandled) return
    logger.warn('[T192] 15s 无推送超时', { lastCode: provisionCode.value })
    stopMockWifiStatusSequence()
    failureType.value = 'timeout'
    deviceStore.setWifiStatus('failed')
    view.value = 'failure'
  }, PROVISION_TIMEOUT_MS)
}

function stopProvisionTimer() {
  if (timeoutTimer) {
    clearTimeout(timeoutTimer)
    timeoutTimer = null
  }
}

async function onProvisionSuccess() {
  if (successHandled) return // 固件可能重复 Notify 9
  successHandled = true
  stopProvisionTimer()
  stopMockWifiStatusSequence()
  deviceStore.setWifiStatus('connected')
  await teardownBle()
  logger.info('[T192] 配网成功 → 05', { deviceId: cloudDeviceId.value, ssid: enteredSsid.value })
  view.value = 'success'

  try {
    if (cloudDeviceId.value) await reportDeviceWifi(cloudDeviceId.value, enteredSsid.value)
  } catch (e) {
    logger.warn('[T192] WiFi 状态回写失败', { msg: (e as Error)?.message ?? String(e) })
    uni.showToast({ title: SUCCESS.syncFailToast, icon: 'none' })
  }
}

function onCancelProvision() {
  stopProvisionTimer()
  stopMockWifiStatusSequence()
  uni.showToast({ title: PROGRESS.cancelToast, icon: 'none' })
  connectPhase.value = 'form'
  view.value = 'connect'
}

/* ===== 05-success ===== */

function goMonitor() {
  uni.showToast({ title: SUCCESS.backToast, icon: 'none' })
  setTimeout(() => uni.switchTab({ url: '/pages/monitor/index' }), 800)
}

function goDevicePage() {
  uni.switchTab({ url: '/pages/device/index', fail: () => uni.navigateBack() })
}

/* ===== 06a–06e ===== */

async function onFailurePrimary() {
  const to = FAILURES[failureType.value].primaryTo
  logger.info('[T192] 06 主按钮', { failureType: failureType.value, to })
  if (to === 'connect') {
    connectPhase.value = 'form'
    view.value = 'connect'
    return
  }
  if (to === 'retry') {
    await retryProvision()
    return
  }
  await teardownBle()
  view.value = 'entry'
}

function onFailureSecondary() {
  void retryProvision()
}

/** 「重试配网」= 回到 04 并重跑下发；BLE 已断则先回 02 重新连接 */
async function retryProvision() {
  if (!enteredSsid.value || !enteredPwd) {
    logger.info('[T192] 重试缺凭据，回 03 表单')
    connectPhase.value = 'form'
    view.value = 'connect'
    return
  }
  await runProvision()
}

/* ===== 07-contact ===== */

function goContact(issue: ContactIssue, from: View) {
  contactIssue.value = issue
  contactBack.value = from
  const at = new Date()
  occurredAt.value = formatFeedbackTime(at)
  logger.info('[T192] → 07 联系客服', { issue, from })
  view.value = 'contact'
  void submitFeedback(issue, at)
}

async function submitFeedback(issue: ContactIssue, at: Date) {
  feedbackState.value = 'pending'
  try {
    await submitWifiFailureFeedback({
      patientId: authStore.patientId || '',
      content: buildFailureContent(CONTACT.typeMap[issue], deviceLabel.value, at),
    })
    feedbackState.value = 'ok'
    logger.info('[T192] 反馈提交成功', { issue })
  } catch (e) {
    logger.error('[T192] 配网反馈提交失败', { msg: (e as Error)?.message ?? String(e), issue })
    feedbackState.value = 'failed'
    uni.showToast({ title: CONTACT.submitFailToast, icon: 'none' })
  }
}

function onOpenChat() {
  // 小程序端由 <button open-type="contact"> 原生打开客服会话
  uni.showToast({ title: CONTACT.openingToast, icon: 'none' })
}

function onContactBack() {
  view.value = contactBack.value
}

/* ===== 收尾 ===== */

async function teardownBle() {
  if (!bleDeviceId.value) return
  try {
    await closeBLEConnection(bleDeviceId.value)
  } catch (e) {
    logger.warn('[T192] 关闭 BLE 失败（不阻断收尾）', { msg: (e as Error)?.message ?? String(e) })
  }
  deviceStore.setBleConnected(false)
  bleLinkUp.value = false
}

onUnmounted(() => {
  logger.info('[T192] 页面卸载，收尾', { view: view.value, bleDeviceId: bleDeviceId.value })
  stopProvisionTimer()
  stopBleScan()
  stopMockWifiStatusSequence()
  void teardownBle()
})
</script>

<style scoped>
.wifi-setup { background: #f8fafc; min-height: 100vh; }
</style>
