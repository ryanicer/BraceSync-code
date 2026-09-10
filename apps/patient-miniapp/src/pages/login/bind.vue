<template>
  <view class="page">
    <view class="nav-bar">
      <view class="nav-back" @click="goBack">
        <text class="back-icon">‹</text>
      </view>
      <text class="nav-title">绑定就诊档案</text>
    </view>

    <view class="bind-icon">
      <text class="icon-phone">📞</text>
    </view>
    <view class="page-title">绑定您孩子的就诊档案</view>
    <view class="desc">为了匹配您孩子在门诊的就诊档案，需要您授权微信手机号进行验证。授权后即可查看孩子的矫正进展数据。</view>

    <view class="benefits">
      <view class="benefit">
        <view class="benefit-ic"><text>📊</text></view>
        <view>
          <view class="benefit-txt">矫正进展数据</view>
          <view class="benefit-sub">佩戴时长、压力热力图</view>
        </view>
      </view>
      <view class="benefit">
        <view class="benefit-ic"><text>🔔</text></view>
        <view>
          <view class="benefit-txt">随访提醒</view>
          <view class="benefit-sub">复查、佩戴提醒通知</view>
        </view>
      </view>
      <view class="benefit">
        <view class="benefit-ic"><text>💬</text></view>
        <view>
          <view class="benefit-txt">医生沟通</view>
          <view class="benefit-sub">在线咨询主治医生</view>
        </view>
      </view>
    </view>

    <view class="content">
      <button
        :class="['btn-phone', { 'btn-phone-retry': isRetry }]"
        open-type="getPhoneNumber"
        @getphonenumber="onGetPhoneNumber"
        :disabled="binding"
      >
        <text v-if="!binding">{{ isRetry ? '重试绑定' : '微信授权手机号' }}</text>
        <text v-else>绑定中...</text>
      </button>
      <view v-if="phoneToken" class="token-tip">已缓存手机号凭证，7 天内可重试</view>
      <view class="contact-link" @click="contactStaff">遇到问题？联系工作人员 ›</view>

      <view class="agreement">
        <view :class="['checkbox', { 'checkbox-checked': agreed }]" @click="agreed = !agreed">
          <text v-if="agreed" class="check-mark">✓</text>
        </view>
        <text class="agree-text">已阅读并同意</text>
        <text class="agree-link">《用户协议》</text>
        <text class="agree-text">和</text>
        <text class="agree-link">《隐私政策》</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { bindPhone, type BindPhoneData, type BindPhoneFailData } from '../../api/patient'
import { resolveBindPhoneResult } from '../../utils/auth-state'

const authStore = useAuthStore()
const agreed = ref(true)
const binding = ref(false)
const phoneToken = ref<string | null>(authStore.getPhoneToken())

const isRetry = computed(() => !!phoneToken.value)

function goBack() {
  uni.navigateBack({ fail: () => uni.reLaunch({ url: '/pages/login/index' }) })
}

function contactStaff() {
  uni.showToast({ title: '请联系门诊工作人员协助建档', icon: 'none' })
}

