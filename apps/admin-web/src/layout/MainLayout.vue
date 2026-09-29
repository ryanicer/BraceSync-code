<template>
  <el-container class="layout" :class="{ 'layout-mobile': isMobile }">
    <!-- 桌面/平板：侧边栏 -->
    <el-aside
      v-if="!isMobile"
      :width="sidebarWidth"
      class="sidebar"
      :class="{ 'sidebar-collapsed': sidebarCollapsed && isTablet }"
    >
      <div class="sidebar-header">
        <span v-if="!sidebarCollapsed || isDesktop">🏥 运营平台</span>
        <span v-else>🏥</span>
      </div>
      <el-menu
        :default-active="route.path"
        router
        background-color="#2c3e50"
        text-color="#bdc3c7"
        active-text-color="#ffffff"
        :collapse="sidebarCollapsed && isTablet"
        class="sidebar-menu"
      >
        <el-menu-item v-for="item in visibleMenus" :key="item.path" :index="item.path">
          <span class="menu-icon">{{ item.icon }}</span>
          <template #title>{{ item.title }}</template>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <!-- 移动端：drawer 侧边栏（仅移动端渲染） -->
    <el-drawer
      v-if="isMobile"
      v-model="drawerVisible"
      direction="ltr"
      size="210px"
      :with-header="false"
      class="mobile-drawer"
    >
      <div class="sidebar sidebar-mobile">
        <div class="sidebar-header">🏥 运营平台</div>
        <el-menu
          :default-active="route.path"
          router
          background-color="#2c3e50"
          text-color="#bdc3c7"
          active-text-color="#ffffff"
          class="sidebar-menu"
          @select="handleMenuSelect"
        >
          <el-menu-item v-for="item in visibleMenus" :key="item.path" :index="item.path">
            <span class="menu-icon">{{ item.icon }}</span>
            <template #title>{{ item.title }}</template>
          </el-menu-item>
        </el-menu>
      </div>
    </el-drawer>

    <el-container>
      <el-header class="top-nav">
        <div class="top-nav-left">
          <!-- 汉堡按钮：仅平板/移动端显示 -->
          <el-button
            v-if="!isDesktop"
            text
            class="hamburger-btn"
            @click="toggleSidebar"
            :title="sidebarCollapsed ? '展开菜单' : '收起菜单'"
          >
            <el-icon :size="20">
              <component :is="sidebarCollapsed || (isMobile && !drawerVisible) ? 'Menu' : 'Fold'" />
            </el-icon>
          </el-button>
          <h2 class="top-nav-title">{{ currentTitle }}</h2>
        </div>
        <div class="top-nav-right">
          <el-tag v-if="auth.role" size="small" type="info" effect="plain">{{ roleName(auth.role) }}</el-tag>
          <div class="user-info">
            <span class="user-avatar">{{ avatarChar }}</span>
            <span class="user-name">{{ auth.user?.name }}</span>
          </div>
          <el-button size="small" @click="openPwdDialog">修改密码</el-button>
          <el-button size="small" @click="handleLogout">退出</el-button>
        </div>
      </el-header>
      <el-main class="page-content">
        <router-view />
      </el-main>
    </el-container>

    <!-- T487 自助改密弹窗：改的是「你本人」这条后台账号的登录口令，身份由令牌决定，表单里没有账号字段 -->
    <el-dialog
      v-model="pwdVisible"
      title="修改密码"
      width="420px"
      :close-on-click-modal="false"
      @closed="resetPwdForm"
    >
      <el-form ref="pwdFormRef" :model="pwdForm" :rules="pwdRules" label-position="top" @submit.prevent>
        <el-form-item label="当前密码" prop="oldPassword">
          <el-input
            v-model="pwdForm.oldPassword"
            type="password"
            placeholder="请输入当前登录密码"
            autocomplete="current-password"
            show-password
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="pwdForm.newPassword"
            type="password"
            :placeholder="`请输入新密码（${ADMIN_PWD_RULE}）`"
            autocomplete="new-password"
            show-password
          />
        </el-form-item>
        <el-form-item label="确认新密码" prop="confirmPassword">
          <el-input
            v-model="pwdForm.confirmPassword"
            type="password"
            placeholder="请再次输入新密码"
            autocomplete="new-password"
            show-password
            @keyup.enter="submitPwdChange"
          />
        </el-form-item>
        <p class="pwd-note">
          修改成功后当前登录态继续有效，下次登录请使用新密码；如忘记密码，仍可由运营管理员在「医护账号」页重置。
        </p>
      </el-form>
      <template #footer>
        <el-button @click="pwdVisible = false">取消</el-button>
        <el-button type="primary" :loading="pwdSubmitting" @click="submitPwdChange">确认修改</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Menu, Fold } from '@element-plus/icons-vue'
