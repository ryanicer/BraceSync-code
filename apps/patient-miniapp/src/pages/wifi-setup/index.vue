<template>
  <view class="page">
    <!-- 导航栏（非入口/成功页显示） -->
    <view class="nav-bar" v-if="view !== 'entry' && view !== 'success'">
      <view class="nav-back" @click="onBack">
        <text class="back-icon">‹</text>
      </view>
      <text class="nav-title">{{ navTitle }}</text>
    </view>

    <!-- 01 入口 + 前置检查 -->
    <view v-if="view === 'entry'" class="entry-view">
      <view class="entry-card">
        <view class="entry-label">当前设备</view>
        <view class="entry-name">矫形支具监测器</view>
        <view class="entry-meta">设备编号 {{ deviceName || 'BSYNC-701001' }} · 未连接家庭网络</view>
        <view class="btn-primary" @click="startProvision"><text>开始配置家庭 WiFi</text></view>
      </view>

      <view class="section">
        <view class="prep-title">配置前准备</view>
        <view class="prep-item">
          <view class="prep-ic blue">📶</view>
          <view class="prep-txt">
            <view class="prep-name">打开手机蓝牙</view>
            <view class="prep-sub">用于近距离发现您的监测器</view>
          </view>
          <text class="prep-status" :class="btReady ? 'status-done' : 'status-todo'">{{ btReady ? '已开启' : '未开启' }}</text>
        </view>
        <view class="prep-item">
          <view class="prep-ic amber">📍</view>
          <view class="prep-txt">
            <view class="prep-name">允许位置权限</view>
            <view class="prep-sub">安卓系统需要此权限来扫描附近设备</view>
          </view>
          <text class="prep-status" :class="locReady ? 'status-done' : 'status-todo'">{{ locReady ? '已授权' : '未授权' }}</text>
        </view>
        <view class="prep-item">
          <view class="prep-ic blue">⚡</view>
          <view class="prep-txt">
            <view class="prep-name">设备已上电</view>
            <view class="prep-sub">监测器指示灯闪烁，表示处于配网状态</view>
          </view>
          <text class="prep-status status-done">已就绪</text>
        </view>
      </view>

      <view class="hint-box">
        <text class="hint-text"><text class="hint-bold">提示：</text>本设备仅支持 2.4GHz 家庭 WiFi。如果您家是双频路由器，请连接 2.4G 频段（通常名称带 "2.4G" 字样）。</text>
      </view>
    </view>

    <!-- 02 扫描 -->
    <view v-if="view === 'scan'" class="scan-view">
      <view class="notice-2g">
        <text class="notice-ic">⚠️</text>
        <text class="notice-text">本设备仅支持 <text class="bold">2.4GHz</text> 家庭 WiFi。若您家是双频路由器，请选择名称带 "2.4G" 的网络。</text>
      </view>

      <view class="scan-area">
        <view class="radar">
          <view class="ring r1"></view>
          <view class="ring r2"></view>
          <view class="ring r3"></view>
          <view class="radar-core">📶</view>
        </view>
        <view class="scan-text">{{ devices.length > 0 ? `已发现 ${devices.length} 台设备` : '正在搜索附近的监测器…' }}</view>
        <view class="scan-sub">请确保设备已上电且靠近手机</view>
      </view>

      <view v-if="devices.length > 0" class="device-list">
        <view class="device-item" v-for="d in devices" :key="d.deviceId" @click="selectDevice(d)">
          <view class="device-ic">📱</view>
          <view class="device-info">
            <view class="device-name">{{ d.name }}</view>
            <text class="device-tag">{{ d.RSSI > -60 ? '信号良好' : '信号一般' }}</text>
          </view>
          <text class="device-arrow">›</text>
        </view>
      </view>

      <view v-if="!scanning && devices.length === 0" class="empty-state">
        <view class="empty-ic">🔍</view>
        <view class="empty-title">暂时没找到您的监测器</view>
        <view class="empty-tips">
          <view class="empty-tip"><span class="empty-tip-num">1</span><span>确认设备已上电，指示灯处于闪烁状态</span></view>
          <view class="empty-tip"><span class="empty-tip-num">2</span><span>将手机靠近设备（建议 1 米以内）</span></view>
          <view class="empty-tip"><span class="empty-tip-num">3</span><span>确认选择了 2.4GHz WiFi（不支持 5G）</span></view>
          <view class="empty-tip"><span class="empty-tip-num">4</span><span>仍不行？请联系您的技师协助</span></view>
        </view>
        <view class="retry-btn" @click="startScan"><text>重新搜索</text></view>
      </view>
    </view>

    <!-- 03 连接 + WiFi 输入 -->
    <view v-if="view === 'connect'" class="connect-view">
      <view v-if="connecting" class="connecting">
        <view class="connect-ic"><view class="spinner"></view></view>
        <view class="connect-text">正在连接您的监测器…</view>
        <view class="connect-sub">请保持手机靠近设备</view>
        <view class="device-card">
          <view class="device-ic-sm">📱</view>
          <view>
            <view class="device-name-sm">{{ selectedDevice?.name }}</view>
            <text class="device-tag">连接中</text>
          </view>
        </view>
      </view>

      <view v-else class="form-view">
        <view class="device-card-connected">
          <view class="device-ic-sm connected">📱</view>
          <view>
            <view class="device-name-sm">{{ selectedDevice?.name }}</view>
            <text class="device-tag connected">已连接</text>
          </view>
        </view>

        <view class="form-section">
          <view class="form-label">家庭 WiFi 名称</view>
          <view class="input-row">
            <text class="input-ic">📶</text>
            <input class="input-field" v-model="ssid" placeholder="请输入 WiFi 名称" />
          </view>

          <view class="form-label">WiFi 密码</view>
          <view class="input-row">
            <text class="input-ic">🔒</text>
            <input class="input-field" :password="!showPwd" v-model="password" placeholder="请输入 WiFi 密码" />
            <text class="pwd-toggle" @click="showPwd = !showPwd">{{ showPwd ? '🙈' : '👁️' }}</text>
          </view>

          <view class="tips">
            仅支持 <text class="bold">2.4GHz</text> WiFi。密码区分大小写，请仔细输入。
          </view>
        </view>

        <view class="btn-primary" @click="startConfig"><text>开始配网</text></view>
      </view>
    </view>

    <!-- 04 进度 -->
    <view v-if="view === 'progress'" class="progress-view">
      <view class="progress-area">
        <view class="progress-ring">
          <view class="ring-center"><text class="percent">{{ progressPct }}%</text></view>
        </view>
        <view class="progress-title">{{ progressTitle }}</view>
        <view class="progress-sub">{{ progressSub }}</view>
      </view>

      <view class="device-card-connected">
        <view class="device-ic-sm connected">📱</view>
        <view>
          <view class="device-name-sm">{{ selectedDevice?.name }}</view>
          <text class="device-tag">配置中</text>
        </view>
      </view>

      <view class="step-list">
        <view class="step" v-for="(step, i) in steps" :key="i">
          <view class="step-ic" :class="stepStatus(i).cls">
            <text v-if="stepStatus(i).cls === 'done'">✓</text>
            <view v-else-if="stepStatus(i).cls === 'active'" class="spinner-sm"></view>
            <text v-else>{{ i + 1 }}</text>
          </view>
          <view class="step-txt" :class="stepStatus(i).cls">{{ step }}</view>
          <view class="step-status" :class="stepStatus(i).cls">{{ stepStatus(i).text }}</view>
        </view>
      </view>

      <view class="cancel-btn" @click="cancelProvision"><text>取消配置</text></view>
    </view>

    <!-- 05 成功 -->
    <view v-if="view === 'success'" class="success-view">
      <view class="success-anim">
        <view class="check-circle"><text class="check-icon">✓</text></view>
        <view class="success-title">配网成功</view>
        <view class="success-sub">您的监测器已连接家庭网络</view>
      </view>

      <view class="info-card">
        <view class="info-label">连接信息</view>
        <view class="info-row">
          <view class="info-key">设备</view>
          <view class="info-val">{{ selectedDevice?.name || deviceName }}</view>
        </view>
        <view class="info-row">
          <view class="info-key">家庭网络</view>
          <view class="info-val">{{ ssid }}</view>
        </view>
        <view class="info-row">
          <view class="info-key">连接状态</view>
          <view class="info-val"><text class="tag-online">● 已联网</text></view>
        </view>
      </view>

      <view class="benefit">
        <text class="benefit-bold">现在可以：</text>
        <text class="benefit-line">· 实时查看孩子的支具佩戴压力数据</text>
        <text class="benefit-line">· 接收佩戴时长与异常提醒</text>
        <text class="benefit-line">· 与医生同步复查数据</text>
      </view>

      <view class="btn-primary" @click="goHome"><text>查看佩戴数据</text></view>
      <view class="btn-secondary" @click="goDevice"><text>返回设备管理</text></view>
    </view>

    <!-- 06 失败 -->
    <view v-if="view === 'failure'" class="failure-view">
      <view class="fail-area">
        <view class="fail-ic" :class="failIconCls">{{ failIcon }}</view>
        <view class="fail-title">{{ failTitle }}</view>
        <view class="fail-desc">{{ failDesc }}</view>
      </view>

      <view class="action-card">
        <view class="action-label">建议操作</view>
        <view class="action-item" v-for="(a, i) in failActions" :key="i">
          <span class="action-num">{{ i + 1 }}</span>
          <span class="action-text">{{ a }}</span>
        </view>
      </view>

      <view class="btn-row">
        <view class="btn-primary" @click="retryInput"><text>重新输入密码</text></view>
        <view class="btn-secondary" @click="retryProvision"><text>重试配网</text></view>
        <view class="btn-help" @click="goContact"><text>联系技师</text></view>
      </view>
    </view>

    <!-- 07 联系客服 -->
    <view v-if="view === 'contact'" class="contact-view">
      <view class="contact-content">
        <view class="icon-circle">💬</view>
        <view class="title">正在为您联系客服</view>
        <view class="subtitle">我们已记录本次配网遇到的问题<br>客服会尽快与您联系</view>

        <view class="context-card">
          <view class="context-title">已附上的信息</view>
          <view class="context-row">
            <span class="context-label">问题类型</span>
            <span class="context-value">{{ failTitle }}</span>
          </view>
          <view class="context-row">
            <span class="context-label">设备</span>
            <span class="context-value">{{ selectedDevice?.name || deviceName }}</span>
          </view>
          <view class="context-row">
            <span class="context-label">发生时间</span>
            <span class="context-value">{{ contactTime }}</span>
          </view>
        </view>

        <button class="btn-primary" open-type="contact">打开客服对话</button>
        <view class="btn-secondary" @click="goBack"><text>稍后再说</text></view>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { onLoad, onUnload } from '@dcloudio/uni-app'
