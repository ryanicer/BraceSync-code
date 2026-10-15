<template>
  <view class="contact">
    <view class="content">
      <view class="icon-circle"><text class="glyph">💬</text></view>
      <text class="title">{{ CONTACT.title }}</text>
      <text class="subtitle">{{ CONTACT.subtitle }}</text>

      <!-- 已自动记录的配网信息 + 自动上报状态（07-contact context-card / context-auto） -->
      <view class="context-card">
        <text class="context-title">{{ CONTACT.contextTitle }}</text>
        <view class="context-row">
          <text class="context-label">{{ CONTACT.typeLabel }}</text>
          <text class="context-value">{{ issueTypeText }}</text>
        </view>
        <view class="context-row">
          <text class="context-label">{{ CONTACT.deviceLabel }}</text>
          <text class="context-value">{{ deviceLabel }}</text>
        </view>
        <view class="context-row">
          <text class="context-label">{{ CONTACT.timeLabel }}</text>
          <text class="context-value">{{ occurredAt }}</text>
        </view>
        <!--
          状态条＝POST /api/v1/feedbacks 的真实结果：设计稿只画成功绿条，
          pending/failed 由实现侧如实呈现，不得把失败画成已提交。
        -->
        <view :class="['context-auto', submitState]">
          <text class="context-auto-mark">{{ submitState === 'ok' ? '✓' : submitState === 'failed' ? '!' : '…' }}</text>
          <text class="context-auto-text">{{ autoText }}</text>
        </view>
      </view>

      <text class="channel-hint">{{ CONTACT.channelHint }}</text>

      <!-- 小程序由原生客服能力承接（session-from 身份透传见 PRD §7A.13 通道一）；
           H5 无 open-type，退回页面内提示。 -->
      <!-- #ifdef MP-WEIXIN -->
      <button class="contact-btn" open-type="contact" :session-from="sessionFrom" @click="$emit('open-chat')">
        {{ CONTACT.primaryBtn }}
      </button>
      <!-- #endif -->
      <!-- #ifndef MP-WEIXIN -->
      <view class="btn-primary" @click="$emit('open-chat')"><text>{{ CONTACT.primaryBtn }}</text></view>
      <!-- #endif -->
      <!-- 配网变体不另立业务：补充留言回统一入口留言表单（类型由用户自选，wifi_setup_failure 不暴露） -->
      <view class="btn-secondary" @click="$emit('leave')"><text>{{ CONTACT.leaveBtn }}</text></view>
      <view class="btn-ghost" @click="$emit('back')"><text>{{ CONTACT.backBtn }}</text></view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CONTACT, type ContactIssue } from '../../utils/wifi-copy'

/**
 * submitState 反映 POST /api/v1/feedbacks 的真实结果：
 * 后端/网络失败时保持 failed 并如实提示，不得把状态条画成已提交。
 * sessionFrom = 患者ID（|设备号），由父页装配（首段=患者ID，服务端解析）。
 */
const props = defineProps<{
  issueType: ContactIssue
  deviceLabel: string
  occurredAt: string
  submitState: 'pending' | 'ok' | 'failed'
  sessionFrom?: string
}>()

defineEmits<{ (e: 'open-chat'): void; (e: 'leave'): void; (e: 'back'): void }>()

const issueTypeText = computed(() => CONTACT.typeMap[props.issueType])
const autoText = computed(() =>
  props.submitState === 'ok'
    ? CONTACT.autoOk
    : props.submitState === 'failed'
      ? CONTACT.autoFailed
      : CONTACT.autoPending
)
</script>

<style scoped>
.contact { background: #f8fafc; min-height: 100vh; }
.content { padding: 48rpx 40rpx; }
.icon-circle { width: 144rpx; height: 144rpx; border-radius: 50%; background: #eff6ff; display: flex; align-items: center; justify-content: center; margin: 44rpx auto 36rpx; }
.glyph { font-size: 60rpx; }
.title { font-size: 40rpx; font-weight: 700; color: #1e293b; text-align: center; display: block; }
.subtitle { font-size: 28rpx; color: #64748b; text-align: center; line-height: 1.6; margin-top: 16rpx; display: block; white-space: pre-line; }

.context-card { background: #fff; border-radius: 28rpx; padding: 32rpx; margin-top: 48rpx; border: 2rpx solid #e2e8f0; }
.context-title { font-size: 26rpx; color: #94a3b8; margin-bottom: 24rpx; display: block; }
.context-row { display: flex; justify-content: space-between; padding: 16rpx 0; border-bottom: 2rpx solid #f1f5f9; }
.context-row:last-of-type { border-bottom: none; }
.context-label { font-size: 28rpx; color: #64748b; }
.context-value { font-size: 28rpx; color: #1e293b; font-weight: 500; text-align: right; max-width: 60%; }

.context-auto { display: flex; align-items: center; gap: 16rpx; font-size: 24rpx; background: #f0fdf4; color: #10ac84; border-radius: 16rpx; padding: 16rpx 20rpx; margin-top: 24rpx; }
.context-auto.pending { background: #f8fafc; color: #64748b; }
.context-auto.failed { background: #fef2f2; color: #ef4444; }
.context-auto-mark { font-weight: 700; flex-shrink: 0; }
.context-auto-text { flex: 1; line-height: 1.5; }

.channel-hint { display: block; font-size: 26rpx; color: #475569; line-height: 1.7; margin: 32rpx 0 32rpx; }

.contact-btn { width: 100%; padding: 28rpx; background: #2563EB; color: #fff; font-size: 30rpx; font-weight: 600; border-radius: 24rpx; line-height: 1.4; border: none; box-sizing: border-box; }
.contact-btn::after { border: none; }
/* 与 .contact-btn 同视觉的 H5 降级按钮（button 标签无法复用 view 样式选择器） */
.btn-primary { padding: 28rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 600; }
.btn-secondary { margin-top: 20rpx; padding: 28rpx; background: #fff; border: 2rpx solid #bfdbfe; border-radius: 24rpx; text-align: center; }
.btn-secondary text { color: #2563EB; font-size: 30rpx; font-weight: 500; }
.btn-ghost { margin-top: 8rpx; padding: 26rpx; background: transparent; border-radius: 24rpx; text-align: center; }
.btn-ghost text { color: #64748b; font-size: 28rpx; font-weight: 500; }
</style>
