<template>
  <view class="contact">
    <view class="content">
      <view class="icon-circle"><text class="glyph">CS</text></view>
      <text class="title">{{ CONTACT.title }}</text>
      <text class="subtitle">{{ CONTACT.subtitle }}</text>

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
      </view>

      <view class="steps">
        <view class="step">
          <text :class="['step-dot', submitState === 'ok' ? 'done' : '', submitState === 'failed' ? 'failed' : '']">
            {{ submitState === 'ok' ? '✓' : submitState === 'failed' ? '!' : '1' }}
          </text>
          <text class="step-txt">{{ step1Text }}</text>
        </view>
        <view class="step">
          <text class="step-dot">2</text>
          <text class="step-txt">{{ CONTACT.steps[1] }}</text>
        </view>
        <view class="step">
          <text class="step-dot">3</text>
          <text class="step-txt">{{ CONTACT.steps[2] }}</text>
        </view>
      </view>

      <!--
        小程序由原生客服能力承接（设计稿 07 头注释口径）；
        H5 无 open-type，退回页面内提示。
      -->
      <!-- #ifdef MP-WEIXIN -->
      <button class="contact-btn" open-type="contact" @click="$emit('open-chat')">
        {{ CONTACT.primaryBtn }}
      </button>
      <!-- #endif -->
      <!-- #ifndef MP-WEIXIN -->
      <view class="btn-primary" @click="$emit('open-chat')"><text>{{ CONTACT.primaryBtn }}</text></view>
      <!-- #endif -->
      <view class="btn-secondary" @click="$emit('back')"><text>{{ CONTACT.secondaryBtn }}</text></view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CONTACT, type ContactIssue } from '../../utils/wifi-copy'

/**
 * submitState 反映 POST /api/v1/feedbacks 的真实结果：
 * 后端路由缺失时保持 failed 并如实提示，不得把步骤 1 画成已完成。
 */
const props = defineProps<{
  issueType: ContactIssue
  deviceLabel: string
  occurredAt: string
  submitState: 'pending' | 'ok' | 'failed'
}>()

defineEmits<{ (e: 'open-chat'): void; (e: 'back'): void }>()

const issueTypeText = computed(() => CONTACT.typeMap[props.issueType])
const step1Text = computed(() =>
  props.submitState === 'ok'
    ? CONTACT.step1Done
    : props.submitState === 'failed'
      ? CONTACT.step1Failed
      : CONTACT.step1Pending
)
</script>

<style scoped>
.contact { background: #f8fafc; min-height: 100vh; }
.content { padding: 48rpx 40rpx; }
.icon-circle { width: 144rpx; height: 144rpx; border-radius: 50%; background: #eff6ff; display: flex; align-items: center; justify-content: center; margin: 64rpx auto 40rpx; }
.glyph { font-size: 40rpx; font-weight: 700; color: #2563EB; }
.title { font-size: 40rpx; font-weight: 700; color: #1e293b; text-align: center; display: block; }
.subtitle { font-size: 28rpx; color: #64748b; text-align: center; line-height: 1.6; margin-top: 16rpx; display: block; }

.context-card { background: #fff; border-radius: 28rpx; padding: 32rpx; margin-top: 56rpx; border: 2rpx solid #e2e8f0; }
.context-title { font-size: 26rpx; color: #94a3b8; margin-bottom: 24rpx; display: block; }
.context-row { display: flex; justify-content: space-between; padding: 16rpx 0; border-bottom: 2rpx solid #f1f5f9; }
.context-row:last-child { border-bottom: none; }
.context-label { font-size: 28rpx; color: #64748b; }
.context-value { font-size: 28rpx; color: #1e293b; font-weight: 500; text-align: right; max-width: 60%; }

.steps { background: #fff; border-radius: 28rpx; padding: 32rpx; margin-top: 40rpx; border: 2rpx solid #e2e8f0; }
.step { display: flex; align-items: flex-start; padding: 16rpx 0; }
.step-dot { width: 36rpx; height: 36rpx; border-radius: 50%; background: #2563EB; color: #fff; font-size: 22rpx; text-align: center; line-height: 36rpx; flex-shrink: 0; margin-right: 20rpx; }
.step-dot.done { background: #07C160; }
.step-dot.failed { background: #ef4444; }
.step-txt { flex: 1; font-size: 26rpx; color: #475569; line-height: 1.5; }

.btn-primary { margin-top: 48rpx; padding: 28rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 600; }
/* 与 .btn-primary 同视觉的原生客服按钮（button 标签无法复用 view 样式选择器） */
.contact-btn { margin-top: 48rpx; width: 100%; padding: 28rpx; background: #2563EB; color: #fff; font-size: 30rpx; font-weight: 600; border-radius: 24rpx; line-height: 1.4; border: none; }
.contact-btn::after { border: none; }
.btn-secondary { margin-top: 20rpx; padding: 28rpx; background: #f1f5f9; border-radius: 24rpx; text-align: center; }
.btn-secondary text { color: #475569; font-size: 30rpx; font-weight: 500; }
</style>
