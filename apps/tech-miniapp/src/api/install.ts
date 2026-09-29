import { request, USE_MOCK } from '../utils/request'
import type { InstallRecord } from '@bracesync/shared-types'

interface CreateInstallResp {
  installId: string
}

/**
 * 创建安装记录（bind 成功后立即创建，拿 installId）
 * 真实：POST /install-records，后端 T084 未实现 → mock 先行
 * P0-1 时序：install 先行，基线提交时必须带 installId
 */
export async function createInstall(
  deviceId: string,
  patientId: string,
  techId: string
): Promise<CreateInstallResp> {
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 就绪后切换
    await new Promise((r) => setTimeout(r, 300))
    const stamp = new Date()
    const ymd = `${stamp.getFullYear()}${String(stamp.getMonth() + 1).padStart(2, '0')}${String(
      stamp.getDate()
    ).padStart(2, '0')}`
    const seq = Math.floor(Math.random() * 900) + 100
    return { installId: `INS-${ymd}-${seq}` }
  }
  return request<CreateInstallResp>({
    url: '/api/v1/install-records',
    method: 'POST',
    data: {
      deviceId,
      patientId,
      techId,
      status: 'in_progress',
      wifiStatus: 'unconfigured',
    },
  })
}

/**
 * PUT 的实际请求体类型（T447）：wifiStatus 已随库侧 CHECK 扩到四值。
 * 后端 installMetaRequest 的语义是「空字符串 = 该列不改」，所以这里别传 ''；
 * 键整体省略同样是不改。
 * T474：这里的键必须是后端 bind struct（notes / signatureUrl / wifiStatus）的子集 ——
 * 原来挂在这儿的 baselineId／calibrateTime 后端根本不 bind，ShouldBindJSON 静默丢弃，
 * 前端发了也到不了库（基线走 saveBaseline 独立接口、校准时刻由服务端记），故两键删除。
 */
interface UpdateInstallMetaParams {
  wifiStatus?: 'connected' | 'unconfigured' | 'failed' | 'skipped'
  notes?: string
}

/**
 * 完成安装时补字段（PUT /api/v1/install-records/:id，后端 T122 已实现）
 */
export async function updateInstallMeta(
  installId: string,
  meta: UpdateInstallMetaParams
): Promise<{ installId: string }> {
  if (USE_MOCK) {
    // mock 分支：真实 PUT 通道早已就绪（T122），这里只是 USE_MOCK 下不发请求
    await new Promise((r) => setTimeout(r, 250))
    return { installId }
  }
  return request<{ installId: string }>({
    url: `/api/v1/install-records/${installId}`,
    method: 'PUT',
    data: meta,
  })
}

interface ListInstallParams {
  page?: number
  pageSize?: number
  // 四值同 UpdateInstallMetaParams（T447 扩值）。服务端不按该键过滤，
  // 真正生效的是记录页的本地过滤；这里放开类型是为了让筛选 chips 能列出四档。
  wifiStatus?: 'connected' | 'unconfigured' | 'failed' | 'skipped'
}

/** mock 集大小：刻意 > 后端缺省单页 20，让「翻页取满」那条腿在本地 e2e 里真被执行（T433） */
const MOCK_INSTALL_TOTAL = 26

/**
 * T459：mock 集按四档循环，让展示面三色在本地 e2e 里各有可断言的行
 * （26 条 ⇒ connected 7／unconfigured 7／failed 6／skipped 6，首条仍是 connected）。
 * 改这里的分配要同步 e2e/tests/tech-records.spec.ts 的三处计数与那条四档徽章用例。
 */
const WIFI_MOCK_CYCLE = ['connected', 'unconfigured', 'failed', 'skipped'] as const

function mockInstallSeed(): InstallRecord[] {
  const names = ['张明远', '李欣怡', '王子轩', '刘思雨', '陈俊豪', '杨梓涵']
  return Array.from({ length: MOCK_INSTALL_TOTAL }).map((_, i) => ({
    installId: `INS-202609-${String(i + 1).padStart(3, '0')}`,
    // T406（U4）：旧值 `PRS-ML05-RC-00${i + 1}` 是 3 位尾号假串（现网精确命中 0）。
    // 这里不改成现网串——现网 5 台是 20260701001–005 批次（T406 只读 GET /devices 实测），
    // mock 冒充现网设备会让「本地绿、联调对不上号」更难查；故用形态合法
    // （日期 8 + 序号 3）但属保留合成批次的 19700101 日期段，epoch 日期永不可能是真实装机日。
    // 首条被 e2e/tests/tech-records.spec.ts 断言，改这里要同步改那条用例。
    deviceId: `PRS-ML05-RC-19700101${String(i + 1).padStart(3, '0')}`,
    patientId: `pat-${String((i % names.length) + 1).padStart(3, '0')}`,
    patientName: names[i % names.length],
    techId: 'T-001',
    techName: '李技师',
    calibrateTime: new Date(Date.now() - i * 86400000).toISOString(),
    baselineId: i % 3 === 2 ? null : `BSL-202609${String(i + 1).padStart(2, '0')}-ABC`,
    notes: i === 0 ? '患者初诊安装，支具型号 ML05' : '',
    signatureUrl: '',
    wifiStatus: WIFI_MOCK_CYCLE[i % WIFI_MOCK_CYCLE.length],
  }))
}

/**
 * 安装记录列表（真实：GET /install-records）
 * mock 分支按 page/pageSize 切片回 { list, total }，与后端 pageData 信封同形
 * （services/device-service/internal/handler/query.go:63-68），
 * 否则 mock 下永远「一页装得下」，T433 格二（取数只发一页）本地测不出来。
 */
export async function listInstallRecords(
  params: ListInstallParams = {}
): Promise<{ list: InstallRecord[]; total: number }> {
  const pageSize = params.pageSize ?? 20
  const page = params.page ?? 1
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 就绪后切换
    await new Promise((r) => setTimeout(r, 200))
    const seed = mockInstallSeed()
    const start = (page - 1) * pageSize
    return { total: seed.length, list: seed.slice(start, start + pageSize) }
  }
  const qs = Object.entries({ page, pageSize, wifiStatus: params.wifiStatus })
    .filter(([, v]) => v !== undefined)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
    .join('&')
  return request<{ list: InstallRecord[]; total: number }>({
    url: `/api/v1/install-records${qs ? '?' + qs : ''}`,
    method: 'GET',
  })
}
