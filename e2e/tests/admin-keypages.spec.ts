import { test, expect } from '@playwright/test'
import { adminRoutes, adminLogin, adminMessage, pickSelectOption, tableRows } from '../admin-helpers'

/**
 * admin-web 关键页抽查：设备管理（筛选）/ 技师管理（启停）/ 系统配置（阈值+通知规则）
 * mock 对齐：devices.ts（6 台，online 3 / abnormal 1 / offline 1 / unbound 1）、
 * org.ts（4 技师，冯师傅禁用）、system.ts（阈值默认值 + 4 条通知规则）
 */

test.describe('设备管理', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.devices)
  })

  test('渲染 6 台设备且状态 tag 正确', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(6)
    const onlineRow = rows.filter({ hasText: 'DEV-A3F312' })
    await expect(onlineRow.locator('.el-tag--success')).toContainText('在线')
    await expect(rows.filter({ hasText: 'DEV-C9D789' }).locator('.el-tag--danger')).toContainText('异常')
    await expect(rows.filter({ hasText: 'DEV-D2A012' }).locator('.el-tag--warning')).toContainText('离线')
    await expect(rows.filter({ hasText: 'DEV-F8C590' }).locator('.el-tag--info')).toContainText('未绑定')
  })

  test('状态筛选：在线 → 3 台', async ({ page }) => {
    await pickSelectOption(page, page.locator('.status-select'), '在线')
    await expect(tableRows(page)).toHaveCount(3)
  })

  test('状态筛选：异常 → 1 台（DEV-C9D789）', async ({ page }) => {
    await pickSelectOption(page, page.locator('.status-select'), '异常')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('DEV-C9D789')
  })

  test('关键词搜索设备ID（回车触发）', async ({ page }) => {
    await page.locator('.search-input input').fill('DEV-B7E456')
    await page.locator('.search-input input').press('Enter')
    const rows = tableRows(page)
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('陈子航')
  })
})

test.describe('技师管理', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.technicians)
  })

  test('渲染 4 名技师及认证/账号状态', async ({ page }) => {
    const rows = tableRows(page)
    await expect(rows).toHaveCount(4)
    await expect(rows.filter({ hasText: '周师傅' })).toContainText('已认证')
    await expect(rows.filter({ hasText: '郑师傅' })).toContainText('未认证')
    await expect(rows.filter({ hasText: '冯师傅' })).toContainText('禁用')
  })

  test('禁用技师：popconfirm 确认后状态翻转', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '周师傅' })
    await row.getByRole('button', { name: '禁用' }).click()
    await expect(page.locator('.el-popconfirm')).toContainText('确认禁用技师 周师傅？')
    await page.locator('.el-popconfirm').getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('已禁用')
    await expect(row).toContainText('禁用')
    await expect(row.getByRole('button', { name: '启用' })).toBeVisible()
  })

  test('启用技师：冯师傅 禁用 → 启用', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '冯师傅' })
    await row.getByRole('button', { name: '启用' }).click()
    await page.locator('.el-popconfirm').getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('已启用')
    await expect(row).toContainText('启用')
  })

  test('popconfirm 取消不改变状态', async ({ page }) => {
    const row = tableRows(page).filter({ hasText: '吴师傅' })
    await row.getByRole('button', { name: '禁用' }).click()
    await page.locator('.el-popconfirm').getByRole('button', { name: '取消' }).click()
    await expect(row).toContainText('启用')
  })
})