import { scanBleDevices, stopBleScan, provisionBleDevice, type BleDeviceInfo } from '../../utils/ble'
import { reportWifiStatus } from '../../api/device'

type View = 'entry' | 'scan' | 'connect' | 'progress' | 'success' | 'failure' | 'contact'

const view = ref<View>('entry')
const deviceName = ref('BSYNC-701001')
const btReady = ref(false)
const locReady = ref(false)
const scanning = ref(false)
const devices = ref<BleDeviceInfo[]>([])
const selectedDevice = ref<BleDeviceInfo | null>(null)
const connecting = ref(true)
const ssid = ref('')
const password = ref('')
const showPwd = ref(false)
const currentStep = ref(0)
const failType = ref('')
const failTitle = ref('')
const failDesc = ref('')
const failIcon = ref('')
const failIconCls = ref('red')
const failActions = ref<string[]>([])
const contactTime = ref('')

const steps = ['正在同步设置', '正在连接家庭 WiFi', '网络连接成功', '正在连接服务器']

const navTitle = computed(() => {
  const map: Record<string, string> = {
    scan: '搜索监测器',
    connect: '连接监测器',
    progress: '正在配置',
    failure: '配网未成功',
    contact: '联系客服',
  }
  return map[view.value] || ''
})

const progressPct = computed(() => {
  const pcts = [10, 35, 65, 90]
  return pcts[currentStep.value] || 10
})