import { changeOwnPasswordApi } from '../api'
import { ADMIN_PWD_RULE, changePasswordErrorCopy, pwdFieldIssue, pwdFormIssue } from '../utils/password'
import { useAuthStore } from '../stores/auth'
import { pageRoutes } from '../router'
import { canAccess, roleName } from '../router/permissions'
import { useResponsive, useLocalStorageBool } from '../composables/useResponsive'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { isDesktop, isTablet, isMobile } = useResponsive()

// 侧边栏折叠偏好（localStorage 持久化，可直接 v-model 绑定）
const sidebarCollapsed = useLocalStorageBool('admin_sidebar_collapsed', false)
// 移动端 drawer 可见性（localStorage 持久化）
const drawerVisible = useLocalStorageBool('admin_sidebar_drawer', false)

// 计算侧边栏宽度：桌面始终 210px，平板折叠时变窄，移动无侧边栏
const sidebarWidth = computed(() => {
  if (isDesktop.value) return '210px'
  if (isTablet.value) return sidebarCollapsed.value ? '64px' : '210px'
  return '0px'
})

// 桌面端始终展开
watch(isDesktop, (desktop) => {
  if (desktop) {
    sidebarCollapsed.value = false
  }
})

function toggleSidebar() {
  if (isMobile.value) {
    drawerVisible.value = !drawerVisible.value
  } else {
    sidebarCollapsed.value = !sidebarCollapsed.value
  }
}

function handleMenuSelect() {
  if (isMobile.value) {
    drawerVisible.value = false
  }
}

interface MenuItem {
  path: string
  title: string
  icon: string
}

// 侧边栏菜单：pageRoutes 全集按当前角色权限过滤（PRD §7D.11）
// 条数不写死在这里 —— 由 test/permissions.spec.ts 的 PAGE_MODULES 基数断言把关（T345）
const visibleMenus = computed<MenuItem[]>(() => {
  return pageRoutes
    .filter((r) => canAccess(auth.role ?? '', r.path))
    .map((r) => ({
      path: r.path,
      title: String(r.meta?.title ?? ''),
      icon: String(r.meta?.icon ?? ''),
    }))
})

// G3：设计稿各页顶栏标题带 emoji 前缀（如 数据概览.html:87「📊 数据概览」），emoji 取自路由 meta.icon
const currentTitle = computed(() => {
  const title = String(route.meta?.title ?? '')
  const icon = String(route.meta?.icon ?? '')
  return icon ? `${icon} ${title}` : title
})

// G2（PRD §7D.0:994）：顶栏右侧圆形头像取当前登录人姓名首字，不得硬编码「管」字
const avatarChar = computed(() => (auth.user?.name ?? '').trim().charAt(0) || '?')

async function handleLogout() {
  try {
    await ElMessageBox.confirm('确认退出登录？', '提示', { type: 'warning' })
  } catch {
    return
  }
  auth.logout()
  router.push('/login')
}

// ===== T487 自助改密 =====
const pwdVisible = ref(false)
const pwdSubmitting = ref(false)
const pwdFormRef = ref<FormInstance>()
const pwdForm = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })

