<template>
  <view class="page">
    <view class="page-header">
      <text class="page-title">复查管理</text>
    </view>

    <view v-if="loading" class="card empty-card">
      <text class="empty-text">加载中...</text>
    </view>

    <view v-else-if="records.length > 0" class="section">
      <view v-for="item in records" :key="item.reviewId" class="card review-card">
        <view class="review-head">
          <text class="review-date">{{ item.reviewDate }}</text>
          <view :class="['review-type-tag', item.reviewType === 'initial' ? 'tag-initial' : 'tag-follow']">
            <text>{{ item.reviewType === 'initial' ? '初诊' : '复诊' }}</text>
          </view>
        </view>

        <view v-if="item.findings" class="review-findings">
          <text class="label">检查所见：</text>
          <text>{{ item.findings }}</text>
        </view>

        <view v-if="item.nextReviewDate" class="review-meta">
          <text class="label">下次复查：</text>
          <text>{{ item.nextReviewDate }}</text>
        </view>

        <view v-if="item.reportFileId" class="review-report">
          <view class="report-info">
            <text class="label">复查报告：</text>
            <text class="report-name">{{ item.reportFileName || '报告文件' }}</text>
          </view>
          <button
            class="download-btn"
            :class="{ 'btn-disabled': downloadingId === item.reviewId }"
            :disabled="downloadingId === item.reviewId"
            @click="downloadReport(item)"
          >
            {{ downloadingId === item.reviewId ? '下载中...' : '下载报告' }}
          </button>
        </view>
      </view>
    </view>

    <view v-else class="card empty-card">
      <text class="empty-text">暂无复查记录</text>
      <text class="empty-sub">医生上传复查报告后将在此展示</text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { request } from '../../utils/request'
import { useAuthStore } from '../../stores/auth'
import type { ReviewRecord } from '@bracesync/shared-types'

const auth = useAuthStore()
const records = ref<ReviewRecord[]>([])
const loading = ref(false)
const errorMsg = ref('')
const downloadingId = ref<string | null>(null)

async function loadRecords() {
  const pid = auth.patientId
  if (!pid) {
    errorMsg.value = '请先登录'
    return
  }
  loading.value = true
  errorMsg.value = ''
  try {
    const list = await request<ReviewRecord[]>({
      url: `/api/v1/patients/${pid}/review-records`,
    })
    records.value = Array.isArray(list) ? list : []
  } catch (e: unknown) {
    errorMsg.value = e instanceof Error ? e.message : '加载复查记录失败'
  } finally {
    loading.value = false
  }
}

/**
 * 下载复查报告：使用后端返回的预签名下载 URL，下载后本地打开。
 * 合同 R2：不做在线预览，下载后本地打开。
 */
async function downloadReport(item: ReviewRecord) {
  if (!item.reportDownloadUrl) {
    uni.showToast({ title: '暂无下载链接', icon: 'none' })
    return
  }
  downloadingId.value = item.reviewId
  try {
    const res = await new Promise<UniApp.DownloadSuccessData>((resolve, reject) => {
      uni.downloadFile({
        url: item.reportDownloadUrl!,
        success: resolve,
        fail: reject,
      })
    })
    if (res.statusCode !== 200) {
      throw new Error(`下载失败（HTTP ${res.statusCode}）`)
    }
    // 打开下载的文件（PDF/图片由系统对应应用打开）
    await new Promise<void>((resolve, reject) => {
      uni.openDocument({
        filePath: res.tempFilePath,
        showMenu: true,
        success: () => resolve(),
        fail: reject,
      })
    })
  } catch (e: unknown) {
    uni.showToast({
      title: e instanceof Error ? e.message : '下载失败',
      icon: 'none',
    })
  } finally {
    downloadingId.value = null
  }
}

onMounted(() => {
  loadRecords()
})
</script>

<style scoped lang="scss">
.page {
  padding: 24rpx;
  min-height: 100vh;
  background: #f5f7fa;
}
.page-header {
  margin-bottom: 24rpx;
}
.page-title {
  font-size: 36rpx;
  font-weight: 600;
  color: #1f2937;
}
.section {
  display: flex;
  flex-direction: column;
  gap: 16rpx;
}
.card {
  background: #fff;
  border-radius: 12rpx;
  padding: 24rpx;
  box-shadow: 0 1rpx 4rpx rgba(0, 0, 0, 0.04);
}
.review-card {
  display: flex;
  flex-direction: column;
  gap: 12rpx;
}
.review-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.review-date {
  font-size: 30rpx;
  font-weight: 600;
  color: #1f2937;
}
.review-type-tag {
  padding: 4rpx 16rpx;
  border-radius: 16rpx;
  font-size: 22rpx;
}
.tag-initial {
  background: #e0f2fe;
  color: #0369a1;
}
.tag-follow {
  background: #dcfce7;
  color: #15803d;
}
.review-findings,
.review-meta,
.report-info {
  font-size: 26rpx;
  color: #4b5563;
  line-height: 1.6;
}
.label {
  color: #6b7280;
}
.report-name {
  color: #2563eb;
}
.download-btn {
  margin-top: 8rpx;
  background: #2563eb;
  color: #fff;
  font-size: 26rpx;
  border-radius: 8rpx;
  padding: 12rpx 0;
}
.btn-disabled {
  background: #94a3b8;
}
.empty-card {
  text-align: center;
  padding: 64rpx 24rpx;
}
.empty-text {
  font-size: 28rpx;
  color: #6b7280;
}
.empty-sub {
  display: block;
  margin-top: 12rpx;
  font-size: 24rpx;
  color: #9ca3af;
}
</style>
