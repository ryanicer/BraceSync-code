/**
 * 压力读数展示口径：负值归零（T322 问题二）
 *
 * 负数从哪来：设备上行值按点位减校准基线后才落库（data-service `calibration.Apply`），
 * 基线大于当前读数的点位就会得到负值。这是后端有意保留的口径（`record_t296_test.go:148`
 * 断言「设备报 0 的点减偏移后可为负，但位置不得移动」），staging 末次帧实测 20 点里有 5 个负值。
 * 物理上压力不可为负，展示层要归零，与患者端 formatPressureValue（T233）和告警 ActualValue 同口径。
 *
 * 只喂展示值，不改「哪个点是最大点」：归约用的 isMax 标记仍取后端下发的那一格，
 * 校准前后的大小说明留给后端，前端不重排。
 *
 * NaN 原样透传，不在这个函数里被顺手洗成 0：那是数据缺失，得让页面显示成异常读数而不是假装无压力。
 */

interface PressurePointLike {
  pressureValue: number
}

interface SnapshotLike {
  pressureHeatmap?: PressurePointLike[]
}

export function nonNegative(v: number): number {
  return v < 0 ? 0 : v
}

/** 返回逐点归零后的快照；本帧没有负值时原样返回入参，避免每 2 秒轮询都新建一份 20 点数组 */
export function normalizeFramePressure<T extends SnapshotLike>(snap: T): T {
  const pts = snap.pressureHeatmap
  if (!pts || !pts.some((p) => p.pressureValue < 0)) return snap
  return { ...snap, pressureHeatmap: pts.map((p) => ({ ...p, pressureValue: nonNegative(p.pressureValue) })) }
}
