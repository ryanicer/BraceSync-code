/**
 * 技师端安装流程展示层单一真源（T443）。
 *
 * 依据：docs/tasks/peter/T417-对照清单-技师端.md 的 TC-1／TC-2／TR-11；
 * PRD §7C.4 第 862 行（跳过配网按钮＋弹窗确认文案）、§7C.6 第 935 行（-4 可跳过）、
 * §8.2 第 1772 行（V3.12／T138：可达性为本地派生展示，不落库）。
 *
 * 页面模板在本包测不到（vitest.config.ts 是 environment: 'node'，无 VTU 挂载），
 * 故判定与文案构造全部下沉到这里，install／complete／records 三页同读一处。
 */

/**
 * wifi_status 取值面：DB CHECK 只放行 connected／unconfigured
 * （scripts/db/migrations/000001_init_schema.up.sql:129-130，类型层 packages/shared-types/src/index.ts:207）。
 * 稿面 records.html:102,134,204 的第三态「连接失败」因此不在本卡硬编——
 * 前端拿不到该值，写出来就是假数据；扩值要先改 CHECK＋契约＋后端写侧，归 T446。
 */
export const WIFI_STATUS_LABEL = {
  connected: '已连接',
  unconfigured: '未配置',
} as const

export function wifiStatusLabel(status?: string | null): string {
  const label = status ? WIFI_STATUS_LABEL[status as keyof typeof WIFI_STATUS_LABEL] : undefined
  return label ?? WIFI_STATUS_LABEL.unconfigured
}

/** TC-2：跳过配网时 WiFi 行的显示值，不再冒充「已联网／已连接」 */
export const WIFI_SKIPPED_LABEL = '已跳过配网'

export function wifiRowLabel(status: string, networkSkipped: boolean): string {
  if (networkSkipped) return WIFI_SKIPPED_LABEL
  return wifiStatusLabel(status)
}

/** TC-1：完成页基线状态与 install 页同一真源（installStore.baselineSaved），不再是硬编码 */
export function baselineStatusLabel(baselineSaved: boolean): string {
  return baselineSaved ? '已保存' : '未保存'
}

export function baselineStatusBadgeClass(baselineSaved: boolean): string {
  return baselineSaved ? 'status-ok' : 'status-warn'
}

/** 可达性三态（§8.2 第 1772 行：纯前端派生，不落库）。跳过优先，避免与「已验证」同屏互斥。 */
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
 * TI-12：PRD 第 862 行的弹窗文案，逐字取用。
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
