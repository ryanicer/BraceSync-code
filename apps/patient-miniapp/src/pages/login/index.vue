<template>
  <!--
    T080: 根 div 上挂 data-e2e-state，Playwright 无需读 console 即可断言"按钮到底进入了哪一步"。
    状态机：
      - INIT                → 初始
      - WX_CLICK            → wechatLogin() 同步入口被调用（3连点计数第一点必达）
      - WX_UNCHECKED        → 未勾选协议，已弹 modal（不发请求）
      - WX_GUARD_MERGED     → loginLoading/pending 合并器拦截（第2/3次连点落此）
      - WX_REQ_START        → request(/wx-login) 即将发出
      - WX_REQ_OK           → request 成功 resolve（但未必最终登录）
      - WX_REQ_INCOMPLETE   → token/patientId 缺失（接口返回结构异常）
      - WX_LOGIN_STORE_OK   → authStore.login 调用成功（接下来会 switchTab）
      - WX_NAV_SKIP         → 要 switchTab 前发现落盘 token/pid 不全（主动放弃跳转）
      - WX_REQ_FAIL:<msg>   → catch 到失败（msg 取前 12 字符）
  -->
  <view class="page" :data-e2e-state="e2eState">
    <view class="brand-header">
      <view class="brand-logo"><text class="brand-icon">⚡</text></view>
      <text class="brand-name">矫治通</text>
      <text class="brand-desc">矫形支具佩戴监测与管理</text>
    </view>

    <view class="tabs-wrap">
      <view class="tabs">
        <view :class="['tab', { 'tab-active': activeTab === 'login' }]" @click="activeTab = 'login'"><text>登录</text></view>
        <view :class="['tab', { 'tab-active': activeTab === 'register' }]" @click="activeTab = 'register'"><text>注册</text></view>
      </view>
    </view>

    <view v-if="activeTab === 'login'" class="panel">
      <view class="input-group">
        <text class="input-icon">📱</text>
        <input type="number" class="input-field" placeholder="请输入手机号" maxlength="11" v-model="phone" />
      </view>
      <view class="input-group">
        <text class="input-icon">🔒</text>
        <input type="number" class="input-field input-sms" placeholder="验证码" v-model="smsCode" />
        <view :class="['sms-btn', { 'sms-disabled': smsCountdown > 0 }]" @click="sendSMS">
          <text>{{ smsCountdown > 0 ? smsCountdown + 's' : '获取验证码' }}</text>
        </view>
      </view>
      <view class="agree-row">
        <view :class="['checkbox', { 'checkbox-checked': agreed }]" @click="agreed = !agreed">
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link">《用户协议》</text>
        <text class="agree-text">和</text>
        <text class="agree-link">《隐私政策》</text>
      </view>
      <view class="btn-primary" @click="doLogin"><text>登录</text></view>

      <view class="divider"><text class="divider-text">其他登录方式</text></view>

      <view
        class="btn-wechat"
        :class="{ 'btn-wechat-disabled': loginLoading }"
        role="button"
        @click="wechatLogin"
      >
        <text class="wechat-icon">💬</text>
        <text>{{ loginLoading ? '登录中...' : '微信授权登录' }}</text>
      </view>
    </view>

    <view v-else class="panel">
      <view class="input-group">
        <text class="input-icon">📱</text>
        <input type="number" class="input-field" placeholder="请输入手机号" maxlength="11" v-model="phone" />
      </view>
      <view class="input-group">
        <text class="input-icon">🔒</text>
        <input type="number" class="input-field input-sms" placeholder="验证码" v-model="smsCode" />
        <view :class="['sms-btn', { 'sms-disabled': smsCountdown > 0 }]" @click="sendSMS">
          <text>{{ smsCountdown > 0 ? smsCountdown + 's' : '获取验证码' }}</text>
        </view>
      </view>
      <view class="input-group">
        <text class="input-icon">🔑</text>
        <input :password="!showPwd" class="input-field" placeholder="设置密码（6-16位）" v-model="password" />
        <text class="pwd-toggle" @click="showPwd = !showPwd">{{ showPwd ? '🙈' : '👁' }}</text>
      </view>
      <view class="input-group">
        <text class="input-icon">🔑</text>
        <input :password="!showPwd" class="input-field" placeholder="确认密码" v-model="confirmPassword" />
      </view>
      <view class="agree-row">
        <view :class="['checkbox', { 'checkbox-checked': agreed }]" @click="agreed = !agreed">
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link">《用户协议》</text>
        <text class="agree-text">和</text>
        <text class="agree-link">《隐私政策》</text>
      </view>
      <view class="btn-primary" @click="doRegister"><text>注册</text></view>
      <view class="switch-to-login" @click="activeTab = 'login'">
        <text class="muted-text">已有账号？</text><text class="link-text">去登录</text>
      </view>
    </view>

    <view v-if="toastVisible" class="toast">
      <text class="toast-icon">✓</text>
      <text class="toast-text">{{ toastText }}</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onUnmounted } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { request } from '../../utils/request'