const progressTitle = computed(() => {
  const titles = ['正在同步设置…', '正在连接家庭 WiFi', '网络连接成功', '正在连接服务器']
  return titles[currentStep.value] || '正在同步设置…'
})

const progressSub = computed(() => {
  const subs = ['已发送 WiFi 信息，设备正在处理', '设备尝试连接您的家庭网络', '设备已获取网络地址', '设备正在与云端建立连接']
  return subs[currentStep.value] || ''
})

function stepStatus(i: number) {
  if (i < currentStep.value) return { cls: 'done', text: '完成' }
  if (i === currentStep.value) return { cls: 'active', text: '进行中' }
  return { cls: 'pending', text: '等待中' }
}

onLoad((query) => {
  if (query?.deviceId) deviceName.value = query.deviceId as string
  checkPermissions()
})

onUnload(() => {
  stopBleScan()
})

function checkPermissions() {
  // 蓝牙状态
  ;(uni as any).getBluetoothAdapterState?.({
    success: (res: any) => { btReady.value = res.available },
    fail: () => { btReady.value = false },
  })
  // 位置权限（简化：安卓需授权）
  uni.getSetting({
    success: (res) => { locReady.value = res.authSetting['scope.userLocation'] !== false },
  })
}

function goBack() {
  uni.navigateBack({ fail: () => uni.reLaunch({ url: '/pages/device/index' }) })
}

