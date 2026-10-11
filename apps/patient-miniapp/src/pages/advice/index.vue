<template>
  <view class="page">
    <!-- ===== 区块①：你的医护团队（只两列：角色 + 职称，无姓名；稿面 advice.html:43-62）===== -->
    <view class="section">
      <text class="section-title">你的医护团队</text>

      <view v-if="teamLoading" class="card state-card">
        <text class="state-text">加载中...</text>
      </view>
      <view v-else-if="teamError" class="card state-card">
        <text class="state-text">{{ teamError }}</text>
        <text class="state-sub" @click="loadCareTeam">点击重试</text>
      </view>
      <view v-else-if="team.length === 0" class="card state-card">
        <text class="state-text">尚未绑定医护团队</text>
      </view>
      <view v-else class="card team-strip">
        <!-- T659 按稿 advice.html:45-63：横排单行，每成员一列＝装饰 emoji 圆头像（角色+底色承担，
             无文字角色标签、无姓名）＋下方职称；团队可有多枚在职医生（SQL 逐医生出行），故键带下标 -->
        <view v-for="(member, idx) in team" :key="member.memberType + '-' + idx" class="team-member">
          <view :class="['team-avatar', member.memberType === 'technician' ? 'team-avatar-tech' : 'team-avatar-doctor']">
            <text class="team-avatar-emoji">{{ careTeamAvatar(member.memberType) }}</text>
          </view>
          <text :class="['team-title', { 'team-title-tech': member.memberType === 'technician' }]">
            {{ careTeamTitleLabel(member) }}
          </text>
        </view>
      </view>
    </view>

    <!-- ===== 区块②：建议时间轴（倒序由后端保证；摘要 CSS 两行截断、点击展开全文；稿面 :66-104）===== -->
    <view class="section">
      <text class="section-title">康复建议</text>

      <view v-if="listLoading" class="card state-card">
        <text class="state-text">加载中...</text>
      </view>
      <view v-else-if="listError" class="card state-card">
        <text class="state-text">{{ listError }}</text>
        <text class="state-sub" @click="loadAdvice">点击重试</text>
      </view>
      <view v-else-if="advices.length === 0" class="card state-card">
        <text class="state-text">暂无医护建议</text>
        <text class="state-sub">医护写下的建议会出现在这里</text>
      </view>
      <view v-else class="timeline">
        <view
          v-for="item in advices"
          :key="item.adviceId"
          class="advice-item"
          @click="toggleExpand(item.adviceId)"
        >
          <text class="advice-meta">{{ adviceMetaLabel(item) }}</text>
          <!-- T659 按稿：摘要恒渲染原文，两行截断交给 CSS line-clamp；展开显全文 -->
          <text v-if="expanded[item.adviceId]" class="advice-full">{{ item.content }}</text>
          <text v-else class="advice-summary">{{ item.content }}</text>
          <!-- 稿面 advice.html:71-97 四条（含短正文）均恒显展开控件；本页唯一点击行为，
               纯前端展示切换，不落服务端、不产生已读态 -->
          <text class="advice-toggle">
            {{ expanded[item.adviceId] ? '收起 ▴' : '展开全文 ▾' }}
          </text>
        </view>
      </view>
    </view>

    <!-- ===== 区块③：页脚静态指引（§十 R4 甲；纯文案，非按钮、非链接、不可点；稿面 :105-106）===== -->
    <view class="section">
      <text class="footer-note">如需反馈佩戴情况，可在「我的 · 矫形日志」里记录。</text>
    </view>
  </view>
</template>

<script setup lang="ts">
/**
 * T641 患者端「康复建议」页（稿面 docs/design/patient/advice.html 唯一基准）。
 *
 * 链路 = 留言板（Boss 2026-10-08 13:12 拍板 1）：无推送、无已读回执，所以本页没有
 * 「新消息」徽标，也没有任何写通道 —— 全页 0 个输入控件、0 次请求写端点
 * （POST/PUT/DELETE 在网关是 doctorAdminOnly，患者令牌进不来）。
 *
 * 与稿面「零交互」的关系：唯一的点击是「摘要 ⇄ 全文」的纯展示切换（稿面 :170 自己的画法），
 * 它不产生服务端痕迹；退场的是回复 / 点赞 / 已读 / 输入这四类会被服务端记下来的元素。
 *
 * 🔴 隐私（§八）：团队区两列 = 角色 + 职称。职称回落链 title → department →「医护团队」在
 * 服务端组 DTO 时就算完（handler.go adviceDisplayTitle），前端这里没有任何「拿姓名兜底」的分支，
 * 也不读「我的」页档案接口里那两枚医护/团队姓名键（针清单在本包的 advice-t641.spec.ts，
 * 姓名键字面量不进被筛的那枚页面文件，否则这枚针自己就成了泄漏面）。
 */
import { onMounted, reactive, ref } from 'vue'
import { userErrorCopy } from '@bracesync/shared-utils'
import type { Advice, CareTeamMember } from '@bracesync/shared-types'
import { getCareTeam, listPatientAdvice } from '../../api/advice'
import { useAuthStore } from '../../stores/auth'
import { adviceMetaLabel, careTeamAvatar, careTeamTitleLabel } from '../../utils/advice'

