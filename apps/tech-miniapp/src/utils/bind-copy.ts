/**
 * 技师端绑定页 — 示例与提示文案集中表（T362）
 *
 * 集中存放的原因与患者端 `utils/wifi-copy.ts` 同例：文案要与设计稿逐字对照，
 * 且「患者ID / 设备ID 示例口径」这类事实只有一个来源，不允许在页面里散落字符串。
 * 唯一来源：docs/design/tech/bind.html（占位口径）+ 后台患者ID 真实生成规则 + 现网设备编号形态。
 */

/**
 * 患者ID 真实形态 = `P` + 4 位年份 + 12 位随机 hex（共 17 字符），
 * 生成处 `services/user-service/internal/repo/pg.go` 的 `newPatientID()`。
 * 旧示例 `pat-001` 现网零命中（staging 8 名患者实测无一条以 `pat-` 开头）。
 * 本串仅为占位示例，不对应任何真实患者。
 */
export const PATIENT_ID_EXAMPLE = 'P2026a1b2c3d4e5f6'

/** 患者ID 形态判据：示例串与真实生成规则的一致性由 test/bind-scan.spec.ts 钉住 */
export const PATIENT_ID_SHAPE = /^P\d{4}[0-9a-f]{12}$/

export const PATIENT_ID_PLACEHOLDER = `例: ${PATIENT_ID_EXAMPLE}`

/**
 * 设备ID 真实形态 = `PRS-ML05-RC-` + 11 位数字（日期 8 位 + 序号 3 位，共 23 字符）。
 * 依据：T362 第 29 轮现网实测 staging `devices` 表 total 5，编号 `PRS-ML05-RC-20260701001`
 * 到 `...005`（`scripts/db/seed/seed.sql` 同一族，后台 Go 用例亦取此形态）。
 * 旧示例 `PRS-ML05-RC-001` 现网精确命中 0 —— 它正是 T362 删掉的那段 mock 硬编码的原值，
 * 技师照它手输必然绑不到设备。本串是现网真实形态，仍只是占位示例。
 */
export const DEVICE_ID_EXAMPLE = 'PRS-ML05-RC-20260701001'

/** 设备ID 形态判据：与 DEVICE_ID_EXAMPLE 的一致性由 test/bind-scan.spec.ts 钉住 */
export const DEVICE_ID_SHAPE = /^PRS-ML05-RC-\d{11}$/

export const DEVICE_ID_PLACEHOLDER = `例: ${DEVICE_ID_EXAMPLE}`

/** 扫码四态提示（T362：取消/失败/无内容都不得回落成"已填入"的假成功） */
export const SCAN_TOAST = {
  success: '扫码成功',
  cancelled: '已取消扫码',
  empty: '未识别到二维码内容，请手动输入',
  failed: '扫码失败，请手动输入设备 ID',
} as const
