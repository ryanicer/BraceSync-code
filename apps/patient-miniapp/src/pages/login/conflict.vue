<template>
  <view class="page">
    <view class="nav-bar">
      <view class="nav-back" @click="goBack">
        <text class="back-icon">‹</text>
      </view>
      <text class="nav-title">绑定冲突</text>
    </view>

    <view class="conflict-icon">
      <text class="icon-lock">🔒</text>
    </view>
    <view class="page-title">该手机号已绑定其他微信</view>
    <view class="desc">该就诊档案已绑定到另一个微信账号。一个手机号只能绑定一个微信，请使用已绑定的微信登录，或联系门诊工作人员处理。</view>

    <view class="content">
      <view class="btn-primary" @click="retryBind"><text>重试绑定</text></view>
      <view class="contact-link" @click="contactStaff">联系工作人员处理 ›</view>
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
  uni.showToast({ title: '请联系门诊工作人员协助处理', icon: 'none' })
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
      case 'NO_MATCH': {
        const data = env.data as BindPhoneFailData
        if (data?.phone_token) authStore.setPhoneToken(data.phone_token)
        uni.redirectTo({ url: '/pages/login/no-match' })
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
.conflict-icon { width: 176rpx; height: 176rpx; border-radius: 48rpx; background: linear-gradient(135deg, #dbeafe, #e0e7ff); margin: 96rpx auto 40rpx; display: flex; align-items: center; justify-content: center; }
.icon-lock { font-size: 88rpx; }
.page-title { text-align: center; font-size: 44rpx; font-weight: 600; color: #1e293b; }
.desc { text-align: center; font-size: 28rpx; color: #64748b; line-height: 1.7; margin: 28rpx 64rpx 0; }
.content { padding: 0 64rpx; margin-top: 128rpx; }
.btn-primary { width: 100%; padding: 28rpx 0; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.contact-link { text-align: center; margin-top: 32rpx; font-size: 26rpx; color: #2563EB; }
</style>