const auth = useAuthStore()

const team = ref<CareTeamMember[]>([])
const teamLoading = ref(false)
const teamError = ref('')

const advices = ref<Advice[]>([])
const listLoading = ref(false)
const listError = ref('')

/** adviceId → 是否展开全文；键存在与否即展开态，收起时删键（不留 false 那种「记得我折过」的状态） */
const expanded = reactive<Record<string, boolean>>({})

function toggleExpand(adviceId: string) {
  if (expanded[adviceId]) {
    delete expanded[adviceId]
    return
  }
  expanded[adviceId] = true
}

async function loadCareTeam() {
  // self-scope 读口靠令牌认人：没有登录态就不发这一枪（发了也只吃到网关 401/403）
  if (!auth.isLoggedIn) {
    teamError.value = '请先登录'
    return
  }
  teamLoading.value = true
  teamError.value = ''
  try {
    const list = await getCareTeam()
    team.value = Array.isArray(list) ? list : []
  } catch (e: unknown) {
    teamError.value = userErrorCopy(e, { scope: 'patient', fallback: '加载医护团队失败' })
  } finally {
    teamLoading.value = false
  }
}

async function loadAdvice() {
  if (!auth.patientId) {
    listError.value = '请先登录'
    return
  }
  listLoading.value = true
  listError.value = ''
  try {
    const list = await listPatientAdvice(auth.patientId)
    advices.value = Array.isArray(list) ? list : []
  } catch (e: unknown) {
    listError.value = userErrorCopy(e, { scope: 'patient', fallback: '加载医护建议失败' })
  } finally {
    listLoading.value = false
  }
}

onMounted(() => {
  loadCareTeam()
  loadAdvice()
})
</script>

<style scoped lang="scss">
.page {
  padding: 24rpx;
  min-height: 100vh;
  background: #f8fafc;
}
.section {
  margin-bottom: 24rpx;
}
.section-title {
  display: block;
  font-size: 28rpx;
  font-weight: 500;
  color: #1e293b;
  margin-bottom: 16rpx;
}
.card {
  background: #fff;
  border: 1rpx solid #e2e8f0;
  border-radius: 24rpx;
  padding: 8rpx 28rpx;
}
.state-card {
  text-align: center;
  padding: 48rpx 24rpx;
}
.state-text {
  font-size: 26rpx;
  color: #6b7280;
}
.state-sub {
  display: block;
  margin-top: 8rpx;
  font-size: 24rpx;
  color: #9ca3af;
}

/* T659 按稿 app-team-strip：横排单行，每成员一列＝圆头像 + 下方职称（advice.html:186-192） */
.team-strip {
  display: flex;
  align-items: flex-start;
  gap: 16rpx;
  padding: 28rpx 20rpx;
}
.team-member {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12rpx;
  min-width: 0;
}
.team-avatar {
  width: 88rpx;
  height: 88rpx;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.team-avatar-emoji {
  font-size: 40rpx;
  line-height: 1;
}
.team-avatar-doctor {
  background: #dbeafe;
}
.team-avatar-tech {
  background: #f1f5f9;
}
.team-title {
  font-size: 24rpx;
  color: #1e293b;
  white-space: nowrap;
}
.team-title-tech {
  color: #64748b;
}

/* 稿面 app-timeline：左侧竖线 + 每枚圆点；小程序里用容器边框与条目 ::before 画同一形状 */
.timeline {
  position: relative;
  padding-left: 30rpx;
}
.timeline::before {
  content: '';
  position: absolute;
  left: 12rpx;
  top: 20rpx;
  bottom: 20rpx;
  width: 2rpx;
  background: #e2e8f0;
}
.advice-item {
  position: relative;
  background: #fff;
  border: 1rpx solid #e2e8f0;
  border-radius: 24rpx;
  padding: 28rpx;
  margin-bottom: 20rpx;
}
.advice-item::before {
  content: '';
  position: absolute;
  left: -25rpx;
  top: 38rpx;
  width: 14rpx;
  height: 14rpx;
  border-radius: 50%;
  background: #2563EB;
  border: 4rpx solid #dbeafe;
}
.advice-meta {
  display: block;
  font-size: 24rpx;
  font-weight: 500;
  color: #2563EB;
  margin-bottom: 12rpx;
}
.advice-summary {
  font-size: 26rpx;
  color: #475569;
  line-height: 1.6;
  /* T659 按稿 advice.html:201：两行截断（视觉截断，不做 JS 字符切；展开看全文） */
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
}
.advice-full {
  font-size: 26rpx;
  color: #475569;
  line-height: 1.7;
}
.advice-toggle {
  display: block;
  font-size: 22rpx;
  color: #94a3b8;
  margin-top: 12rpx;
}
.footer-note {
  display: block;
  font-size: 24rpx;
  color: #94a3b8;
  text-align: center;
  line-height: 1.6;
  padding: 16rpx 0 8rpx;
}
</style>
