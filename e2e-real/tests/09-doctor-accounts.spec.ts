import { test, expect } from '@playwright/test'
import { realLogin, gotoMenu, menuItems } from '../real-helpers'
import { requireDeployedBuild } from '../deploy-guard'

/**
 * T324 · e2e-real 部署守卫的落位示例：T315「医护账号」页（admin-web 第 15 页）
 *
 * 为什么是这一条：T315 已于 2026-09-22 19:21 合入 main（#172），但 staging 上部署的前端包是
 * 当天 13:58 构建的那版，侧栏还只有 14 页、入口文字「医护账号」在已部署包里根本不存在。
 * 这正是本卡要解的那一类断言 —— 换在 T324 之前，它会把【任何】改前端行为的 PR 的门禁钉成假红
 * （同形状的事故见 T322：3 failed / 29 passed）。
 *
 * 现在的口径：先探已部署包有没有该入口；没有 ⇒ 显式 post-deploy 跳过（报告里能按标签反查），
 * 有 ⇒ 下面这些断言照常真跑，此后该页任何回归一样判红。部署后回归阶段（定时 / 手动）
 * 仍探不到入口就直接判红，不允许长期挂在「跳过」上蒙混。
 */

const MENU_TITLE = '医护账号'

test.describe('09-医护账号（T315，第 15 页）', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await expect(menuItems(page).first()).toBeVisible({ timeout: 20_000 })
  })

  test('9.1 侧栏入口可进入，列表按设计稿渲染 10 列', async ({ page }) => {
    await requireDeployedBuild(page, {
      marker: 'T315-doctor-accounts',
      why: 'T315 合入 main 后 staging 尚未部署该前端包',
      probe: async (p) => (await menuItems(p).filter({ hasText: MENU_TITLE }).count()) > 0,
    })

    await gotoMenu(page, MENU_TITLE)
    // 登录后浏览器落在根路径（无 /admin 前缀，见 real-helpers 顶部 T279 实测说明），故按 router 路径断
    await expect(page).toHaveURL(/\/doctor-accounts$/, { timeout: 15_000 })

    // 表头 10 列，顺序与文案按 apps/admin-web/src/pages/doctors/index.vue（设计稿 医护账号.html:141）
    await expect(page.locator('.medical-accounts .el-table__header-wrapper thead th')).toHaveText(
      ['姓名', '登录账号', '手机号', '科室', '所属团队', '职称', '管理患者数', '状态', '创建时间', '操作'],
      { timeout: 20_000 },
    )

    // 页面骨架：搜索框 + 两个筛选 + 新建按钮 + 计数提示（条数由真实数据决定，只断格式）
    await expect(page.locator('.medical-accounts .search-input input')).toBeVisible()
    await expect(page.getByRole('button', { name: '+ 新建医护账号' })).toBeVisible()
    await expect(page.locator('.medical-accounts .count-hint')).toContainText(/共 \d+ 个账号/)
    await expect(page.locator('.medical-accounts .count-hint')).toContainText(/启用 \d+ \/ 禁用 \d+/)

    // 无渲染事故（脱敏/占位逻辑漏网时会冒出 undefined/NaN）
    await expect(page.locator('.medical-accounts')).not.toContainText(/undefined|NaN/)
  })
})
