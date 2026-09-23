// T330 复查报告页患者下拉取数回归：pageSize 越界会被 user-service parsePaging 判 400，整页不可用
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia, setActivePinia } from 'pinia'
import type { Patient } from '@bracesync/shared-types'

const fetchPatients = vi.fn()

vi.mock('../src/api', () => ({
  fetchPatients: (...args: unknown[]) => fetchPatients(...args),
  fetchReviewRecords: vi.fn(async () => []),
  createReviewRecordApi: vi.fn(),
  presignFile: vi.fn(),
  uploadFileDirect: vi.fn(),
  completeUpload: vi.fn(),
}))

const CONTRACT_MAX_PAGE_SIZE = 100 // api-contracts.ts getPatients / 架构 §3.5

function patients(n: number): Patient[] {
  return Array.from({ length: n }, (_, i) => ({
    patientId: `P2026${String(i).padStart(4, '0')}`,
    name: `患者${i + 1}`,
  })) as Patient[]
}

async function mountPage() {
  const page = (await import('../src/pages/review-records/index.vue')).default
  const wrapper = mount(page, { global: { plugins: [createPinia(), ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('T330 复查报告页取数分页', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    fetchPatients.mockReset()
    // 复刻 user-service parsePaging：pageSize 超上限即 400，页面拿不到患者
    fetchPatients.mockImplementation((params: { pageSize: number }) =>
      params.pageSize > CONTRACT_MAX_PAGE_SIZE
        ? Promise.reject(new Error(`invalid pageSize "${params.pageSize}"`))
        : Promise.resolve({ list: patients(3), total: 3, page: 1, pageSize: params.pageSize }),
    )
  })

  it('患者下拉请求不超过契约上限 100', async () => {
    await mountPage()
    expect(fetchPatients).toHaveBeenCalledTimes(1)
    const params = fetchPatients.mock.calls[0][0] as { page: number; pageSize: number }
    expect(params.pageSize).toBeGreaterThan(0)
    expect(params.pageSize).toBeLessThanOrEqual(CONTRACT_MAX_PAGE_SIZE)
    expect(params.page).toBe(1)
  })

  it('取数成功后下拉有患者选项（页面可用，不再空列表）', async () => {
    const wrapper = await mountPage()
    const options = wrapper.findAllComponents({ name: 'ElOption' })
    expect(options).toHaveLength(3)
  })
})
