<template>
  <view class="page">
    <view class="nav-bar">
      <view class="nav-back" @click="goBack">
        <text class="back-icon">‹</text>
      </view>
      <text class="nav-title">未找到就诊档案</text>
    </view>

    <view class="nomatch-icon">
      <text class="icon-warn">⚠️</text>
    </view>
    <view class="page-title">未找到匹配的就诊档案</view>
    <view class="desc">系统未能根据您的手机号匹配到就诊档案。请确认以下信息后重试：</view>

    <view class="reason-card">
      <view class="reason-title">可能的原因</view>
      <view class="reason-item">
        <view class="reason-num">1</view>
        <view class="reason-txt">就诊时登记的手机号与当前微信绑定手机号不一致</view>
      </view>
      <view class="reason-item">
        <view class="reason-num">2</view>
        <view class="reason-txt">门诊尚未为您建立电子档案或档案状态未激活</view>
      </view>
      <view class="reason-item">
        <view class="reason-num">3</view>
        <view class="reason-txt">您使用的是家人的微信，手机号不属于患者本人</view>
      </view>
    </view>

    <view class="content">
      <view class="btn-primary" @click="retryBind"><text>重试绑定</text></view>
      <view class="contact-link" @click="contactStaff">遇到问题？联系工作人员 ›</view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { bindPhone, type BindPhoneData, type BindPhoneFailData } from '../../api/patient'
import { resolveBindPhoneResult } from '../../utils/auth-state'

const authStore = useAuthStore()
const loading = ref(false)

function goBack() {
  uni.navigateBack({ fail: () => uni.reLaunch({ url: '/pages/login/index' }) })
}

function contactStaff() {
  uni.showToast({ title: '请联系门诊工作人员协助建档', icon: 'none' })
}

async function retryBind() {
  const bindToken = authStore.bindToken
  const phoneToken = authStore.getPhoneToken()
  if (!bindToken) {
    uni.showToast({ title: '绑定凭证已失效，请重新登录', icon: 'none' })
    setTimeout(() => uni.reLaunch({ url: '/pages/login/index' }), 1200)
    return
  }
  if (!phoneToken) {
    // 无 phoneToken → 回绑定页重新授权手机号
    uni.redirectTo({ url: '/pages/login/bind' })
    return
  }

  loading.value = true
  try {
    const env = await bindPhone(bindToken, { phoneToken })
    const result = resolveBindPhoneResult(env.code)
    switch (result.state) {
      case 'BOUND': {
        const data = env.data as BindPhoneData
        if (data?.token && data?.patientId) {
          authStore.login(data.token, data.patientId)
          uni.redirectTo({
            url: `/pages/login/bind-success?name=${encodeURIComponent(data.name || '')}&patientId=${encodeURIComponent(data.patientId)}`,
          })
        }
        break
      }
      case 'CONFLICT': {
        const data = env.data as BindPhoneFailData
        if (data?.phone_token) authStore.setPhoneToken(data.phone_token)
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
    loading.value = false
  }
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 80rpx; }
.nav-bar { height: 88rpx; display: flex; align-items: center; padding: 0 32rpx; position: relative; }
.nav-back { width: 64rpx; height: 64rpx; display: flex; align-items: center; justify-content: center; }
.back-icon { font-size: 48rpx; color: #1e293b; }
.nav-title { position: absolute; left: 0; right: 0; text-align: center; font-size: 32rpx; font-weight: 600; color: #1e293b; }
.nomatch-icon { width: 176rpx; height: 176rpx; border-radius: 48rpx; background: #fef3c7; margin: 96rpx auto 40rpx; display: flex; align-items: center; justify-content: center; }
.icon-warn { font-size: 88rpx; }
.page-title { text-align: center; font-size: 44rpx; font-weight: 600; color: #1e293b; }
.desc { font-size: 28rpx; color: #64748b; line-height: 1.7; margin: 28rpx 64rpx 0; text-align: center; }
.reason-card { margin: 56rpx 64rpx 0; background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 36rpx; }
.reason-title { font-size: 26rpx; font-weight: 600; color: #475569; margin-bottom: 24rpx; }
.reason-item { display: flex; gap: 20rpx; padding: 16rpx 0; font-size: 26rpx; color: #64748b; line-height: 1.6; }
.reason-num { width: 36rpx; height: 36rpx; border-radius: 50%; background: #fef3c7; color: #b45309; font-size: 22rpx; font-weight: 600; display: flex; align-items: center; justify-content: center; flex-shrink: 0; margin-top: 2rpx; }
.reason-txt { flex: 1; }
.content { padding: 0 64rpx; margin-top: 64rpx; }
.btn-primary { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.contact-link { text-align: center; margin-top: 32rpx; font-size: 26rpx; color: #2563EB; }
</style>
