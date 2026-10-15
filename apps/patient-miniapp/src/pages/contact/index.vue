<template>
  <view class="page">
    <!-- 首屏引导（设计稿 contact.html intro） -->
    <view class="intro">
      <view class="intro-icon">💬</view>
      <text class="intro-title">联系产品客服</text>
      <text class="intro-sub">设备使用、配网与产品问题由运维团队为您处理&#10;可选择在线沟通或给我们留言</text>
    </view>

    <!-- 通道一 · 在线客服（即时）：微信原生客服会话，R3 复用微信客服、不建消息中心 -->
    <view class="channel">
      <view class="channel-head">
        <view class="channel-badge online">💬</view>
        <view class="channel-name-row">
          <text class="channel-name">在线客服</text>
          <text class="channel-tag">即时回复</text>
        </view>
      </view>
      <text class="channel-desc">打开微信客服会话，与客服实时沟通；客服工作时间内通常即刻响应。</text>
      <!-- V3.48 身份透传（§7A.13 通道一）：session-from 携带患者标识，微信进会话事件
           （user_enter_tempsession）触发服务端 1 分钟内自动下发身份消息；内容仅本人档案信息。 -->
      <!-- #ifdef MP-WEIXIN -->
      <button class="btn-primary" open-type="contact" :session-from="sessionFrom">在线客服</button>
      <!-- #endif -->
      <!-- #ifndef MP-WEIXIN -->
      <view class="btn-primary" @click="openChatFallback"><text>在线客服</text></view>
      <!-- #endif -->
    </view>

    <!-- 通道二 · 客服留言（异步）：三选类型 + 300 字内容 → POST /api/v1/feedbacks，status=pending -->
    <view class="channel">
      <view class="channel-head">
        <view class="channel-badge leave">✏️</view>
        <view class="channel-name-row">
          <text class="channel-name">客服留言</text>
          <text class="channel-tag async">稍后回复</text>
        </view>
      </view>
      <!-- 文案红线（V3.48）：禁「工单」措辞、禁「微信客服回复您」承诺；定稿描述原句 -->
      <text class="channel-desc">提交后运营人员会尽快查看处理；需要即时沟通时，请直接使用上方「在线客服」。</text>

      <view v-if="!submitted" class="form-box">
        <text class="form-label">问题类型</text>
        <view class="type-group">
          <view
            v-for="t in FEEDBACK_TYPES"
            :key="t"
            :class="['type-opt', selectedType === t ? 'active' : '']"
            @click="selectedType = t"
          >
            <text>{{ t }}</text>
          </view>
        </view>
        <text class="form-label">留言内容</text>
        <textarea
          v-model="content"
          class="leave-input"
          :maxlength="300"
          placeholder="请描述您遇到的问题或建议（必填）"
        />
        <view class="form-foot">
          <text class="char-count">{{ content.length }}/300</text>
          <button class="btn-submit" :disabled="!canSubmit" :loading="submitting" @click="submitLeave">
            提交留言
          </button>
        </view>
        <text class="form-hint">内容必填；情况紧急时，提交留言后也可直接使用在线客服即时沟通。</text>
      </view>

      <view v-else class="success-box">
        <view class="success-check">✓</view>
        <text class="success-title">留言已提交</text>
        <text class="success-desc">运营人员会尽快查看处理。&#10;需要即时沟通时，可直接使用上方「在线客服」。</text>
        <button class="btn-primary" @click="resetForm">再写一条</button>
      </view>
    </view>

    <!-- 流转口径（设计稿 route-note 虚框静态文案）：三类业务归医护，不在本页受理 -->
    <view class="route-note">
      <text>
        <text class="route-strong">康复建议、矫形日志、复查报告</text>等业务事项由医护团队处理，请在小程序对应页面查看；本入口仅受理产品使用咨询与问题反馈。
      </text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import { FEEDBACK_TYPES, submitFeedback, type FeedbackType } from '../../api/feedback'
import { getPatientProfile } from '../../api/profile'
import { useAuthStore } from '../../stores/auth'
import { logger } from '../../utils/logger'

const auth = useAuthStore()

// session-from（≤1024）：患者ID｜姓名｜设备号，首段=患者ID（服务端按 | 首段解析；
// 异常时服务端以会话 openid 反查 patients.wx_openid 兜底，见 PRD §7A.13 通道一）。
const sessionFrom = ref(auth.patientId ?? '')

const selectedType = ref<FeedbackType>(FEEDBACK_TYPES[0])
const content = ref('')
const submitting = ref(false)
const submitted = ref(false)

const canSubmit = computed(() => content.value.trim().length > 0 && !submitting.value)

// 档案只为丰满 session-from（姓名/设备号属 PRD「建议附」项）；失败不阻塞按钮
void getPatientProfile()
  .then((p) => {
    sessionFrom.value = [p.patientId || auth.patientId, p.name, p.deviceId]
      .filter((s): s is string => !!s)
      .join('|')
  })
  .catch(() => {
    // 仅有患者ID也满足 session-from 最低口径
  })

function openChatFallback() {
  // H5 无 open-type：如实提示，不伪造会话
  uni.showToast({ title: '请在微信小程序内使用在线客服', icon: 'none' })
}

