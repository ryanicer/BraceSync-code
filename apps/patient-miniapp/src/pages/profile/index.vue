<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">我的</text>
    </view>

    <view v-if="loading" class="section">
      <view class="card empty-card"><text class="empty-text">加载中...</text></view>
    </view>

    <template v-else>
      <view v-if="errorMsg" class="section">
        <view class="card empty-card" @click="loadProfile">
          <text class="error-text">{{ errorMsg }}</text>
          <text class="empty-text">点击重试</text>
        </view>
      </view>

      <!-- §7A.5-1 头像（姓名首字占位）+ 姓名 + 患者 ID·年龄·Cobb 角 + 编辑入口（profile.html 基准） -->
      <view class="section">
        <view class="profile-card">
          <view class="avatar">{{ avatarChar }}</view>
          <view class="profile-info">
            <view class="profile-name">{{ nameText }}</view>
            <view class="profile-meta">{{ metaText }}</view>
          </view>
          <view class="profile-edit" @click="openEditSheet">编辑</view>
        </view>
      </view>

      <!-- T223 六项功能菜单（顺序/文案/图标对齐 profile.html） -->
      <view class="section">
        <view class="menu-group">
          <view class="menu-item" @click="comingSoon">
            <text class="menu-ic">👨‍⚕️</text>
            <text class="menu-text">我的医生</text>
            <text class="menu-sub">{{ doctorSubText }}</text>
            <text class="menu-arrow">›</text>
          </view>
          <view class="menu-item" @click="goDevice">
            <text class="menu-ic">📱</text>
            <text class="menu-text">我的设备</text>
            <text class="menu-sub">{{ deviceIdText }}</text>
            <text class="menu-arrow">›</text>
          </view>
          <view class="menu-item" @click="goWearing">
            <text class="menu-ic">⏱️</text>
            <text class="menu-text">佩戴管理</text>
            <text class="menu-sub">时长统计 · 佩戴提醒</text>
            <text class="menu-arrow">›</text>
          </view>
          <view class="menu-item" @click="comingSoon">
            <text class="menu-ic">📝</text>
            <text class="menu-text">矫形日志</text>
            <text class="menu-sub">记录每日感受与调整</text>
            <text class="menu-arrow">›</text>
          </view>
          <view class="menu-item" @click="goReport">
            <text class="menu-ic">📋</text>
            <text class="menu-text">复查报告</text>
            <text class="menu-sub">查看历史复查记录</text>
            <text class="menu-arrow">›</text>
          </view>
          <view class="menu-item" @click="openSettingsSheet">
            <text class="menu-ic">⚙️</text>
            <text class="menu-text">设置</text>
            <text class="menu-arrow">›</text>
          </view>
        </view>
      </view>
    </template>

    <!-- T223 编辑资料表单（字段按 profile.html 设计稿；患者资料写接口后端尚未提供，保存暂提示） -->
    <view class="sheet-overlay" :class="{ 'sheet-overlay-show': editSheetVisible }" @click="closeEditSheet"></view>
    <view class="bottom-sheet" :class="{ 'bottom-sheet-show': editSheetVisible }">
      <view class="sheet-handle"></view>
      <view class="sheet-header">
        <text class="sheet-cancel-text" @click="closeEditSheet">取消</text>
        <text class="sheet-title">编辑个人信息</text>
        <text class="sheet-confirm-text" @click="saveProfile">保存</text>
      </view>
      <view class="sheet-form">
        <view class="form-row">
          <text class="form-label">昵称</text>
          <input v-model="editNickname" class="form-input" type="text" placeholder="请输入昵称" />
        </view>
        <view class="form-row">
          <text class="form-label">性别</text>
          <radio-group class="form-radio-group" @change="onGenderChange">
            <label class="form-radio">
              <radio value="male" :checked="editGender === 'male'" color="#2563EB" style="transform: scale(0.8)" />男
            </label>
            <label class="form-radio">
              <radio value="female" :checked="editGender === 'female'" color="#2563EB" style="transform: scale(0.8)" />女
            </label>
          </radio-group>
        </view>
        <view class="form-row">
          <text class="form-label">年龄</text>
          <input v-model="editAge" class="form-input" type="number" placeholder="请输入年龄" />
        </view>
        <view class="form-row">
          <text class="form-label">身高 (cm)</text>
          <input v-model="editHeight" class="form-input" type="digit" placeholder="请输入身高" />
        </view>
        <view class="form-row">
          <text class="form-label">体重 (kg)</text>
          <input v-model="editWeight" class="form-input" type="digit" placeholder="请输入体重" />
        </view>
        <!-- Cobb角：影像学测量值，由临床端写入，患者不可自助编辑（T230 / Boss 2026-09-17 裁定 B） -->
        <text class="form-section-label">紧急联系人</text>
        <view class="form-row">
          <text class="form-label">紧急联系人</text>
          <input v-model="editEmergencyName" class="form-input" type="text" placeholder="请输入姓名" />
        </view>
        <view class="form-row">
          <text class="form-label">紧急联系人电话</text>
          <input v-model="editEmergencyPhone" class="form-input" type="number" maxlength="11" placeholder="请输入电话" />
        </view>
        <view class="form-row">
          <text class="form-label">与本人关系</text>
          <input v-model="editEmergencyRelation" class="form-input" type="text" placeholder="如：父亲、母亲" />
        </view>
        <text class="form-section-label">手机号由微信授权提供，如需变更请联系客服</text>
      </view>
    </view>

    <!-- T223 设置弹层：承载 T098-Q4 profile Must（客服入口 + 退出登录）与绑定微信；配置家庭 WiFi 入口在设备页 -->
    <view class="sheet-overlay" :class="{ 'sheet-overlay-show': settingsSheetVisible }" @click="closeSettingsSheet"></view>
    <view class="bottom-sheet" :class="{ 'bottom-sheet-show': settingsSheetVisible }">
      <view class="sheet-handle"></view>
      <view class="sheet-title">设置</view>
      <!-- R3：客服复用微信客服会话，不新建消息中心 -->
      <button class="sheet-option" open-type="contact">
        <text class="sheet-option-ic">💬</text>
        <text>联系客服</text>
      </button>
      <view class="sheet-option" @click="bindWechat">
        <text class="sheet-option-ic">🔁</text>
        <text>绑定微信</text>
      </view>
      <view v-if="auth.isLoggedIn" class="sheet-option sheet-option-danger" @click="logout">
        <text class="sheet-option-ic">🚪</text>
        <text>退出登录</text>
      </view>
      <view class="sheet-cancel" @click="closeSettingsSheet">取消</view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { onShow } from '@dcloudio/uni-app'
