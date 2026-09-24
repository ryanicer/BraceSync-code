import { test, expect, type Page, type Locator } from '@playwright/test'
import QRCode from 'qrcode'
import { realLogin, gotoMenuAndWaitTable, getAuthToken } from '../real-helpers'

/**
 * T356 ④（并入项）：真实模式下「患者详情抽屉二维码」的载荷必须绑定到该行患者ID。
 *
 * 判据不是「svg 存在」，而是把浏览器里画出来的 path 逐字节对上由该行 patientId 独立编出来的矩阵
 * —— 抽屉写死一个患者、列表换行码不换、载荷拼错前缀这类缺陷都会当场判红。
 * 反证同样显式：码面 path 不得等于「另一行患者ID」编出来的 path。
 *
 * 为什么这里不走 requireDeployedBuild 跳过：T327 的二维码构建已在 staging 现网
 * （探针 T356-evidence/E7-staging-pidcard-probe.txt：入口 chunk 里同时命中 pid-card 与扫码文案），
 * 所以本用例按真跑写死断言，部署回退就让它红，而不是静默 skip。
 *
 * 只读用例：不写库、不造数，seed 患者按现网顺序取前两行。
 */

/** 列表断言钉在「患者列表」卡内：本页另有一张「批量患者-团队绑定」表（T289），全局取行会串表 */
function patientTable(page: Page): Locator {
  return page.locator('.patient-list-card')
}

/** 表头文案 → 列下标（列序变过，写死序号会取错列） */
async function headerIndex(page: Page, title: string): Promise<number> {
  const texts = await patientTable(page)
    .locator('.el-table__header-wrapper thead th')
    .evaluateAll((ths) => ths.map((th) => (th.textContent ?? '').trim()))
  const idx = texts.indexOf(title)
  expect(idx, `表头应含「${title}」列，实得 ${texts.join('|')}`).toBeGreaterThan(-1)
  return idx
}

async function cellText(row: Locator, idx: number): Promise<string> {
  return row.evaluate((tr, i) => {
    const td = tr.querySelectorAll('td')[i]
    return (td?.textContent ?? '').trim()
  }, idx)
}

/** 独立重算码面 path：与 QR 矩阵同形，同一行内连续暗模块合成一段水平描边 */
function qrPathOf(pid: string): string {
  const qr = QRCode.create(pid, { errorCorrectionLevel: 'M' })
  const { size, data } = qr.modules
  const seg: string[] = []
  for (let y = 0; y < size; y++) {
    let x = 0
    while (x < size) {
      if (!data[y * size + x]) {
        x++
        continue
      }
      let run = 0
      while (x + run < size && data[y * size + x + run]) run++
      seg.push(`M${x} ${y}h${run}v1h-${run}z`)
      x += run
    }
  }
  return seg.join('')
}

function qrSizeOf(pid: string): number {
  return QRCode.create(pid, { errorCorrectionLevel: 'M' }).modules.size
}

async function openRowAndReadQr(page: Page, rowIndex: number) {
  await patientTable(page).locator('.el-table__body tr').nth(rowIndex).click()
  const card = page.locator('.el-drawer .pid-card')
  await expect(card, `点第 ${rowIndex + 1} 行后抽屉里要有二维码卡片`).toBeVisible()
  const svg = card.locator('.qr-frame svg')
  const d = await card.locator('.qr-frame path').getAttribute('d')
  return {
    viewBox: await svg.getAttribute('viewBox'),
    d: d ?? '',
    pidShown: (await card.locator('.pid-value').innerText()).trim(),
  }
}

async function closeDrawer(page: Page) {
  await page.locator('.el-drawer__close-btn').click()
  // el-drawer 关闭后 DOM 不销毁（display:none），只能断言隐藏而非 count 0
  await expect(page.locator('.el-drawer .pid-card')).toBeHidden()
}

test.describe('15-患者详情二维码载荷', () => {
  test.beforeEach(async ({ page }) => {
    await realLogin(page)
    await gotoMenuAndWaitTable(page, '患者管理', 'patients', patientTable(page))
  })

  test('15.1 码面载荷 = 该行 patientId，且不等于其他行', async ({ page }) => {
    test.skip(
      (await patientTable(page).locator('.el-table__body tr').count()) < 2,
      'seed 患者不足 2 行 ⇒ 无法做「不等于其他行」的反证，交由数据前置保障',
    )
    const idCol = await headerIndex(page, '患者ID')
    const rows = patientTable(page).locator('.el-table__body tr')
    const pid0 = await cellText(rows.nth(0), idCol)
    const pid1 = await cellText(rows.nth(1), idCol)
    expect(pid0, '首行患者ID非空').toBeTruthy()
    expect(pid1, '两行须是不同患者').not.toBe(pid0)

    // 页面值 vs 接口值：抽屉的码要钉在真后端认得的那个ID上
    const token = await getAuthToken(page)
    expect(token, '真实模式需登录态才能对拍接口').toBeTruthy()
    const api = await page.request.get(`/api/v1/admin/patients/${pid0}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(api.status(), '患者详情接口应 200').toBe(200)
    const body = await api.json()
    expect(body?.data?.patientId, `接口回 ${JSON.stringify(body?.data?.patientId)} ≠ 页面行 ${pid0}`)
      .toBe(pid0)

    const first = await openRowAndReadQr(page, 0)
    expect(first.pidShown, '卡片左列ID要等于列表行的患者ID').toBe(pid0)
    expect(first.viewBox, '矩阵边长要等于按该ID编出来的 QR 尺寸')
      .toBe(`0 0 ${qrSizeOf(pid0)} ${qrSizeOf(pid0)}`)
    expect(first.d.length, '码面 path 非空（空 = 只渲染了容器）').toBeGreaterThan(0)
    expect(first.d, '码面逐段描边必须等于该患者ID编出来的 QR 矩阵').toBe(qrPathOf(pid0))
    expect(first.d, '码面不得是别的患者的码').not.toBe(qrPathOf(pid1))
    await closeDrawer(page)

    const second = await openRowAndReadQr(page, 1)
    expect(second.pidShown).toBe(pid1)
    expect(second.d, '第二行须按它自己的ID编出来').toBe(qrPathOf(pid1))
    expect(second.d, '换行必须换码').not.toBe(first.d)
  })
})
