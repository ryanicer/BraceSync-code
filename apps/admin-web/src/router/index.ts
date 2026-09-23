import { createRouter, createWebHistory, type RouterHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { canAccess, landingPathFor } from './permissions'

// 业务页路由（对齐架构 §5.4 / PRD §7D，meta.title 用于顶栏与菜单，菜单顺序即此数组顺序）
export const pageRoutes: RouteRecordRaw[] = [
  { path: '/dashboard', name: 'Dashboard', component: () => import('../pages/dashboard/index.vue'), meta: { title: '数据概览', icon: '📊' } },
  { path: '/monitor', name: 'Monitor', component: () => import('../pages/monitor/index.vue'), meta: { title: '实时监控', icon: '🔍' } },
  { path: '/patients', name: 'Patients', component: () => import('../pages/patients/index.vue'), meta: { title: '患者管理', icon: '👤' } },
  { path: '/teams', name: 'Teams', component: () => import('../pages/teams/index.vue'), meta: { title: '团队管理', icon: '👥' } },
  { path: '/devices', name: 'Devices', component: () => import('../pages/devices/index.vue'), meta: { title: '设备管理', icon: '📱' } },
  { path: '/alerts', name: 'Alerts', component: () => import('../pages/alerts/index.vue'), meta: { title: '告警管理', icon: '🚨' } },
  { path: '/communication', name: 'Communication', component: () => import('../pages/communication/index.vue'), meta: { title: '患者沟通', icon: '💬' } },
  { path: '/orthosis-log', name: 'OrthosisLog', component: () => import('../pages/orthosis-log/index.vue'), meta: { title: '矫形日志', icon: '📝' } },
  { path: '/install-records', name: 'InstallRecords', component: () => import('../pages/install-records/index.vue'), meta: { title: '安装记录', icon: '📋' } },
  { path: '/review-records', name: 'ReviewRecords', component: () => import('../pages/review-records/index.vue'), meta: { title: '复查报告', icon: '📄' } },
  { path: '/review-templates', name: 'ReviewTemplates', component: () => import('../pages/review-templates/index.vue'), meta: { title: '复查模板管理', icon: '🗂️' } },
  { path: '/technicians', name: 'Technicians', component: () => import('../pages/technicians/index.vue'), meta: { title: '技师管理', icon: '🔧' } },
  { path: '/doctor-accounts', name: 'DoctorAccounts', component: () => import('../pages/doctors/index.vue'), meta: { title: '医护账号', icon: '🩺' } }, // T315 / PRD §7D.10，位置对齐设计稿侧栏第 12 项
  { path: '/roles', name: 'Roles', component: () => import('../pages/roles/index.vue'), meta: { title: '权限控制', icon: '🔐' } },
  { path: '/settings', name: 'Settings', component: () => import('../pages/settings/index.vue'), meta: { title: '系统配置', icon: '⚙️' } },
]

/**
 * 登录态下的落地页（T269 D3）。未登录时沿用 /dashboard，由守卫改跳 /login 并带 redirect。
 */
export function authLandingPath(): string {
  const auth = useAuthStore()
  if (!auth.isLoggedIn) return '/dashboard'
  return landingPathFor(auth.role)
}

export const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'Login', component: () => import('../pages/login/index.vue'), meta: { title: '登录' } },
  { path: '/403', name: 'Forbidden', component: () => import('../pages/forbidden/index.vue'), meta: { title: '无权限' } },
  {
    path: '/',
    component: () => import('../layout/MainLayout.vue'),
    children: [
      { path: '', redirect: () => authLandingPath() },
      ...pageRoutes,
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: () => authLandingPath() },
]

// 权限路由守卫：未登录跳登录；已登录但无页面权限跳 403（PRD §7D.11 权限矩阵）
export function registerPermissionGuard(router: ReturnType<typeof createRouter>): void {
  router.beforeEach((to) => {
    if (to.path === '/login' || to.path === '/403') return true
    const auth = useAuthStore()
    if (!auth.isLoggedIn) {
      return { path: '/login', query: { redirect: to.fullPath } }
    }
    if (!canAccess(auth.role ?? '', to.path)) {
      return { path: '/403' }
    }
    return true
  })
}

export function createAppRouter(history?: RouterHistory) {
  const router = createRouter({
    // T336：history base 取构建 base（vite.config.ts 的 base = ADMIN_BASE），路由表本身仍是根路径
    // （/patients），浏览器地址则是 ${base}patients（/admin/patients）——深链与刷新才能匹配到本页。
    history: history ?? createWebHistory(import.meta.env.BASE_URL),
    routes,
  })
  registerPermissionGuard(router)
  return router
}

export const router = createAppRouter()