function onBack() {
  if (view.value === 'scan') { view.value = 'entry'; stopBleScan(); return }
  if (view.value === 'connect') { view.value = 'scan'; return }
  if (view.value === 'progress') { return } // 不允许中途返回
  if (view.value === 'failure') { view.value = 'connect'; return }
  if (view.value === 'contact') { view.value = 'failure'; return }
  goBack()
}

function startProvision() {
  if (!btReady.value) {
    uni.showModal({
      title: '请打开手机蓝牙',
      content: '配置家庭 WiFi 需要通过蓝牙近距离连接您的监测器。请在手机系统设置中打开蓝牙。',
      confirmText: '去打开',
      success: (r) => { if (r.confirm) view.value = 'scan' },
    })
    return
  }
  view.value = 'scan'
  startScan()
}

async function startScan() {
  scanning.value = true
  devices.value = []
  try {
    await scanBleDevices((list) => {
      devices.value = list
    }, 10000)
  } catch (e) {
    uni.showToast({ title: e instanceof Error ? e.message : '扫描失败', icon: 'none' })
  } finally {
    scanning.value = false
  }
}

async function selectDevice(d: BleDeviceInfo) {
  selectedDevice.value = d
  view.value = 'connect'
  connecting.value = true
  try {
    // 连接由 provisionBleDevice 内部处理；这里仅展示连接中态后切表单
    await new Promise((r) => setTimeout(r, 800))
    connecting.value = false
  } catch {
    connecting.value = false
  }
}

async function startConfig() {
  if (!ssid.value.trim()) {
    uni.showToast({ title: '请输入 WiFi 名称', icon: 'none' })
    return
  }
  if (!selectedDevice.value) return

  view.value = 'progress'
  currentStep.value = 0

  const result = await provisionBleDevice(selectedDevice.value.deviceId, ssid.value, password.value)

  if (result.success) {
    view.value = 'success'
    reportWifiStatus({
      deviceId: selectedDevice.value.deviceId,
      status: 'success',
      ssid: ssid.value,
    }).catch(() => {})
  } else {
    handleFailure(result.error_code || '', result.error_message || '')
  }
}

