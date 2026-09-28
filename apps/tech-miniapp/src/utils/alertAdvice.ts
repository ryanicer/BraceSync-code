/**
 * 告警「处置建议」固定模板（T443 裁定①丙，Boss 2026-09-28）。
 *
 * 口径：告警域后端零字段（见 docs/tasks/winner/T446-字段级对读-结论与字段表.md §五 ——
 * alerts 表 17 列无建议列，全仓唯一的 suggestion 属 health_reports 人工文本），
 * 稿面 alerts.html:84,85,87 的 action 是撰写示例、不是数据源。
 * 因此这里既不接后端下发、也不从告警内容推导（响应体除阈值／实际值／点位外无语义素材），
 * 只按告警类型给出固定四步文案。
 *
 * 键表跟 packages/shared-utils 的 ALERT_TYPE_LABELS（术语唯一来源，T419 G-6 收口）：
 * 改词不改码。pressure_fluctuation 刻意不在表内 —— 该型 T257 起引擎不再产生、
 * T430 已裁展示侧隐藏（HIDDEN_ALERT_TYPES），给它配文案等于把砍除类型又露出来。
 * 未收录的码值返回空数组，详情弹窗整行不渲染，不硬凑文案。
 */
export const ALERT_ADVICE: Record<string, readonly string[]> = {
  // 稿面 alerts.html:84（id:1 压力偏高）四步，第 3 步去掉样例点位 P12
  pressure_high: [
    '检查支架佩戴紧固程度',
    '观察患者有无局部疼痛或皮肤发红',
    '必要时微调该采集点的传感器位置',
    '24 小时后复测压力值',
  ],
  // 稿面 alerts.html:85（id:2「佩戴中断」）四步，按现术语「设备离线」改写
  wear_interrupt: [
    '联系患者确认设备是否被取下或已关机',
    '检查设备电量与网络连接状态',
    '指导患者保持设备开机并联网上行',
    '记录离线事件并追踪后续恢复情况',
  ],
  // 无稿面样例，本条为实作侧自拟文案（已挂卡请 Peter／PM 复核）
  wear_duration_short: [
    '联系患者确认当日实际佩戴起止时间',
    '核对佩戴目标时长与医嘱依从计划',
    '提醒患者按医嘱延长佩戴时间',
    '连续多日不足时安排随访复核',
  ],
  // 稿面 alerts.html:87（id:4「传感器异常」）四步，按现术语「传感器标定异常」改写、去掉样例点位 P15
  sensor_drift: [
    '安排技师上门检修',
    '检查该采集点传感器的物理连接与线缆完整性',
    '尝试重新插拔或更换该路传感器模块',
    '维修完成后执行全通道自检',
  ],
}

const ADVICE_PREFIX = '处置建议:'

/** 详情弹窗用的整块行：首行是标签，后面逐步一行；无模板时返回空数组（调用方 filter 掉） */
export function alertAdviceLines(type?: string | null): string[] {
  if (!type) return []
  const steps = ALERT_ADVICE[type]
  if (!steps || steps.length === 0) return []
  return [ADVICE_PREFIX, ...steps.map((s, i) => `${i + 1}. ${s}`)]
}