import { getPatientProfile, updatePatientProfile, type PatientProfile, type PatientProfileUpdate } from '../../api/profile'
import { useAuthStore } from '../../stores/auth'
import { logger } from '../../utils/logger'
import { avatarCharOf, cobbText as fmtCobb, textOrDash } from '../../utils/profile-format'

const auth = useAuthStore()

const profile = ref<PatientProfile | null>(null)
const loading = ref(false)
const errorMsg = ref('')

const avatarChar = computed(() => avatarCharOf(profile.value?.name))
const nameText = computed(() => textOrDash(profile.value?.name))
const patientIdText = computed(() => textOrDash(profile.value?.patientId || auth.patientId))
const deviceIdText = computed(() => textOrDash(profile.value?.deviceId))

// 设计稿头部 meta 一行：「患者ID: X · 14岁 · Cobb角28°」；无诊断时回退 Cobb 角
const metaText = computed(() => {
  const parts = [`患者ID：${patientIdText.value}`]
  if (profile.value?.age != null) parts.push(`${profile.value.age}岁`)
  if (profile.value?.diagnosis) parts.push(profile.value.diagnosis)
  else if (profile.value?.cobbAngle != null) parts.push(fmtCobb(profile.value.cobbAngle))
  return parts.join(' · ')
})

// 我的医生菜单副文案：医生名 + 团队名（原信息卡的医生/团队信息在此保留）
const doctorSubText = computed(() => {
  const name = profile.value?.doctorName
  const team = profile.value?.teamName
  if (name && team) return `${name} · ${team}`
  return name || team || (profile.value?.teamId ? '已绑定' : '未绑定')
})

const editSheetVisible = ref(false)
const editNickname = ref('')
const editGender = ref<'male' | 'female' | ''>('')
const editAge = ref('')
const editHeight = ref('')
const editWeight = ref('')
const editEmergencyName = ref('')
const editEmergencyPhone = ref('')
const editEmergencyRelation = ref('')
const savingProfile = ref(false)

// T223 编辑资料表单（字段按 profile.html 设计稿）；手机号行按 PM 2026-09-16 裁定移除：
// 手机号由微信登录授权写入，患者不可自助改、无任何填号入口
function openEditSheet() {
  editNickname.value = profile.value?.name ?? ''
  editGender.value = profile.value?.gender ?? ''
  editAge.value = profile.value?.age != null ? String(profile.value.age) : ''
  editHeight.value = profile.value?.heightCm != null ? String(profile.value.heightCm) : ''
  editWeight.value = profile.value?.weightKg != null ? String(profile.value.weightKg) : ''
  editEmergencyName.value = profile.value?.emergencyContactName ?? ''
  editEmergencyPhone.value = profile.value?.emergencyContactPhone ?? ''
  editEmergencyRelation.value = profile.value?.emergencyContactRelation ?? ''
  editSheetVisible.value = true
}