function handleFailure(code: string, msg: string) {
  view.value = 'failure'
  failType.value = code

  const map: Record<string, { title: string; desc: string; icon: string; cls: string; actions: string[] }> = {
    '-1': { title: 'WiFi 密码不正确', desc: '设备无法连接到您的家庭网络，可能是 WiFi 密码输入有误。', icon: '🔒', cls: 'red', actions: ['回到上一步，重新输入 WiFi 密码（注意区分大小写）', '确认选择的是 2.4GHz 网络', '重新尝试配网'] },
    '-2': { title: '找不到 WiFi 网络', desc: '设备未能扫描到您输入的 WiFi 网络。', icon: '📡', cls: 'red', actions: ['确认 WiFi 名称拼写正确', '确认选择的是 2.4GHz 网络', '靠近路由器后重试'] },
    '-3': { title: '网络地址获取失败', desc: '设备连接 WiFi 后无法获取网络地址。', icon: '🌐', cls: 'amber', actions: ['重启路由器后重试', '确认路由器 DHCP 已开启', '重新尝试配网'] },
    '-4': { title: '服务器不可达', desc: '设备已连接 WiFi，但无法连接到云端服务器。', icon: '☁️', cls: 'amber', actions: ['确认家庭网络可正常上网', '稍后重试', '联系技师协助'] },
    TIMEOUT: { title: '设备响应超时', desc: '长时间没有收到设备的反馈，可能是设备离手机太远或已断电。', icon: '⏱️', cls: 'amber', actions: ['确认设备已上电（指示灯闪烁）', '将手机靠近设备（1 米以内）', '重新尝试配网'] },
    ERROR: { title: '配网失败', desc: msg || '配网过程中发生未知错误。', icon: '⚠️', cls: 'amber', actions: ['确认设备已上电且靠近手机', '重新尝试配网', '联系技师协助'] },
  }

  const info = map[code] || map.ERROR
  failTitle.value = info.title
  failDesc.value = info.desc
  failIcon.value = info.icon
  failIconCls.value = info.cls
  failActions.value = info.actions

  if (selectedDevice.value) {
    reportWifiStatus({
      deviceId: selectedDevice.value.deviceId,
      status: 'failed',
      ssid: ssid.value,
      error_code: code,
      error_message: info.title,
    }).catch(() => {})
  }
}

function cancelProvision() {
  uni.showModal({
    title: '取消配置',
    content: '确定要取消本次配网吗？',
    success: (r) => { if (r.confirm) view.value = 'entry' },
  })
}

function retryInput() {
  view.value = 'connect'
  connecting.value = false
}

function retryProvision() {
  startConfig()
}

function goContact() {
  contactTime.value = new Date().toLocaleString('zh-CN', { hour12: false })
  view.value = 'contact'
}

function goHome() {
  uni.switchTab({ url: '/pages/monitor/index' })
}

function goDevice() {
  uni.switchTab({ url: '/pages/device/index' })
}
</script>

