/**
 * 技师端绑定页 — 示例与提示文案集中表（T362）
 *
 * 集中存放的原因与患者端 `utils/wifi-copy.ts` 同例：文案要与设计稿逐字对照，
 * 且「患者ID 示例口径」这类事实只有一个来源，不允许在页面里散落字符串。
 * 唯一来源：docs/design/tech/bind.html（占位口径）+ 后台患者ID 真实生成规则。
 */

/**
 * 患者ID 真实形态 = `P` + 4 位年份 + 12 位随机 hex（共 17 字符），
 * 生成处 `services/user-service/internal/repo/pg.go` 的 `newPatientID()`。
 * 旧示例 `pat-001` 现网零命中（staging 8 名患者实测无一条以 `pat-` 开头）。
 * 本串仅为占位示例，不对应任何真实患者。
 */
export const PATIENT_ID_EXAMPLE = 'P2026a1b2c3d4e5f6'

/** 患者ID 形态判据：示例串与真实生成规则的一致性由 test/bind-copy.spec.ts 钉住 */
export const PATIENT_ID_SHAPE = /^P\d{4}[0-9a-f]{12}$/

export const PATIENT_ID_PLACEHOLDER = `例: ${PATIENT_ID_EXAMPLE}`

/** 扫码四态提示（T362：取消/失败/无内容都不得回落成"已填入"的假成功） */
export const SCAN_TOAST = {
  success: '扫码成功',
  cancelled: '已取消扫码',
  empty: '未识别到二维码内容，请手动输入',
  failed: '扫码失败，请手动输入设备 ID',
} as const