async function doBind(phoneCode?: string) {
  if (!agreed.value) {
    uni.showToast({ title: '请先阅读并同意用户协议', icon: 'none' })
    return
  }
  const bindToken = authStore.bindToken
  if (!bindToken) {
    uni.showToast({ title: '绑定凭证已失效，请重新登录', icon: 'none' })
    setTimeout(() => uni.reLaunch({ url: '/pages/login/index' }), 1200)
    return
  }

  binding.value = true
  try {
    const env = await bindPhone(bindToken, {
      phoneCode,
      phoneToken: phoneToken.value || undefined,
    })
    const result = resolveBindPhoneResult(env.code)

    switch (result.state) {
      case 'BOUND': {
        const data = env.data as BindPhoneData
        if (data?.token && data?.patientId) {
          authStore.login(data.token, data.patientId)
          uni.redirectTo({
            url: `/pages/login/bind-success?name=${encodeURIComponent(data.name || '')}&patientId=${encodeURIComponent(data.patientId)}`,
          })
        } else {
          uni.showToast({ title: '绑定失败，请重试', icon: 'none' })
        }
        break
      }
      case 'NO_MATCH': {
        const data = env.data as BindPhoneFailData
        if (data?.phoneToken) {
          phoneToken.value = data.phoneToken
          authStore.setPhoneToken(data.phoneToken)
        }
        uni.redirectTo({ url: '/pages/login/no-match' })
        break
      }
      case 'CONFLICT': {
        const data = env.data as BindPhoneFailData
        if (data?.phoneToken) {
          phoneToken.value = data.phoneToken
          authStore.setPhoneToken(data.phoneToken)
        }
        uni.redirectTo({ url: '/pages/login/conflict' })
        break
      }
      default:
        uni.showToast({ title: result.message || '绑定失败，请重试', icon: 'none' })
        break
    }
  } catch (e) {
    const msg = e instanceof Error ? e.message : '绑定失败，请重试'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    binding.value = false
  }
}

function onGetPhoneNumber(e: any) {
  // 用户拒绝授权
  if (e.detail.errMsg !== 'getPhoneNumber:ok') {
    uni.showToast({ title: '需要授权手机号才能匹配您的就诊档案哦', icon: 'none' })
    return
  }
  // 同意授权：取 phoneCode 调 bind-phone
  doBind(e.detail.code)
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 80rpx; }
.nav-bar { height: 88rpx; display: flex; align-items: center; padding: 0 32rpx; position: relative; }
.nav-back { width: 64rpx; height: 64rpx; display: flex; align-items: center; justify-content: center; }
.back-icon { font-size: 48rpx; color: #1e293b; }
.nav-title { position: absolute; left: 0; right: 0; text-align: center; font-size: 32rpx; font-weight: 600; color: #1e293b; }
.bind-icon { width: 176rpx; height: 176rpx; border-radius: 48rpx; background: linear-gradient(135deg, #dbeafe, #e0e7ff); margin: 64rpx auto 40rpx; display: flex; align-items: center; justify-content: center; }
.icon-phone { font-size: 88rpx; }
.page-title { text-align: center; font-size: 44rpx; font-weight: 600; color: #1e293b; }
.desc { text-align: center; font-size: 28rpx; color: #64748b; line-height: 1.7; margin: 28rpx 64rpx 0; }
.benefits { margin: 56rpx 64rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 12rpx 36rpx; }
.benefit { display: flex; align-items: center; gap: 24rpx; padding: 28rpx 0; border-bottom: 1rpx solid #f1f5f9; }
.benefit:last-child { border-bottom: none; }
.benefit-ic { width: 56rpx; height: 56rpx; border-radius: 16rpx; background: #eff6ff; display: flex; align-items: center; justify-content: center; color: #2563EB; flex-shrink: 0; font-size: 28rpx; }
.benefit-txt { font-size: 28rpx; color: #334155; }
.benefit-sub { font-size: 22rpx; color: #94a3b8; margin-top: 4rpx; }
.content { padding: 0 64rpx; margin-top: 64rpx; }
.btn-phone { width: 100%; padding: 28rpx 0; background: #07C160; color: #fff; border: none; border-radius: 24rpx; font-size: 30rpx; font-weight: 500; }
.btn-phone-retry { background: #2563EB; }
.token-tip { text-align: center; margin-top: 20rpx; font-size: 22rpx; color: #94a3b8; }
.contact-link { text-align: center; margin-top: 32rpx; font-size: 26rpx; color: #2563EB; }
.agreement { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 8rpx; margin-top: 40rpx; font-size: 22rpx; line-height: 1.6; }
.checkbox { width: 28rpx; height: 28rpx; border: 2rpx solid #cbd5e1; border-radius: 6rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-top: 2rpx; }
.checkbox-checked { background: #2563EB; border-color: #2563EB; }
.check-mark { color: #fff; font-size: 20rpx; line-height: 1; }
.agree-text { color: #64748b; }
.agree-link { color: #2563EB; }
</style>
