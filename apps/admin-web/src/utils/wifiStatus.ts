/**
 * 管理端安装记录页 WiFi 显示词的唯一来源（T450 第四件）。
 *
 * 词形逐字取 docs/tasks/winner/T447-WiFi状态唯一词形表.md §一 的四档标准词
 * （connected=已连接 / failed=连接失败 / unconfigured=未配置 / skipped=已跳过配网）。
 * 改前列表列与详情抽屉各写一份 `wifiStatus === 'connected'` 的三元表达式，四值被塌成两词：
 * failed 与 skipped 显示不出来，且 skipped 被冒充成第二词。
 *
 * 颜色沿用稿面 docs/design/admin/安装记录.html 的 wifiStatusMap
 * （connected green → success、skipped gray → info）；failed 取技师端稿面
 * tech/records.html「连接失败」的 red → danger；unconfigured 与改前的「非 connected」一档同为 info。
 * 稿面映射本身只有 connected/skipped 两键，后一键词形已与词形表不符（T447 词形表 §五-5 已向 Peter 登记），
 * 本表按词形表而非稿面。
 */
import type { InstallRecord } from '@bracesync/shared-types'

export type WifiStatus = InstallRecord['wifiStatus']

const LABEL: Record<WifiStatus, string> = {
  connected: '已连接',
  failed: '连接失败',
  unconfigured: '未配置',
  skipped: '已跳过配网',
}

const TAG: Record<WifiStatus, 'success' | 'danger' | 'info'> = {
  connected: 'success',
  failed: 'danger',
  unconfigured: 'info',
  skipped: 'info',
}

export const WIFI_STATUS_VALUES = Object.keys(LABEL) as WifiStatus[]

/** el-table 的 #default="{ row }" 槽位里 row 是 any，取词统一走这两个函数，页面不各自写判据 */
export function wifiStatusLabel(status: WifiStatus): string {
  return LABEL[status]
}

export function wifiStatusTagType(status: WifiStatus): 'success' | 'danger' | 'info' {
  return TAG[status]
}
