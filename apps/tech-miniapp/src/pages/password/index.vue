<template>
  <view class="page">
    <view class="intro">
      <text class="intro-title">修改登录密码</text>
      <text class="intro-sub">改密成功后需用新密码重新登录</text>
    </view>

    <view class="panel">
      <view class="input-group">
        <text class="input-icon">🔒</text>
        <input
          :password="!showOld"
          class="input-field"
          placeholder="请输入原密码"
          v-model="oldPassword"
        />
        <text class="pwd-toggle" @click="showOld = !showOld">
          {{ showOld ? '🙈' : '👁' }}
        </text>
      </view>

      <view class="input-group">
        <text class="input-icon">🆕</text>
        <input
          :password="!showNew"
          class="input-field"
          placeholder="新密码（6-16 位，字母+数字）"
          maxlength="16"
          v-model="newPassword"
        />
        <text class="pwd-toggle" @click="showNew = !showNew">
          {{ showNew ? '🙈' : '👁' }}
        </text>
      </view>
      <text v-if="newPwdError" class="field-error">{{ newPwdError }}</text>

      <view class="input-group">
        <text class="input-icon">✅</text>
        <input
          :password="!showConfirm"
          class="input-field"
          placeholder="再次输入新密码"
          maxlength="16"
          v-model="confirmPassword"
        />
        <text class="pwd-toggle" @click="showConfirm = !showConfirm">
          {{ showConfirm ? '🙈' : '👁' }}
        </text>
      </view>
      <text v-if="confirmError" class="field-error">{{ confirmError }}</text>

      <text class="rule-hint">{{ TECH_PASSWORD_RULE_HINT }}</text>

      <view :class="['btn-primary', { 'btn-disabled': submitting }]" @click="doChange">
        <text>{{ submitting ? '提交中...' : '确认修改' }}</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { changeTechPassword } from '../../api/password'
import { TECH_PASSWORD_RULE_HINT, techPasswordError } from '../../utils/password'
import { LOGIN_PAGE } from '../../utils/authError'
import { logger } from '../../utils/logger'
import { logErrorText, userErrorCopy } from '@bracesync/shared-utils'

const authStore = useAuthStore()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const showOld = ref(false)
const showNew = ref(false)
const showConfirm = ref(false)
const newPwdError = ref('')
const confirmError = ref('')
const submitting = ref(false)

function toast(title: string) {
  uni.showToast({ title, icon: 'none' })
}

async function doChange() {
  if (submitting.value) return

  if (!oldPassword.value) {
    toast('请输入原密码')
    return
  }

  newPwdError.value = techPasswordError(newPassword.value) || ''
  if (newPwdError.value) return

  if (newPassword.value === oldPassword.value) {
    newPwdError.value = '新密码不能与原密码相同'
    return
  }

  confirmError.value = confirmPassword.value === newPassword.value ? '' : '两次输入的新密码不一致'
  if (confirmError.value) return

  submitting.value = true
  try {
    await changeTechPassword(oldPassword.value, newPassword.value)
    // 卡面口径：成功后跳登录页重新登录。这里连凭据一起清掉——口令已换，
    // 手里这份旧 token 对应的会话不该继续有效（不清则 reLaunch 后 storage 里还留着）。
    authStore.logout()
    uni.showToast({ title: '密码修改成功，请重新登录', icon: 'success' })
    setTimeout(() => {
      uni.reLaunch({ url: LOGIN_PAGE })
    }, 1500)
  } catch (e: any) {
    // 口令不进日志：logErrorText 只取文案，且后端这句是「原密码不正确」这类无语义信息的文案
    logger.error('[T486]', 'changePassword failed', {
      message: logErrorText(e),
      code: e?.code,
      httpStatus: e?.httpStatus,
    })
    // 10001 已被 api/password.ts 打成 userCopy，这里走唯一展示出口即可拿到「原密码不正确」，
    // 而不是共享码表里那句「登录状态已失效，请重新登录」。
    toast(userErrorCopy(e, { scope: 'tech', fallback: '修改失败' }))
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  if (!authStore.isLoggedIn) {
    uni.reLaunch({ url: LOGIN_PAGE })
  }
})
</script>

<style scoped>
.page { min-height: 100vh; background: #f3f4f6; padding: 0 64rpx 120rpx; }
.intro { padding: 48rpx 0 32rpx; }
.intro-title { display: block; font-size: 40rpx; font-weight: 700; color: #1f2937; }
.intro-sub { display: block; font-size: 24rpx; color: #9ca3af; margin-top: 8rpx; }
.panel { background: #fff; border-radius: 32rpx; padding: 40rpx 32rpx; }
.input-group {
  display: flex;
  align-items: center;
  background: #f9fafb;
  border: 1rpx solid #e5e7eb;
  border-radius: 24rpx;
  padding: 0 28rpx;
  margin-bottom: 16rpx;
}
.input-icon { font-size: 32rpx; margin-right: 20rpx; }
.input-field { flex: 1; width: 0; height: 88rpx; line-height: 88rpx; border: none; padding: 0; font-size: 30rpx; color: #1e293b; }
.pwd-toggle { font-size: 32rpx; padding: 0 8rpx; cursor: pointer; }
.field-error { display: block; font-size: 24rpx; color: #dc2626; margin-bottom: 16rpx; padding-left: 8rpx; }
.rule-hint { display: block; font-size: 22rpx; color: #9ca3af; margin: 8rpx 0 32rpx; }
.btn-primary {
  width: 100%;
  padding: 28rpx 0;
  background: #2563EB;
  border-radius: 24rpx;
  text-align: center;
}
.btn-primary text { color: #fff; font-size: 32rpx; font-weight: 500; }
.btn-disabled { opacity: 0.6; }
</style>
