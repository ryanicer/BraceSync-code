import type { SensorPoint, PressureRecord } from '@bracesync/shared-types'

// 生成 20 个传感器点（4 行 5 列，P01-P20），P12 固定 42.18N（e2e 基线）
export function mockSensorPoints(): SensorPoint[] {
  const points: SensorPoint[] = []
  for (let r = 1; r <= 4; r++) {
    for (let c = 1; c <= 5; c++) {
      const idx = (r - 1) * 5 + (c - 1)
      const base = 20 + r * 6 + Math.sin(c * 1.2) * 5
      const value = parseFloat((base + Math.random() * 8).toFixed(2))
      points.push({
        pointId: `P${String(idx + 1).padStart(2, '0')}`,
        row: r,
        col: c,
        label: `R${r}C${c}`,
        pressureValue: value,
        status: value < 20 ? 'normal' : value < 40 ? 'normal' : value < 60 ? 'warning' : 'critical',
      })
    }
  }
  // P12（索引 11）固定为最大压力点 42.18N
  points[11].pressureValue = 42.18
  points[11].status = 'warning'
  return points
}

// 生成 48 点日趋势数据（每 30 分钟一个点）
export function mockTrendData(baseVal: number = 42): { timestamp: string; value: number }[] {
  const seed = 7
  return Array.from({ length: 48 }, (_, i) => ({
    timestamp: `2026-08-10T${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}:00Z`,
    value: parseFloat((baseVal + Math.sin(i * 0.3 + seed) * 8 + Math.random() * 6 - 3).toFixed(2)),
  }))
}

// 模拟实时快照
export function mockRealtime() {
  const points = mockSensorPoints()
  const maxPoint = points.reduce((max, p) => (p.pressureValue > max.pressureValue ? p : max))
  return {
    status: 'online',
    todayHours: 8.5,
    maxPressure: maxPoint.pressureValue,
    maxPoint: maxPoint.pointId,
    events: 2,
    pressureRecords: [
      {
        recordId: 'rec-001',
        deviceId: 'PRS-ML05-RC-001',
        patientId: 'pat-001',
        timestamp: new Date().toISOString(),
        points,
        uploadTime: new Date().toISOString(),
      },
    ] as PressureRecord[],
    alerts: [],
  }
}