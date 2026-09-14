<template>
  <view class="failure">
    <view class="fail-area">
      <view :class="['fail-ic', copy.tone]"><text class="glyph">!</text></view>
      <text class="fail-title">{{ copy.title }}</text>
      <text class="fail-desc">{{ copy.desc }}</text>
    </view>

    <view class="action-card">
      <text class="action-label">{{ FAILURE_COMMON.actionLabel }}</text>
      <view v-for="(a, i) in copy.actions" :key="i" class="action-item">
        <text class="action-num">{{ i + 1 }}</text>
        <text class="action-text">{{ a }}</text>
      </view>
    </view>

    <view class="btn-row">
      <view class="btn-primary" @click="$emit('primary')"><text>{{ copy.primaryLabel }}</text></view>
      <view v-if="copy.secondaryLabel" class="btn-secondary" @click="$emit('secondary')">
        <text>{{ copy.secondaryLabel }}</text>
      </view>
      <view class="btn-help" @click="$emit('contact')"><text>{{ FAILURE_COMMON.contactBtn }}</text></view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { FAILURES, FAILURE_COMMON, type FailureKey } from '../../utils/wifi-copy'

/**
 * 06a–06e 五类失败态共用一套版式（设计稿仅图标色与按钮组不同），
 * tone / 文案 / 主按钮落点全部来自 FAILURES[type]，不在组件内写分支。
 */
const props = defineProps<{ type: FailureKey }>()

defineEmits<{ (e: 'primary'): void; (e: 'secondary'): void; (e: 'contact'): void }>()

const copy = computed(() => FAILURES[props.type])
</script>

<style scoped>
.failure { background: #f8fafc; min-height: 100vh; }
.fail-area { text-align: center; padding: 120rpx 64rpx 48rpx; }
.fail-ic { width: 160rpx; height: 160rpx; border-radius: 50%; margin: 0 auto; display: flex; align-items: center; justify-content: center; }
.fail-ic.red { background: #fee2e2; }
.fail-ic.amber { background: #fef3c7; }
.glyph { font-size: 80rpx; font-weight: 700; line-height: 1; }
.fail-ic.red .glyph { color: #ef4444; }
.fail-ic.amber .glyph { color: #f59e0b; }
.fail-title { font-size: 40rpx; font-weight: 600; color: #1e293b; margin-top: 40rpx; display: block; }
.fail-desc { font-size: 28rpx; color: #64748b; line-height: 1.7; margin-top: 20rpx; display: block; }

.action-card { margin: 56rpx 40rpx 0; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 28rpx; padding: 32rpx; }
.action-label { font-size: 24rpx; color: #94a3b8; margin-bottom: 20rpx; display: block; }
.action-item { display: flex; align-items: flex-start; padding: 16rpx 0; }
.action-num { width: 36rpx; height: 36rpx; border-radius: 50%; background: #eff6ff; color: #2563EB; font-size: 20rpx; font-weight: 600; text-align: center; line-height: 36rpx; flex-shrink: 0; margin-right: 20rpx; }
.action-text { flex: 1; font-size: 26rpx; color: #475569; line-height: 1.6; }

.btn-row { margin: 48rpx 40rpx 0; }
.btn-primary { padding: 26rpx; background: #2563EB; border-radius: 24rpx; text-align: center; }
.btn-primary text { color: #fff; font-size: 30rpx; font-weight: 500; }
.btn-secondary { margin-top: 20rpx; padding: 26rpx; background: #f1f5f9; border-radius: 24rpx; text-align: center; }
.btn-secondary text { color: #475569; font-size: 28rpx; font-weight: 500; }
.btn-help { margin-top: 20rpx; padding: 26rpx; background: #fff; border: 2rpx solid #e2e8f0; border-radius: 24rpx; text-align: center; }
.btn-help text { color: #2563EB; font-size: 28rpx; font-weight: 500; }
</style>
