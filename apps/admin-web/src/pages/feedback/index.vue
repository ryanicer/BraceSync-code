<template>
  <div class="feedback-page">
    <iframe
      v-if="frameSrc"
      :src="frameSrc"
      class="feedback-frame"
      title="问题反馈"
      data-testid="feedback-frame"
    ></iframe>
    <div v-else class="feedback-placeholder" data-testid="feedback-placeholder">
      <div class="ph-icon">📮</div>
      <h3>问题反馈通道尚未开通</h3>
      <p>本环境的反馈服务还没配置好。急事请先在运维协作群说明，通道开通后在此页提交即可留档跟进。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
// T708 运维反馈（案 C）：本页只是 iframe 宿主——表单、建单、附件全在独立容器的代理 service 静态页里，
// 客户系统零表零接口改动；环境由构建变量随包带出，运维不可见也不可改（详见设计稿 S1 入口流）。
import { computed } from 'vue'
import { buildFeedbackFrameSrc } from '../../utils/feedback'

const frameSrc = computed(() =>
  buildFeedbackFrameSrc(import.meta.env.VITE_FEEDBACK_SERVICE_URL, import.meta.env.VITE_FEEDBACK_ENV),
)
</script>

<style scoped>
.feedback-page {
  min-height: 100%;
  display: flex;
  flex-direction: column;
}
.feedback-frame {
  flex: 1;
  width: 100%;
  min-height: calc(100vh - 120px);
  border: none;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.06);
}
.feedback-placeholder {
  margin: 40px auto;
  max-width: 420px;
  text-align: center;
  background: #fff;
  border-radius: 12px;
  padding: 40px 32px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}
.ph-icon {
  font-size: 44px;
  margin-bottom: 12px;
}
.feedback-placeholder h3 {
  margin: 0 0 10px;
  color: #333;
  font-size: 16px;
}
.feedback-placeholder p {
  margin: 0;
  color: #909399;
  font-size: 13px;
  line-height: 1.7;
}
</style>