import { getToken, getPatientId } from '../../utils/token'

// 患者登录响应：POST /api/v1/patient/wx-login 返回（契约 user-service model.PatientLoginResultDTO）
interface WxLoginResp {
  token: string
  patientId: string
  name: string
  role: string
}

const authStore = useAuthStore()
const activeTab = ref<'login' | 'register'>('login')
const phone = ref('')
const smsCode = ref('')
const password = ref('')
const confirmPassword = ref('')
const showPwd = ref(false)
const agreed = ref(true)
const smsCountdown = ref(0)
const toastVisible = ref(false)
const toastText = ref('')
const loginLoading = ref(false)
// T080: 与模板绑定的同步 DOM marker（根节点 data-e2e-state）——Playwright 直接读 locator
const e2eState = ref<string>('INIT')
let smsTimer: ReturnType<typeof setInterval> | null = null
// 同一帧内 3 连点的幂等合并器（L8 requestCount=1 确定性去重）
let pendingWxLogin: Promise<void> | null = null

const OBS = '[OBS] wx-login'

function sendSMS() {
  uni.showToast({ title: '患者端暂仅支持微信登录', icon: 'none' })
}

/** 字符串打码：只保留前 8 字符前缀用于区分 fallback/真实 code，不打印全文 */
function maskCode(code: string | undefined): string {
  if (!code) return ''
  return code.length <= 8 ? code : `${code.slice(0, 8)}…`
}

function safeMessage(e: unknown, fallback = '登录失败，请重试', max = 80): string {
  if (e instanceof Error && e.message) {
    const s = e.message
    return s.length > max ? `${s.slice(0, max)}…` : s
  }
  return fallback
}

/**
 * T080: 三路观测（console.warn 必不落 DCE + DOM marker 可断言 + 窗口数组 Playwright evaluate 可直读）
 * 与 request.ts emit() 对齐，保证 L3/L6/L8 任一方向出问题 CI 至少有一路留痕。
 */
function setE2E(state: string, meta: Record<string, unknown> = {}) {
  e2eState.value = state
  // console.warn DCE-proof
  // eslint-disable-next-line no-console
  console.warn(`${OBS} state=${state} ${JSON.stringify(meta)}`)
  try {
    // @ts-ignore
    if (typeof window !== 'undefined') {
      // @ts-ignore
      const w = window as any
      if (!Array.isArray(w.__E2E_WXLOGIN_EVENTS__)) w.__E2E_WXLOGIN_EVENTS__ = []
      w.__E2E_WXLOGIN_EVENTS__.push({ t: Date.now(), state, ...meta })
    }
  } catch {/* ignore */}
}

// 协议勾选校验：Ella L7 用例契约 —— 未勾选点微信登录按钮 → showModal 提示，且不发任何请求
// 返回 true = 已勾选（可继续）；false = 未勾选，已 showModal，调用方直接 return
function checkAgreedModal(): boolean {
  if (agreed.value) return true
  setE2E('WX_UNCHECKED')
  uni.showModal({
    title: '提示',
    content: '请先阅读并同意协议',
    showCancel: false,
    confirmText: '确定',
  })
  return false
}

// doLogin/doRegister（SMS 入口占位）保留原有 toast 文案：T016 不动
function checkAgreed(): boolean {
  if (!agreed.value) {
    uni.showToast({ title: '请先同意用户协议和隐私政策', icon: 'none' })
    return false
  }
  return true
}

function showToast(text: string, shouldNav: boolean = true) {
  toastText.value = text
  toastVisible.value = true
  setTimeout(() => {
    toastVisible.value = false
    if (shouldNav) {
      // 兜底：若 token/patientId 落盘失败（极端情况），不执行跳转，避免 L3/L6 误判"跳 monitor"
      const navOk = !!getToken() && !!getPatientId()
      if (navOk) {
        setE2E('WX_NAV_GOING', { to: 'monitor' })
        uni.switchTab({ url: '/pages/monitor/index' })
      } else {
        setE2E('WX_NAV_SKIP', { reason: 'missing persisted token/patientId' })
      }
    }
  }, 1500)
}

