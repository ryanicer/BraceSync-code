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
          <span class="user-name">{{ auth.user?.name }}</span>
          <el-button size="small" @click="handleLogout">退出</el-button>
        </div>
      </el-header>
      <el-main class="page-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { Menu, Fold } from '@element-plus/icons-vue'
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

// 侧边栏菜单：12 页按当前角色权限过滤（PRD §7D.11）
const visibleMenus = computed<MenuItem[]>(() => {
  return pageRoutes
    .filter((r) => canAccess(auth.role ?? '', r.path))
    .map((r) => ({
      path: r.path,
      title: String(r.meta?.title ?? ''),
      icon: String(r.meta?.icon ?? ''),
    }))
})

const currentTitle = computed(() => String(route.meta?.title ?? ''))

async function handleLogout() {
  try {
    await ElMessageBox.confirm('确认退出登录？', '提示', { type: 'warning' })
  } catch {
    return
  }
  auth.logout()
  router.push('/login')
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
.user-name {
  font-size: 13px;
  color: #333;
}
.page-content {
  background: #f5f7fa;
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
