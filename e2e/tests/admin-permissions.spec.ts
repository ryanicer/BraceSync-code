import { test, expect } from '@playwright/test'
import {
  adminRoutes, adminLogin, menuItems, ADMIN_PAGES, DOCTOR_PAGES, CS_PAGES,
} from '../admin-helpers'

/**
 * admin-web 权限守卫（PRD §7D.11 预置角色权限矩阵）
 * admin 12 页 / doctor 4 页 / cs 1 页，越权访问跳 /403
 */

test.describe('admin 全量权限', () => {
  test('侧边栏显示全部 12 个菜单', async ({ page }) => {
    await adminLogin(page, 'admin')
    await expect(menuItems(page)).toHaveCount(12)
    for (const title of ['数据概览', '实时监控', '患者管理', '团队管理', '设备管理', '告警管理', '患者沟通', '矫形日志', '安装记录', '技师管理', '权限控制', '系统配置']) {
      await expect(menuItems(page).filter({ hasText: title })).toHaveCount(1)
    }
  })

  test('admin 直达 12 页全部放行（不落 403）', async ({ page }) => {
    await adminLogin(page, 'admin')
    for (const path of ADMIN_PAGES) {
      await page.goto(path)
      await expect(page).toHaveURL(new RegExp(path.replace(/-/g, '\\-') + '$'))
      await expect(page.locator('.forbidden-card')).toHaveCount(0)
    }
  })
})

test.describe('doctor 受限权限', () => {
  test('侧边栏仅显示 4 个菜单', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await expect(menuItems(page)).toHaveCount(4)
    for (const title of ['数据概览', '实时监控', '告警管理', '矫形日志']) {
      await expect(menuItems(page).filter({ hasText: title })).toHaveCount(1)
    }
  })

  test('doctor 可见 4 页放行', async ({ page }) => {
    await adminLogin(page, 'doctor')
    for (const path of DOCTOR_PAGES) {
      await page.goto(path)
      await expect(page.locator('.forbidden-card')).toHaveCount(0)
    }
  })

  test('doctor 越权访问 /patients 跳 403', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await page.goto(adminRoutes.patients)
    await expect(page).toHaveURL(/\/403/)
    await expect(page.locator('.forbidden-card')).toContainText('403 · 无访问权限')
    await expect(page.locator('.forbidden-card')).toContainText('医生')
  })

  test('doctor 越权访问 /settings 与 /technicians 跳 403', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await page.goto(adminRoutes.settings)
    await expect(page).toHaveURL(/\/403/)
    await page.goto(adminRoutes.technicians)
    await expect(page).toHaveURL(/\/403/)
  })

  test('403 页返回首页按钮可用', async ({ page }) => {
    await adminLogin(page, 'doctor')
    await page.goto(adminRoutes.patients)
    await expect(page).toHaveURL(/\/403/)
    await page.locator('.forbidden-card').getByRole('button', { name: '返回首页' }).click()
    await expect(page).toHaveURL(/\/dashboard/)
  })
})

test.describe('cs 受限权限', () => {
  test('侧边栏仅显示 1 个菜单（患者沟通）', async ({ page }) => {
    await adminLogin(page, 'cs')
    await page.goto(adminRoutes.communication)
    await expect(menuItems(page)).toHaveCount(1)
    await expect(menuItems(page).first()).toContainText('患者沟通')
  })

  test('cs 可见页放行、越权页全部 403', async ({ page }) => {
    await adminLogin(page, 'cs')
    for (const path of CS_PAGES) {
      await page.goto(path)
      await expect(page.locator('.forbidden-card')).toHaveCount(0)
    }
    for (const path of ['/dashboard', '/alerts', '/patients', '/monitor']) {
      await page.goto(path)
      await expect(page).toHaveURL(/\/403/)
    }
  })

  test('未知路径重定向到 /dashboard（cs 则再被守卫拦到 403）', async ({ page }) => {
    await adminLogin(page, 'cs')
    await page.goto('/not-exist-page')
    await expect(page).toHaveURL(/\/403/)
  })
})