test.describe('系统配置', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
  })

  test('阈值表单加载默认值（对齐 DEFAULT_THRESHOLDS）', async ({ page }) => {
    await expect(page.getByText('全局系统参数')).toBeVisible()
    await expect(page.locator('.settings-form')).toContainText('数据采集间隔')
    await expect(page.locator('.settings-form')).toContainText('每日佩戴目标时长')
    await expect(page.locator('.settings-form')).toContainText('压力偏高阈值')
    await expect(page.locator('.settings-form')).toContainText('佩戴中断判定时间')
    // T247 新增采集间隔为第一项；定位"每日佩戴目标时长"对应的 el-input-number
    const formItem = page.locator('.el-form-item', { hasText: '每日佩戴目标时长' })
    await expect(formItem.locator('.el-input-number input')).toHaveValue('22')
    // 采集间隔默认 60s
    const intervalItem = page.locator('.el-form-item', { hasText: '数据采集间隔' })
    await expect(intervalItem.locator('.el-input-number input')).toHaveValue('60')
  })

  /**
   * T270 假绿 #2 订正（README §5 第二类）：
   * mock 模式下 saveSettingsApi 走 mockSaveSystemSettings（只 resolve，不发 HTTP），
   * 所以本条**证明不了「配置落库」**，能证明的只是「前端把成功提示接对了」。
   * 原用例只 toContainText('配置已保存') —— 连保存失败时误弹的 error 提示都可能被放过，
   * 这里补上「必须是 success 型 + 不得有 error 型」这条真断言。
   * 落库判据（PUT 后 GET 回读）只能在真实模式补：e2e-real 目前无 settings 用例，见交件评论。
   */
  test('保存配置：success 型提示（仅前端语义，不含落库）', async ({ page }) => {
    await page.locator('.settings-form').getByRole('button', { name: '保存配置' }).click()
    const msg = adminMessage(page)
    await expect(msg).toContainText('配置已保存')
    await expect(msg).toHaveClass(/el-message--success/)
    await expect(page.locator('.el-message--error')).toHaveCount(0)
  })

  test('WiFi 预设列表展示脱敏密码', async ({ page }) => {
    await expect(page.getByText('WiFi 预设列表')).toBeVisible()
    await expect(page.getByText('Hospital-WiFi').first()).toBeVisible()
    await expect(page.getByText('********').first()).toBeVisible()
  })

  test('通知规则 tab：4 类告警规则与渠道/对象勾选', async ({ page }) => {
    await page.getByRole('tab', { name: '通知规则' }).click()
    const card = page.locator('.page-card').filter({ hasText: '告警通知规则' })
    await expect(card.locator('.el-table__body-wrapper tbody tr')).toHaveCount(4)
    // 压力偏高：微信+短信 双渠道勾选
    const pressureRow = card.locator('tbody tr').filter({ hasText: '压力偏高' })
    await expect(pressureRow.locator('.el-checkbox.is-checked')).toHaveCount(4) // 渠道2 + 对象2
  })

  test('通知规则切换勾选后提示更新成功', async ({ page }) => {
    await page.getByRole('tab', { name: '通知规则' }).click()
    const card = page.locator('.page-card').filter({ hasText: '告警通知规则' })
    const wearRow = card.locator('tbody tr').filter({ hasText: '佩戴中断' })
    // 佩戴中断默认仅微信 + 患者；追加勾选短信渠道
    await wearRow.locator('.el-checkbox').filter({ hasText: '短信' }).click()
    await expect(adminMessage(page)).toContainText('通知渠道已更新')
  })

  /**
   * T270 假绿 #6 订正（README §5 第二类 / A-SET-08）：
   * 原用例「发送记录 tab：4 条记录与状态 tag」断的是 **mock 常量**（mockNotificationLogs
   * 硬编码 4 条），真实后端 total=28 → 条数断言在 mock 下没有信息量，且完全没碰
   * A-SET-08 的判据（表头七列、渠道中文化、状态文案↔颜色、时间格式、内容 tooltip）。
   * 这里换成**逐行格式契约**：行数只要求 >0，每行按列验语义，换一批数据依然成立。
   *
   * 登记（不在本卡边界内修）：该 Tab **无分页控件**（pages/settings/index.vue:85-109 未放
   * el-pagination，onMounted 固定拉 page=1,pageSize=20）⇒ 真实 28 条只能看前 20 条、无法翻页；
   * 另「患者」列真实模式显示患者 ID（api/index.ts 回落 patientId，即 D1 同族）。
   */
  test('发送记录 tab：七列表头 + 逐行格式契约（T270 假绿#6 / A-SET-08）', async ({ page }) => {
    await page.getByRole('tab', { name: '发送记录' }).click()
    // el-tabs 各 pane 同时挂载，仅可见 pane 的表格参与断言
    const table = page.locator('.el-table:visible')
    const rows = table.locator('.el-table__body-wrapper tbody tr')
    await expect(rows.first()).toBeVisible({ timeout: 15_000 })

    const headers = await table.locator('.el-table__header-wrapper thead th').evaluateAll((ths) =>
      ths.map((th) => (th.textContent ?? '').trim()),
    )
    expect(headers, '表头七列（顺序即契约）').toEqual(['记录ID', '患者', '告警类型', '渠道', '内容', '状态', '发送时间'])

    const count = await rows.count()
    expect(count, '应有数据行（条数不作常量断言：mock 4 / 真实 total=28）').toBeGreaterThan(0)

    for (let i = 0; i < count; i++) {
      const cells = await rows.nth(i).evaluate((el) =>
        Array.from(el.querySelectorAll('td')).map((td) => ({
          text: (td.textContent ?? '').trim(),
          cls: (td.querySelector('.cell') ?? td).className,
          tag: td.querySelector('.el-tag')?.className ?? '',
        })),
      )
      expect(cells, `第 ${i + 1} 行应有 7 列`).toHaveLength(7)
      const [recordId, patient, alertType, channel, content, status, sentAt] = cells.map((c) => c.text)
      const where = `第 ${i + 1} 行（${recordId || '(空)'}）`

      expect(recordId, `${where} 记录ID 非空`).not.toBe('')
      expect(patient, `${where} 患者列不得漏 undefined/NaN`).not.toMatch(/undefined|NaN/)
      expect(['压力偏高', '压力波动', '佩戴中断', '传感器漂移', '非告警'], `${where} 告警类型须中文枚举`).toContain(alertType)
      expect(['微信', '短信'], `${where} 渠道须中文，不得漏 wechat/sms 原文`).toContain(channel)
      expect(content, `${where} 内容非空`).not.toBe('')
      // 内容列 show-overflow-tooltip：EP 会给单元格加 .el-tooltip（悬停出全文的前提）
      expect(cells[4].cls, `${where} 内容列应可悬停 tooltip`).toContain('el-tooltip')
      expect(['待发送', '已发送', '失败', '降级短信'], `${where} 状态文案`).toContain(status)
      // 状态必须是带颜色 tag，且颜色与文案对应（logStatusType 的契约）
      const wantClass = { 待发送: 'el-tag--info', 已发送: 'el-tag--success', 失败: 'el-tag--danger', 降级短信: 'el-tag--warning' }[status]!
      expect(cells[5].tag, `${where} 状态应为 tag 且颜色与文案对应`).toContain(wantClass)
      // 发送时间：未发送显示 '-'，否则 MM-DD HH:mm（formatTime 的契约，不是原始 ISO）
      expect(sentAt, `${where} 发送时间格式`).toMatch(/^-|\d{2}-\d{2} \d{2}:\d{2}$/)
    }
  })
})