// 三格都在前端先判一遍：弱密码与「新密码＝当前密码」后端也拒（10400），但那句是三种原因共用的码，
// 让用户先看到「哪一格不合格」再提交，比让他吃一句含糊的「信息有误」有用。
// 判定本体在 utils/password.ts 的 pwdFieldIssue —— 规则与下面的提交前自证共用同一份，不在此重复。
function fieldRule(field: keyof typeof pwdForm) {
  return {
    validator: (_rule: unknown, _value: unknown, callback: (e?: Error) => void) => {
      const issue = pwdFieldIssue(pwdForm, field)
      if (issue) callback(new Error(issue))
      else callback()
    },
    trigger: 'blur',
  }
}

const pwdRules: FormRules = {
  oldPassword: [fieldRule('oldPassword')],
  newPassword: [fieldRule('newPassword')],
  confirmPassword: [fieldRule('confirmPassword')],
}

function openPwdDialog() {
  pwdVisible.value = true
}

/** 关闭后清空三格：口令不许留在组件状态里，下次打开也不该带出上一次填的 */
function resetPwdForm() {
  pwdForm.oldPassword = ''
  pwdForm.newPassword = ''
  pwdForm.confirmPassword = ''
  pwdFormRef.value?.clearValidate()
}

async function submitPwdChange() {
  // 提交前自证：el-form 的校验是异步的，实测在单测环境里 await validate() 会直接放行，
  // 而「不合格就不该发这一枪」是卡上判据（少一道就是一次必然 10400 的往返）。
  const issue = pwdFormIssue(pwdForm)
  if (issue) {
    ElMessage.warning(issue)
    return
  }
  const valid = await pwdFormRef.value?.validate().catch(() => false)
  if (!valid) return
  pwdSubmitting.value = true
  try {
    await changeOwnPasswordApi(pwdForm.oldPassword, pwdForm.newPassword)
    pwdVisible.value = false
    ElMessage.success('密码已修改，下次登录请使用新密码')
  } catch (e: unknown) {
    ElMessage.error(changePasswordErrorCopy(e))
  } finally {
    pwdSubmitting.value = false
  }
}
</script>

<style scoped>
.layout {
  min-height: 100vh;
}
.sidebar {
  background: #2c3e50;
  height: 100vh;
  overflow-y: auto;
  overflow-x: hidden;
}
.sidebar-mobile {
  height: 100%;
}
.sidebar-header {
  color: #fff;
  font-size: 16px;
  font-weight: 600;
  padding: 20px;
  border-bottom: 1px solid #34495e;
  white-space: nowrap;
  overflow: hidden;
  text-align: center;
}
.sidebar-collapsed .sidebar-header {
  padding: 20px 10px;
  font-size: 20px;
}
.sidebar-menu {
  border-right: none;
}
.sidebar-menu :deep(.el-menu-item.is-active) {
  background: #1a6db5;
}
.menu-icon {
  margin-right: 10px;
}
.sidebar-collapsed .menu-icon {
  margin-right: 0;
}
.top-nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #e8ecf0;
  padding: 0 20px;
}
.top-nav-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.hamburger-btn {
  color: #333;
  padding: 4px 8px;
}
.top-nav-title {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.top-nav-right {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}
.user-info {
  display: flex;
  align-items: center;
  gap: 8px;
}
.user-name {
  font-size: 13px;
  color: #333;
}
/* T289 G2：设计稿 数据概览.html:26 .user-avatar 原样 */
.user-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: #1a6db5;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  flex-shrink: 0;
}
.page-content {
  background: #f5f7fa;
}

/* T487 改密弹窗里的说明行：比表单字段弱一级，不抢输入焦点 */
.pwd-note {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: #909399;
}

/* 移动端 drawer 样式 */
:deep(.mobile-drawer .el-drawer__body) {
  padding: 0;
}

/* 平板断点（768-1279px） */
@media (max-width: 1279px) and (min-width: 768px) {
  .top-nav {
    padding: 0 16px;
  }
  .top-nav-title {
    font-size: 16px;
  }
}

/* 移动端断点（<768px） */
@media (max-width: 767px) {
  .top-nav {
    padding: 0 12px;
  }
  .top-nav-title {
    font-size: 15px;
  }
  .top-nav-right .user-name {
    display: none;
  }
  .top-nav-right {
    gap: 8px;
  }
  .page-content {
    padding: 12px;
  }
}
</style>