function closeEditSheet() {
  editSheetVisible.value = false
}

function onGenderChange(e: unknown) {
  const value = (e as { detail?: { value?: string } })?.detail?.value
  if (value === 'male' || value === 'female') editGender.value = value
}

// 空串→不提交该字段（服务端指针语义 nil=不改）；数字字段非法时忽略
function numOrUndef(v: string): number | undefined {
  if (v.trim() === '') return undefined
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

async function saveProfile() {
  // T226：接 PUT /api/v1/patients/:patientId（白名单+限本人；wx.request 不支持 PATCH 故用 PUT），成功后回读刷新
  if (!profile.value || savingProfile.value) return
  const patientId = profile.value.patientId || auth.patientId
  if (!patientId) {
    uni.showToast({ title: '登录态缺失，请重新登录', icon: 'none' })
    return
  }
  const payload: PatientProfileUpdate = {}
  if (editNickname.value.trim()) payload.name = editNickname.value.trim()
  if (editGender.value) payload.gender = editGender.value
  const age = numOrUndef(editAge.value)
  if (age !== undefined) payload.age = age
  const height = numOrUndef(editHeight.value)
  if (height !== undefined) payload.heightCm = height
  const weight = numOrUndef(editWeight.value)
  if (weight !== undefined) payload.weightKg = weight
  if (editEmergencyName.value.trim()) payload.emergencyContactName = editEmergencyName.value.trim()
  if (editEmergencyPhone.value.trim()) payload.emergencyContactPhone = editEmergencyPhone.value.trim()
  if (editEmergencyRelation.value.trim()) payload.emergencyContactRelation = editEmergencyRelation.value.trim()

  savingProfile.value = true
  try {
    await updatePatientProfile(patientId, payload)
    uni.showToast({ title: '已保存', icon: 'success' })
    editSheetVisible.value = false
    await loadProfile()
    logger.info('[T226] profile saved', { patientId })
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : '保存失败，请稍后重试'
    uni.showToast({ title: msg, icon: 'none' })
    logger.warn('[T226] profile save failed', { error: msg })
  } finally {
    savingProfile.value = false
  }
}

const settingsSheetVisible = ref(false)

function openSettingsSheet() {
  settingsSheetVisible.value = true
}

function closeSettingsSheet() {
  settingsSheetVisible.value = false
}

async function loadProfile() {
  if (!auth.isLoggedIn) {
    profile.value = null
    errorMsg.value = '请先登录'
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    profile.value = await getPatientProfile()
    logger.info('[T187] profile loaded', {
      patientId: profile.value.patientId,
      hasTeam: !!profile.value.teamName,
      hasDoctor: !!profile.value.doctorName,
      hasDevice: !!profile.value.deviceId,
    })
  } catch (e: unknown) {
    errorMsg.value = e instanceof Error ? e.message : '加载个人信息失败'
    logger.warn('[T187] profile load failed', { error: errorMsg.value })
  } finally {
    loading.value = false
  }
}

// tabBar 页常驻，切回「我的」时重新取数（如配网后 deviceId 会变）
onShow(loadProfile)

function goDevice() {
  // 设备管理是 tabBar 页，必须 switchTab
  uni.switchTab({ url: '/pages/device/index' })
}

function goReport() {
  // 复查管理已从 tabBar 移除（T223），改为普通页 navigateTo
  uni.navigateTo({ url: '/pages/report/index' })
}

function goWearing() {
  // 佩戴管理非 tabBar 页（T224）
  uni.navigateTo({ url: '/pages/wearing/index' })
}

function comingSoon() {
  // 矫形日志页未建：只呈现入口，不接坏链
  uni.showToast({ title: '即将开放', icon: 'none' })
}

function bindWechat() {
  // 退出当前登录态 → 走微信登录 → 命中未绑定 → 授权手机号匹配同一档案
  auth.logout()
  uni.reLaunch({ url: '/pages/login/index' })
}

function logout() {
  uni.showModal({
    title: '提示',
    content: '确定要退出登录吗？',
    success: (r) => {
      if (r.confirm) {
        auth.logout()
        uni.reLaunch({ url: '/pages/login/index' })
      }
    },
  })
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 180rpx; background: #f8fafc; }
.page-header { padding: 32rpx 48rpx 16rpx; }
.page-title { font-size: 28rpx; font-weight: 500; color: #94a3b8; letter-spacing: 1rpx; }
.section { padding: 24rpx 40rpx 0; }
.card { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 32rpx; }
.empty-card { text-align: center; }
.empty-text { font-size: 26rpx; color: #94a3b8; display: block; margin-top: 8rpx; }
.error-text { font-size: 26rpx; color: #ef4444; display: block; }

.profile-card { display: flex; align-items: center; gap: 32rpx; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 40rpx; }
.avatar { width: 104rpx; height: 104rpx; border-radius: 50%; background: #2563EB; color: #fff; display: flex; align-items: center; justify-content: center; font-size: 44rpx; font-weight: 500; flex-shrink: 0; }
.profile-info { flex: 1; min-width: 0; }
.profile-name { font-size: 36rpx; font-weight: 500; color: #1e293b; }
.profile-meta { font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.profile-edit { font-size: 26rpx; color: #2563EB; flex-shrink: 0; padding: 8rpx 0 8rpx 16rpx; }

.menu-group { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; overflow: hidden; }
.menu-item { display: flex; align-items: center; gap: 24rpx; padding: 28rpx 32rpx; border-bottom: 1rpx solid #f1f5f9; }
.menu-item:last-child { border-bottom: none; }
.menu-ic { font-size: 32rpx; }
.menu-text { flex: 1; font-size: 28rpx; color: #1e293b; }
.menu-arrow { font-size: 32rpx; color: #cbd5e1; }
.menu-sub { font-size: 22rpx; color: #94a3b8; flex-shrink: 0; max-width: 320rpx; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ====== T223 编辑资料 bottom sheet（对齐 profile.html） ====== */
.sheet-overlay { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.4); z-index: 200; opacity: 0; pointer-events: none; transition: opacity 0.25s; }
.sheet-overlay-show { opacity: 1; pointer-events: auto; }
.bottom-sheet { position: fixed; bottom: 0; left: 0; right: 0; background: #fff; border-radius: 32rpx 32rpx 0 0; z-index: 201; transform: translateY(100%); transition: transform 0.3s cubic-bezier(0.32, 0.72, 0, 1); padding-bottom: env(safe-area-inset-bottom, 20rpx); max-height: 70vh; overflow-y: auto; }
.bottom-sheet-show { transform: translateY(0); }
.sheet-handle { width: 72rpx; height: 8rpx; background: #e2e8f0; border-radius: 4rpx; margin: 24rpx auto 16rpx; }
.sheet-header { display: flex; align-items: center; justify-content: space-between; padding: 0 40rpx 28rpx; }
.sheet-title { flex: 1; font-size: 32rpx; font-weight: 500; color: #1e293b; text-align: center; }
.sheet-cancel-text { font-size: 30rpx; color: #94a3b8; }
.sheet-confirm-text { font-size: 30rpx; color: #2563EB; font-weight: 500; }
.sheet-form { padding: 8rpx 40rpx 40rpx; }
.form-row { display: flex; align-items: center; padding: 24rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.form-label { font-size: 28rpx; color: #1e293b; width: 220rpx; flex-shrink: 0; }
.form-input { flex: 1; height: 80rpx; min-height: 80rpx; box-sizing: border-box; border: 1rpx solid #e2e8f0; border-radius: 16rpx; padding: 0 24rpx; font-size: 28rpx; color: #1e293b; }
.form-radio-group { display: flex; gap: 32rpx; }
.form-radio { font-size: 28rpx; color: #64748b; display: flex; align-items: center; }
.form-section-label { display: block; font-size: 24rpx; color: #94a3b8; padding: 28rpx 0 8rpx; font-weight: 500; }

/* ====== T223 设置弹层选项（对齐 profile.html sheet-option） ====== */
.sheet-option { display: flex; align-items: center; gap: 28rpx; width: 100%; padding: 32rpx 48rpx; font-size: 30rpx; color: #1e293b; background: #fff; border: none; border-radius: 0; line-height: normal; text-align: left; box-sizing: border-box; }
.sheet-option::after { border: none; }
.sheet-option:active { background: #f1f5f9; }
.sheet-option-ic { font-size: 32rpx; }
.sheet-option-danger, .sheet-option-danger .sheet-option-ic { color: #ef4444; }
.sheet-cancel { text-align: center; padding: 28rpx; font-size: 30rpx; color: #94a3b8; border-top: 1rpx solid #e2e8f0; margin-top: 16rpx; }
</style>
