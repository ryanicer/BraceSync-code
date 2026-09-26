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

interface UpdateInstallMetaParams {
  wifiStatus?: 'connected' | 'unconfigured'
  baselineId?: string | null
  notes?: string
  calibrateTime?: string
}

/**
 * 完成安装时补字段（PATCH /install-records/:id，后端 T084 未实现）
 */
export async function updateInstallMeta(
  installId: string,
  meta: UpdateInstallMetaParams
): Promise<{ installId: string }> {
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 PATCH 就绪后切换
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
  wifiStatus?: 'connected' | 'unconfigured'
}

/**
 * 安装记录列表（真实：GET /install-records，后端 T084 未实现）
 */
export async function listInstallRecords(
  params: ListInstallParams = {}
): Promise<{ list: InstallRecord[]; total: number }> {
  if (USE_MOCK) {
    // T089-MOCK: 等后端 T084 就绪后切换
    await new Promise((r) => setTimeout(r, 200))
    const seed: InstallRecord[] = Array.from({ length: 6 }).map((_, i) => ({
      installId: `INS-2026090${i + 1}-00${i + 1}`,
      // T406（U4）：旧值 `PRS-ML05-RC-00${i + 1}` 是 3 位尾号假串（现网精确命中 0）。
      // 这里不改成现网串——现网 5 台是 20260701001–005 批次（T406 只读 GET /devices 实测），
      // mock 冒充现网设备会让「本地绿、联调对不上号」更难查；故用形态合法
      // （日期 8 + 序号 3）但属保留合成批次的 19700101 日期段，epoch 日期永不可能是真实装机日。
      // 首条被 e2e/tests/tech-records.spec.ts 断言，改这里要同步改那条用例。
      deviceId: `PRS-ML05-RC-19700101${String(i + 1).padStart(3, '0')}`,
      patientId: `pat-00${i + 1}`,
      patientName: ['张明远', '李欣怡', '王子轩', '刘思雨', '陈俊豪', '杨梓涵'][i],
      techId: 'T-001',
      techName: '李技师',
      calibrateTime: new Date(Date.now() - i * 86400000).toISOString(),
      baselineId: i % 3 === 2 ? null : `BSL-202609${String(i + 1).padStart(2, '0')}-ABC`,
      notes: i === 0 ? '患者初诊安装，支具型号 ML05' : '',
      signatureUrl: '',
      wifiStatus: i % 2 === 0 ? 'connected' : 'unconfigured',
    }))
    return { total: seed.length, list: seed }
  }
  const qs = Object.entries(params)
    .filter(([, v]) => v !== undefined)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
    .join('&')
  return request<{ list: InstallRecord[]; total: number }>({
    url: `/api/v1/install-records${qs ? '?' + qs : ''}`,
    method: 'GET',
  })
}
