import type { Baseline } from '@bracesync/shared-types'

/** 模拟 20 点 offset_values 基线数据 */
export function mockBaseline(): Baseline {
  return {
    baselineId: 'bl-mock-001',
    installId: 'inst-mock-001',
    deviceId: 'PRS-ML05-RC-001',
    offsetValues: Array.from({ length: 20 }, (_, i) =>
      parseFloat((Math.sin(i * 0.7) * 0.3 + (Math.random() - 0.5) * 0.2).toFixed(2))
    ),
    calibratorId: 'tech-001',
    createdAt: new Date().toISOString(),
  }
}

/** 模拟实时传感器读数（安装过程中使用） */
export function mockRealtimeSensorData() {
  const points = []
  for (let r = 1; r <= 4; r++) {
    for (let c = 1; c <= 5; c++) {
      const idx = (r - 1) * 5 + (c - 1)
      const base = 15 + r * 3 + Math.sin(c * 1.2) * 4
      const value = parseFloat((base + Math.random() * 5).toFixed(2))
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
  return points
}