test.describe('操作日志（T253-12.3）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.settings)
    await page.getByRole('tab', { name: '操作日志' }).click()
  })

  test('渲染 6 条日志与操作类型 tag、总数', async ({ page }) => {
    const rows = page.locator('.el-table:visible .el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(6)
    await expect(page.locator('.pagination')).toContainText('共 6 条')
    await expect(rows.filter({ hasText: '配置变更' })).toHaveCount(2)
    await expect(rows.filter({ hasText: '权限变更' })).toHaveCount(1)
    await expect(rows.filter({ hasText: '数据查看' })).toHaveCount(1)
    const loginRow = rows.filter({ hasText: '登录成功' })
    await expect(loginRow).toContainText('张建国')
    await expect(loginRow.locator('.el-tag--success')).toContainText('登录')
  })

  test('操作类型筛选：登录 → 1 条', async ({ page }) => {
    await pickSelectOption(page, page.locator('.audit-action'), '登录')
    const rows = page.locator('.el-table:visible .el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('张建国')
  })

  test('操作人员搜索：张建国 → 2 条（回车触发）', async ({ page }) => {
    await page.locator('.audit-operator input').fill('张建国')
    await page.locator('.audit-operator input').press('Enter')
    const rows = page.locator('.el-table:visible .el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(2)
    await expect(rows.filter({ hasText: '数据查看' })).toContainText('PT-001')
    await expect(rows.filter({ hasText: '登录成功' })).toHaveCount(1)
  })

  test('日期筛选：2026-09-19 → 1 条配置变更', async ({ page }) => {
    await page.locator('.audit-date input').fill('2026-09-19')
    await page.locator('.audit-date input').press('Enter')
    const rows = page.locator('.el-table:visible .el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText('写入系统参数')
  })
})

test.describe('设备管理 T268（注册入口 + 列表列 + 详情）', () => {
  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.devices)
  })

  test('列表列头对齐设计稿且顶栏有注册设备按钮', async ({ page }) => {
    for (const head of ['设备ID', '型号', '患者', '绑定时间', '固件版本', '连接的WiFi', '状态', '操作']) {
      await expect(page.locator('.el-table__header-wrapper th', { hasText: head })).toBeVisible()
    }
    await expect(page.getByRole('button', { name: '注册设备' })).toBeVisible()
    // 操作列「详情」
    await expect(tableRows(page).first().getByRole('button', { name: '详情' })).toBeVisible()
  })

  test('注册设备：非法设备ID前端拦截', async ({ page }) => {
    await page.getByRole('button', { name: '注册设备' }).click()
    const dialog = page.locator('.el-dialog')
    await dialog.locator('input').first().fill('AB')
    await dialog.getByRole('button', { name: '注册' }).click()
    await expect(adminMessage(page)).toContainText('4-48 位')
    await expect(tableRows(page).filter({ hasText: 'AB' })).toHaveCount(0)
    await dialog.getByRole('button', { name: '取消' }).click()
  })

  test('注册设备：合法ID注册成功出现在列表（未绑定）', async ({ page }) => {
    await page.getByRole('button', { name: '注册设备' }).click()
    const dialog = page.locator('.el-dialog')
    await dialog.locator('input').first().fill('E2E-DEV-001')
    await dialog.getByRole('button', { name: '注册' }).click()
    await expect(adminMessage(page)).toContainText('设备已注册')
    const row = tableRows(page).filter({ hasText: 'E2E-DEV-001' })
    await expect(row).toHaveCount(1)
    await expect(row).toContainText('未绑定')
    await expect(row).toContainText('PRS-ML05-RC')
  })

  test('详情抽屉：设备信息 + 绑定历史', async ({ page }) => {
    await tableRows(page).filter({ hasText: 'DEV-A3F312' }).getByRole('button', { name: '详情' }).click()
    const drawer = page.locator('.el-drawer')
    await expect(drawer).toContainText('设备详情')
    await expect(drawer).toContainText('DEV-A3F312')
    await expect(drawer).toContainText('绑定历史')
    await expect(drawer.locator('.binding-table tbody tr').first()).toContainText('PT-001')
  })
})