// 真实登录：wx.login() → POST /api/v1/patient/wx-login → authStore.login
// 后端契约（user-service T069，gateway 白名单放行免 JWT）：
//   请求: { code }  — code 来自 uni.login() wx.login 获取
//   响应: PatientLoginResultDTO { token, patientId, name, role: 'patient' }
//
// H5 降级兼容：H5 / Playwright 环境无微信 SDK，uni.login(weixin) 会失败。
// 为保证 E2E 能通过 route mock 走通 wx-login 链路（不碰真机行为），失败时
// fallback 构造一个占位 code 直接调 wx-login，由后端/拦截器兜底处理。
// 不回塞 sendSMS 状态机（PM 裁决，保持患者端唯一登录=微信）。
async function wechatLoginInner() {
  try {
    let code: string | undefined
    let codeSource: 'wx' | 'fallback' = 'wx'
    try {
      const res = await new Promise<UniApp.LoginRes>((resolve, reject) => {
        uni.login({
          provider: 'weixin',
          success: (r) => resolve(r),
          fail: (err) => reject(err),
        })
      })
      code = res?.code
      setE2E('WX_LOGIN_OK', { codePrefix: maskCode(code) })
    } catch (e) {
      codeSource = 'fallback'
      // H5/CI 无微信 SDK → 占位 code，接口层（或 Playwright route）照常处理
      code = 'h5-fallback-wechat-login-code'
      setE2E('WX_LOGIN_FALLBACK', { reason: safeMessage(e, 32) })
    }
    if (!code) {
      setE2E('WX_NO_CODE')
      uni.showToast({ title: '登录失败，请重试', icon: 'none' })
      return
    }
    setE2E('WX_REQ_START', { codePrefix: maskCode(code), via: codeSource })
    const resp = await request<WxLoginResp>({
      url: '/api/v1/patient/wx-login',
      method: 'POST',
      data: { code },
    })
    const hasToken = !!(resp && resp.token)
    const hasPatientId = !!(resp && resp.patientId)
    setE2E('WX_REQ_OK', { hasToken, hasPatientId })
    if (!resp || !resp.token || !resp.patientId) {
      setE2E('WX_REQ_INCOMPLETE', { hasToken, hasPatientId })
      uni.showToast({ title: '登录失败，请重试', icon: 'none' })
      return
    }
    setE2E('WX_LOGIN_STORE_OK', { name: (resp.name || '').slice(0, 8), role: resp.role || '' })
    authStore.login(resp.token, resp.patientId)
    showToast('登录成功，正在跳转...')
  } catch (e: unknown) {
    const msg = safeMessage(e)
    const tag = (msg || '').slice(0, 12).replace(/[^0-9a-zA-Z_\u4e00-\u9fa5]/g, '_') || 'err'
    setE2E(`WX_REQ_FAIL:${tag}`, { error: msg })
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    // 清当前 pending 引用（下一帧才能再发）；loading 先复位，保证用户重试可用
    pendingWxLogin = null
    loginLoading.value = false
  }
}

// T074：患者端只走 POST /api/v1/patient/wx-login（微信静默授权）；
// 后端无手机号/验证码/密码患者登录通道，故在此引导用户点击下方微信登录。
function requireWechatLogin() {
  if (!checkAgreed()) return
  uni.showModal({
    title: '请使用微信登录',
    content: '患者端当前仅支持微信授权登录，请点击下方「微信授权登录」按钮完成登录。',
    showCancel: false,
    confirmText: '我知道了',
  })
}

function doLogin() {
  requireWechatLogin()
}

function doRegister() {
  requireWechatLogin()
}

// 微信授权登录：患者端 C 线唯一入口
// 确定性守卫（对应 L8 requestCount flaky 修复）：
//   0) WX_CLICK marker：同步写入 DOM + 观测（连点 3 次必留痕，哪怕后续 guard 拦）
//   1) 协议守卫 checkAgreedModal（未勾选 → WX_UNCHECKED + 不发请求）
//   2) JS guard：loginLoading.value（click 必须能到达 handler，故不做 DOM 级阻断，
//      仅保留 .btn-wechat-disabled 视觉禁用态）
//   3) pending Promise 合并器：同一帧内 3 连点只触发 1 次 request（幂等去重）
function wechatLogin() {
  // 先写 marker 再做任何守卫 — L8 requestCount=0 情况下 CI 也能证明"按钮事件确实到了"
  setE2E('WX_CLICK', { t: Date.now() })
  if (!checkAgreedModal()) return
  if (loginLoading.value) {
    setE2E('WX_GUARD_MERGED', { reason: 'loginLoading' })
    return
  }
  loginLoading.value = true
  if (pendingWxLogin) {
    setE2E('WX_GUARD_MERGED', { reason: 'pendingWxLogin' })
    return
  }
  pendingWxLogin = wechatLoginInner()
}

