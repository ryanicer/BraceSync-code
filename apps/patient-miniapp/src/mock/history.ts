export interface WearingRecord {
  date: string
  hours: number
  status: 'ok' | 'warn' | 'error'
  label: string
}

export interface PressureAnomalyItem {
  point: string
  type: string
  level: 'warn' | 'error'
  detail: string
  threshold: string
  meta: string
}

export interface PressureAnomaly {
  date: string
  items: PressureAnomalyItem[]
}

export function mockWearingData(): WearingRecord[] {
  return [
    { date: '2026-07-12', hours: 10.2, status: 'warn', label: '不足' },
    { date: '2026-07-11', hours: 16.8, status: 'ok', label: '达标' },
    { date: '2026-07-10', hours: 8.5, status: 'warn', label: '不足' },
    { date: '2026-07-09', hours: 15.2, status: 'ok', label: '达标' },
    { date: '2026-07-08', hours: 3.1, status: 'error', label: '严重不足' },
    { date: '2026-07-07', hours: 18.2, status: 'ok', label: '达标' },
    { date: '2026-07-06', hours: 17.5, status: 'ok', label: '达标' },
    { date: '2026-07-05', hours: 12.1, status: 'warn', label: '不足' },
    { date: '2026-07-04', hours: 9.3, status: 'warn', label: '不足' },
    { date: '2026-07-03', hours: 16.0, status: 'ok', label: '达标' },
    { date: '2026-07-02', hours: 11.7, status: 'warn', label: '不足' },
    { date: '2026-07-01', hours: 17.1, status: 'ok', label: '达标' },
    { date: '2026-06-30', hours: 14.5, status: 'warn', label: '不足' },
    { date: '2026-06-29', hours: 6.2, status: 'error', label: '严重不足' },
    { date: '2026-06-28', hours: 18.5, status: 'ok', label: '达标' },
  ]
}

export function mockPressureAnomalies(): PressureAnomaly[] {
  return [
    {
      date: '2026-07-12',
      items: [
        { point: 'P10', type: '偏高', level: 'error', detail: '持续偏高 6h，峰值 63.8N', threshold: '>60N', meta: '累计超阈值 12 次 · 最高 68.5N' },
        { point: 'P07', type: '偏高', level: 'warn', detail: '持续偏高 4h，峰值 52.1N', threshold: '>40N', meta: '累计超阈值 8 次 · 最高 58.3N' },
        { point: 'P03', type: '偏低', level: 'warn', detail: '持续偏低 8h，最低 12.3N', threshold: '<20N', meta: '累计低于阈值 15 次 · 最低 8.1N' },
      ],
    },
    {
      date: '2026-07-11',
      items: [
        { point: 'P11', type: '偏高', level: 'warn', detail: '持续偏高 5h，峰值 55.6N', threshold: '>40N', meta: '累计超阈值 10 次 · 最高 61.2N' },
        { point: 'P08', type: '偏低', level: 'warn', detail: '持续偏低 3h，最低 16.5N', threshold: '<20N', meta: '累计低于阈值 5 次 · 最低 14.1N' },
      ],
    },
    {
      date: '2026-07-10',
      items: [
        { point: 'P12', type: '偏高', level: 'error', detail: '持续偏高 8h，峰值 71.2N', threshold: '>60N', meta: '累计超阈值 18 次 · 最高 74.3N' },
      ],
    },
    {
      date: '2026-07-09',
      items: [
        { point: 'P04', type: '偏低', level: 'warn', detail: '持续偏低 5h，最低 11.8N', threshold: '<20N', meta: '累计低于阈值 9 次 · 最低 8.5N' },
        { point: 'P09', type: '偏高', level: 'warn', detail: '持续偏高 3h，峰值 47.2N', threshold: '>40N', meta: '累计超阈值 6 次 · 最高 50.1N' },
      ],
    },
    {
      date: '2026-07-08',
      items: [
        { point: 'P10', type: '偏高', level: 'error', detail: '持续偏高 10h，峰值 69.5N', threshold: '>60N', meta: '累计超阈值 22 次 · 最高 72.1N' },
        { point: 'P02', type: '偏低', level: 'error', detail: '持续偏低 12h，最低 7.2N', threshold: '<20N', meta: '累计低于阈值 28 次 · 最低 5.6N' },
      ],
    },
    {
      date: '2026-07-07',
      items: [
        { point: 'P05', type: '偏高', level: 'warn', detail: '持续偏高 3h，峰值 44.1N', threshold: '>40N', meta: '累计超阈值 5 次 · 最高 46.3N' },
      ],
    },
    {
      date: '2026-07-05',
      items: [
        { point: 'P01', type: '偏低', level: 'warn', detail: '持续偏低 4h，最低 14.2N', threshold: '<20N', meta: '累计低于阈值 7 次 · 最低 11.3N' },
      ],
    },
  ]
}