<style scoped>
.page { min-height: 100%; background: #f8fafc; }
.nav-bar { height: 88rpx; display: flex; align-items: center; padding: 0 32rpx; position: relative; border-bottom: 1rpx solid #f1f5f9; }
.nav-back { width: 64rpx; height: 64rpx; display: flex; align-items: center; justify-content: center; }
.back-icon { font-size: 48rpx; color: #1e293b; }
.nav-title { position: absolute; left: 0; right: 0; text-align: center; font-size: 32rpx; font-weight: 600; color: #1e293b; }

/* 入口 */
.entry-card { margin: 48rpx 40rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 40rpx; }
.entry-label { font-size: 24rpx; color: #94a3b8; }
.entry-name { font-size: 36rpx; font-weight: 600; color: #1e293b; margin-top: 12rpx; }
.entry-meta { font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.section { padding: 0 40rpx; margin-top: 48rpx; }
.prep-title { font-size: 28rpx; font-weight: 600; color: #1e293b; margin-bottom: 24rpx; }
.prep-item { display: flex; align-items: center; gap: 24rpx; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; margin-bottom: 20rpx; }
.prep-ic { width: 72rpx; height: 72rpx; border-radius: 20rpx; display: flex; align-items: center; justify-content: center; font-size: 36rpx; flex-shrink: 0; }
.prep-ic.blue { background: #eff6ff; }
.prep-ic.amber { background: #fef3c7; }
.prep-txt { flex: 1; }
.prep-name { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.prep-sub { font-size: 22rpx; color: #94a3b8; margin-top: 4rpx; }
.prep-status { font-size: 22rpx; font-weight: 500; }
.status-done { color: #10ac84; }
.status-todo { color: #f59e0b; }
.hint-box { margin: 32rpx 40rpx 0; background: #eff6ff; border: 1rpx solid #bfdbfe; border-radius: 20rpx; padding: 24rpx; }
.hint-text { font-size: 24rpx; color: #2563eb; line-height: 1.6; }
.hint-bold { font-weight: 600; }

/* 扫描 */
.notice-2g { margin: 24rpx 40rpx 0; background: #fff7ed; border: 1rpx solid #fed7aa; border-radius: 20rpx; padding: 24rpx 28rpx; display: flex; gap: 16rpx; }
.notice-ic { font-size: 28rpx; flex-shrink: 0; }
.notice-text { font-size: 24rpx; color: #c2410c; line-height: 1.6; }
.bold { font-weight: 600; }
.scan-area { text-align: center; padding: 64rpx 0 40rpx; }
.radar { width: 240rpx; height: 240rpx; margin: 0 auto; position: relative; display: flex; align-items: center; justify-content: center; }
.ring { position: absolute; border: 4rpx solid rgba(37,99,235,0.25); border-radius: 50%; }
.ring.r1 { width: 144rpx; height: 144rpx; animation: pulse 2s ease-out infinite; }
.ring.r2 { width: 192rpx; height: 192rpx; animation: pulse 2s ease-out 0.6s infinite; }
.ring.r3 { width: 240rpx; height: 240rpx; animation: pulse 2s ease-out 1.2s infinite; }
@keyframes pulse { 0% { transform: scale(0.5); opacity: 0.8; } 100% { transform: scale(1.2); opacity: 0; } }
.radar-core { width: 112rpx; height: 112rpx; border-radius: 50%; background: linear-gradient(135deg, #2563EB, #6366f1); display: flex; align-items: center; justify-content: center; font-size: 48rpx; z-index: 2; }
.scan-text { font-size: 30rpx; font-weight: 500; color: #1e293b; margin-top: 32rpx; }
.scan-sub { font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.device-list { padding: 0 40rpx; margin-top: 16rpx; }
.device-item { display: flex; align-items: center; gap: 24rpx; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; margin-bottom: 20rpx; }
.device-ic { width: 80rpx; height: 80rpx; border-radius: 20rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; font-size: 40rpx; flex-shrink: 0; }
.device-info { flex: 1; min-width: 0; }
.device-name { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.device-tag { font-size: 20rpx; color: #10ac84; background: #e8f5e9; padding: 2rpx 12rpx; border-radius: 12rpx; display: inline-block; margin-top: 6rpx; }
.device-arrow { color: #cbd5e1; font-size: 36rpx; }
.empty-state { text-align: center; padding: 80rpx 64rpx; }
.empty-ic { width: 160rpx; height: 160rpx; border-radius: 40rpx; background: #f1f5f9; margin: 0 auto; display: flex; align-items: center; justify-content: center; font-size: 72rpx; }
.empty-title { font-size: 30rpx; font-weight: 500; color: #475569; margin-top: 32rpx; }
.empty-tips { margin-top: 32rpx; text-align: left; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; }
.empty-tip { font-size: 24rpx; color: #64748b; line-height: 1.7; padding: 12rpx 0; display: flex; gap: 16rpx; }
.empty-tip-num { width: 36rpx; height: 36rpx; border-radius: 50%; background: #eff6ff; color: #2563EB; font-size: 20rpx; font-weight: 600; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.retry-btn { margin: 40rpx 40rpx 0; padding: 24rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.retry-btn text { color: #fff; font-size: 30rpx; font-weight: 500; }

/* 连接 */
.connecting { text-align: center; padding: 120rpx 64rpx 48rpx; }
.connect-ic { width: 160rpx; height: 160rpx; border-radius: 50%; background: linear-gradient(135deg, #2563EB, #6366f1); margin: 0 auto; display: flex; align-items: center; justify-content: center; }
.spinner { width: 72rpx; height: 72rpx; border: 6rpx solid rgba(255,255,255,0.3); border-top-color: #fff; border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.connect-text { font-size: 32rpx; font-weight: 500; color: #1e293b; margin-top: 40rpx; }
.connect-sub { font-size: 24rpx; color: #94a3b8; margin-top: 12rpx; }
.device-card { margin: 0 40rpx; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; display: flex; align-items: center; gap: 24rpx; }
.device-card-connected { margin: 40rpx 40rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 28rpx; display: flex; align-items: center; gap: 24rpx; }
.device-ic-sm { width: 72rpx; height: 72rpx; border-radius: 20rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; font-size: 36rpx; }
.device-ic-sm.connected { background: #e8f5e9; }
.device-name-sm { font-size: 28rpx; font-weight: 500; color: #1e293b; }
.device-tag.connected { color: #10ac84; }
.form-section { padding: 0 40rpx; margin-top: 48rpx; }
.form-label { font-size: 26rpx; font-weight: 500; color: #475569; margin-bottom: 16rpx; display: block; }
.input-row { display: flex; align-items: center; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 0 28rpx; margin-bottom: 28rpx; }
.input-ic { color: #94a3b8; margin-right: 20rpx; font-size: 32rpx; }
.input-field { flex: 1; padding: 28rpx 0; font-size: 30rpx; color: #1e293b; }
.pwd-toggle { color: #94a3b8; padding: 8rpx; font-size: 32rpx; }
.tips { margin: 32rpx 0; background: #eff6ff; border: 1rpx solid #bfdbfe; border-radius: 20rpx; padding: 24rpx; font-size: 24rpx; color: #2563eb; line-height: 1.6; }

/* 进度 */
.progress-area { text-align: center; padding: 96rpx 64rpx 48rpx; }
.progress-ring { width: 200rpx; height: 200rpx; margin: 0 auto; border-radius: 50%; border: 16rpx solid #e2e8f0; border-top-color: #2563EB; animation: spin 1.2s linear infinite; display: flex; align-items: center; justify-content: center; }
.ring-center { position: absolute; }
.percent { font-size: 36rpx; font-weight: 600; color: #2563EB; }
.progress-title { font-size: 34rpx; font-weight: 600; color: #1e293b; margin-top: 48rpx; }
.progress-sub { font-size: 26rpx; color: #94a3b8; margin-top: 12rpx; }
.step-list { margin: 48rpx 40rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; overflow: hidden; }
.step { display: flex; align-items: center; gap: 24rpx; padding: 28rpx 32rpx; border-bottom: 1rpx solid #f1f5f9; }
.step:last-child { border-bottom: none; }
.step-ic { width: 44rpx; height: 44rpx; border-radius: 50%; display: flex; align-items: center; justify-content: center; flex-shrink: 0; font-size: 24rpx; font-weight: 600; }
.step-ic.done { background: #dcfce7; color: #16a34a; }
.step-ic.active { background: #2563EB; color: #fff; }
.step-ic.pending { background: #f1f5f9; color: #94a3b8; }
.step-txt { flex: 1; font-size: 28rpx; }
.step-txt.done { color: #1e293b; }
.step-txt.active { color: #1e293b; font-weight: 500; }
.step-txt.pending { color: #94a3b8; }
.step-status { font-size: 22rpx; flex-shrink: 0; }
.step-status.done { color: #16a34a; }
.step-status.active { color: #2563EB; }
.step-status.pending { color: #cbd5e1; }
.spinner-sm { width: 24rpx; height: 24rpx; border: 4rpx solid rgba(37,99,235,0.3); border-top-color: #2563EB; border-radius: 50%; animation: spin 0.7s linear infinite; }
.cancel-btn { margin: 48rpx 40rpx 0; padding: 24rpx; background: #f1f5f9; border-radius: 24rpx; text-align: center; }
.cancel-btn text { color: #475569; font-size: 28rpx; font-weight: 500; }

/* 成功 */
.success-view { padding-bottom: 80rpx; }
.success-anim { margin-top: 180rpx; text-align: center; }
.check-circle { width: 176rpx; height: 176rpx; border-radius: 50%; background: #22c55e; margin: 0 auto; display: flex; align-items: center; justify-content: center; box-shadow: 0 16rpx 48rpx rgba(34,197,94,0.35); }
.check-icon { font-size: 88rpx; color: #fff; }
.success-title { font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 48rpx; }
.success-sub { font-size: 28rpx; color: #64748b; margin-top: 16rpx; }
.info-card { margin: 72rpx 40rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 36rpx; }
.info-label { font-size: 24rpx; color: #94a3b8; margin-bottom: 24rpx; }
.info-row { display: flex; align-items: center; padding: 20rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.info-row:last-child { border-bottom: none; }
.info-key { font-size: 26rpx; color: #64748b; width: 160rpx; flex-shrink: 0; }
.info-val { font-size: 28rpx; color: #1e293b; font-weight: 500; flex: 1; }
.tag-online { color: #10ac84; font-size: 24rpx; }
.benefit { margin: 48rpx 40rpx 0; background: #eff6ff; border: 1rpx solid #bfdbfe; border-radius: 20rpx; padding: 28rpx; }
.benefit-bold { font-size: 26rpx; color: #2563eb; font-weight: 600; display: block; margin-bottom: 12rpx; }
.benefit-line { font-size: 24rpx; color: #2563eb; line-height: 1.8; display: block; }

/* 失败 */
.fail-area { text-align: center; padding: 120rpx 64rpx 48rpx; }
.fail-ic { width: 160rpx; height: 160rpx; border-radius: 50%; margin: 0 auto; display: flex; align-items: center; justify-content: center; font-size: 72rpx; }
.fail-ic.red { background: #fee2e2; }
.fail-ic.amber { background: #fef3c7; }
.fail-title { font-size: 40rpx; font-weight: 600; color: #1e293b; margin-top: 40rpx; }
.fail-desc { font-size: 28rpx; color: #64748b; line-height: 1.7; margin-top: 20rpx; padding: 0 16rpx; }
.action-card { margin: 56rpx 40rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 32rpx; }
.action-label { font-size: 24rpx; color: #94a3b8; margin-bottom: 20rpx; }
.action-item { display: flex; gap: 20rpx; padding: 16rpx 0; }
.action-num { width: 36rpx; height: 36rpx; border-radius: 50%; background: #eff6ff; color: #2563EB; font-size: 20rpx; font-weight: 600; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-top: 2rpx; }
.action-text { font-size: 26rpx; color: #475569; line-height: 1.6; flex: 1; }
.btn-row { margin: 48rpx 40rpx 0; display: flex; flex-direction: column; gap: 20rpx; }
.btn-primary { padding: 26rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-secondary { padding: 26rpx; background: #f1f5f9; border-radius: 24rpx; text-align: center; }
.btn-secondary text { color: #475569; font-size: 28rpx; font-weight: 500; }
.btn-help { padding: 26rpx; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; text-align: center; }
.btn-help text { color: #2563EB; font-size: 28rpx; font-weight: 500; }

/* 联系客服 */
.contact-content { padding: 48rpx 40rpx; }
.icon-circle { width: 144rpx; height: 144rpx; border-radius: 50%; background: #eff6ff; display: flex; align-items: center; justify-content: center; margin: 64rpx auto 40rpx; font-size: 72rpx; }
.title { font-size: 40rpx; font-weight: 700; color: #1e293b; text-align: center; margin-bottom: 16rpx; }
.subtitle { font-size: 28rpx; color: #64748b; text-align: center; line-height: 1.6; margin-bottom: 56rpx; }
.context-card { background: #fff; border-radius: 28rpx; padding: 32rpx; margin-bottom: 40rpx; border: 1rpx solid #e2e8f0; }
.context-title { font-size: 26rpx; color: #94a3b8; margin-bottom: 24rpx; }
.context-row { display: flex; justify-content: space-between; padding: 16rpx 0; font-size: 28rpx; border-bottom: 1rpx solid #f1f5f9; }
.context-row:last-child { border-bottom: none; }
.context-label { color: #64748b; }
.context-value { color: #1e293b; font-weight: 500; text-align: right; max-width: 60%; }
</style>