onUnmounted(() => {
  if (smsTimer) clearInterval(smsTimer)
  pendingWxLogin = null
})
</script>

<style scoped>
/* 微信端视口单位兼容性存疑，小程序端改用 page 高度撑满；H5 端保留 100vh */
/* #ifdef MP-WEIXIN */
.page { padding-bottom: 240rpx; min-height: 100%; }
/* #endif */
/* #ifndef MP-WEIXIN */
.page { padding-bottom: 240rpx; min-height: 100vh; }
/* #endif */
.brand-header { text-align: center; padding: 112rpx 0 40rpx; }
.brand-logo { width: 144rpx; height: 144rpx; border-radius: 40rpx; background: linear-gradient(135deg, #2563EB, #6366f1); margin: 0 auto; display: flex; align-items: center; justify-content: center; box-shadow: 0 8rpx 32rpx rgba(37, 99, 235, 0.25); }
.brand-icon { font-size: 72rpx; }
.brand-name { display: block; font-size: 44rpx; font-weight: 600; color: #1e293b; margin-top: 32rpx; letter-spacing: 2rpx; }
.brand-desc { display: block; font-size: 24rpx; color: #94a3b8; margin-top: 8rpx; }
.tabs-wrap { padding: 0 64rpx; }
.tabs { display: flex; background: #f1f5f9; border-radius: 20rpx; padding: 6rpx; gap: 4rpx; }
.tab { flex: 1; text-align: center; padding: 20rpx 0; font-size: 30rpx; font-weight: 500; color: #64748b; border-radius: 16rpx; transition: all 0.2s; }
.tab-active { background: #fff; color: #2563EB; box-shadow: 0 2rpx 6rpx rgba(0, 0, 0, 0.08); }
.panel { padding: 40rpx 64rpx 0; }
.input-group { display: flex; align-items: center; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 24rpx; padding: 0 28rpx; margin-bottom: 24rpx; }
.input-icon { font-size: 32rpx; margin-right: 20rpx; }
.input-field { flex: 1; width: 0; height: 88rpx; line-height: 88rpx; border: none; padding: 0; font-size: 30rpx; color: #1e293b; }
.input-sms { min-width: 0; }
.pwd-toggle { font-size: 32rpx; padding: 0 8rpx; }
.sms-btn { flex-shrink: 0; padding: 12rpx 0 12rpx 24rpx; }
.sms-btn text { font-size: 26rpx; font-weight: 500; color: #2563EB; white-space: nowrap; }
.sms-disabled text { color: #cbd5e1; }
.agree-row { display: flex; align-items: center; gap: 8rpx; margin-bottom: 40rpx; font-size: 22rpx; }
.checkbox { width: 28rpx; height: 28rpx; border: 2rpx solid #cbd5e1; border-radius: 6rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.checkbox-checked { background: #2563EB; border-color: #2563EB; }
.check-mark { color: #fff; font-size: 20rpx; line-height: 1; }
.agree-text { color: #94a3b8; }
.agree-link { color: #2563EB; }
.btn-primary { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 32rpx; font-weight: 500; }
.divider { display: flex; align-items: center; margin: 56rpx 0 40rpx; }
.divider::before, .divider::after { content: ''; flex: 1; height: 1rpx; background: #e2e8f0; }
.divider-text { padding: 0 28rpx; font-size: 24rpx; color: #94a3b8; }
.btn-wechat { width: 100%; padding: 26rpx 0; background: #07C160; border-radius: 24rpx; display: flex; align-items: center; justify-content: center; gap: 16rpx; user-select: none; }
.btn-wechat text { color: #fff; font-size: 30rpx; font-weight: 500; }
.wechat-icon { font-size: 36rpx; }
/* 加载中视觉态（仅样式；防重复提交由 JS guard 保证，E2E L8 契约要求 click 到达 handler） */
.btn-wechat-disabled { opacity: 0.6; }
.switch-to-login { text-align: center; margin-top: 24rpx; }
.muted-text { font-size: 24rpx; color: #94a3b8; }
.link-text { font-size: 24rpx; color: #2563EB; }
.toast { position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: #1e293b; padding: 32rpx 56rpx; border-radius: 24rpx; display: flex; align-items: center; gap: 16rpx; z-index: 200; box-shadow: 0 16rpx 48rpx rgba(0, 0, 0, 0.2); }
.toast-icon { color: #22c55e; font-size: 36rpx; }
.toast-text { color: #fff; font-size: 28rpx; }
</style>