test.describe('角色管理（T253-11.2）', () => {
  // 页面同时有 角色列表 + 权限矩阵 两张表，断言须圈定在角色列表卡内
  function listRows(page: import('@playwright/test').Page) {
    return tableRows(page, page.locator('.page-card').filter({ hasText: '角色列表' }))
  }

  test.beforeEach(async ({ page }) => {
    await adminLogin(page, 'admin')
    await page.goto(adminRoutes.roles)
  })

  test('渲染角色列表（按后端返回）与操作列，预置角色删除禁用', async ({ page }) => {
    const rows = listRows(page)
    await expect(rows).toHaveCount(3) // mock = 3 个预置登录角色
    await expect(rows.filter({ hasText: '运营管理员' })).toContainText('启用')
    await expect(rows.filter({ hasText: '运营管理员' }).getByRole('button', { name: '删除' })).toBeDisabled()
    await expect(rows.first().getByRole('button', { name: '编辑' })).toBeVisible()
  })

  test('新增角色（模板）→ 编辑描述 → popconfirm 删除，全链路', async ({ page }) => {
    await page.getByRole('button', { name: '+ 新增角色' }).click()
    const dialog = page.locator('.el-dialog')
    await dialog.locator('input').first().fill('E2E巡检角色')
    await pickSelectOption(page, dialog.locator('.role-template'), '康复师')
    await dialog.getByRole('button', { name: '保存角色' }).click()
    await expect(adminMessage(page)).toContainText('角色已创建')

    const row = listRows(page).filter({ hasText: 'E2E巡检角色' })
    await expect(row).toHaveCount(1)
    await row.getByRole('button', { name: '编辑' }).click()
    await dialog.locator('input').nth(1).fill('巡检用自定义角色描述')
    await dialog.getByRole('button', { name: '保存角色' }).click()
    await expect(adminMessage(page)).toContainText('角色已保存')
    await expect(row).toContainText('巡检用自定义角色描述')

    await row.getByRole('button', { name: '删除' }).click()
    await page.locator('.el-popconfirm').getByRole('button', { name: '确定' }).click()
    await expect(adminMessage(page)).toContainText('角色已删除')
    await expect(listRows(page).filter({ hasText: 'E2E巡检角色' })).toHaveCount(0)
  })

  test('新增角色（自定义）未勾选模块提示且不提交', async ({ page }) => {
    await page.getByRole('button', { name: '+ 新增角色' }).click()
    const dialog = page.locator('.el-dialog')
    await dialog.locator('input').first().fill('E2E空模块角色')
    await dialog.getByRole('button', { name: '保存角色' }).click()
    await expect(adminMessage(page)).toContainText('至少勾选一个功能模块')
    await expect(listRows(page).filter({ hasText: 'E2E空模块角色' })).toHaveCount(0)
    await dialog.getByRole('button', { name: '取消' }).click()
  })

  test('编辑预置角色：名称锁定仅描述可改', async ({ page }) => {
    const row = listRows(page).filter({ hasText: '运营管理员' })
    await row.getByRole('button', { name: '编辑' }).click()
    const dialog = page.locator('.el-dialog')
    await expect(dialog.locator('input').first()).toBeDisabled()
    await dialog.locator('input').nth(1).fill('系统全部权限（运营 / 配置 / 权限管理）')
    await dialog.getByRole('button', { name: '保存角色' }).click()
    await expect(adminMessage(page)).toContainText('角色已保存')
  })
})
