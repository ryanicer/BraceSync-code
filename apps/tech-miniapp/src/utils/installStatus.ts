/**
 * 技师端安装流程展示层单一真源（T443 建档，T459 按 T447 词形表补齐四态）。
 *
 * 依据：docs/tasks/peter/T417-对照清单-技师端.md 的 TC-1／TC-2／TR-11；
 * docs/tasks/winner/T447-WiFi状态唯一词形表.md 第一节四档；
 * PRD §7C.4（跳过配网按钮＋弹窗确认文案）、§7C.6（-4 可跳过）、§8.2（可达性为本地派生展示）。
 * PRD 的行号会随并稿漂移（T452 已实测漂移过一次），复核按引号内的文案定位，不要只按行号跳。
 *
 * 页面模板在本包测不到（vitest.config.ts 是 environment: 'node'，无 VTU 挂载），
 * 故判定与文案构造全部下沉到这里，install／complete／records 三页同读一处。
 */

/**
 * wifi_status 取值面：库里已是四值
 * （scripts/db/migrations/000031_t447_wifi_status_four_values_winner.up.sql 重建 CHECK，
 * 类型层 packages/shared-types/src/index.ts 的 InstallRecord.wifiStatus 同集合）。
 * 显示词一律取 T447 词形表第一节，大小写敏感，不自创、不沿用旧短标签。
 */

/** TC-2：跳过配网时 WiFi 行的显示值，不再冒充「已联网／已连接」；本行是 skipped 档字面量的唯一出处 */
export const WIFI_SKIPPED_LABEL = '已跳过配网'

export const WIFI_STATUS_LABEL = {
  connected: '已连接',
  unconfigured: '未配置',
  failed: '连接失败',
  skipped: WIFI_SKIPPED_LABEL,
} as const

/** 库里四值即本表的键集；写侧（store／PUT 体）同用这个类型，避免两处各写一遍联合 */
export type WifiStatusValue = keyof typeof WIFI_STATUS_LABEL

/** 未知值（含 null／undefined）回落「未配置」＝列默认值语义，不猜成其他档 */
export function wifiStatusLabel(status?: string | null): string {
  const label = status ? WIFI_STATUS_LABEL[status as WifiStatusValue] : undefined
  return label ?? WIFI_STATUS_LABEL.unconfigured
}

export function wifiRowLabel(status: string, networkSkipped: boolean): string {
  if (networkSkipped) return WIFI_SKIPPED_LABEL
  return wifiStatusLabel(status)
}

/**
 * 徽章三色的判据：稿面 records.html 三色（:95 已连接＝绿、:127 连接失败＝红、:197 未配置＝灰）。
 * skipped 档稿面未画（T447 第五节第 2 项已归 Peter 补稿），归 pending，登记为遗留。
 * 返回语义色而不是页内类名——三页各自的样式类名不同，色到类名的映射留在页面。
 */
export type WifiBadgeTone = 'ok' | 'fail' | 'pending'

export function wifiBadgeTone(status?: string | null): WifiBadgeTone {
  if (status === 'connected') return 'ok'
  if (status === 'failed') return 'fail'
  return 'pending'
}

/**
 * 配网失败码 → 该不该把 wifi_status 扳成 failed。
 * T459·C2（PM 2026-09-28 裁定）：只有 B512 的 -4（云端不可达）落 failed；
 * -1 密码错／-2 未找到网络／-3 DHCP 失败都是 WiFi 本身没连上，返回 null＝维持列默认 unconfigured，
 * 失败原因由 wifi-config 那四条稿面错误文案（toast）承载，不落库。
 */
export function provisionFailureWifiStatus(code: number): 'failed' | null {
  return code === -4 ? 'failed' : null
}

/** TC-1：完成页基线状态与 install 页同一真源（installStore.baselineSaved），不再是硬编码 */
export function baselineStatusLabel(baselineSaved: boolean): string {
  return baselineSaved ? '已保存' : '未保存'
}

export function baselineStatusBadgeClass(baselineSaved: boolean): string {
  return baselineSaved ? 'status-ok' : 'status-warn'
}

/** 可达性三态（PRD §8.2：纯前端派生，不落库）。跳过优先，避免与「已验证」同屏互斥。 */
export function reachabilityLabel(networkSkipped: boolean, wifiConnected: boolean): string {
  if (networkSkipped) return '已跳过'
  if (wifiConnected) return '已验证'
  return '待验证'
}

export function reachabilityBadgeClass(networkSkipped: boolean, wifiConnected: boolean): string {
  if (networkSkipped) return 'status-pending'
  if (wifiConnected) return 'status-ok'
  return 'status-warn'
}

/**
 * failed 档的提示句：逐字取 PRD §7C.6 错误态表 -4 那条（稿面 wifi-config.html 的错误文案映射同文，
 * 实现 wifi-config/index.vue 的 errorMessage 也读这一个常量）。
 * install 页阶段三的失败提示不自拟句子；要改这句先改 PRD。
 */
export const WIFI_FAILED_NOTE =
  '云端暂不可达，设备将在后台持续重试（约每 5 分钟一次），请保持设备通电与 WiFi 环境'

/**
 * TI-12：PRD §7C.4「跳过配网」按钮配套的弹窗文案，逐字取用。
 * 改这三个字要先改 PRD，不在实现里自拟。
 */
export const SKIP_NETWORK_CONFIRM = {
  title: '跳过配网',
  content: '跳过配网后设备将无法自动上传数据，确定跳过？',
} as const

/** showModal 二次确认；用户点取消或弹窗失败都按「没跳过」处理 */
export function confirmSkipNetwork(): Promise<boolean> {
  return new Promise((resolve) => {
    uni.showModal({
      title: SKIP_NETWORK_CONFIRM.title,
      content: SKIP_NETWORK_CONFIRM.content,
      success: (res) => resolve(!!res.confirm),
      fail: () => resolve(false),
    })
  })
}