async function submitLeave() {
  const text = content.value.trim()
  if (!text || submitting.value) return
  const patientId = auth.patientId
  if (!patientId) {
    uni.showToast({ title: '登录态缺失，请重新登录', icon: 'none' })
    return
  }
  submitting.value = true
  try {
    await submitFeedback({ patientId, type: selectedType.value, content: text })
    submitted.value = true
    logger.info('[T690] 客服留言提交成功', { type: selectedType.value, len: text.length })
  } catch (e: unknown) {
    const msg = userErrorCopy(e, { scope: 'patient', fallback: '提交失败，请稍后重试' })
    uni.showToast({ title: msg, icon: 'none' })
    logger.warn('[T690] 客服留言提交失败', { error: msg })
  } finally {
    submitting.value = false
  }
}

function resetForm() {
  content.value = ''
  selectedType.value = FEEDBACK_TYPES[0]
  submitted.value = false
}
</script>

<style scoped>
.page { min-height: 100%; padding-bottom: 48rpx; background: #f8fafc; }

.intro { text-align: center; padding: 16rpx 16rpx 36rpx; }
.intro-icon { width: 128rpx; height: 128rpx; border-radius: 50%; background: #eff6ff; display: flex; align-items: center; justify-content: center; margin: 0 auto 24rpx; font-size: 56rpx; }
.intro-title { display: block; font-size: 38rpx; font-weight: 700; color: #1e293b; margin-bottom: 12rpx; }
.intro-sub { display: block; font-size: 26rpx; color: #64748b; line-height: 1.7; white-space: pre-line; }

.channel { background: #fff; border: 1rpx solid #e2e8f0; border-radius: 28rpx; padding: 32rpx; margin: 0 32rpx 28rpx; }
.channel-head { display: flex; align-items: center; gap: 20rpx; margin-bottom: 12rpx; }
.channel-badge { width: 68rpx; height: 68rpx; border-radius: 20rpx; display: flex; align-items: center; justify-content: center; flex-shrink: 0; font-size: 32rpx; }
.channel-badge.online { background: #e8f5e9; }
.channel-badge.leave { background: #eff6ff; }
.channel-name-row { display: flex; align-items: center; gap: 12rpx; }
.channel-name { font-size: 30rpx; font-weight: 600; color: #1e293b; }
.channel-tag { font-size: 22rpx; color: #10ac84; background: #e8f5e9; border-radius: 8rpx; padding: 2rpx 12rpx; font-weight: 400; }
.channel-tag.async { color: #2563eb; background: #eff6ff; }
.channel-desc { display: block; font-size: 25rpx; color: #64748b; line-height: 1.7; margin: 4rpx 0 24rpx 88rpx; }

.btn-primary { width: 100%; padding: 26rpx; background: #2563eb; color: #fff; border: none; border-radius: 22rpx; font-size: 30rpx; font-weight: 600; line-height: 1.4; text-align: center; box-sizing: border-box; }
.btn-primary::after { border: none; }
.btn-primary text { color: #fff; }

.form-label { display: block; font-size: 25rpx; color: #475569; margin: 8rpx 0 16rpx; font-weight: 500; }
.type-group { display: flex; gap: 16rpx; margin-bottom: 28rpx; }
.type-opt { flex: 1; text-align: center; padding: 18rpx 0; font-size: 26rpx; color: #475569; background: #f8fafc; border: 1rpx solid #e2e8f0; border-radius: 18rpx; transition: all 0.15s; }
.type-opt.active { color: #2563eb; background: #eff6ff; border-color: #bfdbfe; font-weight: 600; }
.leave-input { width: 100%; height: 176rpx; border: 1rpx solid #e2e8f0; border-radius: 20rpx; padding: 22rpx 24rpx; font-size: 27rpx; line-height: 1.7; box-sizing: border-box; background: #fff; }
.form-foot { display: flex; justify-content: space-between; align-items: center; margin-top: 16rpx; }
.char-count { font-size: 22rpx; color: #94a3b8; }
.btn-submit { padding: 18rpx 36rpx; background: #2563eb; color: #fff; border: none; border-radius: 18rpx; font-size: 27rpx; font-weight: 600; line-height: 1.4; }
.btn-submit::after { border: none; }
.btn-submit[disabled] { background: #cbd5e1; color: #fff; opacity: 1; }
.form-hint { display: block; font-size: 22rpx; color: #94a3b8; line-height: 1.6; margin-top: 20rpx; }

.success-box { text-align: center; padding: 28rpx 12rpx 12rpx; }
.success-check { width: 104rpx; height: 104rpx; border-radius: 50%; background: #e8f5e9; color: #10ac84; display: flex; align-items: center; justify-content: center; margin: 0 auto 20rpx; font-size: 52rpx; font-weight: 600; }
.success-title { display: block; font-size: 30rpx; font-weight: 600; color: #1e293b; margin-bottom: 12rpx; }
.success-desc { display: block; font-size: 25rpx; color: #64748b; line-height: 1.7; margin-bottom: 24rpx; white-space: pre-line; }

.route-note { background: #f8fafc; border: 1rpx dashed #e2e8f0; border-radius: 20rpx; padding: 22rpx 26rpx; margin: 0 32rpx; font-size: 24rpx; color: #64748b; line-height: 1.7; }
.route-strong { color: #475569; font-weight: 600; }
</style>
